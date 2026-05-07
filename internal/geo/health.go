package geo

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/cha195/meridian/internal/config"
)

type NodeHealthStatus struct {
	ID                  string     `json:"id"`
	Healthy             bool       `json:"healthy"`
	LastCheck           *time.Time `json:"last_check,omitempty"`
	LatencyMs           *float64   `json:"latency_ms"`
	ConsecutiveFailures int        `json:"consecutive_failures,omitempty"`
	DownSince           *time.Time `json:"down_since,omitempty"`
	IsSelf              bool       `json:"self,omitempty"`
}

// Health check state is embedded directly in ClusterState to keep routing
// and liveness in a single lock domain.

func (cs *ClusterState) initHealthState() {
	cs.healthy = make(map[string]bool)
	cs.lastCheck = make(map[string]time.Time)
	cs.lastLatency = make(map[string]time.Duration)
	cs.consecutiveFailures = make(map[string]int)
	cs.downSince = make(map[string]*time.Time)
	for _, n := range cs.nodes {
		cs.healthy[n.ID] = true
	}
}

func (cs *ClusterState) StartHealthChecks(cfg config.HealthCheckConfig) {
	interval := 10 * time.Second
	timeout := 3 * time.Second
	threshold := 3

	if cfg.Interval != "" {
		if d, err := time.ParseDuration(cfg.Interval); err == nil {
			interval = d
		}
	}
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = d
		}
	}
	if cfg.FailThreshold > 0 {
		threshold = cfg.FailThreshold
	}

	cs.mu.Lock()
	cs.failThreshold = threshold
	cs.checkInterval = interval
	cs.checkTimeout = timeout
	cs.initHealthState()
	ctx, cancel := context.WithCancel(context.Background())
	cs.cancel = cancel
	cs.mu.Unlock()

	client := &http.Client{Timeout: timeout}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cs.checkAllPeers(client)
			}
		}
	}()

	log.Printf("health checks started: interval=%s timeout=%s threshold=%d", interval, timeout, threshold)
}

func (cs *ClusterState) StopHealthChecks() {
	cs.mu.Lock()
	if cs.cancel != nil {
		cs.cancel()
		cs.cancel = nil
	}
	cs.mu.Unlock()
}

func (cs *ClusterState) checkAllPeers(client *http.Client) {
	cs.mu.RLock()
	nodes := make([]config.NodeLocation, len(cs.nodes))
	copy(nodes, cs.nodes)
	selfID := cs.selfID
	cs.mu.RUnlock()

	var wg sync.WaitGroup
	for _, node := range nodes {
		if node.ID == selfID {
			continue
		}
		if node.Addr == "" {
			continue
		}
		wg.Add(1)
		go func(n config.NodeLocation) {
			defer wg.Done()
			cs.checkNode(client, n)
		}(node)
	}
	wg.Wait()
}

func (cs *ClusterState) checkNode(client *http.Client, node config.NodeLocation) {
	url := fmt.Sprintf("%s/healthz", node.Addr)
	start := time.Now()
	resp, err := client.Get(url)
	latency := time.Since(start)
	now := time.Now()

	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.lastCheck[node.ID] = now

	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		cs.consecutiveFailures[node.ID]++
		if cs.consecutiveFailures[node.ID] >= cs.failThreshold && cs.healthy[node.ID] {
			cs.healthy[node.ID] = false
			t := now
			cs.downSince[node.ID] = &t
			log.Printf("node %s marked DOWN after %d consecutive failures", node.ID, cs.consecutiveFailures[node.ID])
		}
		return
	}
	resp.Body.Close()

	cs.lastLatency[node.ID] = latency

	if !cs.healthy[node.ID] {
		var downtime time.Duration
		if cs.downSince[node.ID] != nil {
			downtime = now.Sub(*cs.downSince[node.ID])
		}
		log.Printf("node %s recovered after %s downtime", node.ID, downtime.Round(time.Second))
	}

	cs.healthy[node.ID] = true
	cs.consecutiveFailures[node.ID] = 0
	cs.downSince[node.ID] = nil
}

func (cs *ClusterState) NodeHealth() []NodeHealthStatus {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	result := make([]NodeHealthStatus, 0, len(cs.nodes))
	for _, n := range cs.nodes {
		status := NodeHealthStatus{
			ID:      n.ID,
			Healthy: cs.healthy[n.ID],
			IsSelf:  n.ID == cs.selfID,
		}

		if n.ID == cs.selfID {
			status.Healthy = true
		} else {
			if t, ok := cs.lastCheck[n.ID]; ok {
				status.LastCheck = &t
			}
			if lat, ok := cs.lastLatency[n.ID]; ok {
				ms := float64(lat.Microseconds()) / 1000.0
				status.LatencyMs = &ms
			}
			status.ConsecutiveFailures = cs.consecutiveFailures[n.ID]
			status.DownSince = cs.downSince[n.ID]
		}

		result = append(result, status)
	}
	return result
}

func (cs *ClusterState) IsHealthy(nodeID string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if nodeID == cs.selfID {
		return true
	}
	h, ok := cs.healthy[nodeID]
	if !ok {
		return true
	}
	return h
}

func (cs *ClusterState) SelfID() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.selfID
}
