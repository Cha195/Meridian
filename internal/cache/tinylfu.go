package cache

import (
	"path/filepath"
	"sync"
	"time"
)

type segID int

const (
	segWindow    segID = iota
	segProbation segID = iota
	segProtected segID = iota
)

type tlfuNode struct {
	key   string
	entry *CacheEntry
	seg   segID
	prev  *tlfuNode
	next  *tlfuNode
	size  int64
}

// lruSeg is a doubly-linked LRU list that tracks its own byte size.
// All list mutations update lruSeg.size automatically except moveToFront
// (which keeps the node in the same segment).
type lruSeg struct {
	head *tlfuNode
	tail *tlfuNode
	size int64
}

func (s *lruSeg) addToFront(node *tlfuNode) {
	node.prev = nil
	node.next = s.head
	if s.head != nil {
		s.head.prev = node
	}
	s.head = node
	if s.tail == nil {
		s.tail = node
	}
	s.size += node.size
}

func (s *lruSeg) remove(node *tlfuNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		s.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		s.tail = node.prev
	}
	node.prev = nil
	node.next = nil
	s.size -= node.size
}

func (s *lruSeg) moveToFront(node *tlfuNode) {
	if s.head == node {
		return
	}
	// Unlink without touching size (node stays in same segment).
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		s.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		s.tail = node.prev
	}
	// Relink at front.
	node.prev = nil
	node.next = s.head
	if s.head != nil {
		s.head.prev = node
	}
	s.head = node
	if s.tail == nil {
		s.tail = node
	}
}

func (s *lruSeg) peekTail() *tlfuNode { return s.tail }

// evictTail removes and returns the tail node. Callers must update
// the entries map and any external size counters separately.
func (s *lruSeg) evictTail() *tlfuNode {
	node := s.tail
	if node == nil {
		return nil
	}
	s.remove(node)
	return node
}

// WTinyLFUCache implements W-TinyLFU: a small window LRU feeds into a
// segmented main cache (probation + protected) gated by a CountMinSketch
// frequency filter.
type WTinyLFUCache struct {
	entries   map[string]*tlfuNode
	window    lruSeg
	probation lruSeg
	protected lruSeg
	sketch    *CountMinSketch
	mu        sync.Mutex
	maxSize   int64
	windowMax int64
	mainMax   int64
	protMax   int64
	hits      int64
	misses    int64
	evictions int64
}

func NewWTinyLFUCache(maxSize int64) *WTinyLFUCache {
	windowMax := maxSize / 100
	if windowMax < 1 {
		windowMax = 1
	}
	mainMax := maxSize - windowMax
	// Protected gets 80% of main; probation holds the remainder.
	protMax := mainMax * 4 / 5

	expectedItems := int(maxSize / 512)
	if expectedItems < 64 {
		expectedItems = 64
	}

	return &WTinyLFUCache{
		entries:   make(map[string]*tlfuNode),
		sketch:    NewCountMinSketch(expectedItems),
		maxSize:   maxSize,
		windowMax: windowMax,
		mainMax:   mainMax,
		protMax:   protMax,
	}
}

func (c *WTinyLFUCache) Get(key string) (*CacheEntry, CacheStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, CacheMISS
	}

	now := time.Now()

	if now.After(node.entry.ExpiresAt) {
		// Stale-while-revalidate window: return stale copy without evicting.
		if !node.entry.StaleDeadline.IsZero() && now.Before(node.entry.StaleDeadline) {
			c.hits++
			return node.entry, CacheSTALE
		}
		c.deleteNode(node)
		c.misses++
		return nil, CacheMISS
	}

	c.sketch.Increment(key)
	node.entry.AccessCount++

	switch node.seg {
	case segWindow:
		c.window.moveToFront(node)
	case segProbation:
		// Promote hot item from probation → protected.
		c.probation.remove(node)
		node.seg = segProtected
		c.protected.addToFront(node)
		c.trimProtected()
	case segProtected:
		c.protected.moveToFront(node)
	}

	c.hits++
	return node.entry, CacheHIT
}

func (c *WTinyLFUCache) Set(key string, entry *CacheEntry, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	entry.CreatedAt = now
	entry.ExpiresAt = now.Add(ttl)

	c.sketch.Increment(key)

	if existing, ok := c.entries[key]; ok {
		c.updateInPlace(existing, entry)
		return
	}

	node := &tlfuNode{
		key:   key,
		entry: entry,
		seg:   segWindow,
		size:  int64(len(entry.Response.Body)),
	}
	c.entries[key] = node
	c.window.addToFront(node)

	c.drainWindow()
}

func (c *WTinyLFUCache) updateInPlace(node *tlfuNode, entry *CacheEntry) {
	newSize := int64(len(entry.Response.Body))
	diff := newSize - node.size
	node.entry = entry
	node.size = newSize

	switch node.seg {
	case segWindow:
		c.window.size += diff
		c.window.moveToFront(node)
	case segProbation:
		c.probation.size += diff
		c.probation.moveToFront(node)
	case segProtected:
		c.protected.size += diff
		c.protected.moveToFront(node)
	}

	// Re-balance in case size grew.
	c.trimProtected()
	c.drainWindow()
}

// drainWindow pushes window overflow into main via the TinyLFU admission gate.
func (c *WTinyLFUCache) drainWindow() {
	for c.window.size > c.windowMax {
		candidate := c.window.evictTail()
		if candidate == nil {
			break
		}
		c.admitToMain(candidate)
	}
}

// admitToMain decides whether a window eviction candidate enters main cache.
// If main has room, the candidate goes directly to probation.
// Otherwise the CountMinSketch decides: the item with higher estimated
// frequency survives; the other is evicted from the cache entirely.
func (c *WTinyLFUCache) admitToMain(candidate *tlfuNode) {
	mainCurrent := c.probation.size + c.protected.size

	if mainCurrent+candidate.size <= c.mainMax {
		candidate.seg = segProbation
		c.probation.addToFront(candidate)
		return
	}

	// Main is full — run the TinyLFU admission gate.
	mainVictim := c.probation.peekTail()
	if mainVictim == nil {
		// Probation empty; use protected tail as last resort.
		mainVictim = c.protected.peekTail()
	}
	if mainVictim == nil {
		// Nothing to compare against — discard candidate.
		delete(c.entries, candidate.key)
		c.evictions++
		return
	}

	if c.sketch.Estimate(candidate.key) >= c.sketch.Estimate(mainVictim.key) {
		// Candidate wins: evict main victim, admit candidate to probation.
		if mainVictim.seg == segProbation {
			c.probation.evictTail()
		} else {
			c.protected.evictTail()
		}
		delete(c.entries, mainVictim.key)
		c.evictions++

		candidate.seg = segProbation
		c.probation.addToFront(candidate)

		// If candidate was larger than victim, main may still be over limit.
		c.trimMain()
	} else {
		// Candidate loses: discard it (already removed from window).
		delete(c.entries, candidate.key)
		c.evictions++
	}
}

// trimProtected demotes the protected tail to probation whenever protected
// exceeds its soft capacity limit.
func (c *WTinyLFUCache) trimProtected() {
	for c.protected.size > c.protMax {
		demoted := c.protected.evictTail()
		if demoted == nil {
			break
		}
		demoted.seg = segProbation
		c.probation.addToFront(demoted)
	}
}

// trimMain evicts from probation until combined main size fits within mainMax.
func (c *WTinyLFUCache) trimMain() {
	for c.probation.size+c.protected.size > c.mainMax {
		v := c.probation.peekTail()
		if v == nil {
			break
		}
		c.probation.evictTail()
		delete(c.entries, v.key)
		c.evictions++
	}
}

// deleteNode removes a node from its segment list and the entries map.
func (c *WTinyLFUCache) deleteNode(node *tlfuNode) {
	delete(c.entries, node.key)
	switch node.seg {
	case segWindow:
		c.window.remove(node)
	case segProbation:
		c.probation.remove(node)
	case segProtected:
		c.protected.remove(node)
	}
}

func (c *WTinyLFUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node, ok := c.entries[key]; ok {
		c.deleteNode(node)
	}
}

func (c *WTinyLFUCache) Purge(pattern string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toDelete []*tlfuNode
	for key, node := range c.entries {
		if matched, err := filepath.Match(pattern, key); err == nil && matched {
			toDelete = append(toDelete, node)
		}
	}
	for _, node := range toDelete {
		c.deleteNode(node)
	}
	return len(toDelete)
}

func (c *WTinyLFUCache) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.window.size + c.probation.size + c.protected.size
}

func (c *WTinyLFUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *WTinyLFUCache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	total := c.hits + c.misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}
	return CacheStats{
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		HitRate:   hitRate,
	}
}

func (c *WTinyLFUCache) Name() string { return "w-tinylfu" }

func (c *WTinyLFUCache) Destroy() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*tlfuNode)
	c.window = lruSeg{}
	c.probation = lruSeg{}
	c.protected = lruSeg{}
}
