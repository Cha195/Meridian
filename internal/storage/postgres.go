package storage

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cha195/meridian/internal/events"
)

var eventColumns = []string{
	"event_id", "project_id", "timestamp",
	"client_country", "client_city", "client_lat", "client_lng", "client_continent",
	"edge_node", "closest_node", "routing_correct", "distance_to_node_km",
	"cache_status", "cache_policy", "cache_age_seconds", "cache_ttl",
	"total_latency_ms", "origin_latency_ms", "ttfb_ms",
	"method", "path", "status_code", "response_size_bytes",
	"device_type", "is_bot", "user_agent", "referer",
	"migration_active", "warming_policy", "warming_would_hit",
	"deployment_id",
}

type PostgresStore struct {
	pool          *pgxpool.Pool
	buffer        []events.DiagnosticEvent
	mu            sync.Mutex
	batchSize     int
	flushInterval time.Duration
	done          chan struct{}
	wg            sync.WaitGroup
}

func NewPostgresStore(connString string) (*PostgresStore, error) {
	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, err
	}

	s := &PostgresStore{
		pool:          pool,
		buffer:        make([]events.DiagnosticEvent, 0, 100),
		batchSize:     100,
		flushInterval: 5 * time.Second,
		done:          make(chan struct{}),
	}

	s.wg.Add(1)
	go s.flushLoop()

	return s, nil
}

func (s *PostgresStore) Write(event events.DiagnosticEvent) error {
	s.mu.Lock()
	s.buffer = append(s.buffer, event)
	needsFlush := len(s.buffer) >= s.batchSize
	s.mu.Unlock()

	if needsFlush {
		s.flush()
	}
	return nil
}

func (s *PostgresStore) Flush() error {
	s.flush()
	return nil
}

func (s *PostgresStore) Close() error {
	close(s.done)
	s.wg.Wait()
	s.flush()
	s.pool.Close()
	return nil
}

func (s *PostgresStore) flushLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

// flush swaps out the buffer under the mutex, then performs the CopyFrom
// network call WITHOUT holding the mutex. This allows Write() to keep
// buffering events while a slow CopyFrom is in flight.
func (s *PostgresStore) flush() {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return
	}
	batch := s.buffer
	s.buffer = make([]events.DiagnosticEvent, 0, s.batchSize)
	s.mu.Unlock()

	rows := make([][]any, len(batch))
	for i, e := range batch {
		rows[i] = []any{
			e.EventID, e.ProjectID, e.Timestamp,
			e.ClientCountry, e.ClientCity, e.ClientLat, e.ClientLng, e.ClientContinent,
			e.EdgeNode, e.ClosestNode, e.RoutingCorrect, e.DistanceToNodeKm,
			e.CacheStatus, e.CachePolicy, e.CacheAgeSeconds, e.CacheTTL,
			e.TotalLatencyMs, e.OriginLatencyMs, e.TTFBMs,
			e.Method, e.Path, e.StatusCode, e.ResponseSize,
			e.DeviceType, e.IsBot, e.UserAgent, e.Referer,
			e.MigrationActive, e.WarmingPolicy, e.WarmingWouldHit,
			e.DeploymentID,
		}
	}

	_, err := s.pool.CopyFrom(
		context.Background(),
		pgx.Identifier{"events"},
		eventColumns,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		log.Printf("postgres flush failed (%d events dropped): %v", len(batch), err)
	}
}
