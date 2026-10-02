package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/querycache"
)

type unreadReconcileRequest struct {
	UserIDs []string `json:"userIds"`
}

const unreadReconcileMaxSampleSize = 16

func StartUnreadReconciliation(ctx context.Context, db *pgxpool.Pool, cache *querycache.Cache, enabled bool, interval time.Duration, batchSize int) {
	if !enabled || db == nil || cache == nil || interval <= 0 || batchSize <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sampleSize := min(batchSize, unreadReconcileMaxSampleSize)
				userIDs := cache.UnreadReconciliationCandidates(sampleSize)
				if len(userIDs) == 0 {
					continue
				}
				_, _, err := reconcileUnreadUsers(ctx, db, cache, userIDs)
				if err != nil {
					slog.Warn("reconcile unread cache", "module", "unread", "error", err)
				}
			}
		}
	}()
}

func reconcileUnreadUsers(ctx context.Context, db *pgxpool.Pool, cache *querycache.Cache, userIDs []int64) (int, int, error) {
	if len(userIDs) == 0 {
		return 0, 0, nil
	}
	items, err := loadUnreadTruth(ctx, db, unreadTruthInternalUsersSQL, userIDs)
	if err != nil {
		return 0, 0, err
	}
	processed, drifts := 0, 0
	for _, item := range items {
		drifted, reconcileErr := cache.ReconcileUnread(ctx, item.UserID, item.Summary)
		if reconcileErr != nil {
			return processed, drifts, reconcileErr
		}
		processed++
		if drifted {
			drifts++
		}
	}
	return processed, drifts, nil
}

func (s *Server) adminReconcileUnread(w http.ResponseWriter, r *http.Request) {
	var request unreadReconcileRequest
	if decodeJSON(r, &request) != nil || len(request.UserIDs) == 0 || len(request.UserIDs) > 100 {
		writeError(w, http.StatusBadRequest, "between one and 100 user IDs are required")
		return
	}
	rows, err := s.db.Query(r.Context(), `select id from users where public_id=any($1::text[]) order by id`, request.UserIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load unread truth")
		return
	}
	userIDs := make([]int64, 0, len(request.UserIDs))
	for rows.Next() {
		var userID int64
		if err = rows.Scan(&userID); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to decode unread users")
			return
		}
		userIDs = append(userIDs, userID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "failed to decode unread users")
		return
	}
	rows.Close()
	truthItems, err := loadUnreadTruth(r.Context(), s.db, unreadTruthAnyInternalUsersSQL, userIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load unread truth")
		return
	}
	items := make([]map[string]any, 0, len(request.UserIDs))
	for _, truth := range truthItems {
		drifted, reconcileErr := s.cache.ReconcileUnread(r.Context(), truth.UserID, truth.Summary)
		if reconcileErr != nil {
			writeError(w, http.StatusServiceUnavailable, "failed to reconcile unread cache")
			return
		}
		items = append(items, map[string]any{"userId": truth.PublicID, "drifted": drifted, "notifications": truth.Summary.Notifications, "messages": truth.Summary.Messages})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
