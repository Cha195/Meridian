package geo

import (
	"math"
	"testing"

	"github.com/cha195/meridian/internal/config"
)

func threeNodeCluster(t *testing.T) *ClusterState {
	t.Helper()
	nodes := []config.NodeLocation{
		{ID: "us-east-1", Lat: 39.0481, Lng: -77.4728},
		{ID: "eu-west-1", Lat: 53.3331, Lng: -6.2489},
		{ID: "ap-northeast-1", Lat: 35.6762, Lng: 139.6503},
	}
	cs, err := NewClusterState("us-east-1", nodes)
	if err != nil {
		t.Fatalf("NewClusterState: %v", err)
	}
	return cs
}

func TestHaversineNewYorkToLondon(t *testing.T) {
	dist := HaversineDistance(40.7128, -74.0060, 51.5074, -0.1278)
	expected := 5570.0
	if math.Abs(dist-expected) > 100 {
		t.Errorf("New York → London: expected ~%gkm, got %.0fkm", expected, dist)
	}
}

func TestHaversineTokyoToSydney(t *testing.T) {
	dist := HaversineDistance(35.6762, 139.6503, -33.8688, 151.2093)
	expected := 7820.0
	if math.Abs(dist-expected) > 100 {
		t.Errorf("Tokyo → Sydney: expected ~%gkm, got %.0fkm", expected, dist)
	}
}

func TestClosestNodeBerlin(t *testing.T) {
	cs := threeNodeCluster(t)
	node := cs.ClosestNode(52.5200, 13.4050)
	if node != "eu-west-1" {
		t.Errorf("Berlin: expected eu-west-1, got %s", node)
	}
}

func TestClosestNodeTokyo(t *testing.T) {
	cs := threeNodeCluster(t)
	node := cs.ClosestNode(35.6762, 139.6503)
	if node != "ap-northeast-1" {
		t.Errorf("Tokyo: expected ap-northeast-1, got %s", node)
	}
}

func TestDistanceToNode(t *testing.T) {
	cs := threeNodeCluster(t)
	dist := cs.DistanceToNode(35.6762, 139.6503, "ap-northeast-1")
	if dist > 1 {
		t.Errorf("expected ~0km to ap-northeast-1 from Tokyo, got %.2f", dist)
	}
}

func TestDistanceToNodeUnknown(t *testing.T) {
	cs := threeNodeCluster(t)
	dist := cs.DistanceToNode(0, 0, "nonexistent-node")
	if dist != -1 {
		t.Errorf("expected -1 for unknown node, got %f", dist)
	}
}

func TestUpdateNodes(t *testing.T) {
	cs := threeNodeCluster(t)

	// Tokyo is closest to ap-northeast-1
	node := cs.ClosestNode(35.6762, 139.6503)
	if node != "ap-northeast-1" {
		t.Fatalf("before update: expected ap-northeast-1, got %s", node)
	}

	// Remove ap-northeast-1 from cluster
	cs.UpdateNodes([]config.NodeLocation{
		{ID: "us-east-1", Lat: 39.0481, Lng: -77.4728},
		{ID: "eu-west-1", Lat: 53.3331, Lng: -6.2489},
	})

	// Tokyo should now route to eu-west-1 (closer than us-east-1)
	node = cs.ClosestNode(35.6762, 139.6503)
	if node == "ap-northeast-1" {
		t.Error("after removing ap-northeast-1, Tokyo should route elsewhere")
	}
	if node == "" {
		t.Error("expected a non-empty node after update")
	}
}

func TestSelfIDValidation(t *testing.T) {
	nodes := []config.NodeLocation{
		{ID: "us-east-1", Lat: 39.0481, Lng: -77.4728},
		{ID: "eu-west-1", Lat: 53.3331, Lng: -6.2489},
	}

	_, err := NewClusterState("nonexistent-node", nodes)
	if err == nil {
		t.Error("expected error when selfID is not in node list")
	}

	_, err = NewClusterState("us-east-1", nodes)
	if err != nil {
		t.Errorf("expected no error for valid selfID, got: %v", err)
	}
}

func TestGetNodesReturnsCopy(t *testing.T) {
	cs := threeNodeCluster(t)
	nodes := cs.GetNodes()

	// Mutating the returned slice should not affect ClusterState
	nodes[0].ID = "tampered"
	original := cs.GetNodes()
	if original[0].ID == "tampered" {
		t.Error("GetNodes should return a copy, not the internal slice")
	}
}
