package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type createProjectEditorApplicationRequest struct {
	ProofMarkdown string   `json:"proofMarkdown"`
	AttachmentIDs []string `json:"attachmentIds"`
}

type reviewProjectEditorApplicationRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type projectEditorApplicationAttachment struct {
	ID           string `json:"id"`
	OriginalName string `json:"originalName"`
	ObjectKey    string `json:"objectKey"`
	SizeBytes    int64  `json:"sizeBytes"`
}

type projectEditorApplicationResponse struct {
	ID            string                               `json:"id"`
	InternalID    int64                                `json:"-"`
	TargetType    string                               `json:"targetType"`
	TargetID      string                               `json:"targetId"`
	TargetName    string                               `json:"targetName"`
	TargetURL     string                               `json:"targetUrl"`
	UserID        string                               `json:"userId"`
	Username      string                               `json:"username"`
	ProofMarkdown string                               `json:"proofMarkdown"`
	Status        string                               `json:"status"`
	ReviewNote    string                               `json:"reviewNote"`
	Attachments   []projectEditorApplicationAttachment `json:"attachments"`
	CreatedAt     time.Time                            `json:"createdAt"`
	ReviewedAt    *time.Time                           `json:"reviewedAt,omitempty"`
}

type projectEditorTarget struct {
	RouteID    int64
	InternalID int64
	Type       string
	PublicID   string
	Name       string
	URL        string
}

var projectEditorTargetTypes = map[string]struct{}{
	"mod": {}, "modpack": {}, "plugin": {}, "map": {}, "resource_pack": {}, "shader_pack": {},
	"datapack": {}, "addon": {}, "minecraft_server": {}, "blueprint": {}, "skin": {}, "community_post": {},
}

func normalizeProjectEditorTargetType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "server" {
		return "minecraft_server"
	}
	return value
}

func (s *Server) projectEditorTargetByPublicID(ctx context.Context, projectType, publicID string) (projectEditorTarget, error) {
	projectType = normalizeProjectEditorTargetType(projectType)
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if _, ok := projectEditorTargetTypes[projectType]; !ok || !validCatalogPublicID(publicID) {
		return projectEditorTarget{}, pgx.ErrNoRows
	}
	var target projectEditorTarget
	err := s.db.QueryRow(ctx, `select id,internal_id,entity_type,public_id,canonical_path
		from public_routes where entity_type=$1 and public_id=$2`, projectType, publicID).
		Scan(&target.RouteID, &target.InternalID, &target.Type, &target.PublicID, &target.URL)
	if err != nil {
		return target, err
	}
	target.Name, err = s.projectEditorTargetName(ctx, target.Type, target.InternalID)
	return target, err
}

func (s *Server) projectEditorTargetName(ctx context.Context, projectType string, internalID int64) (string, error) {
	return projectEditorTargetNameQuery(ctx, s.db, projectType, internalID)
}

func projectEditorTargetNameQuery(ctx context.Context, database databaseQuery, projectType string, internalID int64) (string, error) {
	var query string
	switch projectType {
	case "mod":
		query = `select primary_name from mods where id=$1 and review_status='approved'`
	case "modpack":
		query = `select primary_name from modpacks where id=$1 and review_status='approved'`
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		query = `select primary_name from simple_projects where id=$1 and review_status='approved'`
	case "minecraft_server":
		query = `select name from minecraft_servers where id=$1 and review_status='approved'`
	case "blueprint":
		query = `select title from blueprints where id=$1 and status='ready' and review_status in ('approved','not_required')`
	case "skin":
		query = `select display_name from skin_assets where id=$1 and status='active' and review_status='approved'`
	case "community_post":
		query = `select title from community_posts where id=$1 and review_status='approved'`
	default:
		return "", pgx.ErrNoRows
	}
	var name string
	err := database.QueryRow(ctx, query, internalID).Scan(&name)
	return name, err
}

func (s *Server) projectEditorApplications(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	target, err := s.projectEditorTargetByPublicID(r.Context(), r.PathValue("projectType"), r.PathValue("projectId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	if r.Method == http.MethodGet {
		limit := boundedInt(r.URL.Query().Get("limit"), 100, 1, 200)
		offset := boundedInt(r.URL.Query().Get("offset"), 0, 0, 1000000)
		items, total, listErr := s.queryProjectEditorApplications(r.Context(), 0, target.RouteID, claims.Subject, "", limit, offset)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load editor applications")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset, "hasMore": offset+len(items) < total})
		return
	}
	if claimsAllow(claims, "project.edit."+target.PublicID) || claimsAllow(claims, "project.edit") {
		writeError(w, http.StatusConflict, "you already have access to this project")
		return
	}
	var request createProjectEditorApplicationRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid editor application")
		return
	}
	request.ProofMarkdown = strings.TrimSpace(request.ProofMarkdown)
	request.AttachmentIDs = uniquePublicIDs(request.AttachmentIDs)
	if request.ProofMarkdown == "" || utf8.RuneCountInString(request.ProofMarkdown) > 10000 || len(request.AttachmentIDs) > 10 {
		writeError(w, http.StatusBadRequest, "proof is required and at most 10 attachments are allowed")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create editor application")
		return
	}
	defer tx.Rollback(r.Context())
	var alreadyAssigned bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from effective_project_access
		where user_id=$1 and project_type=$2 and project_id=$3)`, claims.Subject, target.Type, target.InternalID).Scan(&alreadyAssigned); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect project access")
		return
	}
	if alreadyAssigned {
		writeError(w, http.StatusConflict, "you already have access to this project")
		return
	}
	var applicationID int64
	err = tx.QueryRow(r.Context(), `insert into project_editor_applications(target_route_id,user_id,proof_markdown)
		values($1,$2,$3) returning id`, target.RouteID, claims.Subject, request.ProofMarkdown).Scan(&applicationID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an editor application is already pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create editor application")
		return
	}
	for _, fileID := range request.AttachmentIDs {
		file, lookupErr := lookupReviewAttachment(r.Context(), tx, reviewAttachmentLookup{Kind: reviewAttachmentByUploader, UploaderID: claims.Subject}, fileID)
		if lookupErr != nil {
			writeError(w, http.StatusBadRequest, "application attachment does not exist or is not owned by this user")
			return
		}
		if _, err = tx.Exec(r.Context(), `insert into project_editor_application_attachments(application_id,oss_file_id)
			values($1,$2)`, applicationID, file.InternalID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save editor application attachment")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit editor application")
		return
	}
	items, _, err := s.queryProjectEditorApplications(r.Context(), applicationID, 0, 0, "", 1, 0)
	if err != nil || len(items) != 1 {
		writeError(w, http.StatusInternalServerError, "failed to read submitted editor application")
		return
	}
	writeJSON(w, http.StatusCreated, items[0])
}

func (s *Server) adminProjectEditorApplications(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" && status != "withdrawn" {
		writeError(w, http.StatusBadRequest, "invalid editor application status")
		return
	}
	limit := boundedInt(r.URL.Query().Get("limit"), 100, 1, 200)
	offset := boundedInt(r.URL.Query().Get("offset"), 0, 0, 1000000)
	items, total, err := s.queryProjectEditorApplications(r.Context(), 0, 0, 0, status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load editor applications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset, "hasMore": offset+len(items) < total})
}

func (s *Server) reviewProjectEditorApplication(w http.ResponseWriter, r *http.Request) {
	applicationPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	var request reviewProjectEditorApplicationRequest
	if !validCatalogPublicID(applicationPublicID) || decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid editor application review")
		return
	}
	request.Status = strings.TrimSpace(request.Status)
	request.Note = strings.TrimSpace(request.Note)
	if request.Status != "approved" && request.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "invalid review result")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review editor application")
		return
	}
	defer tx.Rollback(r.Context())
	var applicationID, targetRouteID, userID, targetInternalID int64
	var targetType, targetID, targetName string
	err = tx.QueryRow(r.Context(), `select application.id,application.target_route_id,application.user_id,
		route.entity_type,route.public_id,route.internal_id
		from project_editor_applications application join public_routes route on route.id=application.target_route_id
		where application.public_id=$1 and application.status='pending' for update of application`, applicationPublicID).
		Scan(&applicationID, &targetRouteID, &userID, &targetType, &targetID, &targetInternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "application does not exist or was already reviewed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load editor application")
		return
	}
	targetName, err = projectEditorTargetNameQuery(r.Context(), tx, targetType, targetInternalID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load reviewed project name")
		return
	}
	claims := currentClaims(r)
	if _, err = tx.Exec(r.Context(), `update project_editor_applications set status=$2,reviewed_by=$3,
		review_note=$4,reviewed_at=now(),updated_at=now() where id=$1`, applicationID, request.Status, claims.Subject, request.Note); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save editor application review")
		return
	}
	if request.Status == "approved" {
		if _, err = tx.Exec(r.Context(), `insert into project_editor_assignments(target_route_id,user_id,application_id,granted_by)
			values($1,$2,$3,$4) on conflict(target_route_id,user_id) do update set status='active',application_id=excluded.application_id,
			granted_by=excluded.granted_by,revoked_by=null,revoked_at=null,revoke_reason='',updated_at=now()`,
			targetRouteID, userID, applicationID, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to grant project editor assignment")
			return
		}
		if err = s.auditPermissionChangeTx(r.Context(), tx, claims.Subject, &userID, "project_editor.grant", map[string]any{"applicationId": applicationPublicID, "targetType": targetType, "targetId": targetID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record editor permission grant")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit editor application review")
		return
	}
	if request.Status == "approved" {
		_ = s.refreshPermissionVersion(r.Context(), userID)
	}
	statusText := "was rejected"
	if request.Status == "approved" {
		statusText = "was approved"
	}
	s.enqueueOrCreateDirectNotification(r.Context(), userID, claims.Subject, "review", "Project editor application", fmt.Sprintf("Your editor application for %s %s. %s", targetName, statusText, request.Note), map[string]any{
		"targetType": targetType, "targetId": targetID, "applicationId": applicationPublicID,
	}, "en-US")
	writeJSON(w, http.StatusOK, map[string]any{"status": request.Status})
}

func (s *Server) revokeProjectEditorAssignment(w http.ResponseWriter, r *http.Request) {
	target, err := s.projectEditorTargetByPublicID(r.Context(), r.PathValue("projectType"), r.PathValue("projectId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	userPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("userId")))
	var request struct {
		Reason string `json:"reason"`
	}
	if !validCatalogPublicID(userPublicID) || decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid editor revocation")
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke editor assignment")
		return
	}
	defer tx.Rollback(r.Context())
	var userID int64
	if err = tx.QueryRow(r.Context(), `select id from users where public_id=$1`, userPublicID).Scan(&userID); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	claims := currentClaims(r)
	result, err := tx.Exec(r.Context(), `update project_editor_assignments set status='revoked',revoked_by=$3,
		revoked_at=now(),revoke_reason=$4,updated_at=now() where target_route_id=$1 and user_id=$2 and status='active'`,
		target.RouteID, userID, claims.Subject, request.Reason)
	if err != nil || result.RowsAffected() != 1 {
		writeError(w, http.StatusNotFound, "active editor assignment not found")
		return
	}
	if err = s.auditPermissionChangeTx(r.Context(), tx, claims.Subject, &userID, "project_editor.revoke", map[string]any{"targetType": target.Type, "targetId": target.PublicID, "reason": request.Reason}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record editor permission revocation")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit editor revocation")
		return
	}
	_ = s.refreshPermissionVersion(r.Context(), userID)
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
}

func (s *Server) presignProjectEditorApplicationAttachment(w http.ResponseWriter, r *http.Request) {
	applicationID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	fileID := strings.ToLower(strings.TrimSpace(r.PathValue("fileId")))
	file, err := lookupReviewAttachment(r.Context(), s.db, reviewAttachmentLookup{
		Kind: reviewAttachmentForProjectEditorApplication, SubjectPublicID: applicationID,
	}, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "editor application attachment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load editor application attachment")
		return
	}
	s.presignOSSFileWithRequest(w, r, ossPresignRequest{ObjectKey: file.ObjectKey})
}

func (s *Server) queryProjectEditorApplications(ctx context.Context, applicationID, targetRouteID, userID int64, status string, limit, offset int) ([]projectEditorApplicationResponse, int, error) {
	var total int
	if err := s.db.QueryRow(ctx, `select count(*) from project_editor_applications application
		where ($1::bigint=0 or application.id=$1) and ($2::bigint=0 or application.target_route_id=$2)
		and ($3::bigint=0 or application.user_id=$3) and ($4='' or application.status=$4)`, applicationID, targetRouteID, userID, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(ctx, `select application.public_id,application.id,route.entity_type,route.public_id,
		route.canonical_path,account.public_id,account.username,application.proof_markdown,application.status,
		application.review_note,application.created_at,application.reviewed_at,
		coalesce(mod.primary_name,modpack.primary_name,project.primary_name,server.name,blueprint.title,skin.display_name,post.title,'')
		from project_editor_applications application
		join public_routes route on route.id=application.target_route_id
		join users account on account.id=application.user_id
		left join mods mod on route.entity_type='mod' and mod.id=route.internal_id and mod.review_status='approved'
		left join modpacks modpack on route.entity_type='modpack' and modpack.id=route.internal_id and modpack.review_status='approved'
		left join simple_projects project on route.entity_type in ('plugin','map','resource_pack','shader_pack','datapack','addon') and project.id=route.internal_id and project.review_status='approved'
		left join minecraft_servers server on route.entity_type='minecraft_server' and server.id=route.internal_id and server.review_status='approved'
		left join blueprints blueprint on route.entity_type='blueprint' and blueprint.id=route.internal_id and blueprint.status='ready' and blueprint.review_status in ('approved','not_required')
		left join skin_assets skin on route.entity_type='skin' and skin.id=route.internal_id and skin.status='active' and skin.review_status='approved'
		left join community_posts post on route.entity_type='community_post' and post.id=route.internal_id and post.review_status='approved'
		where ($1::bigint=0 or application.id=$1) and ($2::bigint=0 or application.target_route_id=$2)
		  and ($3::bigint=0 or application.user_id=$3) and ($4='' or application.status=$4)
		order by application.created_at desc,application.id desc limit $5 offset $6`, applicationID, targetRouteID, userID, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]projectEditorApplicationResponse, 0)
	applicationIDs := make([]int64, 0)
	for rows.Next() {
		var item projectEditorApplicationResponse
		if err = rows.Scan(&item.ID, &item.InternalID, &item.TargetType, &item.TargetID,
			&item.TargetURL, &item.UserID, &item.Username, &item.ProofMarkdown, &item.Status, &item.ReviewNote,
			&item.CreatedAt, &item.ReviewedAt, &item.TargetName); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
		applicationIDs = append(applicationIDs, item.InternalID)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	attachments, err := s.projectEditorApplicationAttachments(ctx, applicationIDs)
	if err != nil {
		return nil, 0, err
	}
	for index := range items {
		items[index].Attachments = attachments[items[index].InternalID]
		if items[index].Attachments == nil {
			items[index].Attachments = []projectEditorApplicationAttachment{}
		}
	}
	return items, total, nil
}

func (s *Server) projectEditorApplicationAttachments(ctx context.Context, applicationIDs []int64) (map[int64][]projectEditorApplicationAttachment, error) {
	result := make(map[int64][]projectEditorApplicationAttachment, len(applicationIDs))
	if len(applicationIDs) == 0 {
		return result, nil
	}
	rows, err := s.db.Query(ctx, `select attachment.application_id,file.public_id,file.original_name,file.object_key,file.size_bytes
		from project_editor_application_attachments attachment join oss_files file on file.id=attachment.oss_file_id
		where attachment.application_id=any($1::bigint[]) and `+safeReviewAttachmentPredicate+`
		order by attachment.application_id,file.id`, applicationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var applicationID int64
		var item projectEditorApplicationAttachment
		if err = rows.Scan(&applicationID, &item.ID, &item.OriginalName, &item.ObjectKey, &item.SizeBytes); err != nil {
			return nil, err
		}
		result[applicationID] = append(result[applicationID], item)
	}
	return result, rows.Err()
}

func (s *Server) enqueueOrCreateDirectNotification(ctx context.Context, recipientID, actorID int64, kind, title, body string, data map[string]any, sourceLocales ...string) {
	sourceLocale := "zh-CN"
	if len(sourceLocales) > 0 {
		sourceLocale = sourceLocales[0]
	}
	event := notificationEvent{Action: "direct", RecipientID: recipientID, ActorID: actorID, Kind: kind, Title: title, Body: body, SourceLocale: sourceLocale, Data: data}
	if (s.queue != nil || s.cfg.NATS.OutboxEnabled) && s.enqueueNotificationTask(ctx, event) == nil {
		return
	}
	raw, _ := json.Marshal(data)
	var notificationID int64
	if s.db.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale,data)
		values($1,$2,$3,$4,$5,$6::jsonb) returning id`, recipientID, kind, title, body, sourceLocale, string(raw)).Scan(&notificationID) == nil && actorID > 0 {
		_, _ = s.db.Exec(ctx, `insert into notification_actors(notification_id,actor_id) values($1,$2) on conflict do nothing`, notificationID, actorID)
	}
}
