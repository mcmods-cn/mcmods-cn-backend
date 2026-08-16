package httpapi

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) activityIngestionStatus(w http.ResponseWriter, r *http.Request) {
	if s.activity == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "disabled"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	snapshot := s.activity.Snapshot(ctx)
	status := "healthy"
	if snapshot.BacklogQueryError != "" || snapshot.LastError != "" {
		status = "degraded"
	}
	if snapshot.OldestDurableAt != nil && time.Since(*snapshot.OldestDurableAt) > 5*time.Minute {
		status = "backlogged"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "ingestion": snapshot})
}
