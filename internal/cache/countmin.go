package cache

import (
	"math/bits"
	"math/rand"
)

const sketchRows = 4

// CountMinSketch estimates key access frequency using 4 hash functions and
// 4-bit saturating counters. Counters are periodically halved ("aging") to
// prevent stale access patterns from dominating admission decisions.
type CountMinSketch struct {
	cols       int
	counters   [sketchRows][]uint8
	seeds      [sketchRows]uint64
	totalCount int64
	sampleSize int64
}

func NewCountMinSketch(expectedItems int) *CountMinSketch {
	cols := nextPowerOf2(expectedItems)
	if cols < 8 {
		cols = 8
	}
	s := &CountMinSketch{
		cols:       cols,
		sampleSize: int64(expectedItems),
	}
	for i := range s.counters {
		s.counters[i] = make([]uint8, cols)
	}
	for i := range s.seeds {
		s.seeds[i] = rand.Uint64() | 1 // ensure non-zero
	}
	return s
}

// Increment records one access to key. Saturates at 15 (4-bit max).
// Triggers aging when totalCount reaches sampleSize.
func (s *CountMinSketch) Increment(key string) {
	mask := uint64(s.cols - 1)
	for i := 0; i < sketchRows; i++ {
		col := sketchHash(key, s.seeds[i]) & mask
		if s.counters[i][col] < 15 {
			s.counters[i][col]++
		}
	}
	s.totalCount++
	if s.totalCount >= s.sampleSize {
		s.reset()
	}
}

// Estimate returns the minimum counter value across all rows — the best
// approximation of how many times key has been accessed.
func (s *CountMinSketch) Estimate(key string) uint8 {
	mask := uint64(s.cols - 1)
	min := uint8(255)
	for i := 0; i < sketchRows; i++ {
		col := sketchHash(key, s.seeds[i]) & mask
		if v := s.counters[i][col]; v < min {
			min = v
		}
	}
	return min
}

// reset halves all counters (aging) and resets the total count.
func (s *CountMinSketch) reset() {
	for i := 0; i < sketchRows; i++ {
		for j := range s.counters[i] {
			s.counters[i][j] >>= 1
		}
	}
	s.totalCount = 0
}

// sketchHash mixes key with a seed using a fast, low-collision hash.
func sketchHash(key string, seed uint64) uint64 {
	h := seed
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h = bits.RotateLeft64(h, 31)
		h *= 0x9e3779b97f4a7c15
	}
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	return h
}

func nextPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
