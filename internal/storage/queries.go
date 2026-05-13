package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cha195/meridian/internal/events"
)

type LiveStats struct {
	ActiveNow       int     `json:"active_now"`
	TodayTotal      int     `json:"today_total"`
	P95LatencyMs    float64 `json:"p95_latency_ms"`
	CacheHitRate    float64 `json:"cache_hit_rate"`
	RoutingAccuracy float64 `json:"routing_accuracy"`
}

type CountryStats struct {
	Country      string  `json:"country"`
	Requests     int     `json:"requests"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	CacheHitRate float64 `json:"cache_hit_rate"`
}

type TimeseriesBucket struct {
	Time         time.Time `json:"time"`
	Requests     int       `json:"requests"`
	AvgLatencyMs float64   `json:"avg_latency_ms"`
	CacheHitRate float64   `json:"cache_hit_rate"`
}

type CacheOverview struct {
	Hits   int              `json:"hits"`
	Misses int              `json:"misses"`
	Stale  int              `json:"stale"`
	ByPolicy []PolicyStats  `json:"by_policy"`
}

type PolicyStats struct {
	Policy   string  `json:"policy"`
	Requests int     `json:"requests"`
	HitRate  float64 `json:"hit_rate"`
}

type RoutingAnalysis struct {
	Total            int        `json:"total"`
	CorrectlyRouted  int        `json:"correctly_routed"`
	Accuracy         float64    `json:"accuracy"`
	Misroutes        []Misroute `json:"misroutes"`
}

type Misroute struct {
	Country     string `json:"country"`
	EdgeNode    string `json:"edge_node"`
	ClosestNode string `json:"closest_node"`
	Count       int    `json:"count"`
}

func GetLiveStats(pool *pgxpool.Pool, projectID string) (LiveStats, error) {
	var s LiveStats
	err := pool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE timestamp > now() - interval '5 minutes'),
			count(*) FILTER (WHERE timestamp > now() - interval '24 hours'),
			coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY total_latency_ms)
				FILTER (WHERE timestamp > now() - interval '5 minutes'), 0),
			coalesce(avg(CASE WHEN cache_status = 'HIT' THEN 1.0 ELSE 0.0 END)
				FILTER (WHERE timestamp > now() - interval '1 hour'), 0),
			coalesce(avg(CASE WHEN routing_correct THEN 1.0 ELSE 0.0 END)
				FILTER (WHERE timestamp > now() - interval '1 hour'), 0)
		FROM events WHERE project_id = $1 AND timestamp > now() - interval '24 hours'`,
		projectID,
	).Scan(&s.ActiveNow, &s.TodayTotal, &s.P95LatencyMs, &s.CacheHitRate, &s.RoutingAccuracy)
	return s, err
}

func GetCountryBreakdown(pool *pgxpool.Pool, projectID string, hours int) ([]CountryStats, error) {
	rows, err := pool.Query(context.Background(), `
		SELECT
			client_country,
			count(*) AS requests,
			avg(total_latency_ms) AS avg_latency,
			avg(CASE WHEN cache_status = 'HIT' THEN 1.0 ELSE 0.0 END) AS hit_rate
		FROM events
		WHERE project_id = $1 AND timestamp > now() - ($2 * interval '1 hour')
		GROUP BY client_country
		ORDER BY requests DESC
		LIMIT 20`,
		projectID, hours,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CountryStats
	for rows.Next() {
		var c CountryStats
		if err := rows.Scan(&c.Country, &c.Requests, &c.AvgLatencyMs, &c.CacheHitRate); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func GetTimeseries(pool *pgxpool.Pool, projectID string, hours, bucketMinutes int) ([]TimeseriesBucket, error) {
	rows, err := pool.Query(context.Background(), `
		SELECT
			date_trunc('hour', timestamp) +
				(floor(extract(minute FROM timestamp) / $2) * ($2 * interval '1 minute')) AS bucket,
			count(*) AS requests,
			avg(total_latency_ms) AS avg_latency,
			avg(CASE WHEN cache_status = 'HIT' THEN 1.0 ELSE 0.0 END) AS hit_rate
		FROM events
		WHERE project_id = $1 AND timestamp > now() - ($3 * interval '1 hour')
		GROUP BY bucket
		ORDER BY bucket`,
		projectID, bucketMinutes, hours,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TimeseriesBucket
	for rows.Next() {
		var b TimeseriesBucket
		if err := rows.Scan(&b.Time, &b.Requests, &b.AvgLatencyMs, &b.CacheHitRate); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func GetCacheOverview(pool *pgxpool.Pool, projectID string, hours int) (CacheOverview, error) {
	var o CacheOverview
	err := pool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE cache_status = 'HIT'),
			count(*) FILTER (WHERE cache_status = 'MISS'),
			count(*) FILTER (WHERE cache_status = 'STALE')
		FROM events
		WHERE project_id = $1 AND timestamp > now() - ($2 * interval '1 hour')`,
		projectID, hours,
	).Scan(&o.Hits, &o.Misses, &o.Stale)
	if err != nil {
		return o, err
	}

	rows, err := pool.Query(context.Background(), `
		SELECT
			cache_policy,
			count(*) AS requests,
			avg(CASE WHEN cache_status = 'HIT' THEN 1.0 ELSE 0.0 END) AS hit_rate
		FROM events
		WHERE project_id = $1 AND timestamp > now() - ($2 * interval '1 hour')
		GROUP BY cache_policy
		ORDER BY requests DESC`,
		projectID, hours,
	)
	if err != nil {
		return o, err
	}
	defer rows.Close()

	for rows.Next() {
		var p PolicyStats
		if err := rows.Scan(&p.Policy, &p.Requests, &p.HitRate); err != nil {
			return o, err
		}
		o.ByPolicy = append(o.ByPolicy, p)
	}
	return o, rows.Err()
}

func GetRoutingAnalysis(pool *pgxpool.Pool, projectID string, hours int) (RoutingAnalysis, error) {
	var r RoutingAnalysis
	err := pool.QueryRow(context.Background(), `
		SELECT
			count(*),
			count(*) FILTER (WHERE routing_correct),
			coalesce(avg(CASE WHEN routing_correct THEN 1.0 ELSE 0.0 END), 0)
		FROM events
		WHERE project_id = $1 AND timestamp > now() - ($2 * interval '1 hour')`,
		projectID, hours,
	).Scan(&r.Total, &r.CorrectlyRouted, &r.Accuracy)
	if err != nil {
		return r, err
	}

	rows, err := pool.Query(context.Background(), `
		SELECT client_country, edge_node, closest_node, count(*) AS cnt
		FROM events
		WHERE project_id = $1 AND timestamp > now() - ($2 * interval '1 hour')
			AND NOT routing_correct
		GROUP BY client_country, edge_node, closest_node
		ORDER BY cnt DESC
		LIMIT 10`,
		projectID, hours,
	)
	if err != nil {
		return r, err
	}
	defer rows.Close()

	for rows.Next() {
		var m Misroute
		if err := rows.Scan(&m.Country, &m.EdgeNode, &m.ClosestNode, &m.Count); err != nil {
			return r, err
		}
		r.Misroutes = append(r.Misroutes, m)
	}
	return r, rows.Err()
}

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
