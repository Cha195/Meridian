package cache

import (
	"path/filepath"
	"sync"
	"time"
)

type SieveNode struct {
	key     string
	entry   *CacheEntry
	visited bool
	prev    *SieveNode
	next    *SieveNode
	size    int64
}

type SieveCache struct {
	entries   map[string]*SieveNode
	head      *SieveNode
	tail      *SieveNode
	hand      *SieveNode
	size      int64
	maxSize   int64
	hits      int64
	misses    int64
	evictions int64
	mu        sync.RWMutex
}

func NewSieveCache(maxSize int64) *SieveCache {
	return &SieveCache{
		entries: make(map[string]*SieveNode),
		maxSize: maxSize,
	}
}

func (s *SieveCache) Get(key string) (*CacheEntry, CacheStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, found := s.entries[key]
	if !found {
		s.misses++
		return nil, CacheMISS
	}

	if time.Now().After(node.entry.ExpiresAt) {
		s.removeNode(node)
		s.misses++
		return nil, CacheMISS
	}

	node.visited = true
	node.entry.AccessCount++
	s.hits++
	return node.entry, CacheHIT
}

func (s *SieveCache) Set(key string, entry *CacheEntry, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	entry.CreatedAt = now
	entry.ExpiresAt = now.Add(ttl)

	if node, found := s.entries[key]; found {
		oldSize := node.size
		node.entry = entry
		node.size = int64(len(entry.Response.Body))
		s.size = s.size - oldSize + node.size
		node.visited = false
		s.moveToHead(node)
	} else {
		node := &SieveNode{
			key:   key,
			entry: entry,
			size:  int64(len(entry.Response.Body)),
		}
		s.entries[key] = node
		s.insertAtHead(node)
		s.size += node.size
	}

	for s.size > s.maxSize && len(s.entries) > 0 {
		s.evict()
	}
}

func (s *SieveCache) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if node, found := s.entries[key]; found {
		s.removeNode(node)
	}
}

func (s *SieveCache) Purge(pattern string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	var toDelete []*SieveNode

	for key, node := range s.entries {
		matched, err := filepath.Match(pattern, key)
		if err == nil && matched {
			toDelete = append(toDelete, node)
		}
	}

	for _, node := range toDelete {
		s.removeNode(node)
		count++
	}

	return count
}

func (s *SieveCache) Size() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.size
}

func (s *SieveCache) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

func (s *SieveCache) Stats() CacheStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := s.hits + s.misses

	var hitRate float64
	if total > 0 {
		hitRate = float64(s.hits) / float64(total)
	}

	return CacheStats{
		Hits:      s.hits,
		Misses:    s.misses,
		Evictions: s.evictions,
		HitRate:   hitRate,
	}
}

func (s *SieveCache) Name() string {
	return "sieve"
}

func (s *SieveCache) Destroy() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = make(map[string]*SieveNode)
	s.head = nil
	s.tail = nil
	s.hand = nil
	s.size = 0
}

func (s *SieveCache) insertAtHead(node *SieveNode) {
	if s.head == nil {
		s.head = node
		s.tail = node
		s.hand = node
		node.prev = nil
		node.next = nil
	} else {
		node.next = s.head
		node.prev = nil
		s.head.prev = node
		s.head = node
	}
}

func (s *SieveCache) moveToHead(node *SieveNode) {
	if node == s.head {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}

	if node == s.tail {
		s.tail = node.prev
	}

	node.next = s.head
	node.prev = nil
	s.head.prev = node
	s.head = node
}

func (s *SieveCache) removeNode(node *SieveNode) {
	delete(s.entries, node.key)
	s.size -= node.size

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

	if s.hand == node {
		s.hand = node.prev
	}
}

func (s *SieveCache) evict() {
	if s.hand == nil {
		s.hand = s.tail
	}

	for s.hand != nil {
		if !s.hand.visited {
			evicted := s.hand
			s.hand = evicted.prev
			s.removeNode(evicted)
			s.evictions++
			return
		}

		s.hand.visited = false
		if s.hand.prev == nil {
			s.hand = s.tail
		} else {
			s.hand = s.hand.prev
		}
	}
}
