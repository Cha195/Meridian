package storage

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cha195/meridian/internal/events"
)

func GetRecentEvents(pool *pgxpool.Pool, projectID string, limit int) ([]events.DiagnosticEvent, error) {
	rows, err := pool.Query(context.Background(),
		`SELECT event_id, project_id, timestamp,
			client_country, client_city, client_lat, client_lng, client_continent,
			edge_node, closest_node, routing_correct, distance_to_node_km,
			cache_status, cache_policy, cache_age_seconds, cache_ttl,
			total_latency_ms, origin_latency_ms, ttfb_ms,
			method, path, status_code, response_size_bytes,
			device_type, is_bot, user_agent, referer,
			migration_active, warming_policy, warming_would_hit,
			deployment_id
		FROM events
		WHERE project_id = $1
		ORDER BY timestamp DESC
		LIMIT $2`,
		projectID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []events.DiagnosticEvent
	for rows.Next() {
		var e events.DiagnosticEvent
		err := rows.Scan(
			&e.EventID, &e.ProjectID, &e.Timestamp,
			&e.ClientCountry, &e.ClientCity, &e.ClientLat, &e.ClientLng, &e.ClientContinent,
			&e.EdgeNode, &e.ClosestNode, &e.RoutingCorrect, &e.DistanceToNodeKm,
			&e.CacheStatus, &e.CachePolicy, &e.CacheAgeSeconds, &e.CacheTTL,
			&e.TotalLatencyMs, &e.OriginLatencyMs, &e.TTFBMs,
			&e.Method, &e.Path, &e.StatusCode, &e.ResponseSize,
			&e.DeviceType, &e.IsBot, &e.UserAgent, &e.Referer,
			&e.MigrationActive, &e.WarmingPolicy, &e.WarmingWouldHit,
			&e.DeploymentID,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
