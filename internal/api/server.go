package api

import (
	"io/fs"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cha195/meridian/internal/cache"
	"github.com/cha195/meridian/internal/config"
	"github.com/cha195/meridian/internal/events"
	"github.com/cha195/meridian/internal/geo"
)

type APIServer struct {
	router        *http.ServeMux
	config        *config.Config
	emitter       *events.Emitter
	clusterState  *geo.ClusterState
	geoLocator    *geo.GeoLocator
	caches        map[string]*cache.MigratingCache
	db            *pgxpool.Pool
	wsBroadcaster *WebSocketBroadcaster
	dashboardFS   fs.FS
	server        *http.Server
}

type APIServerOption func(*APIServer)

func WithDB(pool *pgxpool.Pool) APIServerOption {
	return func(s *APIServer) { s.db = pool }
}

func WithWebSocketBroadcaster(b *WebSocketBroadcaster) APIServerOption {
	return func(s *APIServer) { s.wsBroadcaster = b }
}

func WithDashboardFS(fsys fs.FS) APIServerOption {
	return func(s *APIServer) { s.dashboardFS = fsys }
}

func NewAPIServer(
	cfg *config.Config,
	emitter *events.Emitter,
	clusterState *geo.ClusterState,
	geoLocator *geo.GeoLocator,
	caches map[string]*cache.MigratingCache,
	opts ...APIServerOption,
) *APIServer {
	s := &APIServer{
		router:       http.NewServeMux(),
		config:       cfg,
		emitter:      emitter,
		clusterState: clusterState,
		geoLocator:   geoLocator,
		caches:       caches,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.registerRoutes()
	return s
}

func (s *APIServer) registerRoutes() {
	s.router.HandleFunc("GET /healthz", s.handleHealthz)
	s.router.HandleFunc("GET /readyz", s.handleReadyz)

	if s.dashboardFS != nil {
		s.router.Handle("GET /", http.FileServer(http.FS(s.dashboardFS)))
	}

	// WebSocket (auth via query param, not middleware)
	if s.wsBroadcaster != nil {
		s.router.HandleFunc("GET /api/v1/events/stream", s.handleEventsStream)
	}

	authed := http.NewServeMux()
	authed.HandleFunc("GET /api/v1/cluster/status", s.handleClusterStatus)
	authed.HandleFunc("GET /api/v1/cache/policies", s.handleCachePolicies)
	authed.HandleFunc("GET /api/v1/cache/migration/status", s.handleMigrationStatus)
	authed.HandleFunc("POST /api/v1/cache/purge", s.handleCachePurge)
	authed.HandleFunc("POST /api/v1/cache/migrate", s.handleMigrateStart)
	authed.HandleFunc("POST /api/v1/cache/migrate/abort", s.handleMigrateAbort)

	authed.HandleFunc("GET /api/v1/stats/live", s.handleLiveStats)
	authed.HandleFunc("GET /api/v1/stats/timeseries", s.handleTimeseries)
	authed.HandleFunc("GET /api/v1/geo/countries", s.handleCountries)
	authed.HandleFunc("GET /api/v1/cache/overview", s.handleCacheOverviewStats)
	authed.HandleFunc("GET /api/v1/routing/analysis", s.handleRoutingAnalysis)
	authed.HandleFunc("GET /api/v1/events/recent", s.handleRecentEvents)

	s.router.Handle("/api/", authMiddleware(s.config.Projects, authed))
}

func (s *APIServer) Start(addr string) *http.Server {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	go func() {
		log.Printf("API server listening on %s", addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server error: %v", err)
		}
	}()

	return s.server
}

func (s *APIServer) Handler() http.Handler {
	return s.router
}
