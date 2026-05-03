package cache

import "testing"

func TestCountMinBasicFrequency(t *testing.T) {
	s := NewCountMinSketch(1000)
	for i := 0; i < 10; i++ {
		s.Increment("foo")
	}
	est := s.Estimate("foo")
	// Allow for hash collisions inflating the count, but it must be >= actual.
	if est < 10 {
		t.Fatalf("expected estimate >= 10, got %d", est)
	}
	if s.Estimate("unseen") > 0 {
		t.Fatal("unseen key should estimate 0")
	}
}

func TestCountMinSaturation(t *testing.T) {
	// sampleSize large enough that reset never fires during the test.
	s := NewCountMinSketch(10000)
	for i := 0; i < 100; i++ {
		s.Increment("bar")
	}
	if est := s.Estimate("bar"); est != 15 {
		t.Fatalf("expected saturated value 15, got %d", est)
	}
}

func TestCountMinReset(t *testing.T) {
	// sampleSize=10 → reset fires after 10 increments.
	s := NewCountMinSketch(10)
	for i := 0; i < 9; i++ {
		s.Increment("key")
	}
	before := s.Estimate("key") // should be 9
	s.Increment("key")          // 10th → triggers reset (counter 10→5)
	after := s.Estimate("key")  // should be ~5
	if int(after) >= int(before) {
		t.Fatalf("expected estimate to drop after reset: before=%d after=%d", before, after)
	}
}

func TestCountMinDifferentKeys(t *testing.T) {
	s := NewCountMinSketch(1000)
	for i := 0; i < 10; i++ {
		s.Increment("frequent")
	}
	s.Increment("rare")

	freq := s.Estimate("frequent")
	rare := s.Estimate("rare")
	if freq <= rare {
		t.Fatalf("frequent (%d) should estimate higher than rare (%d)", freq, rare)
	}
}
