package api

import (
	"encoding/json"
	"net/http"

	"github.com/cha195/meridian/internal/cache"
)

// callerCache resolves the authenticated project's cache. Returns nil and writes
// an error response if the project is unknown — this should not happen in
// practice since the auth middleware only admits keys that map to projects in
// s.config.Projects, but guards against config drift.
func (s *APIServer) callerCache(w http.ResponseWriter, r *http.Request) (string, *cache.MigratingCache) {
	projectID := projectIDFromContext(r.Context())
	mc, ok := s.caches[projectID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return "", nil
	}
	return projectID, mc
}

func (s *APIServer) handleCachePolicies(w http.ResponseWriter, r *http.Request) {
	projectID, mc := s.callerCache(w, r)
	if mc == nil {
		return
	}
	stats := mc.Stats()
	status := mc.MigrationStatus()
	writeJSON(w, http.StatusOK, map[string]any{"policies": []map[string]any{{
		"project_id":       projectID,
		"active_policy":    mc.Name(),
		"items":            mc.Len(),
		"size_bytes":       mc.Size(),
		"hit_rate":         stats.HitRate,
		"hits":             stats.Hits,
		"misses":           stats.Misses,
		"evictions":        stats.Evictions,
		"migration_active": status.IsWarming,
	}}})
}

func (s *APIServer) handleMigrationStatus(w http.ResponseWriter, r *http.Request) {
	projectID, mc := s.callerCache(w, r)
	if mc == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]cache.MigrationStatus{
		projectID: mc.MigrationStatus(),
	})
}

func (s *APIServer) handleCachePurge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Pattern == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pattern is required"})
		return
	}

	projectID, mc := s.callerCache(w, r)
	if mc == nil {
		return
	}
	purged := mc.Purge(body.Pattern)
	writeJSON(w, http.StatusOK, map[string]any{
		"purged":     purged,
		"pattern":    body.Pattern,
		"project_id": projectID,
	})
}

func (s *APIServer) handleMigrateStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Policy string `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	projectID, mc := s.callerCache(w, r)
	if mc == nil {
		return
	}

	var maxSize int64
	for _, p := range s.config.Projects {
		if p.ID == projectID {
			maxSize = int64(p.Cache.MaxSizeMB) * 1024 * 1024
			break
		}
	}
	if maxSize == 0 {
		maxSize = 512 * 1024 * 1024
	}

	newPolicy, err := cache.NewPolicy(body.Policy, maxSize)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	fromName := mc.Name()
	mc.StartMigration(newPolicy)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "migration started",
		"project_id": projectID,
		"from":       fromName,
		"to":         body.Policy,
	})
}

func (s *APIServer) handleMigrateAbort(w http.ResponseWriter, r *http.Request) {
	projectID, mc := s.callerCache(w, r)
	if mc == nil {
		return
	}
	mc.AbortMigration()
	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "migration aborted",
		"project_id": projectID,
	})
}
