package cache

import (
	"path/filepath"
	"sync"
	"time"
)

type lruNode struct {
	key   string
	entry *CacheEntry
	prev  *lruNode
	next  *lruNode
	size  int64
}

type LRUCache struct {
	entries   map[string]*lruNode
	head      *lruNode
	tail      *lruNode
	size      int64
	maxSize   int64
	hits      int64
	misses    int64
	evictions int64
	mu        sync.Mutex
}

func NewLRUCache(maxSize int64) *LRUCache {
	return &LRUCache{
		entries: make(map[string]*lruNode),
		maxSize: maxSize,
	}
}

func (c *LRUCache) Get(key string) (*CacheEntry, CacheStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, CacheMISS
	}

	if time.Now().After(node.entry.ExpiresAt) {
		c.removeNode(node)
		c.misses++
		return nil, CacheMISS
	}

	c.moveToFront(node)
	node.entry.AccessCount++
	c.hits++
	return node.entry, CacheHIT
}

func (c *LRUCache) Set(key string, entry *CacheEntry, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	entry.CreatedAt = now
	entry.ExpiresAt = now.Add(ttl)

	if node, ok := c.entries[key]; ok {
		diff := int64(len(entry.Response.Body)) - node.size
		node.entry = entry
		node.size = int64(len(entry.Response.Body))
		c.size += diff
		c.moveToFront(node)
	} else {
		node := &lruNode{
			key:   key,
			entry: entry,
			size:  int64(len(entry.Response.Body)),
		}
		c.entries[key] = node
		c.addToFront(node)
		c.size += node.size
	}

	for c.size > c.maxSize && len(c.entries) > 0 {
		c.evictTail()
	}
}

func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node, ok := c.entries[key]; ok {
		c.removeNode(node)
	}
}

func (c *LRUCache) Purge(pattern string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toDelete []*lruNode
	for key, node := range c.entries {
		if matched, err := filepath.Match(pattern, key); err == nil && matched {
			toDelete = append(toDelete, node)
		}
	}
	for _, node := range toDelete {
		c.removeNode(node)
	}
	return len(toDelete)
}

func (c *LRUCache) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size
}

func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *LRUCache) Stats() CacheStats {
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

func (c *LRUCache) Name() string { return "lru" }

func (c *LRUCache) Destroy() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*lruNode)
	c.head = nil
	c.tail = nil
	c.size = 0
}

func (c *LRUCache) addToFront(node *lruNode) {
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

func (c *LRUCache) moveToFront(node *lruNode) {
	if c.head == node {
		return
	}
	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if c.tail == node {
		c.tail = node.prev
	}
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
}

func (c *LRUCache) removeNode(node *lruNode) {
	delete(c.entries, node.key)
	c.size -= node.size
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
}

func (c *LRUCache) evictTail() {
	if c.tail == nil {
		return
	}
	c.removeNode(c.tail)
	c.evictions++
}
