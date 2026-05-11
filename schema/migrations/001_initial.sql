-- 001_initial.sql
-- Postgres schema for Meridian CDN analytics.
-- Columns in the events table map 1:1 to DiagnosticEvent fields in
-- internal/events/event.go. Column names match JSON tags.

-- Projects: one row per configured origin site.
CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    api_key     TEXT NOT NULL UNIQUE,
    origin      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Events: one row per proxied request.
-- At scale, partition by RANGE (timestamp) with monthly partitions and a
-- retention policy (DROP old partitions). See: future migration 002_partitioning.sql
CREATE TABLE IF NOT EXISTS events (
    event_id            TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL,
    timestamp           TIMESTAMPTZ NOT NULL,

    -- client geo
    client_country      TEXT,
    client_city         TEXT,
    client_lat          DOUBLE PRECISION,
    client_lng          DOUBLE PRECISION,
    client_continent    TEXT,

    -- routing
    edge_node           TEXT NOT NULL,
    closest_node        TEXT,
    routing_correct     BOOLEAN NOT NULL DEFAULT false,
    distance_to_node_km DOUBLE PRECISION,

    -- cache
    cache_status        TEXT NOT NULL,       -- HIT, MISS, STALE, BYPASS
    cache_policy        TEXT NOT NULL,       -- sieve, w-tinylfu, lru
    cache_age_seconds   INTEGER,
    cache_ttl           INTEGER NOT NULL,

    -- performance (milliseconds)
    total_latency_ms    DOUBLE PRECISION NOT NULL,
    origin_latency_ms   DOUBLE PRECISION,   -- NULL on cache HIT
    ttfb_ms             DOUBLE PRECISION NOT NULL,

    -- request
    method              TEXT NOT NULL,
    path                TEXT NOT NULL,
    status_code         INTEGER NOT NULL,
    response_size_bytes BIGINT NOT NULL,

    -- client
    device_type         TEXT,
    is_bot              BOOLEAN NOT NULL DEFAULT false,
    user_agent          TEXT,
    referer             TEXT,

    -- migration
    migration_active    BOOLEAN NOT NULL DEFAULT false,
    warming_policy      TEXT,
    warming_would_hit   BOOLEAN,

    -- meta
    deployment_id       TEXT
);

CREATE INDEX IF NOT EXISTS idx_events_project_time ON events (project_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_events_cache_status ON events (cache_status, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_events_edge_node    ON events (edge_node, timestamp DESC);

-- Hourly aggregates for the analytics dashboard.
-- Populated by the event pipeline (Stage 9) or a cron job.
CREATE TABLE IF NOT EXISTS hourly_stats (
    project_id      TEXT NOT NULL,
    edge_node       TEXT NOT NULL,
    hour            TIMESTAMPTZ NOT NULL,
    cache_policy    TEXT NOT NULL,
    total_requests  BIGINT NOT NULL DEFAULT 0,
    cache_hits      BIGINT NOT NULL DEFAULT 0,
    cache_misses    BIGINT NOT NULL DEFAULT 0,
    cache_stale     BIGINT NOT NULL DEFAULT 0,
    avg_latency_ms  DOUBLE PRECISION,
    avg_ttfb_ms     DOUBLE PRECISION,
    p99_latency_ms  DOUBLE PRECISION,
    total_bytes     BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, edge_node, hour, cache_policy)
);

-- Policy comparison: hit rates side by side during a live migration.
CREATE TABLE IF NOT EXISTS policy_comparisons (
    project_id       TEXT NOT NULL REFERENCES projects(id),
    hour             TIMESTAMPTZ NOT NULL,
    active_policy    TEXT NOT NULL,
    warming_policy   TEXT,
    active_hit_rate  DOUBLE PRECISION,
    warming_hit_rate DOUBLE PRECISION,
    sample_size      BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, hour, active_policy)
);
