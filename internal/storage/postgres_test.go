package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cha195/meridian/internal/events"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	connString := os.Getenv("MERIDIAN_TEST_DB")
	if connString == "" {
		t.Skip("set MERIDIAN_TEST_DB to run Postgres integration tests")
	}
	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	_, err = pool.Exec(context.Background(), "DELETE FROM events")
	if err != nil {
		t.Fatalf("failed to clean events table: %v", err)
	}
	return pool
}

func makeTestEvent(id string) events.DiagnosticEvent {
	return events.DiagnosticEvent{
		EventID:          id,
		ProjectID:        "default",
		Timestamp:        time.Now(),
		ClientCountry:    "US",
		ClientCity:       "New York",
		ClientLat:        40.7128,
		ClientLng:        -74.0060,
		ClientContinent:  "NA",
		EdgeNode:         "virginia",
		ClosestNode:      "virginia",
		RoutingCorrect:   true,
		DistanceToNodeKm: 12.5,
		CacheStatus:      "HIT",
		CachePolicy:      "sieve",
		CacheTTL:         3600,
		TotalLatencyMs:   1.5,
		TTFBMs:           0.8,
		Method:           "GET",
		Path:             "/index.html",
		StatusCode:       200,
		ResponseSize:     4096,
		DeviceType:       "desktop",
		IsBot:            false,
		UserAgent:        "Mozilla/5.0",
		DeploymentID:     "test",
	}
}

func TestPostgresBatchInsert(t *testing.T) {
	pool := testPool(t)

	store, err := NewPostgresStore(os.Getenv("MERIDIAN_TEST_DB"))
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()

	store.batchSize = 5

	for i := 0; i < 10; i++ {
		e := makeTestEvent(events.NewEventID())
		if err := store.Write(e); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}
	store.Flush()

	var count int
	err = pool.QueryRow(context.Background(), "SELECT count(*) FROM events").Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10 events, got %d", count)
	}
}

func TestPostgresFlushOnClose(t *testing.T) {
	pool := testPool(t)

	store, err := NewPostgresStore(os.Getenv("MERIDIAN_TEST_DB"))
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}

	store.batchSize = 100

	for i := 0; i < 3; i++ {
		e := makeTestEvent(events.NewEventID())
		if err := store.Write(e); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	store.Close()

	var count int
	err = pool.QueryRow(context.Background(), "SELECT count(*) FROM events").Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 events flushed on Close, got %d", count)
	}
}
