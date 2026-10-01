package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mcmods-cn-backend/internal/security"
)

const reportReasonVersion = 1

const minimumTemporaryBanDuration = time.Minute

const (
	reportTargetActorSubmitter = "submitter"
	reportTargetActorAuthor    = "author"
	reportTargetActorOwner     = "owner"
	reportTargetActorSubject   = "subject"
)

type reportTargetActor struct {
	ID   *int64
	Role string
}

func identifiedReportTargetActor(id int64, role string) reportTargetActor {
	return reportTargetActor{ID: &id, Role: role}
}

func logGovernanceReadFailure(stage, identity string, err error) {
	slog.Error("governance read failed",
		"module", "governance",
		"stage", stage,
		"identity", identity,
		"error", err,
	)
}

var reportTargetReasons = map[string][]string{
	"mod":           {"copyright_theft", "impersonation", "malware", "misleading_content", "false_version_loader", "broken_download", "false_license", "duplicate_spam", "advertising"},
	"modpack":       {"copyright_theft", "impersonation", "malware", "misleading_content", "false_version_loader", "broken_download", "false_license", "duplicate_spam", "advertising"},
	"plugin":        {"copyright_theft", "impersonation", "malware", "misleading_content", "false_version_loader", "broken_download", "false_license", "duplicate_spam", "advertising"},
	"map":           {"copyright_theft", "misleading_content", "broken_content", "dangerous_commands", "false_minecraft_version", "prohibited_media", "broken_download"},
	"shader":        {"copyright_theft", "impersonation", "misleading_media", "prohibited_media", "malware", "false_minecraft_version", "missing_dependencies"},
	"resource_pack": {"copyright_theft", "impersonation", "misleading_media", "prohibited_media", "malware", "false_minecraft_version", "missing_dependencies"},
	"datapack":      {"copyright_theft", "dangerous_commands", "undisclosed_destructive_behavior", "misleading_content", "false_minecraft_version", "impersonation", "malware"},
	"addon":         {"copyright_theft", "impersonation", "incompatible_parent", "false_dependencies", "misleading_content", "malware"},
	"discussion":    {"spam", "duplicate", "off_topic", "harassment", "misinformation", "privacy", "advertising", "malicious_link"},
	"bug":           {"fabricated_bug", "duplicate", "off_topic", "missing_information_spam", "privacy", "dangerous_exploit", "harassment", "malicious_link"},
	"news":          {"fake_news", "misleading_title", "out_of_context", "missing_source", "copyright_theft", "disguised_advertising", "known_falsehood", "privacy"},
	"tutorial":      {"copyright_theft", "incorrect_steps", "unsafe_without_warning", "obsolete", "dangerous_commands", "disguised_advertising", "misleading_content"},
	"skin":          {"prohibited_media", "extremist_symbol", "copyright_theft", "impersonation", "misleading_media", "malware"},
	"blueprint":     {"copyright_theft", "broken_content", "false_dependencies", "misleading_content", "dangerous_commands", "prohibited_media"},
	"server":        {"commercial_as_public", "server_impersonation", "false_identity", "fake_online_count", "false_advertising", "payment_fraud", "malware", "credential_phishing", "closed_server", "undisclosed_charges", "illegal_operation"},
	"user":          {"avatar_violation", "username_violation", "profile_violation", "impersonation", "advertising_account", "fraud_account", "mass_alt_account", "harassment", "malicious_link"},
	"comment":       {"comment_spam", "spam", "harassment", "misinformation", "off_topic", "privacy", "malicious_link", "impersonation", "prohibited_content"},
}

func supportedReportTarget(target string) bool { _, ok := reportTargetReasons[target]; return ok }

func reportReasons(w http.ResponseWriter, r *http.Request) {
	target := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("targetType")))
	reasons, ok := reportTargetReasons[target]
	if !ok {
		writeError(w, http.StatusBadRequest, "举报目标类型不正确")
		return
	}
	items := make([]map[string]any, 0, len(reasons)+1)
	for _, code := range append(append([]string(nil), reasons...), "other") {
		items = append(items, map[string]any{"code": code, "i18nKey": "reports.reasons." + code, "requiresCustomText": code == "other"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"targetType": target, "version": reportReasonVersion, "items": items})
}

type createUnifiedReportRequest struct {
	TargetType   string   `json:"targetType"`
	TargetID     string   `json:"targetId"`
	ReasonCode   string   `json:"reasonCode"`
	CustomReason string   `json:"customReason"`
	Detail       string   `json:"detail"`
	EvidenceIDs  []string `json:"evidenceIds"`
}

func (s *Server) createUnifiedReport(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "report.create") {
		writeError(w, http.StatusForbidden, "无权提交举报")
		return
	}
	var request createUnifiedReportRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "举报请求格式不正确")
		return
	}
	request.TargetType = strings.ToLower(strings.TrimSpace(request.TargetType))
	request.TargetID = strings.ToLower(strings.TrimSpace(request.TargetID))
	request.ReasonCode = strings.ToLower(strings.TrimSpace(request.ReasonCode))
	request.CustomReason = strings.TrimSpace(request.CustomReason)
	request.Detail = strings.TrimSpace(request.Detail)
	if !supportedReportTarget(request.TargetType) || !validCatalogPublicID(request.TargetID) ||
		!validReportReason(request.TargetType, request.ReasonCode) || len(request.EvidenceIDs) > 5 ||
		(request.ReasonCode == "other" && request.CustomReason == "") || utf8.RuneCountInString(request.CustomReason) > 500 || utf8.RuneCountInString(request.Detail) > 4000 {
		writeError(w, http.StatusBadRequest, "举报内容不正确")
		return
	}
	s.submitUnifiedReport(w, r, request)
}

func (s *Server) createReportEvidenceUpload(w http.ResponseWriter, r *http.Request) {
	if !claimsAllow(currentClaims(r), "report.create") {
		writeError(w, http.StatusForbidden, "无权上传举报附件")
		return
	}
	s.createOSSDirectUploadWithScope(w, r, ossReportEvidenceScope)
}

func (s *Server) completeReportEvidenceUpload(w http.ResponseWriter, r *http.Request) {
	if !claimsAllow(currentClaims(r), "report.create") {
		writeError(w, http.StatusForbidden, "无权上传举报附件")
		return
	}
	s.completeOSSDirectUploadWithScope(w, r, ossReportEvidenceScope)
}

func (s *Server) reportEvidenceAccess(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	claims := currentClaims(r)
	var objectKey, status, scanStatus string
	var uploaderID int64
	var reporterID *int64
	err := s.db.QueryRow(r.Context(), `select evidence.object_key,evidence.status,evidence.scan_status,evidence.uploader_id,report.reporter_id
		from report_evidence evidence left join reports report on report.id=evidence.report_id where evidence.public_id=$1`, publicID).
		Scan(&objectKey, &status, &scanStatus, &uploaderID, &reporterID)
	if errors.Is(err, pgx.ErrNoRows) || status == "deleted" || scanStatus != "clean" {
		writeError(w, http.StatusNotFound, "举报附件不存在或尚不可用")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取举报附件失败")
		return
	}
	isOwner := uploaderID == claims.Subject || reporterID != nil && *reporterID == claims.Subject
	if !isOwner && !claimsAllow(claims, "report.evidence.view") {
		writeError(w, http.StatusForbidden, "无权查看举报附件")
		return
	}
	s.presignOSSFileWithRequest(w, r, ossPresignRequest{ObjectKey: objectKey, ExpiresMinutes: 10})
}

// submitUnifiedReport is the single persistence path for the current report endpoint.
func (s *Server) submitUnifiedReport(w http.ResponseWriter, r *http.Request, request createUnifiedReportRequest) {
	claims := currentClaims(r)
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeError(w, 500, "创建举报事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	snapshot, targetActor, err := s.reportTargetSnapshot(r.Context(), tx, request.TargetType, request.TargetID, claims)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "举报目标不存在或不可见")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取举报目标失败")
		return
	}
	snapshotRaw, _ := json.Marshal(snapshot)
	digest := sha256.Sum256(snapshotRaw)
	var reportID int64
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into reports(target_type,target_public_id,target_actor_id,target_actor_role,reporter_id,reason_code,reason_version,reason_text_snapshot,custom_reason,detail)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) returning id,public_id`, request.TargetType, request.TargetID, targetActor.ID, targetActor.Role, claims.Subject,
		request.ReasonCode, reportReasonVersion, "reports.reasons."+request.ReasonCode, request.CustomReason, request.Detail).Scan(&reportID, &publicID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "uq_reports_open_reporter_target") {
			writeError(w, http.StatusConflict, "你已经提交过该目标的待处理举报")
			return
		}
		writeError(w, 500, "保存举报失败")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into report_snapshots(report_id,schema_version,payload,content_sha256) values($1,1,$2,$3)`, reportID, snapshotRaw, hex.EncodeToString(digest[:])); err != nil {
		writeError(w, 500, "保存举报快照失败")
		return
	}
	if len(request.EvidenceIDs) > 0 {
		tag, bindErr := tx.Exec(r.Context(), `update report_evidence set report_id=$1,status='bound',cleanup_after='infinity'
			where public_id=any($2) and uploader_id=$3 and report_id is null and status='temporary' and scan_status in ('pending','clean')`, reportID, request.EvidenceIDs, claims.Subject)
		if bindErr != nil {
			writeError(w, http.StatusInternalServerError, "绑定举报附件失败")
			return
		}
		if tag.RowsAffected() != int64(len(request.EvidenceIDs)) {
			writeError(w, http.StatusBadRequest, "举报附件无效或尚未通过安全检查")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "提交举报失败")
		return
	}
	s.writeAppLog(context.Background(), "user_interaction", "info", "report.create", publicID, claims.Subject, r, http.StatusCreated, 0, map[string]any{"targetType": request.TargetType})
	writeJSON(w, http.StatusCreated, map[string]any{"id": publicID, "status": "pending"})
}

func validReportReason(target, reason string) bool {
	if reason == "other" {
		return true
	}
	for _, candidate := range reportTargetReasons[target] {
		if candidate == reason {
			return true
		}
	}
	return false
}

type reportSnapshotQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type reportTargetVisibility struct {
	RouteType  string
	InternalID int64
	Path       string
}

func (s *Server) validateReportTargetVisibility(
	ctx context.Context,
	queryer reportSnapshotQueryer,
	targetType, publicID string,
	claims security.Claims,
) (reportTargetVisibility, error) {
	if targetType == "comment" {
		var commentTargetType string
		var commentTargetID int64
		var targetVersionID *int64
		err := queryer.QueryRow(ctx, `select target_type,target_id,target_version_id from comments
			where public_id=$1 and status in ('published','deleted')`, publicID).
			Scan(&commentTargetType, &commentTargetID, &targetVersionID)
		if err != nil {
			return reportTargetVisibility{}, err
		}
		if _, err = resolveCommentTargetByInternalWithQueryer(
			ctx, queryer, commentTargetType, commentTargetID, targetVersionID, claims,
		); err != nil {
			return reportTargetVisibility{}, err
		}
		return reportTargetVisibility{}, nil
	}
	if targetType == "user" {
		var exists bool
		if err := queryer.QueryRow(ctx, `select exists(
			select 1 from users where public_id=$1 and status<>'deleted'
		)`, publicID).Scan(&exists); err != nil {
			return reportTargetVisibility{}, err
		}
		if !exists {
			return reportTargetVisibility{}, pgx.ErrNoRows
		}
		return reportTargetVisibility{}, nil
	}
	target, err := resolveFollowProjectTargetWithQueryer(ctx, queryer, publicID, claims)
	if err != nil {
		return reportTargetVisibility{}, err
	}
	actualTargetType := normalizeReportRouteType(target.Type)
	if target.Type == "community_post" {
		var kind string
		if err = queryer.QueryRow(ctx, `select kind from community_posts where id=$1 and status='active'`, target.InternalID).Scan(&kind); err != nil {
			return reportTargetVisibility{}, err
		}
		actualTargetType = normalizeReportRouteType("community_" + kind)
	}
	if actualTargetType != targetType {
		return reportTargetVisibility{}, pgx.ErrNoRows
	}
	return reportTargetVisibility{RouteType: target.Type, InternalID: target.InternalID, Path: target.URL}, nil
}

func (s *Server) reportTargetSnapshot(
	ctx context.Context,
	queryer reportSnapshotQueryer,
	targetType, publicID string,
	claims security.Claims,
) (map[string]any, reportTargetActor, error) {
	visibility, err := s.validateReportTargetVisibility(ctx, queryer, targetType, publicID, claims)
	if err != nil {
		return nil, reportTargetActor{}, err
	}
	if targetType == "comment" {
		var body, status, authorPublicID, authorName, commentTargetType string
		var commentID, authorID, commentTargetID int64
		var targetVersionID, floorNumber *int64
		var parentPublicID *string
		var created, updated time.Time
		err := queryer.QueryRow(ctx, `select comment.id,comment.body,comment.status,author.id,author.public_id,author.username,
			comment.target_type,comment.target_id,comment.target_version_id,parent.public_id,comment.floor_number,comment.created_at,comment.updated_at
			from comments comment join users author on author.id=comment.author_id
			left join comments parent on parent.id=comment.parent_id
			where comment.public_id=$1 and comment.status in ('published','deleted')`, publicID).
			Scan(&commentID, &body, &status, &authorID, &authorPublicID, &authorName, &commentTargetType, &commentTargetID,
				&targetVersionID, &parentPublicID, &floorNumber, &created, &updated)
		if err != nil {
			return nil, reportTargetActor{}, err
		}
		contextItems := make([]map[string]any, 0, 4)
		rows, queryErr := queryer.Query(ctx, `select nearby.public_id,nearby.body,nearby.status,nearby.floor_number,nearby.created_at,
			author.public_id,author.username from comments nearby join users author on author.id=nearby.author_id
			where nearby.id<>$1 and nearby.target_type=$2 and nearby.target_id=$3 and nearby.target_version_id is not distinct from $4
			and nearby.status in ('published','deleted')
			order by abs(extract(epoch from (nearby.created_at-$5))),nearby.id limit 4`, commentID, commentTargetType, commentTargetID, targetVersionID, created)
		if queryErr != nil {
			return nil, reportTargetActor{}, queryErr
		}
		for rows.Next() {
			var itemID, itemBody, itemStatus, itemAuthorID, itemAuthorName string
			var itemFloor *int64
			var itemCreated time.Time
			if queryErr = rows.Scan(&itemID, &itemBody, &itemStatus, &itemFloor, &itemCreated, &itemAuthorID, &itemAuthorName); queryErr != nil {
				rows.Close()
				return nil, reportTargetActor{}, queryErr
			}
			contextItems = append(contextItems, map[string]any{"id": itemID, "body": itemBody, "status": itemStatus, "floorNumber": itemFloor,
				"createdAt": itemCreated, "authorId": itemAuthorID, "authorName": itemAuthorName})
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			return nil, reportTargetActor{}, queryErr
		}
		rows.Close()
		attachments, queryErr := reportCommentAttachmentSnapshots(ctx, queryer, commentID)
		if queryErr != nil {
			return nil, reportTargetActor{}, queryErr
		}
		return map[string]any{"targetType": "comment", "id": publicID, "body": body, "status": status, "authorId": authorPublicID,
			"authorName": authorName, "commentTargetType": commentTargetType, "commentTargetId": commentTargetID,
			"targetVersionId": targetVersionID, "parentCommentId": parentPublicID, "floorNumber": floorNumber, "createdAt": created,
			"updatedAt": updated, "context": contextItems, "attachments": attachments}, identifiedReportTargetActor(authorID, reportTargetActorAuthor), nil
	}
	if targetType == "user" {
		var id int64
		var username, avatar, signature, status string
		var created, updated time.Time
		err := queryer.QueryRow(ctx, `select id,username,avatar_url,signature,status,created_at,updated_at from users where public_id=$1 and status<>'deleted'`, publicID).
			Scan(&id, &username, &avatar, &signature, &status, &created, &updated)
		return map[string]any{"targetType": "user", "id": publicID, "username": username, "avatarUrl": avatar, "signature": signature, "status": status, "createdAt": created, "updatedAt": updated}, identifiedReportTargetActor(id, reportTargetActorSubject), err
	}
	return s.reportPublicContentSnapshot(
		ctx, queryer, targetType, publicID, visibility.RouteType, visibility.InternalID, visibility.Path,
	)
}

func reportCommentAttachmentSnapshots(ctx context.Context, queryer reportSnapshotQueryer, commentID int64) ([]map[string]any, error) {
	rows, err := queryer.Query(ctx, `select file.public_id,file.original_name,file.content_type,file.size_bytes,file.sha256,
		share.public_code,share.status
		from comment_log_bindings binding join oss_files file on file.id=binding.attachment_file_id
		left join log_shares share on share.id=binding.log_share_id
		where binding.comment_id=$1 order by binding.created_at,binding.attachment_file_id`, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, originalName, contentType, sha256 string
		var sizeBytes int64
		var logShareCode, logShareStatus *string
		if err = rows.Scan(&publicID, &originalName, &contentType, &sizeBytes, &sha256, &logShareCode, &logShareStatus); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"public_id": publicID, "original_name": originalName, "content_type": contentType,
			"size_bytes": sizeBytes, "sha256": sha256,
			"log_share_code": logShareCode, "log_share_status": logShareStatus,
		})
	}
	return items, rows.Err()
}

// reportPublicContentSnapshot captures the mutable public fields reviewers need
// to assess the report later. It deliberately omits private drafts, credentials,
// raw security evidence, and other fields that are not part of the public page.
func (s *Server) reportPublicContentSnapshot(ctx context.Context, queryer reportSnapshotQueryer, targetType, publicID, routeType string, internalID int64, path string) (map[string]any, reportTargetActor, error) {
	var raw []byte
	var actorID *int64
	var actorRole string
	var err error
	switch routeType {
	case "mod":
		actorRole = reportTargetActorSubmitter
		err = queryer.QueryRow(ctx, `select jsonb_build_object(
			'title',mod.primary_name,'secondaryTitle',mod.secondary_name,'summary',mod.summary,'body',mod.body_markdown,
			'category',mod.primary_category,'license',mod.license,'environment',mod.environment,'status',mod.official_status,
			'reviewStatus',mod.review_status,'externalLinks',jsonb_build_object('curseforge',mod.curseforge_project_id,'modrinth',mod.modrinth_project_id,'github',mod.github_project_path),
			'iconUrl',mod.icon_url,'createdAt',mod.created_at,'updatedAt',mod.updated_at,'publishedAt',mod.published_at,
			'submitter',case when submitter.id is null then null else jsonb_build_object('id',submitter.public_id,'name',submitter.username) end),mod.submitted_by
			from mods mod left join users submitter on submitter.id=mod.submitted_by where mod.id=$1`, internalID).Scan(&raw, &actorID)
	case "modpack":
		actorRole = reportTargetActorSubmitter
		err = queryer.QueryRow(ctx, `select jsonb_build_object(
			'title',pack.primary_name,'secondaryTitle',pack.secondary_name,'summary',pack.summary,'body',pack.body_markdown,
			'category',pack.primary_category,'packType',pack.pack_type,'packagingMethod',pack.packaging_method,
			'license',pack.license,'environment',pack.environment,'status',pack.official_status,'sourceStatus',pack.source_status,
			'reviewStatus',pack.review_status,'externalLinks',jsonb_build_object('curseforge',pack.curseforge_project_id,'modrinth',pack.modrinth_project_id),
			'iconUrl',pack.icon_url,'createdAt',pack.created_at,'updatedAt',pack.updated_at,'publishedAt',pack.published_at,
			'submitter',case when submitter.id is null then null else jsonb_build_object('id',submitter.public_id,'name',submitter.username) end),pack.submitted_by
			from modpacks pack left join users submitter on submitter.id=pack.submitted_by where pack.id=$1`, internalID).Scan(&raw, &actorID)
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		actorRole = reportTargetActorSubmitter
		err = queryer.QueryRow(ctx, `select jsonb_build_object(
			'title',project.primary_name,'summary',project.summary,'body',project.body_markdown,'minecraftVersions',project.minecraft_versions,
			'loaders',project.loaders,'categories',project.categories,'features',project.features,'license',project.license,
			'status',project.official_status,'reviewStatus',project.review_status,'iconUrl',project.icon_url,
			'externalLinks',jsonb_build_object('curseforge',project.curseforge_project_id,'modrinth',project.modrinth_project_id),
			'createdAt',project.created_at,'updatedAt',project.updated_at,'publishedAt',project.published_at,
			'submitter',case when submitter.id is null then null else jsonb_build_object('id',submitter.public_id,'name',submitter.username) end),project.submitted_by
			from simple_projects project left join users submitter on submitter.id=project.submitted_by where project.id=$1 and project.project_type=$2`, internalID, routeType).Scan(&raw, &actorID)
	case "community_post":
		actorRole = reportTargetActorAuthor
		err = queryer.QueryRow(ctx, `select jsonb_build_object(
			'title',post.title,'body',post.body_markdown,'kind',post.kind,'category',post.category,'sourceLocale',post.source_locale,
			'minecraftVersions',post.minecraft_versions,'severity',post.severity,'issueUrl',post.issue_url,
			'status',post.status,'reviewStatus',post.review_status,'createdAt',post.created_at,'updatedAt',post.updated_at,'publishedAt',post.published_at,
			'author',jsonb_build_object('id',author.public_id,'name',author.username)),post.author_id
			from community_posts post join users author on author.id=post.author_id where post.id=$1 and post.status='active'`, internalID).Scan(&raw, &actorID)
	case "minecraft_server":
		actorRole = reportTargetActorSubmitter
		err = queryer.QueryRow(ctx, `select jsonb_build_object(
			'title',server.name,'summary',server.short_description,'body',server.body_markdown,'address',server.address,
			'minecraftVersions',server.minecraft_versions,'languages',server.languages,'category',server.primary_tag,
			'dedicatedClient',server.dedicated_client,'whitelist',server.has_whitelist,'onlineMode',server.online_mode,
			'modded',server.modded,'loader',server.loader,'reviewStatus',server.review_status,
			'onlineSnapshot',jsonb_build_object('online',server.last_online,'playersOnline',server.last_players_online,'playersMax',server.last_players_max,'motd',server.last_motd,'minecraftVersion',server.last_minecraft_version,'checkedAt',server.last_checked_at),
			'createdAt',server.created_at,'updatedAt',server.updated_at,'publishedAt',server.published_at,
			'submitter',jsonb_build_object('id',submitter.public_id,'name',submitter.username)),server.submitted_by
			from minecraft_servers server join users submitter on submitter.id=server.submitted_by where server.id=$1`, internalID).Scan(&raw, &actorID)
	case "skin":
		actorRole = reportTargetActorOwner
		err = queryer.QueryRow(ctx, `select jsonb_build_object(
			'title',asset.display_name,'description',asset.description,'kind',asset.kind,'model',asset.model,'tags',asset.tags,
			'visibility',asset.visibility,'reviewStatus',asset.review_status,'status',asset.status,'createdAt',asset.created_at,'updatedAt',asset.updated_at,
			'author',jsonb_build_object('id',author.public_id,'name',author.username)),asset.owner_id
			from skin_assets asset join users author on author.id=asset.owner_id where asset.id=$1 and asset.status='active'`, internalID).Scan(&raw, &actorID)
	case "blueprint":
		actorRole = reportTargetActorOwner
		err = queryer.QueryRow(ctx, `select jsonb_build_object(
			'title',blueprint.title,'body',blueprint.description_markdown,'sourceFormat',blueprint.source_format,
			'dimensions',jsonb_build_array(blueprint.size_x,blueprint.size_y,blueprint.size_z),'blockCount',blueprint.block_count,
			'status',blueprint.status,'reviewStatus',blueprint.review_status,'createdAt',blueprint.created_at,'updatedAt',blueprint.updated_at,
			'author',jsonb_build_object('id',author.public_id,'name',author.username)),blueprint.owner_id
			from blueprints blueprint join users author on author.id=blueprint.owner_id where blueprint.id=$1 and blueprint.status<>'deleted'`, internalID).Scan(&raw, &actorID)
	default:
		return nil, reportTargetActor{}, pgx.ErrNoRows
	}
	if err != nil {
		return nil, reportTargetActor{}, err
	}
	payload := map[string]any{}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return nil, reportTargetActor{}, err
	}
	payload["targetType"] = targetType
	payload["id"] = publicID
	payload["routeEntityType"] = routeType
	payload["canonicalPath"] = path
	payload["capturedAt"] = time.Now().UTC()
	return payload, reportTargetActor{ID: actorID, Role: actorRole}, nil
}

func normalizeReportRouteType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "shader_pack":
		return "shader"
	case "minecraft_server":
		return "server"
	case "community_discussion":
		return "discussion"
	case "community_bug", "community_issue":
		return "bug"
	case "community_news":
		return "news"
	case "community_tutorial":
		return "tutorial"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func reportProjectAccessType(targetType string) (string, bool) {
	switch targetType {
	case "mod", "modpack", "plugin", "map", "resource_pack", "datapack", "addon":
		return targetType, true
	case "shader":
		return "shader_pack", true
	case "server":
		return "minecraft_server", true
	default:
		return "", false
	}
}

func reportModerationRecipientIDs(
	ctx context.Context,
	queryer reportSnapshotQueryer,
	targetType, targetPublicID string,
	actor reportTargetActor,
) ([]int64, error) {
	switch actor.Role {
	case reportTargetActorAuthor, reportTargetActorOwner, reportTargetActorSubject:
		if actor.ID == nil {
			return nil, nil
		}
		return []int64{*actor.ID}, nil
	case reportTargetActorSubmitter:
		projectType, ok := reportProjectAccessType(targetType)
		if !ok {
			return nil, errors.New("report submitter role is only valid for catalog projects")
		}
		rows, err := queryer.Query(ctx, `select distinct user_id from effective_project_access
			where project_type=$1 and project_public_id=$2 and access_level='developer' order by user_id`, projectType, targetPublicID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		recipients := make([]int64, 0)
		for rows.Next() {
			var userID int64
			if err = rows.Scan(&userID); err != nil {
				return nil, err
			}
			recipients = append(recipients, userID)
		}
		return recipients, rows.Err()
	default:
		return nil, errors.New("report target actor role is invalid")
	}
}

func (s *Server) ownReports(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	request, err := parseOwnReportPageRequest(r.URL.Query(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.queryOwnReportPage(r.Context(), claims.Subject, request)
	if err != nil {
		logGovernanceReadFailure("own_reports", request.Scope, err)
		writeError(w, http.StatusInternalServerError, "读取举报失败")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) adminReports(w http.ResponseWriter, r *http.Request) {
	request, err := parseAdminReportPageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.queryAdminReportPage(r.Context(), request)
	if err != nil {
		logGovernanceReadFailure("admin_reports", request.Scope, err)
		writeError(w, http.StatusInternalServerError, "读取举报队列失败")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) adminReportDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	var result map[string]any
	var raw []byte
	var tt, tid, reason, custom, detail, status, reporterID, reporterName string
	var targetActorID, targetActorName, claimedByID, claimedByName *string
	var targetActorRole string
	var claimedByCurrentUser bool
	var version int
	var created time.Time
	err := s.db.QueryRow(r.Context(), `select report.target_type,report.target_public_id,report.reason_code,report.reason_version,report.custom_reason,report.detail,report.status,
		reporter.public_id,reporter.username,target_actor.public_id,target_actor.username,report.target_actor_role,
		claimant.public_id,claimant.username,coalesce(report.claimed_by=$2,false),report.created_at,snapshot.payload
		from reports report join users reporter on reporter.id=report.reporter_id left join users target_actor on target_actor.id=report.target_actor_id
		left join users claimant on claimant.id=report.claimed_by
		join report_snapshots snapshot on snapshot.report_id=report.id where report.public_id=$1`, id, currentClaims(r).Subject).
		Scan(&tt, &tid, &reason, &version, &custom, &detail, &status, &reporterID, &reporterName, &targetActorID, &targetActorName, &targetActorRole,
			&claimedByID, &claimedByName, &claimedByCurrentUser, &created, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "举报不存在")
		return
	}
	if err != nil {
		logGovernanceReadFailure("admin_report_detail", id, err)
		writeError(w, 500, "读取举报失败")
		return
	}
	if claimsAllow(currentClaims(r), "report.snapshot.view") {
		result, err = decodeReportSnapshot(raw)
		if err != nil {
			logGovernanceReadFailure("admin_report_snapshot", id, err)
			writeError(w, http.StatusInternalServerError, "读取举报快照失败")
			return
		}
	}
	evidence := []map[string]any{}
	if claimsAllow(currentClaims(r), "report.evidence.view") {
		evidence, err = s.querySimpleRows(r, `select evidence.public_id,evidence.original_name,evidence.content_type,evidence.byte_size,evidence.sha256,
			evidence.scan_status,evidence.status,evidence.created_at,evidence.deleted_at
			from report_evidence evidence join reports report on report.id=evidence.report_id where report.public_id=$1 order by evidence.id`, id)
		if err != nil {
			logGovernanceReadFailure("admin_report_evidence", id, err)
			writeError(w, http.StatusInternalServerError, "读取举报证据失败")
			return
		}
	}
	reviews, err := s.querySimpleRows(r, `select review.conclusion,review.note,review.created_at,reviewer.public_id as reviewer_id,reviewer.username as reviewer_name
		from report_reviews review join reports report on report.id=review.report_id join users reviewer on reviewer.id=review.reviewer_id
		where report.public_id=$1 order by review.created_at,review.id`, id)
	if err != nil {
		logGovernanceReadFailure("admin_report_reviews", id, err)
		writeError(w, http.StatusInternalServerError, "读取举报审核记录失败")
		return
	}
	actions, err := s.querySimpleRows(r, `select action.public_id,action.action_type,action.target_type,action.target_public_id,action.reason,action.created_at,
		actor.public_id as actor_id,actor.username as actor_name from moderation_actions action join reports report on report.id=action.report_id
		join users actor on actor.id=action.actor_id where report.public_id=$1 order by action.created_at,action.id`, id)
	if err != nil {
		logGovernanceReadFailure("admin_report_actions", id, err)
		writeError(w, http.StatusInternalServerError, "读取举报处置记录失败")
		return
	}
	related, err := s.querySimpleRows(r, `select related.public_id,related.reason_code,related.status,related.created_at,reporter.username as reporter_name
		from reports current join reports related on related.target_type=current.target_type and related.target_public_id=current.target_public_id and related.id<>current.id
		join users reporter on reporter.id=related.reporter_id where current.public_id=$1 order by related.created_at desc,related.id desc limit 20`, id)
	if err != nil {
		logGovernanceReadFailure("admin_report_related", id, err)
		writeError(w, http.StatusInternalServerError, "读取关联举报失败")
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "targetType": tt, "targetId": tid, "reasonCode": reason, "reasonVersion": version,
		"customReason": custom, "detail": detail, "status": status, "reporterId": reporterID, "reporterName": reporterName,
		"targetActorId": targetActorID, "targetActorName": targetActorName, "targetActorRole": targetActorRole, "createdAt": created, "snapshot": result,
		"claimedById": claimedByID, "claimedByName": claimedByName, "claimedByCurrentUser": claimedByCurrentUser,
		"canTakeover": claimsAllow(currentClaims(r), "report.action.takeover"),
		"evidence":    evidence, "reviews": reviews, "actions": actions, "relatedReports": related})
}

func decodeReportSnapshot(raw []byte) (map[string]any, error) {
	return decodeStoredJSONObject(raw, "report snapshot")
}

func (s *Server) claimReport(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建领取事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var reportID int64
	err = tx.QueryRow(r.Context(), `update reports set status='in_review',claimed_by=$2,claimed_at=now(),updated_at=now(),lock_version=lock_version+1
		where public_id=$1 and status='pending' returning id`, r.PathValue("id"), claims.Subject).Scan(&reportID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "举报已被领取或处理")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "领取举报失败")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into report_assignment_events(report_id,action,actor_id,assignee_id)
		values($1,'claim',$2,$2)`, reportID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "记录举报领取失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交举报领取失败")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "in_review"})
}

func (s *Server) takeoverReport(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "report.action.takeover") {
		writeError(w, http.StatusForbidden, "无权接管举报")
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "接管请求格式不正确")
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || utf8.RuneCountInString(request.Reason) > 1000 {
		writeError(w, http.StatusBadRequest, "接管原因不能为空且不能超过 1000 个字符")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建接管事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var reportID int64
	var previousAssigneeID *int64
	err = tx.QueryRow(r.Context(), `select id,claimed_by from reports where public_id=$1 and status='in_review' for update`, r.PathValue("id")).
		Scan(&reportID, &previousAssigneeID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "举报未被领取或已经处理")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取举报责任人失败")
		return
	}
	if previousAssigneeID != nil && *previousAssigneeID == claims.Subject {
		writeError(w, http.StatusConflict, "你已经是该举报的处理人")
		return
	}
	if _, err = tx.Exec(r.Context(), `update reports set claimed_by=$2,claimed_at=now(),updated_at=now(),lock_version=lock_version+1 where id=$1`, reportID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "接管举报失败")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into report_assignment_events(report_id,action,actor_id,previous_assignee_id,assignee_id,reason)
		values($1,'takeover',$2,$3,$2,$4)`, reportID, claims.Subject, previousAssigneeID, request.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, "记录举报接管失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交举报接管失败")
		return
	}
	s.writeAppLog(context.Background(), "admin_operation", "warn", "report.takeover", r.PathValue("id"), claims.Subject, r, http.StatusOK, 0,
		map[string]any{"previousAssigneeId": previousAssigneeID, "reason": request.Reason})
	writeJSON(w, http.StatusOK, map[string]any{"status": "in_review"})
}

type resolveUnifiedReportRequest struct {
	Conclusion           string     `json:"conclusion"`
	Note                 string     `json:"note"`
	DeleteTarget         bool       `json:"deleteTarget"`
	BanUserID            string     `json:"banUserId"`
	BanReasonCode        string     `json:"banReasonCode"`
	BanCustomReason      string     `json:"banCustomReason"`
	BanEndsAt            *time.Time `json:"banEndsAt"`
	PublicRecordMarkdown string     `json:"publicRecordMarkdown"`
	IdempotencyKey       string     `json:"idempotencyKey"`
}

func validateReportResolutionActions(request resolveUnifiedReportRequest) error {
	if request.Conclusion == "invalid" && (request.DeleteTarget || request.BanUserID != "") {
		return errors.New("举报不成立时不能执行隐藏或封禁处置")
	}
	return nil
}

func (s *Server) resolveUnifiedReport(w http.ResponseWriter, r *http.Request) {
	var request resolveUnifiedReportRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, 400, "处理请求格式不正确")
		return
	}
	request.Conclusion = strings.TrimSpace(request.Conclusion)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if (request.Conclusion != "valid" && request.Conclusion != "invalid") || request.IdempotencyKey == "" {
		writeError(w, 400, "处理结论不正确")
		return
	}
	if err := validateReportResolutionActions(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.BanUserID != "" {
		if err := validateTemporaryBanEnd(request.BanEndsAt, time.Now()); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	claims := currentClaims(r)
	if request.DeleteTarget && !claimsAllow(claims, "report.action.delete") {
		writeError(w, 403, "无权删除被举报内容")
		return
	}
	if request.BanUserID != "" && !claimsAllow(claims, "report.action.ban") {
		writeError(w, 403, "无权封禁用户")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "创建审核事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var reportID, reporterID int64
	var targetType, targetID string
	var targetActor reportTargetActor
	err = tx.QueryRow(r.Context(), `select id,target_type,target_public_id,reporter_id,target_actor_id,target_actor_role from reports
		where public_id=$1 and status='in_review' and claimed_by=$2 for update`, r.PathValue("id"), claims.Subject).
		Scan(&reportID, &targetType, &targetID, &reporterID, &targetActor.ID, &targetActor.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "举报未由你领取、责任已转交或已经处理")
		return
	}
	if err != nil {
		writeError(w, 500, "读取举报失败")
		return
	}
	var moderationRecipients []int64
	if request.Conclusion == "valid" && request.DeleteTarget {
		moderationRecipients, err = reportModerationRecipientIDs(r.Context(), tx, targetType, targetID, targetActor)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取内容治理联系人失败")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `insert into report_reviews(report_id,reviewer_id,conclusion,note,idempotency_key) values($1,$2,$3,$4,$5) on conflict(report_id,idempotency_key) do nothing`, reportID, claims.Subject, request.Conclusion, strings.TrimSpace(request.Note), request.IdempotencyKey); err != nil {
		writeError(w, 500, "保存审核结论失败")
		return
	}
	if request.DeleteTarget {
		if err = s.moderationHideTarget(r.Context(), tx, targetType, targetID); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		_, err = tx.Exec(r.Context(), `insert into moderation_actions(report_id,action_type,target_type,target_public_id,actor_id,before_summary,after_summary,reason,idempotency_key)
			select $1,'delete',$2,$3,$4,snapshot.payload,jsonb_build_object('visibility','hidden_by_moderation'),$5,$6
			from report_snapshots snapshot where snapshot.report_id=$1 on conflict do nothing`, reportID, targetType, targetID, claims.Subject, request.Note, request.IdempotencyKey)
		if err != nil {
			writeError(w, 500, "记录删除动作失败")
			return
		}
	}
	var bannedUserID int64
	if request.BanUserID != "" {
		var banPublicID string
		if bannedUserID, banPublicID, err = createBanTx(r.Context(), tx, request.BanUserID, claims.Subject, reportID, request.BanReasonCode, request.BanCustomReason, request.PublicRecordMarkdown, "", request.BanEndsAt); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if err = enqueueTemplatedNotificationTx(r.Context(), tx, "ban.created", bannedUserID, 0, "account_banned", nil,
			map[string]any{"banId": banPublicID, "url": "/site-affairs/blackroom/" + banPublicID}, r.Header.Get("X-Request-ID")); err != nil {
			writeError(w, http.StatusInternalServerError, "创建封禁通知失败")
			return
		}
	}
	state := "resolved_invalid"
	if request.Conclusion == "valid" {
		state = "resolved_valid"
	}
	if _, err = tx.Exec(r.Context(), `update reports set status=$2,resolved_at=now(),updated_at=now(),lock_version=lock_version+1 where id=$1`, reportID, state); err != nil {
		writeError(w, 500, "保存举报状态失败")
		return
	}
	if _, err = tx.Exec(r.Context(), `update report_evidence set status='pending_delete',cleanup_after=now()+interval '14 days' where report_id=$1 and status='bound'`, reportID); err != nil {
		writeError(w, 500, "安排举报附件清理失败")
		return
	}
	reportTemplate := "report_resolved_invalid"
	if request.Conclusion == "valid" {
		reportTemplate = "report_resolved_valid"
	}
	if err = enqueueTemplatedNotificationTx(r.Context(), tx, "report.resolved", reporterID, 0, reportTemplate, nil,
		map[string]any{"reportId": r.PathValue("id")}, r.Header.Get("X-Request-ID")); err != nil {
		writeError(w, http.StatusInternalServerError, "创建举报结果通知失败")
		return
	}
	for _, recipientID := range moderationRecipients {
		if recipientID == reporterID {
			continue
		}
		if err = enqueueTemplatedNotificationTx(r.Context(), tx, "report.target_action", recipientID, 0, "moderation_action", nil,
			map[string]any{"targetType": targetType, "targetId": targetID}, r.Header.Get("X-Request-ID")); err != nil {
			writeError(w, http.StatusInternalServerError, "创建内容处置通知失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "提交举报审核失败")
		return
	}
	s.writeAppLog(context.Background(), "admin_operation", "warn", "report.resolve", r.PathValue("id"), claims.Subject, r, 200, 0, map[string]any{"conclusion": request.Conclusion, "deleteTarget": request.DeleteTarget, "ban": request.BanUserID != ""})
	if bannedUserID > 0 {
		if !s.requireSecurityVersionRefresh(w, r, "resolve_report_with_ban", bannedUserID,
			s.refreshAuthVersionForIdentity(r.Context(), bannedUserID, request.BanUserID),
			s.refreshPermissionVersion(r.Context(), bannedUserID)) {
			return
		}
	}
	writeJSON(w, 200, map[string]any{"status": state})
}

func (s *Server) reopenUnifiedReport(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Reason         string `json:"reason"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if decodeJSON(r, &request) != nil || strings.TrimSpace(request.Reason) == "" || strings.TrimSpace(request.IdempotencyKey) == "" {
		writeError(w, http.StatusBadRequest, "重新打开原因和幂等键不能为空")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建举报事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var reportID int64
	err = tx.QueryRow(r.Context(), `update reports set status='pending',claimed_by=null,claimed_at=null,resolved_at=null,updated_at=now(),lock_version=lock_version+1
		where public_id=$1 and status in ('resolved_valid','resolved_invalid','cancelled') returning id`, r.PathValue("id")).Scan(&reportID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "举报尚未完成或已重新打开")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "重新打开举报失败")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into report_reviews(report_id,reviewer_id,conclusion,note,idempotency_key)
		values($1,$2,'reopened',$3,$4) on conflict(report_id,idempotency_key) do nothing`, reportID, currentClaims(r).Subject, strings.TrimSpace(request.Reason), strings.TrimSpace(request.IdempotencyKey)); err != nil {
		writeError(w, http.StatusInternalServerError, "记录重新打开操作失败")
		return
	}
	// Only cancel deletion that has not yet been claimed. Evidence already
	// deleted from OSS remains metadata-only and cannot be reconstructed.
	if _, err = tx.Exec(r.Context(), `update report_evidence set status='bound',cleanup_after='infinity'
		where report_id=$1 and status='pending_delete'`, reportID); err != nil {
		writeError(w, http.StatusInternalServerError, "取消附件清理失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交举报状态失败")
		return
	}
	s.writeAppLog(context.Background(), "admin_operation", "warn", "report.reopen", r.PathValue("id"), currentClaims(r).Subject, r, http.StatusOK, 0, map[string]string{"reason": strings.TrimSpace(request.Reason)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
}

func (s *Server) moderationHideTarget(ctx context.Context, tx pgx.Tx, targetType, publicID string) error {
	switch targetType {
	case "comment":
		return expectModerationUpdate(tx.Exec(ctx, `update comments set status='hidden',updated_at=now() where public_id=$1 and status not in ('deleted','spam')`, publicID))
	case "user":
		return errors.New("用户主页不能作为内容删除；请使用封禁动作")
	case "mod":
		return expectModerationUpdate(tx.Exec(ctx, `update mods set review_status='rejected',updated_at=now() where project_code=$1 and review_status<>'rejected'`, publicID))
	case "modpack":
		return expectModerationUpdate(tx.Exec(ctx, `update modpacks set review_status='rejected',updated_at=now() where public_id=$1 and review_status<>'rejected'`, publicID))
	case "plugin", "map", "shader", "resource_pack", "datapack", "addon":
		routeType := targetType
		if routeType == "shader" {
			routeType = "shader_pack"
		}
		return expectModerationUpdate(tx.Exec(ctx, `update simple_projects set review_status='rejected',updated_at=now() where public_id=$1 and project_type=$2 and review_status<>'rejected'`, publicID, routeType))
	case "discussion", "bug", "news", "tutorial":
		kind := targetType
		if kind == "bug" {
			kind = "issue"
		}
		return expectModerationUpdate(tx.Exec(ctx, `update community_posts set status='deleted',updated_at=now() where public_id=$1 and kind=$2 and status='active'`, publicID, kind))
	case "skin":
		var assetID int64
		var blobHash string
		if err := tx.QueryRow(ctx, `select id,blob_hash from skin_assets
			where public_id=$1 and status='active' for update`, publicID).Scan(&assetID, &blobHash); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.New("举报目标不存在、已经隐藏或状态已变化")
			}
			return err
		}
		return s.softDeleteSkinAssetTx(ctx, tx, assetID, blobHash, "moderated_skin_asset_deleted")
	case "blueprint":
		return expectModerationUpdate(tx.Exec(ctx, `update blueprints set status='deleted',updated_at=now() where public_id=$1 and status<>'deleted'`, publicID))
	case "server":
		return expectModerationUpdate(tx.Exec(ctx, `update minecraft_servers set review_status='rejected',updated_at=now() where public_id=$1 and review_status<>'rejected'`, publicID))
	}
	return errors.New("不支持处置该举报目标")
}

func expectModerationUpdate(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("举报目标不存在、已经隐藏或状态已变化")
	}
	return nil
}

func validateTemporaryBanEnd(endsAt *time.Time, now time.Time) error {
	if endsAt != nil && endsAt.Before(now.Add(minimumTemporaryBanDuration)) {
		return errors.New("临时封禁结束时间必须至少晚于当前时间 1 分钟")
	}
	return nil
}

func createBanTx(ctx context.Context, tx pgx.Tx, userPublicID string, moderatorID, reportID int64, reasonCode, customReason, publicMarkdown, internalNote string, endsAt *time.Time) (int64, string, error) {
	if err := validateTemporaryBanEnd(endsAt, time.Now()); err != nil {
		return 0, "", err
	}
	reasonCode = strings.TrimSpace(reasonCode)
	customReason = strings.TrimSpace(customReason)
	if reasonCode == "" || (reasonCode == "other" && customReason == "") {
		return 0, "", errors.New("封禁理由不能为空")
	}
	var userID int64
	var username, avatar string
	if err := tx.QueryRow(ctx, `select id,username,avatar_url from users where public_id=$1 and status<>'deleted' for update`, userPublicID).Scan(&userID, &username, &avatar); err != nil {
		return 0, "", errors.New("封禁用户不存在")
	}
	var banID int64
	var banPublicID string
	err := tx.QueryRow(ctx, `insert into ban_records(user_id,moderator_id,report_id,reason_code,custom_reason,public_record_markdown,internal_note,username_snapshot,avatar_snapshot,ends_at) values($1,$2,nullif($3,0),$4,$5,$6,$7,$8,$9,$10) returning id,public_id`, userID, moderatorID, reportID, reasonCode, customReason, publicMarkdown, internalNote, username, avatar, endsAt).Scan(&banID, &banPublicID)
	if err != nil {
		return 0, "", errors.New("用户已有生效封禁或理由无效")
	}
	tag, err := tx.Exec(ctx, `insert into user_role_bindings(user_id,role_id,source,source_key,expires_at)
		select $1,id,$2,$3,$4 from roles where code='banned'`,
		userID, authorizationSourceGovernanceBan, strconv.FormatInt(banID, 10), endsAt)
	if err == nil && tag.RowsAffected() != 1 {
		err = errors.New("封禁权限组不存在")
	}
	if err == nil {
		_, err = tx.Exec(ctx, `update users set auth_version=auth_version+1,updated_at=now() where id=$1`, userID)
	}
	return userID, banPublicID, err
}

type createBanRequest struct {
	UserID               string     `json:"userId"`
	ReasonCode           string     `json:"reasonCode"`
	CustomReason         string     `json:"customReason"`
	PublicRecordMarkdown string     `json:"publicRecordMarkdown"`
	InternalNote         string     `json:"internalNote"`
	EndsAt               *time.Time `json:"endsAt"`
}

func (s *Server) adminBanReasons(w http.ResponseWriter, r *http.Request) {
	locale := normalizedSiteAffairsLocale(r.URL.Query().Get("locale"))
	rows, err := s.db.Query(r.Context(), `select code,coalesce(translations->>$1,translations->>'zh-CN',code),sort_order
		from ban_reasons where active order by sort_order,code`, locale)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取常用封禁理由失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var code, label string
		var sortOrder int
		if err = rows.Scan(&code, &label, &sortOrder); err != nil {
			writeError(w, http.StatusInternalServerError, "读取常用封禁理由失败")
			return
		}
		items = append(items, map[string]any{"code": code, "label": label, "sortOrder": sortOrder})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取常用封禁理由失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "locale": locale})
}

func (s *Server) adminBans(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req createBanRequest
		if decodeJSON(r, &req) != nil {
			writeError(w, 400, "请求格式不正确")
			return
		}
		if err := validateTemporaryBanEnd(req.EndsAt, time.Now()); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		tx, e := s.db.Begin(r.Context())
		if e != nil {
			writeError(w, 500, "创建封禁事务失败")
			return
		}
		defer tx.Rollback(r.Context())
		bannedUserID, banPublicID, createErr := createBanTx(r.Context(), tx, req.UserID, currentClaims(r).Subject, 0, req.ReasonCode, req.CustomReason, req.PublicRecordMarkdown, req.InternalNote, req.EndsAt)
		if createErr != nil {
			e = createErr
			writeError(w, 400, e.Error())
			return
		}
		if e = enqueueTemplatedNotificationTx(r.Context(), tx, "ban.created", bannedUserID, 0, "account_banned", nil,
			map[string]any{"banId": banPublicID, "url": "/site-affairs/blackroom/" + banPublicID}, r.Header.Get("X-Request-ID")); e != nil {
			writeError(w, http.StatusInternalServerError, "创建封禁通知失败")
			return
		}
		if e = tx.Commit(r.Context()); e != nil {
			writeError(w, 500, "提交封禁失败")
			return
		}
		s.writeAppLog(context.Background(), "admin_operation", "warn", "ban.create", req.UserID, currentClaims(r).Subject, r, 201, 0, map[string]any{"reasonCode": req.ReasonCode, "endsAt": req.EndsAt})
		if !s.requireSecurityVersionRefresh(w, r, "create_ban", bannedUserID,
			s.refreshAuthVersionForIdentity(r.Context(), bannedUserID, req.UserID),
			s.refreshPermissionVersion(r.Context(), bannedUserID)) {
			return
		}
		writeJSON(w, 201, map[string]bool{"created": true})
		return
	}
	request, err := parseAdminBlackroomPageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.queryAdminBlackroomPage(r.Context(), request, time.Now())
	if err != nil {
		logGovernanceReadFailure("blackroom", request.Scope, err)
		writeError(w, http.StatusInternalServerError, "读取小黑屋失败")
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (s *Server) publicBlackroom(w http.ResponseWriter, r *http.Request) {
	request, err := parsePublicBlackroomPageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.queryPublicBlackroomPage(r.Context(), request, time.Now())
	if err != nil {
		logGovernanceReadFailure("blackroom", request.Scope, err)
		writeError(w, http.StatusInternalServerError, "读取小黑屋失败")
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (s *Server) blackroomDetail(w http.ResponseWriter, r *http.Request) {
	var uid, name, avatar, reason, custom, status, record string
	var starts time.Time
	var ends, revoked *time.Time
	e := s.db.QueryRow(r.Context(), `select u.public_id,ban.username_snapshot,ban.avatar_snapshot,ban.reason_code,ban.custom_reason,ban.status,ban.public_record_markdown,ban.starts_at,ban.ends_at,ban.revoked_at from ban_records ban join users u on u.id=ban.user_id where ban.public_id=$1`, r.PathValue("id")).Scan(&uid, &name, &avatar, &reason, &custom, &status, &record, &starts, &ends, &revoked)
	if errors.Is(e, pgx.ErrNoRows) {
		writeError(w, 404, "封禁记录不存在")
		return
	}
	if e != nil {
		writeError(w, 500, "读取封禁记录失败")
		return
	}
	writeJSON(w, 200, map[string]any{"id": r.PathValue("id"), "userId": uid, "username": name, "avatarUrl": avatar, "reasonCode": reason, "customReason": custom, "status": publicBanStatus(status, ends, revoked, time.Now()), "publicRecordMarkdown": record, "startsAt": starts, "endsAt": ends, "revokedAt": revoked})
}

func publicBanStatus(status string, endsAt, revokedAt *time.Time, now time.Time) string {
	if status == "revoked" || status == "expired" || revokedAt != nil || endsAt != nil && !endsAt.After(now) {
		return "released"
	}
	if endsAt != nil {
		return "temporary"
	}
	return "permanent"
}
func (s *Server) revokeBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(r, &req) != nil || strings.TrimSpace(req.Reason) == "" {
		writeError(w, 400, "解除原因不能为空")
		return
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		writeError(w, 500, "创建事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var banID, userID int64
	e = tx.QueryRow(r.Context(), `update ban_records set status='revoked',revoked_at=now(),revoked_by=$2,revoke_reason=$3
		where public_id=$1 and status='active' returning id,user_id`,
		r.PathValue("id"), currentClaims(r).Subject, req.Reason).Scan(&banID, &userID)
	if errors.Is(e, pgx.ErrNoRows) {
		writeError(w, 409, "封禁已结束")
		return
	}
	if e != nil {
		writeError(w, 500, "解除封禁失败")
		return
	}
	_, e = tx.Exec(r.Context(), `delete from user_role_bindings
		where user_id=$1 and source=$2 and source_key=$3`,
		userID, authorizationSourceGovernanceBan, strconv.FormatInt(banID, 10))
	if e == nil {
		e = enqueueTemplatedNotificationTx(r.Context(), tx, "ban.revoked", userID, 0, "ban_released", nil,
			map[string]any{"banId": r.PathValue("id")}, r.Header.Get("X-Request-ID"))
	}
	if e == nil {
		e = tx.Commit(r.Context())
	}
	if e != nil {
		writeError(w, 500, "提交解除封禁失败")
		return
	}
	s.writeAppLog(context.Background(), "admin_operation", "warn", "ban.revoke", r.PathValue("id"), currentClaims(r).Subject, r, 200, 0, map[string]string{"reason": req.Reason})
	if !s.requireSecurityVersionRefresh(w, r, "revoke_ban", userID,
		s.refreshPermissionVersion(r.Context(), userID)) {
		return
	}
	writeJSON(w, 200, map[string]string{"status": "released"})
}
