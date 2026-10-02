package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type markdownPlaygroundDraftRequest struct {
	Content        string `json:"content"`
	BaseRevision   int64  `json:"baseRevision"`
	SaveSessionID  string `json:"saveSessionId"`
	ClientSequence int64  `json:"clientSequence"`
}

type markdownPlaygroundDraftRecord struct {
	Content        string    `json:"content"`
	Revision       int64     `json:"revision"`
	UpdatedAt      time.Time `json:"updatedAt"`
	ClientSequence int64     `json:"clientSequence,omitempty"`
}

type markdownDraftQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type userOSSFileQuotaQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type userOSSFileQuotaUsage struct {
	DailySourceUsed int64
	DailyStoredUsed int64
	TotalSourceUsed int64
	TotalStoredUsed int64
}

var errMarkdownDraftConflict = errors.New("Markdown draft revision conflict")

type userFileRequest struct {
	ObjectKey string `json:"objectKey"`
}

func (s *Server) getMarkdownPlaygroundDraft(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	record, err := loadMarkdownPlaygroundDraftRecord(r.Context(), s.db, claims.Subject)
	w.Header().Set("Cache-Control", "private, no-store")
	writeMarkdownPlaygroundDraftReadResult(w, record, err)
}

func writeMarkdownPlaygroundDraftReadResult(w http.ResponseWriter, record markdownPlaygroundDraftRecord, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"content": "", "revision": 0, "updatedAt": nil})
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "MARKDOWN_DRAFT_READ_FAILED", "failed to read Markdown playground draft", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) saveMarkdownPlaygroundDraft(w http.ResponseWriter, r *http.Request) {
	var req markdownPlaygroundDraftRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if err := validateMarkdownPlaygroundDraftRequest(req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "MARKDOWN_DRAFT_INVALID", err.Error(), 0, nil)
		return
	}
	claims := currentClaims(r)
	record, err := saveMarkdownPlaygroundDraftRecord(r.Context(), s.db, claims.Subject, req)
	if errors.Is(err, errMarkdownDraftConflict) {
		current, currentErr := loadMarkdownPlaygroundDraftRecord(r.Context(), s.db, claims.Subject)
		if currentErr != nil && !errors.Is(currentErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "读取 Markdown 试验场冲突内容失败")
			return
		}
		current.ClientSequence = req.ClientSequence
		writeAPIError(w, http.StatusConflict, "MARKDOWN_DRAFT_CONFLICT", "Markdown draft changed in another editor", 0, current)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Markdown 试验场内容失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "revision": record.Revision, "updatedAt": record.UpdatedAt, "clientSequence": req.ClientSequence})
}

func validateMarkdownPlaygroundDraftRequest(req markdownPlaygroundDraftRequest) error {
	const maxMarkdownDraftBytes = 2 << 20
	if len([]byte(req.Content)) > maxMarkdownDraftBytes {
		return errors.New("Markdown content is too large")
	}
	if req.BaseRevision < 0 {
		return errors.New("baseRevision must be non-negative")
	}
	if req.ClientSequence <= 0 {
		return errors.New("clientSequence must be positive")
	}
	if len(req.SaveSessionID) < 8 || len(req.SaveSessionID) > 128 {
		return errors.New("saveSessionId length is invalid")
	}
	for _, character := range req.SaveSessionID {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return errors.New("saveSessionId contains invalid characters")
	}
	return nil
}

func saveMarkdownPlaygroundDraftRecord(ctx context.Context, db markdownDraftQuerier, userID int64, req markdownPlaygroundDraftRequest) (markdownPlaygroundDraftRecord, error) {
	var record markdownPlaygroundDraftRecord
	err := db.QueryRow(ctx, `insert into markdown_playground_drafts as draft
		(user_id,content,revision,save_session_id,client_sequence,updated_at)
		select $1,$2,1,$4,$5,now()
		where $3=0 or exists(select 1 from markdown_playground_drafts current where current.user_id=$1)
		on conflict (user_id) do update set
		content=excluded.content,
		revision=draft.revision+1,
		save_session_id=excluded.save_session_id,
		client_sequence=excluded.client_sequence,
		updated_at=now()
		where (draft.save_session_id=excluded.save_session_id and excluded.client_sequence>draft.client_sequence)
		   or (draft.save_session_id<>excluded.save_session_id and draft.revision=$3)
		returning content,revision,updated_at`, userID, req.Content, req.BaseRevision, req.SaveSessionID, req.ClientSequence).
		Scan(&record.Content, &record.Revision, &record.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return markdownPlaygroundDraftRecord{}, errMarkdownDraftConflict
	}
	return record, err
}

func loadMarkdownPlaygroundDraftRecord(ctx context.Context, db markdownDraftQuerier, userID int64) (markdownPlaygroundDraftRecord, error) {
	var record markdownPlaygroundDraftRecord
	err := db.QueryRow(ctx, `select content,revision,updated_at from markdown_playground_drafts where user_id=$1`, userID).
		Scan(&record.Content, &record.Revision, &record.UpdatedAt)
	return record, err
}

func (s *Server) userOSSFiles(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	cfg := s.ossConfigFromSettings(r.Context())
	userPrefix := ossUserPrefix(cfg.Prefix, claims.Subject) + "/%"
	limit := boundedLimit(r.URL.Query().Get("limit"), 100, 500)
	rows, err := s.db.Query(
		r.Context(),
		`select file.public_id,file.bucket,file.endpoint,file.region,file.object_key,file.category,file.source,
		        file.original_name,file.source_original_name,file.content_type,file.size_bytes,file.source_size_bytes,
		        file.sha256,file.status,file.scan_status,file.created_at,file.updated_at,
		        exists(select 1 from creator_claim_attachments attachment
		          join creator_claims claim on claim.id=attachment.claim_id
		          where attachment.oss_file_id=file.id and claim.status='pending'),
		        exists(select 1 from minecraft_server_proof_files proof
		          join minecraft_servers server on server.id=proof.server_id
		          where proof.oss_file_id=file.id and server.review_status='pending'),
		        exists(select 1 from project_editor_application_attachments attachment
		          join project_editor_applications application on application.id=attachment.application_id
		          where attachment.oss_file_id=file.id and application.status='pending')
		 from oss_files file
		 where file.uploader_id = $1 and file.object_key like $2 and file.status = 'active'
		 order by created_at desc
		 limit $3`,
		claims.Subject,
		userPrefix,
		limit,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户文件失败")
		return
	}
	defer rows.Close()

	files := make([]map[string]any, 0)
	for rows.Next() {
		var size, sourceSize int64
		var id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, sha, status, scanStatus string
		var creatorClaimLocked, serverReviewLocked, editorApplicationLocked bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt, &creatorClaimLocked, &serverReviewLocked, &editorApplicationLocked); err != nil {
			writeError(w, http.StatusInternalServerError, "解析用户文件失败")
			return
		}
		record := ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt)
		record["locked"] = creatorClaimLocked || serverReviewLocked || editorApplicationLocked
		if creatorClaimLocked {
			record["lockReason"] = "creator_claim_review"
		} else if serverReviewLocked {
			record["lockReason"] = "server_review"
		} else if editorApplicationLocked {
			record["lockReason"] = "project_editor_application_review"
		}
		access, accessErr := s.resolveOSSObjectAccessWithConfig(r.Context(), cfg, objectKey, ossObjectAccessOptions{})
		if accessErr != nil {
			writeError(w, http.StatusBadGateway, "生成用户文件访问链接失败")
			return
		}
		record["url"] = access.URL
		files = append(files, record)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户文件失败")
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) userOSSFileQuota(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	cfg := s.ossConfigFromSettings(r.Context())
	usage, err := loadUserOSSFileQuotaUsage(r.Context(), s.db, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户文件额度失败")
		return
	}

	singleLimit := permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.single_limit"))
	dailyLimit := permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.daily_limit"))
	totalLimit := permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.total_limit"))
	writeJSON(w, http.StatusOK, map[string]any{
		"daily": map[string]any{
			"usedBytes":       usage.DailySourceUsed,
			"sourceUsedBytes": usage.DailySourceUsed,
			"storedUsedBytes": usage.DailyStoredUsed,
			"limitBytes":      quotaLimitValue(dailyLimit),
			"unlimited":       dailyLimit == maxPermissionBytes,
		},
		"total": map[string]any{
			"usedBytes":       usage.TotalStoredUsed,
			"sourceUsedBytes": usage.TotalSourceUsed,
			"storedUsedBytes": usage.TotalStoredUsed,
			"limitBytes":      quotaLimitValue(totalLimit),
			"unlimited":       totalLimit == maxPermissionBytes,
		},
		"single": map[string]any{
			"limitBytes": quotaLimitValue(singleLimit),
			"unlimited":  singleLimit == maxPermissionBytes,
		},
		"allowedExtensions": cfg.AllowedExtensions,
	})
}

func loadUserOSSFileQuotaUsage(ctx context.Context, query userOSSFileQuotaQuerier, userID int64) (userOSSFileQuotaUsage, error) {
	var usage userOSSFileQuotaUsage
	if err := query.QueryRow(
		ctx,
		`select coalesce((select active_source_bytes from oss_user_daily_quota_usage
			where user_id=$1 and usage_date=current_date),0),
			coalesce((select active_stored_bytes from oss_user_daily_quota_usage
			where user_id=$1 and usage_date=current_date),0)`,
		userID,
	).Scan(&usage.DailySourceUsed, &usage.DailyStoredUsed); err != nil {
		return userOSSFileQuotaUsage{}, err
	}
	if err := query.QueryRow(
		ctx,
		`select coalesce((select active_source_bytes from oss_user_quota_usage where user_id=$1),0),
			coalesce((select active_stored_bytes from oss_user_quota_usage where user_id=$1),0)`,
		userID,
	).Scan(&usage.TotalSourceUsed, &usage.TotalStoredUsed); err != nil {
		return userOSSFileQuotaUsage{}, err
	}
	return usage, nil
}

func (s *Server) presignUserOSSFile(w http.ResponseWriter, r *http.Request) {
	var req ossPresignRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	claims := currentClaims(r)
	cfg := s.ossConfigFromSettings(r.Context())
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	if req.ObjectKey == "" || !isAllowedObjectKey(req.ObjectKey, ossUserPrefix(cfg.Prefix, claims.Subject)) {
		writeError(w, http.StatusBadRequest, "OSS ObjectKey 不属于用户文件目录")
		return
	}
	var ownerID int64
	err := s.db.QueryRow(r.Context(), `select uploader_id from oss_files where object_key = $1 and status = 'active'`, req.ObjectKey).Scan(&ownerID)
	if err != nil || ownerID != claims.Subject {
		writeError(w, http.StatusForbidden, "只能访问自己的用户文件")
		return
	}
	s.presignOSSFileWithRequest(w, r, req)
}

func (s *Server) deleteUserOSSFile(w http.ResponseWriter, r *http.Request) {
	var req userFileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	claims := currentClaims(r)
	cfg := s.ossConfigFromSettings(r.Context())
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	userPrefix := ossUserPrefix(cfg.Prefix, claims.Subject)
	if req.ObjectKey == "" || !isAllowedObjectKey(req.ObjectKey, userPrefix) {
		writeError(w, http.StatusBadRequest, "OSS ObjectKey 不属于用户文件目录")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "锁定文件记录失败")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var fileID, ownerID int64
	var originalName string
	var locked bool
	err = tx.QueryRow(r.Context(), `select file.id,file.uploader_id,file.original_name,
		exists(select 1 from creator_claim_attachments attachment
		  join creator_claims claim on claim.id=attachment.claim_id
		  where attachment.oss_file_id=file.id and claim.status='pending')
		or exists(select 1 from minecraft_server_proof_files proof
		  join minecraft_servers server on server.id=proof.server_id
		  where proof.oss_file_id=file.id and server.review_status='pending')
		or exists(select 1 from project_editor_application_attachments attachment
		  join project_editor_applications application on application.id=attachment.application_id
		  where attachment.oss_file_id=file.id and application.status='pending')
		from oss_files file where file.object_key=$1 and file.status='active' for update`, req.ObjectKey).Scan(&fileID, &ownerID, &originalName, &locked)
	if err != nil || ownerID != claims.Subject {
		writeError(w, http.StatusForbidden, "只能删除自己的用户文件")
		return
	}
	if !locked {
		locked, err = catalogImportSourceHasActiveJob(r.Context(), tx, fileID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取文件引用失败")
			return
		}
	}
	if locked {
		writeError(w, http.StatusConflict, "文件正在用于待审核证明，审核结束后才能删除")
		return
	}
	if err = s.tombstoneOSSFileTx(r.Context(), tx, fileID, "user-delete"); err != nil {
		writeError(w, http.StatusInternalServerError, "创建 OSS 文件删除任务失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交文件删除记录失败")
		return
	}
	s.insertOSSUploadLog(r.Context(), nil, claims.Subject, req.ObjectKey, originalName, 0, s.requestClientLocation(r).IP, r.UserAgent(), "deleted", "user-delete")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func normalizeOSSUserFileScope(category string, source string) string {
	category = normalizeObjectSegment(strings.SplitN(strings.TrimSpace(category), ":", 2)[0])
	source = normalizeObjectSegment(strings.SplitN(strings.TrimSpace(source), ":", 2)[0])
	for _, value := range []string{category, source} {
		switch value {
		case "comment", "comments":
			return "comments"
		case "message", "messages", "private-message", "private-messages":
			return "messages"
		case "playground", "markdown":
			return "playground"
		case "avatar", "avatars":
			return "avatars"
		case "profile-background", "profile":
			return "profile"
		case "application", "applications":
			return "applications"
		case "server", "servers", "server-proof", "server-content":
			return "servers"
		case "minecraft-skin", "minecraft-cape", "skin", "cape":
			return "skins"
		case "blueprint-library", "blueprint-cover":
			return "blueprints"
		case "mod-gallery":
			return "mod-gallery"
		case "recipe-gui":
			return "recipe-gui"
		case "iconexport", "iconexporter", "iconrenderer", "letmeseesee":
			return "imports"
		case "creator-avatar", "author-avatar", "creator-claim":
			return "authors"
		}
	}
	return defaultString(category, defaultString(source, "misc"))
}

func quotaLimitValue(value int64) int64 {
	if value == maxPermissionBytes {
		return 0
	}
	return value
}
