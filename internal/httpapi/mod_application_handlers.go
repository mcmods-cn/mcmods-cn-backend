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

type createModApplicationRequest struct {
	Kind          string   `json:"kind"`
	Proof         string   `json:"proof"`
	AttachmentIDs []string `json:"attachmentIds"`
}

type reviewModApplicationRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type modApplicationAttachment struct {
	ID           string `json:"id"`
	OriginalName string `json:"originalName"`
	ObjectKey    string `json:"objectKey"`
	SizeBytes    int64  `json:"sizeBytes"`
}

type modApplicationResponse struct {
	ID          string                     `json:"id"`
	InternalID  int64                      `json:"-"`
	ModID       string                     `json:"modId"`
	ModSiteID   string                     `json:"modSiteId"`
	ModName     string                     `json:"modName"`
	UserID      string                     `json:"userId"`
	Username    string                     `json:"username"`
	Kind        string                     `json:"kind"`
	Proof       string                     `json:"proof"`
	Status      string                     `json:"status"`
	ReviewNote  string                     `json:"reviewNote"`
	Attachments []modApplicationAttachment `json:"attachments"`
	CreatedAt   time.Time                  `json:"createdAt"`
	ReviewedAt  *time.Time                 `json:"reviewedAt,omitempty"`
}

type modApplicationFilter struct {
	ApplicationID int64
	ModID         int64
	UserID        int64
	Kind          string
	Status        string
}

func (s *Server) modApplications(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组失败")
		return
	}
	if r.Method == http.MethodPost && canEditMod(claims, identity) {
		writeError(w, http.StatusConflict, "你已经拥有此模组的编辑权限")
		return
	}
	if r.Method == http.MethodGet {
		items, listErr := s.queryModApplications(r.Context(), modApplicationFilter{ModID: identity.ID, UserID: claims.Subject})
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "读取申请失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	var req createModApplicationRequest
	if err = decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	req.Proof = strings.TrimSpace(req.Proof)
	if req.Kind != "editor" && req.Kind != "developer" {
		writeError(w, http.StatusBadRequest, "申请类型不正确")
		return
	}
	if req.Proof == "" || len(req.Proof) > 10000 || len(req.AttachmentIDs) > 10 {
		writeError(w, http.StatusBadRequest, "请填写有效证明，附件不能超过 10 个")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建申请失败")
		return
	}
	defer tx.Rollback(r.Context())
	var lockedIdentity modIdentityRecord
	err = tx.QueryRow(r.Context(), `select id,project_code,slug,created_by
		from mods where id=$1 for update`, identity.ID).Scan(
		&lockedIdentity.ID, &lockedIdentity.UniqueID, &lockedIdentity.SiteID, &lockedIdentity.OwnerID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "锁定模组失败")
		return
	}
	var alreadyMember bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from mod_memberships
		where mod_id=$1 and user_id=$2)`, lockedIdentity.ID, claims.Subject).Scan(&alreadyMember); err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组成员关系失败")
		return
	}
	if alreadyMember || canEditMod(claims, lockedIdentity) {
		writeError(w, http.StatusConflict, "你已经拥有此模组的编辑权限")
		return
	}
	var id int64
	err = tx.QueryRow(
		r.Context(),
		`insert into mod_membership_applications (mod_id,user_id,kind,proof) values ($1,$2,$3,$4) returning id`,
		identity.ID, claims.Subject, req.Kind, req.Proof,
	).Scan(&id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "已有相同类型的申请正在审核")
		} else {
			writeError(w, http.StatusInternalServerError, "创建申请失败")
		}
		return
	}
	for _, filePublicID := range uniquePublicIDs(req.AttachmentIDs) {
		file, lookupErr := lookupReviewAttachment(r.Context(), tx, reviewAttachmentLookup{
			Kind: reviewAttachmentByUploader, UploaderID: claims.Subject,
		}, filePublicID)
		if lookupErr != nil {
			writeError(w, http.StatusBadRequest, "申请附件不存在或不属于当前用户")
			return
		}
		if _, err = tx.Exec(r.Context(), `insert into mod_application_attachments (application_id,oss_file_id) values ($1,$2)`, id, file.InternalID); err != nil {
			writeError(w, http.StatusInternalServerError, "保存申请附件失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交申请失败")
		return
	}
	items, queryErr := s.queryModApplications(r.Context(), modApplicationFilter{ApplicationID: id})
	if queryErr != nil || len(items) == 0 {
		writeError(w, http.StatusInternalServerError, "读取已提交申请失败")
		return
	}
	writeJSON(w, http.StatusCreated, items[0])
}

func (s *Server) adminModApplications(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "editor" && kind != "developer" {
		writeError(w, http.StatusBadRequest, "申请类型不正确")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		writeError(w, http.StatusBadRequest, "申请状态不正确")
		return
	}
	items, err := s.queryModApplications(r.Context(), modApplicationFilter{Kind: kind, Status: status})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取申请列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) reviewModApplication(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "申请编号不正确")
		return
	}
	var req reviewModApplicationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	req.Note = strings.TrimSpace(req.Note)
	if req.Status != "approved" && req.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "审核状态不正确")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建审核事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var id, modID, userID int64
	var kind, modName, projectID, modSiteID string
	err = tx.QueryRow(r.Context(), `select a.id,a.mod_id,a.user_id,a.kind,m.primary_name,m.project_code,m.slug from mod_membership_applications a join mods m on m.id=a.mod_id where a.public_id=$1 and a.status='pending' for update`, publicID).Scan(&id, &modID, &userID, &kind, &modName, &projectID, &modSiteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "申请不存在或已经审核")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取申请失败")
		return
	}
	claims := currentClaims(r)
	if _, err = tx.Exec(r.Context(), `update mod_membership_applications set status=$2,reviewed_by=$3,review_note=$4,reviewed_at=now(),updated_at=now() where id=$1`, id, req.Status, claims.Subject, req.Note); err != nil {
		writeError(w, http.StatusInternalServerError, "保存审核结果失败")
		return
	}
	if req.Status == "approved" {
		roles := []string{"editor"}
		permissionDefaults := s.permissionDefaultsFromSettings(r.Context())
		roleTemplate := permissionDefaults.EditorRole
		if kind == "developer" {
			roles = append(roles, "developer")
			roleTemplate = permissionDefaults.DeveloperRole
		}
		if roleTemplate == "" {
			writeError(w, http.StatusConflict, "请先在权限相关设置中配置对应的变量权限组")
			return
		}
		for _, role := range roles {
			if _, err = tx.Exec(r.Context(), `insert into mod_memberships (mod_id,user_id,role,granted_by) values ($1,$2,$3,$4) on conflict do nothing`, modID, userID, role, claims.Subject); err != nil {
				writeError(w, http.StatusInternalServerError, "保存模组成员失败")
				return
			}
		}
		if err = s.bindProjectRoleTx(r.Context(), tx, userID, roleTemplate, projectID); err != nil {
			writeError(w, http.StatusInternalServerError, "授予模组成员变量权限组失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交审核结果失败")
		return
	}
	statusText := "未通过"
	if req.Status == "approved" {
		statusText = "已通过"
	}
	s.enqueueOrCreateDirectNotification(r.Context(), userID, claims.Subject, "review", "模组成员申请审核结果", fmt.Sprintf("你对 %s 提交的申请%s。%s", modName, statusText, req.Note), map[string]any{
		"modId": projectID, "modSiteId": modSiteID, "applicationId": publicID,
		"targetLabel": modName, "url": "/mods/" + modSiteID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Status})
}

func (s *Server) presignModApplicationAttachment(w http.ResponseWriter, r *http.Request) {
	applicationPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(applicationPublicID) {
		writeError(w, http.StatusBadRequest, "申请编号不正确")
		return
	}
	filePublicID := strings.ToLower(strings.TrimSpace(r.PathValue("fileId")))
	if !validCatalogPublicID(filePublicID) {
		writeError(w, http.StatusBadRequest, "附件编号不正确")
		return
	}
	file, err := lookupReviewAttachment(r.Context(), s.db, reviewAttachmentLookup{
		Kind: reviewAttachmentForModApplication, SubjectPublicID: applicationPublicID,
	}, filePublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "审核附件不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取审核附件失败")
		return
	}
	s.presignOSSFileWithRequest(w, r, ossPresignRequest{ObjectKey: file.ObjectKey})
}

func (s *Server) queryModApplications(ctx context.Context, filter modApplicationFilter) ([]modApplicationResponse, error) {
	rows, err := s.db.Query(ctx, `select a.public_id,a.id,m.project_code,m.slug,m.primary_name,u.public_id,u.username,
		a.kind,a.proof,a.status,a.review_note,a.created_at,a.reviewed_at
		from mod_membership_applications a
		join mods m on m.id=a.mod_id
		join users u on u.id=a.user_id
		where ($1::bigint=0 or a.id=$1)
		  and ($2::bigint=0 or a.mod_id=$2)
		  and ($3::bigint=0 or a.user_id=$3)
		  and ($4='' or a.kind=$4)
		  and ($5='' or a.status=$5)
		order by a.created_at desc`, filter.ApplicationID, filter.ModID, filter.UserID, filter.Kind, filter.Status)
	if err != nil {
		return nil, err
	}
	items := make([]modApplicationResponse, 0)
	applicationIDs := make([]int64, 0)
	for rows.Next() {
		var item modApplicationResponse
		if err = rows.Scan(&item.ID, &item.InternalID, &item.ModID, &item.ModSiteID, &item.ModName, &item.UserID, &item.Username, &item.Kind, &item.Proof, &item.Status, &item.ReviewNote, &item.CreatedAt, &item.ReviewedAt); err != nil {
			rows.Close()
			return nil, err
		}
		item.Attachments = []modApplicationAttachment{}
		items = append(items, item)
		applicationIDs = append(applicationIDs, item.InternalID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	attachments, err := s.modApplicationAttachmentsByApplication(ctx, applicationIDs)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if values := attachments[items[index].InternalID]; values != nil {
			items[index].Attachments = values
		}
	}
	return items, nil
}

func (s *Server) modApplicationAttachmentsByApplication(ctx context.Context, applicationIDs []int64) (map[int64][]modApplicationAttachment, error) {
	result := make(map[int64][]modApplicationAttachment, len(applicationIDs))
	if len(applicationIDs) == 0 {
		return result, nil
	}
	rows, err := s.db.Query(ctx, `select attachment.application_id,file.public_id,file.original_name,file.object_key,file.size_bytes
		from mod_application_attachments attachment
		join oss_files file on file.id=attachment.oss_file_id
		where attachment.application_id=any($1::bigint[]) and `+safeReviewAttachmentPredicate+`
		order by attachment.application_id,file.id`, applicationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var applicationID int64
		var item modApplicationAttachment
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
	if s.db.QueryRow(ctx, `insert into notifications (recipient_id,kind,title,body,source_locale,data) values ($1,$2,$3,$4,'zh-CN',$5::jsonb) returning id`, recipientID, kind, title, body, string(raw)).Scan(&notificationID) == nil && actorID > 0 {
		_, _ = s.db.Exec(ctx, `insert into notification_actors (notification_id,actor_id) values ($1,$2) on conflict do nothing`, notificationID, actorID)
	}
}
