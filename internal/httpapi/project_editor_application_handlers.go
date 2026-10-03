package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
	return projectEditorTargetNameWithQueryer(ctx, s.db, projectType, internalID)
}

func projectEditorTargetNameWithQueryer(ctx context.Context, queryer revisionQuery, projectType string, internalID int64) (string, error) {
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
	err := queryer.QueryRow(ctx, query, internalID).Scan(&name)
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
	request, err := parseProjectEditorApplicationPageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, hasMore, err := s.queryProjectEditorApplicationPage(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load editor applications")
		return
	}
	nextCursor := ""
	if hasMore {
		last := items[len(items)-1]
		nextCursor = encodeProjectEditorApplicationPageCursor(projectEditorApplicationPageCursor{
			Version: projectEditorApplicationCursorVersion,
			Scope:   request.Scope, CreatedAt: last.CreatedAt, ID: last.InternalID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore, "nextCursor": nextCursor})
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
	var applicationID, targetRouteID, targetInternalID, userID int64
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
	targetName, err = projectEditorTargetNameWithQueryer(r.Context(), tx, targetType, targetInternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "application target is no longer available")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to load application target")
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
		payload, _ := json.Marshal(map[string]any{"applicationId": applicationPublicID, "targetType": targetType, "targetId": targetID})
		if _, err = tx.Exec(r.Context(), `insert into permission_audit_logs(operator_id,target_user_id,action,payload)
			values($1,$2,'project_editor.grant',$3::jsonb)`, claims.Subject, userID, payload); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record editor assignment audit")
			return
		}
	}
	templateCode := "project_editor_application_rejected"
	if request.Status == "approved" {
		templateCode = "project_editor_application_approved"
	}
	if err = enqueueTemplatedNotificationTx(r.Context(), tx, "project_editor.reviewed", userID, claims.Subject, templateCode,
		map[string]string{"name": targetName, "note": request.Note},
		map[string]any{"targetType": targetType, "targetId": targetID, "applicationId": applicationPublicID},
		r.Header.Get("X-Request-ID")); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue editor application notification")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit editor application review")
		return
	}
	if request.Status == "approved" {
		if !s.requireSecurityVersionRefresh(w, r, "approve_project_editor", userID,
			s.refreshPermissionVersion(r.Context(), userID)) {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": request.Status})
}

func (s *Server) revokeProjectEditorAssignment(w http.ResponseWriter, r *http.Request) {
	target, err := s.projectEditorTargetByPublicID(r.Context(), r.PathValue("projectType"), r.PathValue("projectId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to load project")
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
	err = tx.QueryRow(r.Context(), `select id from users where public_id=$1`, userPublicID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to load user")
		return
	}
	claims := currentClaims(r)
	result, err := tx.Exec(r.Context(), `update project_editor_assignments set status='revoked',revoked_by=$3,
		revoked_at=now(),revoke_reason=$4,updated_at=now() where target_route_id=$1 and user_id=$2 and status='active'`,
		target.RouteID, userID, claims.Subject, request.Reason)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke editor assignment")
		return
	}
	if result.RowsAffected() != 1 {
		writeError(w, http.StatusNotFound, "active editor assignment not found")
		return
	}
	payload, _ := json.Marshal(map[string]any{"targetType": target.Type, "targetId": target.PublicID, "reason": request.Reason})
	if _, err = tx.Exec(r.Context(), `insert into permission_audit_logs(operator_id,target_user_id,action,payload)
		values($1,$2,'project_editor.revoke',$3::jsonb)`, claims.Subject, userID, payload); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record editor revocation audit")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit editor revocation")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "revoke_project_editor", userID,
		s.refreshPermissionVersion(r.Context(), userID)) {
		return
	}
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
	items, _, err := s.queryProjectEditorApplicationsBounded(ctx, applicationID, targetRouteID, userID, status, nil, 0, 0)
	return items, err
}

func (s *Server) queryProjectEditorApplicationPage(ctx context.Context, request projectEditorApplicationPageRequest) ([]projectEditorApplicationResponse, bool, error) {
	return s.queryProjectEditorApplicationsBounded(ctx, 0, 0, 0, request.Status, request.Cursor, request.Limit+1, request.Limit)
}

func (s *Server) queryProjectEditorApplicationsBounded(
	ctx context.Context,
	applicationID, targetRouteID, userID int64,
	status string,
	cursor *projectEditorApplicationPageCursor,
	queryLimit, resultLimit int,
) ([]projectEditorApplicationResponse, bool, error) {
	var cursorTime *time.Time
	var cursorID int64
	if cursor != nil {
		cursorTime = &cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := s.db.Query(ctx, `select application.public_id,application.id,route.entity_type,route.public_id,
		coalesce(target_mod.primary_name,target_modpack.primary_name,target_simple.primary_name,target_server.name,
			target_blueprint.title,target_skin.display_name,target_post.title,''),
		route.canonical_path,account.public_id,account.username,application.proof_markdown,application.status,
		application.review_note,application.created_at,application.reviewed_at
		from project_editor_applications application
		join public_routes route on route.id=application.target_route_id
		join users account on account.id=application.user_id
		left join mods target_mod on route.entity_type='mod' and target_mod.id=route.internal_id and target_mod.review_status='approved'
		left join modpacks target_modpack on route.entity_type='modpack' and target_modpack.id=route.internal_id and target_modpack.review_status='approved'
		left join simple_projects target_simple on route.entity_type in ('plugin','map','resource_pack','shader_pack','datapack','addon')
			and target_simple.id=route.internal_id and target_simple.project_type=route.entity_type and target_simple.review_status='approved'
		left join minecraft_servers target_server on route.entity_type='minecraft_server' and target_server.id=route.internal_id and target_server.review_status='approved'
		left join blueprints target_blueprint on route.entity_type='blueprint' and target_blueprint.id=route.internal_id
			and target_blueprint.status='ready' and target_blueprint.review_status in ('approved','not_required')
		left join skin_assets target_skin on route.entity_type='skin' and target_skin.id=route.internal_id
			and target_skin.status='active' and target_skin.review_status='approved'
		left join community_posts target_post on route.entity_type='community_post' and target_post.id=route.internal_id and target_post.review_status='approved'
		where ($1::bigint=0 or application.id=$1) and ($2::bigint=0 or application.target_route_id=$2)
		  and ($3::bigint=0 or application.user_id=$3) and ($4='' or application.status=$4)
		  and ($5::timestamptz is null or (application.created_at,application.id)<($5,$6))
		order by application.created_at desc,application.id desc limit nullif($7::integer,0)`,
		applicationID, targetRouteID, userID, status, cursorTime, cursorID, queryLimit)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]projectEditorApplicationResponse, 0)
	applicationIDs := make([]int64, 0)
	for rows.Next() {
		var item projectEditorApplicationResponse
		if err = rows.Scan(&item.ID, &item.InternalID, &item.TargetType, &item.TargetID, &item.TargetName,
			&item.TargetURL, &item.UserID, &item.Username, &item.ProofMarkdown, &item.Status, &item.ReviewNote,
			&item.CreatedAt, &item.ReviewedAt); err != nil {
			return nil, false, err
		}
		item.Attachments = []projectEditorApplicationAttachment{}
		items = append(items, item)
		applicationIDs = append(applicationIDs, item.InternalID)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	rows.Close()
	hasMore := resultLimit > 0 && len(items) > resultLimit
	if hasMore {
		items = items[:resultLimit]
		applicationIDs = applicationIDs[:resultLimit]
	}
	attachments, err := s.projectEditorApplicationAttachments(ctx, applicationIDs)
	if err != nil {
		return nil, false, err
	}
	for index := range items {
		items[index].Attachments = attachments[items[index].InternalID]
		if items[index].Attachments == nil {
			items[index].Attachments = []projectEditorApplicationAttachment{}
		}
	}
	return items, hasMore, nil
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
