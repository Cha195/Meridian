package geo

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cha195/meridian/internal/config"
)

func testClusterWithServers(t *testing.T, selfID string) (*ClusterState, []*httptest.Server) {
	t.Helper()
	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	nodes := []config.NodeLocation{
		{ID: selfID, Lat: 39.04, Lng: -77.47, Addr: "http://localhost:0"},
		{ID: "node-b", Lat: 51.50, Lng: -0.12, Addr: s1.URL},
		{ID: "node-c", Lat: 35.68, Lng: 139.69, Addr: s2.URL},
	}

	cs, err := NewClusterState(selfID, nodes)
	if err != nil {
		t.Fatal(err)
	}
	return cs, []*httptest.Server{s1, s2}
}

func TestHealthCheckMarksNodeDown(t *testing.T) {
	cs, servers := testClusterWithServers(t, "node-a")
	defer servers[0].Close()

	// Close server for node-c so it becomes unreachable
	servers[1].Close()

	cs.StartHealthChecks(config.HealthCheckConfig{
		Interval:      "50ms",
		Timeout:       "20ms",
		FailThreshold: 2,
	})
	defer cs.StopHealthChecks()

	time.Sleep(300 * time.Millisecond)

	health := cs.NodeHealth()
	for _, h := range health {
		if h.ID == "node-c" {
			if h.Healthy {
				t.Error("node-c should be unhealthy")
			}
			if h.ConsecutiveFailures < 2 {
				t.Errorf("expected >= 2 failures, got %d", h.ConsecutiveFailures)
			}
			if h.DownSince == nil {
				t.Error("DownSince should be set")
			}
			return
		}
	}
	t.Error("node-c not found in health status")
}

func TestHealthCheckRecovery(t *testing.T) {
	cs, servers := testClusterWithServers(t, "node-a")
	defer servers[0].Close()

	// Close node-c initially
	servers[1].Close()

	cs.StartHealthChecks(config.HealthCheckConfig{
		Interval:      "50ms",
		Timeout:       "20ms",
		FailThreshold: 2,
	})
	defer cs.StopHealthChecks()

	time.Sleep(300 * time.Millisecond)

	if cs.IsHealthy("node-c") {
		t.Fatal("node-c should be down")
	}

	// Restart node-c
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer newServer.Close()

	cs.mu.Lock()
	for i, n := range cs.nodes {
		if n.ID == "node-c" {
			cs.nodes[i].Addr = newServer.URL
			break
		}
	}
	cs.mu.Unlock()

	time.Sleep(200 * time.Millisecond)

	if !cs.IsHealthy("node-c") {
		t.Error("node-c should have recovered")
	}
}

func TestHealthCheckSelfSkip(t *testing.T) {
	cs, servers := testClusterWithServers(t, "node-a")
	defer servers[0].Close()
	defer servers[1].Close()

	cs.StartHealthChecks(config.HealthCheckConfig{
		Interval:      "50ms",
		Timeout:       "20ms",
		FailThreshold: 2,
	})
	defer cs.StopHealthChecks()

	time.Sleep(200 * time.Millisecond)

	health := cs.NodeHealth()
	for _, h := range health {
		if h.ID == "node-a" {
			if !h.Healthy {
				t.Error("self should always be healthy")
			}
			if h.LastCheck != nil {
				t.Error("self should not have LastCheck set (never checked)")
			}
			return
		}
	}
	t.Error("self node not found in health status")
}

func TestAllNodesDownFallback(t *testing.T) {
	cs, servers := testClusterWithServers(t, "node-a")
	// Close both peer servers
	servers[0].Close()
	servers[1].Close()

	cs.StartHealthChecks(config.HealthCheckConfig{
		Interval:      "50ms",
		Timeout:       "20ms",
		FailThreshold: 2,
	})
	defer cs.StopHealthChecks()

	time.Sleep(300 * time.Millisecond)

	// With all peers down, ClosestNode should return selfID
	closest := cs.ClosestNode(51.50, -0.12) // London coords — closest to node-b
	if closest != "node-a" {
		t.Errorf("expected fallback to self (node-a), got %s", closest)
	}
}

func TestHealthCheckLatencyTracking(t *testing.T) {
	cs, servers := testClusterWithServers(t, "node-a")
	defer servers[0].Close()
	defer servers[1].Close()

	cs.StartHealthChecks(config.HealthCheckConfig{
		Interval:      "50ms",
		Timeout:       "1s",
		FailThreshold: 3,
	})
	defer cs.StopHealthChecks()

	time.Sleep(200 * time.Millisecond)

	health := cs.NodeHealth()
	for _, h := range health {
		if h.ID == "node-b" {
			if h.LatencyMs == nil {
				t.Error("expected latency to be tracked for node-b")
			}
			if *h.LatencyMs <= 0 {
				t.Errorf("expected positive latency, got %f", *h.LatencyMs)
			}
			return
		}
	}
	t.Error("node-b not found in health status")
}

func TestClosestNodeSkipsUnhealthy(t *testing.T) {
	nodes := []config.NodeLocation{
		{ID: "us", Lat: 39.04, Lng: -77.47, Addr: "http://localhost:1"},
		{ID: "eu", Lat: 51.50, Lng: -0.12, Addr: "http://localhost:2"},
		{ID: "jp", Lat: 35.68, Lng: 139.69, Addr: "http://localhost:3"},
	}
	cs, _ := NewClusterState("us", nodes)

	// Manually set up health state
	cs.mu.Lock()
	cs.initHealthState()
	cs.healthy["eu"] = false
	cs.mu.Unlock()

	// London client — eu is closest but unhealthy, should get us or jp
	closest := cs.ClosestNode(51.50, -0.12)
	if closest == "eu" {
		t.Error("should not route to unhealthy node eu")
	}
}

func TestUpdateNodesWithHealthChecks(t *testing.T) {
	nodes := []config.NodeLocation{
		{ID: "us", Lat: 39.04, Lng: -77.47, Addr: "http://localhost:1"},
		{ID: "eu", Lat: 51.50, Lng: -0.12, Addr: "http://localhost:2"},
	}
	cs, _ := NewClusterState("us", nodes)

	cs.mu.Lock()
	cs.initHealthState()
	cs.mu.Unlock()

	// Add a new node (jp) and keep us; remove eu.
	cs.UpdateNodes([]config.NodeLocation{
		{ID: "us", Lat: 39.04, Lng: -77.47, Addr: "http://localhost:1"},
		{ID: "jp", Lat: 35.68, Lng: 139.69, Addr: "http://localhost:3"},
	})

	// New node (jp) should be healthy by default and routable.
	cs.mu.RLock()
	jpHealthy := cs.healthy["jp"]
	_, euExists := cs.healthy["eu"]
	cs.mu.RUnlock()

	if !jpHealthy {
		t.Error("newly added node jp should default to healthy")
	}
	if euExists {
		t.Error("removed node eu should be cleaned from health maps")
	}

	// Tokyo client should route to jp (closest healthy node).
	closest := cs.ClosestNode(35.68, 139.69)
	if closest != "jp" {
		t.Errorf("expected jp as closest for Tokyo client, got %s", closest)
	}
}
