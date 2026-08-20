package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	err := s.db.QueryRow(ctx, query, internalID).Scan(&name)
	return name, err
}

func (s *Server) projectEditorApplications(w http.ResponseWriter, r *http.Request) {
	target, err := s.projectEditorTargetByPublicID(r.Context(), r.PathValue("projectType"), r.PathValue("projectId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	claims := currentClaims(r)
	if r.Method == http.MethodGet {
		items, listErr := s.queryProjectEditorApplications(r.Context(), 0, target.RouteID, claims.Subject, "")
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load editor applications")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
	if request.ProofMarkdown == "" || len(request.ProofMarkdown) > 10000 || len(request.AttachmentIDs) > 10 {
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
	items, err := s.queryProjectEditorApplications(r.Context(), applicationID, 0, 0, "")
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
	items, err := s.queryProjectEditorApplications(r.Context(), 0, 0, 0, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load editor applications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
	var applicationID, targetRouteID, userID int64
	var targetType, targetID, targetName string
	err = tx.QueryRow(r.Context(), `select application.id,application.target_route_id,application.user_id,
		route.entity_type,route.public_id
		from project_editor_applications application join public_routes route on route.id=application.target_route_id
		where application.public_id=$1 and application.status='pending' for update of application`, applicationPublicID).
		Scan(&applicationID, &targetRouteID, &userID, &targetType, &targetID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "application does not exist or was already reviewed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load editor application")
		return
	}
	targetName, _ = s.projectEditorTargetName(r.Context(), targetType, func() int64 {
		var internalID int64
		_ = tx.QueryRow(r.Context(), `select internal_id from public_routes where id=$1`, targetRouteID).Scan(&internalID)
		return internalID
	}())
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
		payload, _ := json.Marshal(map[string]any{"applicationId": applicationPublicID, "targetType": targetType, "targetId": targetID})
		_, _ = tx.Exec(r.Context(), `insert into permission_audit_logs(operator_id,target_user_id,action,payload)
			values($1,$2,'project_editor.grant',$3::jsonb)`, claims.Subject, userID, payload)
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
	})
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
	payload, _ := json.Marshal(map[string]any{"targetType": target.Type, "targetId": target.PublicID, "reason": request.Reason})
	_, _ = tx.Exec(r.Context(), `insert into permission_audit_logs(operator_id,target_user_id,action,payload)
		values($1,$2,'project_editor.revoke',$3::jsonb)`, claims.Subject, userID, payload)
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

func (s *Server) queryProjectEditorApplications(ctx context.Context, applicationID, targetRouteID, userID int64, status string) ([]projectEditorApplicationResponse, error) {
	rows, err := s.db.Query(ctx, `select application.public_id,application.id,route.entity_type,route.public_id,route.internal_id,
		route.canonical_path,account.public_id,account.username,application.proof_markdown,application.status,
		application.review_note,application.created_at,application.reviewed_at
		from project_editor_applications application
		join public_routes route on route.id=application.target_route_id
		join users account on account.id=application.user_id
		where ($1::bigint=0 or application.id=$1) and ($2::bigint=0 or application.target_route_id=$2)
		  and ($3::bigint=0 or application.user_id=$3) and ($4='' or application.status=$4)
		order by application.created_at desc,application.id desc`, applicationID, targetRouteID, userID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]projectEditorApplicationResponse, 0)
	applicationIDs := make([]int64, 0)
	for rows.Next() {
		var item projectEditorApplicationResponse
		var targetInternalID int64
		if err = rows.Scan(&item.ID, &item.InternalID, &item.TargetType, &item.TargetID, &targetInternalID,
			&item.TargetURL, &item.UserID, &item.Username, &item.ProofMarkdown, &item.Status, &item.ReviewNote,
			&item.CreatedAt, &item.ReviewedAt); err != nil {
			return nil, err
		}
		item.TargetName, _ = s.projectEditorTargetName(ctx, item.TargetType, targetInternalID)
		item.Attachments = []projectEditorApplicationAttachment{}
		items = append(items, item)
		applicationIDs = append(applicationIDs, item.InternalID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	attachments, err := s.projectEditorApplicationAttachments(ctx, applicationIDs)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Attachments = attachments[items[index].InternalID]
		if items[index].Attachments == nil {
			items[index].Attachments = []projectEditorApplicationAttachment{}
		}
	}
	return items, nil
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

func (s *Server) enqueueOrCreateDirectNotification(ctx context.Context, recipientID, actorID int64, kind, title, body string, data map[string]any) {
	event := notificationEvent{Action: "direct", RecipientID: recipientID, ActorID: actorID, Kind: kind, Title: title, Body: body, SourceLocale: "zh-CN", Data: data}
	if (s.queue != nil || s.cfg.NATS.OutboxEnabled) && s.enqueueNotificationTask(ctx, event) == nil {
		return
	}
	raw, _ := json.Marshal(data)
	var notificationID int64
	if s.db.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale,data)
		values($1,$2,$3,$4,'zh-CN',$5::jsonb) returning id`, recipientID, kind, title, body, string(raw)).Scan(&notificationID) == nil && actorID > 0 {
		_, _ = s.db.Exec(ctx, `insert into notification_actors(notification_id,actor_id) values($1,$2) on conflict do nothing`, notificationID, actorID)
	}
}
