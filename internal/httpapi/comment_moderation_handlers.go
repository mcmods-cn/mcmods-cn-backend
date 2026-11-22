package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/security"
)

type pendingCommentCursor struct {
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	PublicID  string    `json:"publicId"`
}

type pendingCommentResponse struct {
	ID              string            `json:"id"`
	Body            string            `json:"body"`
	Status          string            `json:"status"`
	AuthorID        string            `json:"authorId"`
	CreatedAt       time.Time         `json:"createdAt"`
	Target          commentTargetInfo `json:"target"`
	targetType      string
	targetID        int64
	targetVersionID *int64
	authorID        int64
}

func (s *Server) pendingComments(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.moderate") {
		writeError(w, 403, "comment moderation permission required")
		return
	}
	limit := 50
	if text := r.URL.Query().Get("limit"); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 100 {
			writeError(w, 400, "invalid comment moderation limit")
			return
		}
		limit = n
	}
	status := "pending"
	if value := r.URL.Query().Get("status"); value != "" {
		if value != "pending" && value != "hidden" {
			writeError(w, 400, "invalid moderation status")
			return
		}
		status = value
	}
	var cursorTime *time.Time
	cursorID := ""
	if encoded := r.URL.Query().Get("cursor"); encoded != "" {
		var cursor pendingCommentCursor
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
		if len(encoded) > 1024 || decodeErr != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Status != status || cursor.CreatedAt.IsZero() || !validCatalogPublicID(cursor.PublicID) {
			writeError(w, http.StatusBadRequest, "invalid comment moderation cursor")
			return
		}
		cursorTime, cursorID = &cursor.CreatedAt, cursor.PublicID
	}
	rows, err := s.db.Query(r.Context(), `select comment.public_id,comment.body,comment.status,author.public_id,comment.created_at,
 comment.target_type,comment.target_id,comment.target_version_id,comment.author_id
 from comments comment join users author on author.id=comment.author_id where comment.status=$1
 and ($3::timestamptz is null or (comment.created_at,comment.public_id)>($3::timestamptz,$4::text))
 order by comment.created_at,comment.public_id limit $2`, status, limit+1, cursorTime, cursorID)
	if err != nil {
		writeError(w, 500, "failed to load pending comments")
		return
	}
	candidates := make([]pendingCommentResponse, 0)
	for rows.Next() {
		var item pendingCommentResponse
		err = rows.Scan(&item.ID, &item.Body, &item.Status, &item.AuthorID, &item.CreatedAt, &item.targetType, &item.targetID, &item.targetVersionID, &item.authorID)
		if err != nil {
			break
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		writeError(w, 500, "failed to read pending comments")
		return
	}
	hasMore := len(candidates) > limit
	nextCursor := ""
	if hasMore {
		candidates = candidates[:limit]
		last := candidates[len(candidates)-1]
		raw, _ := json.Marshal(pendingCommentCursor{Status: status, CreatedAt: last.CreatedAt, PublicID: last.ID})
		nextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	items := make([]pendingCommentResponse, 0, len(candidates))
	for _, item := range candidates {
		target, loadErr := s.resolveCommentTargetByInternal(r.Context(), item.targetType, item.targetID, item.targetVersionID, security.Claims{})
		if errors.Is(loadErr, pgx.ErrNoRows) {
			continue
		}
		if loadErr != nil {
			writeError(w, 500, "failed to load comment target")
			return
		}
		item.Target = target
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{"items": items, "hasMore": hasMore, "nextCursor": nextCursor})
}

func (s *Server) reviewPendingComment(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))
	var request struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !validCatalogPublicID(publicID) || decodeJSON(r, &request) != nil || (request.Status != "approved" && request.Status != "rejected" && request.Status != "restore") || len(request.Note) > 2000 {
		writeError(w, 400, "invalid comment review")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start comment review")
		return
	}
	defer tx.Rollback(r.Context())
	var id, authorID, targetID int64
	var targetType, status, body, projectID string
	var targetVersionID, parentID *int64
	err = tx.QueryRow(r.Context(), `select comment.id,comment.author_id,comment.target_type,comment.target_id,comment.target_version_id,
 comment.parent_id,comment.status,comment.body,coalesce(route.public_id,resource_mod.project_code,'')
 from comments comment
 left join public_routes route on route.entity_type=comment.target_type and route.internal_id=comment.target_id
 left join mod_content_versions version on comment.target_type='mod_resource' and version.id=comment.target_version_id
 left join mods resource_mod on resource_mod.id=version.mod_id
 where comment.public_id=$1 for update of comment`, publicID).Scan(&id, &authorID, &targetType, &targetID, &targetVersionID, &parentID, &status, &body, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "comment not found")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to load comment review")
		return
	}
	globalModerator := claimsAllow(claims, "comment.moderate")
	if !globalModerator && (claims.Subject == authorID || projectID == "" || !claimsAllow(claims, "project.comment.moderate."+projectID)) {
		writeError(w, 403, "comment moderator required")
		return
	}
	_, err = resolveCommentTargetByInternalWithQueryer(r.Context(), tx, targetType, targetID, targetVersionID, security.Claims{})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "comment target unavailable")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to check comment target")
		return
	}
	next := "hidden"
	if request.Status == "approved" {
		next = "published"
	}
	if request.Status == "restore" {
		var restorable bool
		err = tx.QueryRow(r.Context(), `select exists(select 1 from audit_events where aggregate_type='comment_moderation' and aggregate_key=$1 and action='comment.review.rejected')
  and not exists(select 1 from audit_events where aggregate_type='comment_moderation' and aggregate_key=$1 and action='comment.review.approved')`, publicID).Scan(&restorable)
		if err != nil {
			writeError(w, 500, "failed to inspect comment recovery")
			return
		}
		if status != "hidden" || !restorable {
			writeError(w, 409, "comment cannot be restored to review")
			return
		}
		next = "pending"
	} else if status != "pending" {
		writeError(w, 409, "comment is no longer pending review")
		return
	}
	if _, err = tx.Exec(r.Context(), `update comments set status=$2,updated_at=now() where id=$1`, id, next); err != nil {
		writeError(w, 500, "failed to update comment review")
		return
	}
	if next == "published" {
		notifications := make([]pendingWatchNotification, 0)
		var recipientID int64
		if parentID != nil {
			if err = tx.QueryRow(r.Context(), `select author_id from comments where id=$1 and status='published'`, *parentID).Scan(&recipientID); errors.Is(err, pgx.ErrNoRows) {
				writeError(w, 409, "reply target unavailable")
				return
			} else if err != nil {
				writeError(w, 500, "failed to load reply target")
				return
			}
			notifications, err = recordCommentWatchReplies(r.Context(), tx, *parentID, id, authorID)
			if err != nil {
				writeError(w, 500, "failed to update comment watches")
				return
			}
		}
		if err = enqueueCommentDeliveryNotificationsTx(r.Context(), tx, authorID, publicID, parentID, recipientID, notifications); err != nil {
			writeError(w, 500, "failed to persist comment notification")
			return
		}
		if err = activity.EnqueueDurableTx(r.Context(), tx, activity.Event{UserID: authorID, ActionID: activity.ActionCreate, ObjectTypeID: activity.ObjectComment, ObjectPublicID: publicID, MarkdownAddedBytes: len(body)}); err != nil {
			writeError(w, 500, "failed to persist comment activity")
			return
		}
	}
	actor, err := actorSnapshotTx(r.Context(), tx, claims.Subject)
	if err != nil {
		writeError(w, 500, "failed to load moderator")
		return
	}
	if err = appendAuditEventTx(r.Context(), tx, auditEventParams{EntityType: "comment", EntityID: id, AggregateType: "comment_moderation", AggregateKey: publicID, ActorID: claims.Subject, ActorSnapshot: actor, Action: "comment.review." + request.Status, Metadata: map[string]any{"before": status, "after": next, "note": request.Note}}); err != nil {
		writeError(w, 500, "failed to record comment review")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to commit comment review")
		return
	}
	skipRequestActivity(r)
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID, "status": next})
}
