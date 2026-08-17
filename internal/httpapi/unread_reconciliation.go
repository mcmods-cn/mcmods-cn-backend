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

func StartUnreadReconciliation(ctx context.Context, db *pgxpool.Pool, cache *querycache.Cache, enabled bool, interval time.Duration, batchSize int) {
	if !enabled || db == nil || cache == nil || interval <= 0 || batchSize <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var cursor int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				next, _, _, err := reconcileUnreadBatch(ctx, db, cache, cursor, batchSize)
				if err != nil {
					slog.Warn("reconcile unread cache", "module", "unread", "error", err)
					continue
				}
				cursor = next
			}
		}
	}()
}

func reconcileUnreadBatch(ctx context.Context, db *pgxpool.Pool, cache *querycache.Cache, cursor int64, limit int) (int64, int, int, error) {
	rows, err := db.Query(ctx, `select u.id,
		(select count(*) from notifications n left join notification_receipts receipt
			on receipt.notification_id=n.id and receipt.user_id=u.id
			where (n.recipient_id is null or n.recipient_id=u.id) and receipt.read_at is null),
		(select count(*) from direct_messages m where m.recipient_id=u.id and m.read_at is null)
		from users u where u.status='active' and u.id>$1 order by u.id limit $2`, cursor, limit)
	if err != nil {
		return cursor, 0, 0, err
	}
	defer rows.Close()
	next, processed, drifts := cursor, 0, 0
	for rows.Next() {
		var userID int64
		var truth querycache.UnreadSummary
		if err = rows.Scan(&userID, &truth.Notifications, &truth.Messages); err != nil {
			return cursor, processed, drifts, err
		}
		drifted, reconcileErr := cache.ReconcileUnread(ctx, userID, truth)
		if reconcileErr != nil {
			return cursor, processed, drifts, reconcileErr
		}
		processed++
		if drifted {
			drifts++
		}
		next = userID
	}
	if err = rows.Err(); err != nil {
		return cursor, processed, drifts, err
	}
	if processed == 0 {
		next = 0
	}
	return next, processed, drifts, nil
}

func (s *Server) adminReconcileUnread(w http.ResponseWriter, r *http.Request) {
	var request unreadReconcileRequest
	if decodeJSON(r, &request) != nil || len(request.UserIDs) == 0 || len(request.UserIDs) > 100 {
		writeError(w, http.StatusBadRequest, "between one and 100 user IDs are required")
		return
	}
	rows, err := s.db.Query(r.Context(), `select u.id,u.public_id,
		(select count(*) from notifications n left join notification_receipts receipt
			on receipt.notification_id=n.id and receipt.user_id=u.id
			where (n.recipient_id is null or n.recipient_id=u.id) and receipt.read_at is null),
		(select count(*) from direct_messages m where m.recipient_id=u.id and m.read_at is null)
		from users u where u.public_id=any($1::text[])`, request.UserIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load unread truth")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, len(request.UserIDs))
	for rows.Next() {
		var userID int64
		var publicID string
		var truth querycache.UnreadSummary
		if err = rows.Scan(&userID, &publicID, &truth.Notifications, &truth.Messages); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode unread truth")
			return
		}
		drifted, reconcileErr := s.cache.ReconcileUnread(r.Context(), userID, truth)
		if reconcileErr != nil {
			writeError(w, http.StatusServiceUnavailable, "failed to reconcile unread cache")
			return
		}
		items = append(items, map[string]any{"userId": publicID, "drifted": drifted, "notifications": truth.Notifications, "messages": truth.Messages})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to reconcile unread cache")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
