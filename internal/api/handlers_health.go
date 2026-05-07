package api

import (
	"net/http"
)

func (s *APIServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *APIServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.geoLocator == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"reason": "geoip database not loaded",
		})
		return
	}

	policyName := "unknown"
	if len(s.config.Projects) > 0 {
		policyName = s.config.Projects[0].Cache.Policy
		if policyName == "" {
			policyName = "sieve"
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ready",
		"node":         s.config.Node.ID,
		"cache_policy": policyName,
	})
}

func (s *APIServer) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	health := s.clusterState.NodeHealth()
	writeJSON(w, http.StatusOK, map[string]any{
		"self":  s.clusterState.SelfID(),
		"nodes": health,
	})
}
