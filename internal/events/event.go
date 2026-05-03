package events

import (
	"time"

	"github.com/google/uuid"
)

type DiagnosticEvent struct {
	EventID   string    `json:"event_id"`
	ProjectID string    `json:"project_id"`
	Timestamp time.Time `json:"timestamp"`

	// Client geo
	ClientCountry   string  `json:"client_country"`
	ClientCity      string  `json:"client_city"`
	ClientLat       float64 `json:"client_lat"`
	ClientLng       float64 `json:"client_lng"`
	ClientContinent string  `json:"client_continent"`

	// Routing
	EdgeNode         string  `json:"edge_node"`
	ClosestNode      string  `json:"closest_node"`
	RoutingCorrect   bool    `json:"routing_correct"`
	DistanceToNodeKm float64 `json:"distance_to_node_km"`

	// Cache
	CacheStatus     string `json:"cache_status"` // HIT, MISS, STALE, BYPASS
	CachePolicy     string `json:"cache_policy"`
	CacheAgeSeconds *int   `json:"cache_age_seconds,omitempty"`
	CacheTTL        int    `json:"cache_ttl"`

	// Performance (milliseconds)
	TotalLatencyMs  float64  `json:"total_latency_ms"`
	OriginLatencyMs *float64 `json:"origin_latency_ms,omitempty"`
	TTFBMs          float64  `json:"ttfb_ms"`

	// Request
	Method       string `json:"method"`
	Path         string `json:"path"`
	StatusCode   int    `json:"status_code"`
	ResponseSize int64  `json:"response_size_bytes"`

	// Client
	DeviceType string `json:"device_type"`
	IsBot      bool   `json:"is_bot"`
	UserAgent  string `json:"user_agent"`
	Referer    string `json:"referer"`

	// Migration (future use)
	MigrationActive bool   `json:"migration_active"`
	WarmingPolicy   string `json:"warming_policy,omitempty"`
	WarmingWouldHit *bool  `json:"warming_would_hit,omitempty"`

	DeploymentID string `json:"deployment_id"`
}

func NewEventID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New().String()
	}
	return id.String()
}
