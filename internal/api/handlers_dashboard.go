package api

import (
	"net/http"
	"strconv"

	"github.com/cha195/meridian/internal/storage"
)

func (s *APIServer) handleLiveStats(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not configured"})
		return
	}
	projectID := projectIDFromContext(r.Context())
	stats, err := storage.GetLiveStats(s.db, projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *APIServer) handleCountries(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not configured"})
		return
	}
	projectID := projectIDFromContext(r.Context())
	hours := intQuery(r, "hours", 24, 168)
	countries, err := storage.GetCountryBreakdown(s.db, projectID, hours)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, countries)
}

func (s *APIServer) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not configured"})
		return
	}
	projectID := projectIDFromContext(r.Context())
	hours := intQuery(r, "hours", 24, 168)
	bucket := intQuery(r, "bucket", 60, 1440)
	series, err := storage.GetTimeseries(s.db, projectID, hours, bucket)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, series)
}

func (s *APIServer) handleCacheOverviewStats(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not configured"})
		return
	}
	projectID := projectIDFromContext(r.Context())
	hours := intQuery(r, "hours", 1, 168)
	overview, err := storage.GetCacheOverview(s.db, projectID, hours)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *APIServer) handleRoutingAnalysis(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not configured"})
		return
	}
	projectID := projectIDFromContext(r.Context())
	hours := intQuery(r, "hours", 24, 168)
	analysis, err := storage.GetRoutingAnalysis(s.db, projectID, hours)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, analysis)
}

func (s *APIServer) handleRecentEvents(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not configured"})
		return
	}
	projectID := projectIDFromContext(r.Context())
	limit := intQuery(r, "limit", 50, 200)
	events, err := storage.GetRecentEvents(s.db, projectID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func intQuery(r *http.Request, key string, defaultVal, maxVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return defaultVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}
