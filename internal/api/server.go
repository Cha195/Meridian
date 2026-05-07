package api

import (
	"log"
	"net/http"

	"github.com/cha195/meridian/internal/cache"
	"github.com/cha195/meridian/internal/config"
	"github.com/cha195/meridian/internal/events"
	"github.com/cha195/meridian/internal/geo"
)

type APIServer struct {
	router       *http.ServeMux
	config       *config.Config
	emitter      *events.Emitter
	clusterState *geo.ClusterState
	geoLocator   *geo.GeoLocator
	caches       map[string]*cache.MigratingCache
	server       *http.Server
}

func NewAPIServer(
	cfg *config.Config,
	emitter *events.Emitter,
	clusterState *geo.ClusterState,
	geoLocator *geo.GeoLocator,
	caches map[string]*cache.MigratingCache,
) *APIServer {
	s := &APIServer{
		router:       http.NewServeMux(),
		config:       cfg,
		emitter:      emitter,
		clusterState: clusterState,
		geoLocator:   geoLocator,
		caches:       caches,
	}
	s.registerRoutes()
	return s
}

func (s *APIServer) registerRoutes() {
	// Public endpoints (no auth)
	s.router.HandleFunc("GET /healthz", s.handleHealthz)
	s.router.HandleFunc("GET /readyz", s.handleReadyz)

	// Authenticated endpoints
	authed := http.NewServeMux()
	authed.HandleFunc("GET /api/v1/cluster/status", s.handleClusterStatus)
	authed.HandleFunc("GET /api/v1/cache/policies", s.handleCachePolicies)
	authed.HandleFunc("GET /api/v1/cache/migration/status", s.handleMigrationStatus)
	authed.HandleFunc("POST /api/v1/cache/purge", s.handleCachePurge)
	authed.HandleFunc("POST /api/v1/cache/migrate", s.handleMigrateStart)
	authed.HandleFunc("POST /api/v1/cache/migrate/abort", s.handleMigrateAbort)

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
