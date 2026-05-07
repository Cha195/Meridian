package geo

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cha195/meridian/internal/config"
)

type ClusterState struct {
	nodes  []config.NodeLocation
	selfID string
	mu     sync.RWMutex

	// Health check state (initialized by StartHealthChecks)
	healthy             map[string]bool
	lastCheck           map[string]time.Time
	lastLatency         map[string]time.Duration
	consecutiveFailures map[string]int
	downSince           map[string]*time.Time
	failThreshold       int
	checkInterval       time.Duration
	checkTimeout        time.Duration
	cancel              context.CancelFunc
}

func NewClusterState(selfID string, nodes []config.NodeLocation) (*ClusterState, error) {
	found := false
	for _, n := range nodes {
		if n.ID == selfID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("selfID %q not found in node list", selfID)
	}

	nodesCopy := make([]config.NodeLocation, len(nodes))
	copy(nodesCopy, nodes)

	return &ClusterState{
		nodes:  nodesCopy,
		selfID: selfID,
	}, nil
}

func (cs *ClusterState) ClosestNode(clientLat, clientLng float64) string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if len(cs.nodes) == 0 {
		return ""
	}

	var closest string
	minDist := math.MaxFloat64

	for _, node := range cs.nodes {
		if cs.healthy != nil && !cs.healthy[node.ID] {
			continue
		}
		d := HaversineDistance(clientLat, clientLng, node.Lat, node.Lng)
		if d < minDist {
			minDist = d
			closest = node.ID
		}
	}

	if closest == "" {
		return cs.selfID
	}
	return closest
}

func (cs *ClusterState) DistanceToNode(clientLat, clientLng float64, nodeID string) float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	for _, node := range cs.nodes {
		if node.ID == nodeID {
			return HaversineDistance(clientLat, clientLng, node.Lat, node.Lng)
		}
	}
	return -1
}

func (cs *ClusterState) UpdateNodes(nodes []config.NodeLocation) {
	nodesCopy := make([]config.NodeLocation, len(nodes))
	copy(nodesCopy, nodes)

	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.nodes = nodesCopy
}

func (cs *ClusterState) GetNodes() []config.NodeLocation {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	nodesCopy := make([]config.NodeLocation, len(cs.nodes))
	copy(nodesCopy, cs.nodes)
	return nodesCopy
}

func HaversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371.0

	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*
			math.Sin(dLng/2)*math.Sin(dLng/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

func toRad(deg float64) float64 {
	return deg * math.Pi / 180
}
