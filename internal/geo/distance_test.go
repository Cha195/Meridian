package geo

import (
	"math"
	"testing"
)

func TestHaversineNewYorkToLondon(t *testing.T) {
	// New York: 40.7128° N, 74.0060° W
	// London:   51.5074° N, 0.1278° W
	dist := HaversineDistance(40.7128, -74.0060, 51.5074, -0.1278)
	expected := 5570.0
	if math.Abs(dist-expected) > 100 {
		t.Errorf("New York → London: expected ~%gkm, got %.0fkm", expected, dist)
	}
}

func TestHaversineTokyoToSydney(t *testing.T) {
	// Tokyo:  35.6762° N, 139.6503° E
	// Sydney: 33.8688° S, 151.2093° E
	dist := HaversineDistance(35.6762, 139.6503, -33.8688, 151.2093)
	expected := 7820.0
	if math.Abs(dist-expected) > 100 {
		t.Errorf("Tokyo → Sydney: expected ~%gkm, got %.0fkm", expected, dist)
	}
}

func TestClosestNodeBerlin(t *testing.T) {
	// Berlin: 52.5200° N, 13.4050° E — should pick eu-west-1 (Dublin)
	node := ClosestNode(52.5200, 13.4050)
	if node != "eu-west-1" {
		t.Errorf("Berlin: expected eu-west-1, got %s", node)
	}
}

func TestClosestNodeTokyo(t *testing.T) {
	// Tokyo: 35.6762° N, 139.6503° E — should pick ap-northeast-1
	node := ClosestNode(35.6762, 139.6503)
	if node != "ap-northeast-1" {
		t.Errorf("Tokyo: expected ap-northeast-1, got %s", node)
	}
}

func TestDistanceToNode(t *testing.T) {
	// Tokyo to ap-northeast-1 (same coordinates) should be ~0
	dist := DistanceToNode(35.6762, 139.6503, "ap-northeast-1")
	if dist > 1 {
		t.Errorf("expected ~0km to ap-northeast-1 from Tokyo, got %.2f", dist)
	}
}

func TestDistanceToNodeUnknown(t *testing.T) {
	dist := DistanceToNode(0, 0, "nonexistent-node")
	if dist != -1 {
		t.Errorf("expected -1 for unknown node, got %f", dist)
	}
}
