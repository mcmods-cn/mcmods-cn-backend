package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

var reviewLockEntityTypes = stringSet(
	"mod", "modpack", "creator", "community_post", "blueprint", "skin",
	"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon",
)

type reviewLockResponse struct {
	Locked       bool       `json:"locked"`
	RequestID    string     `json:"requestId,omitempty"`
	SubmittedAt  *time.Time `json:"submittedAt,omitempty"`
	Subscribed   bool       `json:"subscribed"`
	CanSubscribe bool       `json:"canSubscribe"`
}

func (s *Server) reviewLock(w http.ResponseWriter, r *http.Request) {
	entityType := strings.ToLower(strings.TrimSpace(r.PathValue("entityType")))
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !reviewLockEntityTypes[entityType] || !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "invalid review target")
		return
	}
	claims := currentClaims(r)
	var internalID int64
	if err := s.db.QueryRow(r.Context(), `select internal_id from public_routes
		where entity_type=$1 and public_id=$2`, entityType, publicID).Scan(&internalID); err != nil {
		writeError(w, http.StatusNotFound, "review target not found")
		return
	}
	if !canEditReviewTarget(r.Context(), s.db, currentClaims(r), entityType, internalID, publicID) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	var response reviewLockResponse
	response.CanSubscribe = claims.Subject > 0
	err := s.db.QueryRow(r.Context(), `select request.public_id,request.submitted_at,
		case when $3::bigint>0 then exists(select 1 from review_completion_subscriptions subscription
			where subscription.change_request_id=request.id and subscription.user_id=$3) else false end
		from change_requests request
		where request.aggregate_type=$1 and request.aggregate_key=$2 and request.status='pending'
		order by request.submitted_at,request.id limit 1`, reviewAggregateType(entityType), publicID, claims.Subject).
		Scan(&response.RequestID, &response.SubmittedAt, &response.Subscribed)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, response)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect review lock")
		return
	}
	response.Locked = true
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) subscribeReviewCompletion(w http.ResponseWriter, r *http.Request) {
	entityType := strings.ToLower(strings.TrimSpace(r.PathValue("entityType")))
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !reviewLockEntityTypes[entityType] || !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "invalid review target")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start review subscription")
		return
	}
	defer tx.Rollback(r.Context())
	var internalID int64
	var canonicalPath string
	if err = tx.QueryRow(r.Context(), `select internal_id,coalesce(canonical_path,'') from public_routes
		where entity_type=$1 and public_id=$2`, entityType, publicID).Scan(&internalID, &canonicalPath); err != nil {
		writeError(w, http.StatusNotFound, "review target not found")
		return
	}
	if !canEditReviewTarget(r.Context(), tx, currentClaims(r), entityType, internalID, publicID) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	var requestID int64
	if err = tx.QueryRow(r.Context(), `select id from change_requests
		where aggregate_type=$1 and aggregate_key=$2 and status='pending'
		order by submitted_at,id limit 1 for share`, reviewAggregateType(entityType), publicID).Scan(&requestID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "review has already finished")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect review")
		return
	}
	targetLabel, targetURL := reviewTargetInfoTx(r.Context(), tx, entityType, internalID, publicID, canonicalPath)
	if _, err = tx.Exec(r.Context(), `insert into review_completion_subscriptions(
		change_request_id,user_id,target_label,target_url) values($1,$2,$3,$4)
		on conflict(change_request_id,user_id) do nothing`, requestID, currentClaims(r).Subject, targetLabel, targetURL); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe to review completion")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save review subscription")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"subscribed": true})
}

func canEditReviewTarget(ctx context.Context, query databaseQuery, claims security.Claims, entityType string, internalID int64, publicID string) bool {
	if claims.Subject <= 0 {
		return false
	}
	if claimsAllow(claims, "admin.*") || claimsAllow(claims, "content.review") || claimsAllow(claims, "project.review") {
		return true
	}
	switch entityType {
	case "mod":
		var identity modIdentityRecord
		if query.QueryRow(ctx, `select id,project_code,slug,created_by from mods where id=$1`, internalID).
			Scan(&identity.ID, &identity.UniqueID, &identity.SiteID, &identity.OwnerID) != nil {
			return false
		}
		return identity.OwnerID != nil && *identity.OwnerID == claims.Subject || canEditMod(claims, identity)
	case "modpack":
		var createdBy *int64
		if query.QueryRow(ctx, `select created_by from modpacks where id=$1`, internalID).Scan(&createdBy) != nil {
			return false
		}
		return createdBy != nil && *createdBy == claims.Subject || claimsAllow(claims, "project.edit."+publicID)
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		var createdBy *int64
		if query.QueryRow(ctx, `select created_by from simple_projects where id=$1 and project_type=$2`, internalID, entityType).Scan(&createdBy) != nil {
			return false
		}
		return createdBy != nil && *createdBy == claims.Subject || claimsAllow(claims, "project.edit."+publicID)
	case "creator":
		var createdBy, claimedBy *int64
		if query.QueryRow(ctx, `select created_by,claimed_by from creators where id=$1`, internalID).Scan(&createdBy, &claimedBy) != nil {
			return false
		}
		return claimsAllow(claims, "creator.edit") || createdBy != nil && *createdBy == claims.Subject || claimedBy != nil && *claimedBy == claims.Subject
	case "community_post":
		var authorID int64
		return query.QueryRow(ctx, `select author_id from community_posts where id=$1`, internalID).Scan(&authorID) == nil &&
			(authorID == claims.Subject || claimsAllow(claims, "community.edit"))
	case "blueprint":
		var ownerID int64
		return query.QueryRow(ctx, `select owner_id from blueprints where id=$1`, internalID).Scan(&ownerID) == nil && ownerID == claims.Subject
	case "skin":
		var ownerID int64
		return query.QueryRow(ctx, `select owner_id from skin_assets where id=$1`, internalID).Scan(&ownerID) == nil && ownerID == claims.Subject
	default:
		return false
	}
}

func reviewTargetInfoTx(ctx context.Context, tx pgx.Tx, entityType string, internalID int64, publicID, canonicalPath string) (string, string) {
	label := publicID
	switch entityType {
	case "mod":
		_ = tx.QueryRow(ctx, `select primary_name from mods where id=$1`, internalID).Scan(&label)
	case "modpack":
		_ = tx.QueryRow(ctx, `select primary_name from modpacks where id=$1`, internalID).Scan(&label)
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		_ = tx.QueryRow(ctx, `select primary_name from simple_projects where id=$1 and project_type=$2`, internalID, entityType).Scan(&label)
	case "creator":
		_ = tx.QueryRow(ctx, `select name from creators where id=$1`, internalID).Scan(&label)
	case "community_post":
		_ = tx.QueryRow(ctx, `select title from community_posts where id=$1`, internalID).Scan(&label)
	case "blueprint":
		_ = tx.QueryRow(ctx, `select title from blueprints where id=$1`, internalID).Scan(&label)
	case "skin":
		_ = tx.QueryRow(ctx, `select display_name from skin_assets where id=$1`, internalID).Scan(&label)
	}
	if strings.TrimSpace(canonicalPath) == "" {
		canonicalPath = "/"
	}
	return label, canonicalPath
}

func reviewAggregateType(entityType string) string {
	if simpleProjectTypes[entityType] {
		return simpleProjectAggregate
	}
	return entityType
}

func createReviewCompletionNotificationsTx(ctx context.Context, tx pgx.Tx, requestID int64, status string) error {
	if _, err := tx.Exec(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale,data)
		select subscription.user_id,'review','项目审核已完成',
			'您订阅的「'||subscription.target_label||'」审核已经完成。','zh-CN',
			jsonb_build_object('targetLabel',subscription.target_label,'url',subscription.target_url,
				'reviewStatus',$2::text,'changeRequestId',request.public_id)
		from review_completion_subscriptions subscription
		join change_requests request on request.id=subscription.change_request_id
		where subscription.change_request_id=$1 and subscription.notified_at is null`, requestID, status); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update review_completion_subscriptions set notified_at=now()
		where change_request_id=$1 and notified_at is null`, requestID)
	return err
}
