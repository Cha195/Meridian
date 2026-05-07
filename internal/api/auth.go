package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cha195/meridian/internal/config"
)

type contextKey string

const projectIDKey contextKey = "projectID"

func authMiddleware(projects []config.ProjectConfig, next http.Handler) http.Handler {
	keyToProject := make(map[string]string, len(projects))
	for _, p := range projects {
		if p.APIKey != "" {
			keyToProject[p.APIKey] = p.ID
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := extractAPIKey(r)
		if key == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
			return
		}

		projectID, ok := keyToProject[key]
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
			return
		}

		ctx := context.WithValue(r.Context(), projectIDKey, projectID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractAPIKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func projectIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(projectIDKey).(string)
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
