package httpapi

import (
	"net/http"
	"strings"
	"time"
)

type markdownPlaygroundDraftRequest struct {
	Content string `json:"content"`
}

type userFileRequest struct {
	ObjectKey string `json:"objectKey"`
}

func (s *Server) getMarkdownPlaygroundDraft(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	var content string
	var updatedAt time.Time
	err := s.db.QueryRow(
		r.Context(),
		`select content, updated_at from markdown_playground_drafts where user_id = $1`,
		claims.Subject,
	).Scan(&content, &updatedAt)
	if err != nil {
		_ = ignoreNoRows(err)
		writeJSON(w, http.StatusOK, map[string]any{"content": "", "updatedAt": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": content, "updatedAt": updatedAt})
}

func (s *Server) saveMarkdownPlaygroundDraft(w http.ResponseWriter, r *http.Request) {
	var req markdownPlaygroundDraftRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	const maxMarkdownDraftBytes = 2 << 20
	if len([]byte(req.Content)) > maxMarkdownDraftBytes {
		writeError(w, http.StatusBadRequest, "Markdown 内容过大")
		return
	}
	claims := currentClaims(r)
	var updatedAt time.Time
	err := s.db.QueryRow(
		r.Context(),
		`insert into markdown_playground_drafts (user_id, content, updated_at)
		 values ($1, $2, now())
		 on conflict (user_id) do update
		 set content = excluded.content, updated_at = now()
		 returning updated_at`,
		claims.Subject,
		req.Content,
	).Scan(&updatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Markdown 试验场内容失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "updatedAt": updatedAt})
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
		        exists(select 1 from mod_application_attachments attachment
		          join mod_membership_applications application on application.id=attachment.application_id
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
		var creatorClaimLocked, serverReviewLocked, modApplicationLocked bool
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt, &creatorClaimLocked, &serverReviewLocked, &modApplicationLocked); err != nil {
			writeError(w, http.StatusInternalServerError, "解析用户文件失败")
			return
		}
		record := ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt)
		record["locked"] = creatorClaimLocked || serverReviewLocked || modApplicationLocked
		if creatorClaimLocked {
			record["lockReason"] = "creator_claim_review"
		} else if serverReviewLocked {
			record["lockReason"] = "server_review"
		} else if modApplicationLocked {
			record["lockReason"] = "mod_application_review"
		}
		record["url"] = buildPublicOSSURL(cfg, objectKey)
		files = append(files, record)
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) userOSSFileQuota(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	cfg := s.ossConfigFromSettings(r.Context())
	var dailySourceUsed, dailyStoredUsed, totalSourceUsed, totalStoredUsed int64
	_ = s.db.QueryRow(
		r.Context(),
		`select coalesce(sum(coalesce(nullif(source_size_bytes, 0), size_bytes)), 0), coalesce(sum(size_bytes), 0)
		 from oss_files
		 where uploader_id = $1 and status = 'active' and created_at >= current_date`,
		claims.Subject,
	).Scan(&dailySourceUsed, &dailyStoredUsed)
	_ = s.db.QueryRow(
		r.Context(),
		`select coalesce(sum(coalesce(nullif(source_size_bytes, 0), size_bytes)), 0), coalesce(sum(size_bytes), 0)
		 from oss_files
		 where uploader_id = $1 and status = 'active'`,
		claims.Subject,
	).Scan(&totalSourceUsed, &totalStoredUsed)

	singleLimit := permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.single_limit"))
	dailyLimit := permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.daily_limit"))
	totalLimit := permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.total_limit"))
	writeJSON(w, http.StatusOK, map[string]any{
		"daily": map[string]any{
			"usedBytes":       dailySourceUsed,
			"sourceUsedBytes": dailySourceUsed,
			"storedUsedBytes": dailyStoredUsed,
			"limitBytes":      quotaLimitValue(dailyLimit),
			"unlimited":       dailyLimit == maxPermissionBytes,
		},
		"total": map[string]any{
			"usedBytes":       totalStoredUsed,
			"sourceUsedBytes": totalSourceUsed,
			"storedUsedBytes": totalStoredUsed,
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
		or exists(select 1 from mod_application_attachments attachment
		  join mod_membership_applications application on application.id=attachment.application_id
		  where attachment.oss_file_id=file.id and application.status='pending')
		from oss_files file where file.object_key=$1 and file.status='active' for update`, req.ObjectKey).Scan(&fileID, &ownerID, &originalName, &locked)
	if err != nil || ownerID != claims.Subject {
		writeError(w, http.StatusForbidden, "只能删除自己的用户文件")
		return
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
