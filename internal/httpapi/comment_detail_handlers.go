package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

func (s *Server) commentWatchItem(w http.ResponseWriter, r *http.Request) {
	skipRequestActivity(r)
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.watch") {
		writeError(w, http.StatusForbidden, "无权管理评论插眼")
		return
	}
	watchID := strings.ToLower(strings.TrimSpace(r.PathValue("watchId")))
	if _, err := s.resolveVisibleCommentWatch(r.Context(), watchID, claims.Subject, claims); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "插眼记录不存在或当前不可见")
		} else {
			writeError(w, http.StatusInternalServerError, "读取插眼目标失败")
		}
		return
	}
	switch r.Method {
	case http.MethodPost:
		tag, err := s.db.Exec(r.Context(), `with selected as (
			select id from comment_watches where public_id=$1 and user_id=$2 and status='active'
		), marked as (
			update comment_watch_replies reply set read_at=now()
			where reply.watch_id in (select id from selected) and reply.read_at is null
		)
		update comment_watches set unread_count=0,last_read_comment_id=(
			select comment_id from comment_watch_replies where watch_id=comment_watches.id
			order by created_at desc,comment_id desc limit 1
		),updated_at=now() where id in (select id from selected)`, watchID, claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "更新插眼已读状态失败")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "插眼记录不存在")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"read": true})
	case http.MethodPatch:
		var request updateCommentWatchRequest
		if decodeJSON(r, &request) != nil {
			writeError(w, http.StatusBadRequest, "请求格式不正确")
			return
		}
		var mutedUntil *time.Time
		mutedForever := false
		now := time.Now()
		switch strings.ToLower(strings.TrimSpace(request.Mute)) {
		case "", "none":
		case "1h":
			value := now.Add(time.Hour)
			mutedUntil = &value
		case "24h":
			value := now.Add(24 * time.Hour)
			mutedUntil = &value
		case "7d":
			value := now.Add(7 * 24 * time.Hour)
			mutedUntil = &value
		case "forever":
			mutedForever = true
		default:
			writeError(w, http.StatusBadRequest, "静音时间不正确")
			return
		}
		tag, err := s.db.Exec(r.Context(), `update comment_watches set muted_until=$3,muted_forever=$4,updated_at=now()
			where public_id=$1 and user_id=$2 and status='active'`, watchID, claims.Subject, mutedUntil, mutedForever)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "更新插眼设置失败")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "插眼记录不存在")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
	}
}

func (s *Server) queryCommentItems(ctx context.Context, ids []int64, includeTree bool, maxDepth int, claims security.Claims) ([]commentResponse, error) {
	items, err := queryCommentItemsWithQueryer(ctx, s.db, ids, includeTree, maxDepth, claims.Subject)
	if err != nil {
		return nil, err
	}
	authorIDs := make([]int64, len(items))
	for index := range items {
		authorIDs[index] = items[index].Author.internalID
	}
	online := s.cache.UsersOnline(ctx, authorIDs, time.Now(), s.cache.Config().PresenceTTL)
	for index := range items {
		items[index].Author.OnlineStatus = mapPublicOnlineVisibility(items[index].Author.showOnline, online[items[index].Author.internalID])
	}
	ossCfg := s.ossConfigFromSettings(ctx)
	resolvedAvatarURLs := make(map[string]string, len(items))
	for index := range items {
		storedURL := items[index].Author.AvatarURL
		resolvedURL, exists := resolvedAvatarURLs[storedURL]
		if !exists {
			resolvedURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, storedURL)
			if err != nil {
				return nil, err
			}
			resolvedAvatarURLs[storedURL] = resolvedURL
		}
		items[index].Author.AvatarURL = resolvedURL
	}
	if err = s.annotateCommentPermissions(ctx, items, claims); err != nil {
		return nil, err
	}
	if err = s.annotateCommentAttachments(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

var errCommentAttachmentUnavailable = errors.New("comment attachment is unavailable")

func bindCommentAttachmentsTx(ctx context.Context, tx pgx.Tx, commentID, userID int64, publicFileIDs []string) error {
	if len(publicFileIDs) == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `insert into comment_attachments(comment_id,attachment_file_id)
		select $1,file.id from oss_files file
		where file.public_id=any($2::text[]) and file.uploader_id=$3 and file.status='active'
		  and file.scan_status in ('pending','clean','trusted_generated')
		on conflict(comment_id,attachment_file_id) do nothing`, commentID, publicFileIDs, userID)
	if err != nil {
		return err
	}
	if int(tag.RowsAffected()) != len(publicFileIDs) {
		return errCommentAttachmentUnavailable
	}
	return nil
}

type commentLogAttachmentJobMessage struct {
	JobID int64 `json:"jobId"`
}

func enqueueCommentLogAttachmentJobsTx(
	ctx context.Context,
	tx pgx.Tx,
	commentID int64,
	userID int64,
	publicFileIDs []string,
) error {
	if len(publicFileIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `select file.id,file.public_id,coalesce(nullif(file.source_original_name,''),file.original_name)
		from comment_attachments attachment join oss_files file on file.id=attachment.attachment_file_id
		where attachment.comment_id=$1 and file.public_id=any($2::text[]) and file.uploader_id=$3 and file.status='active'
		order by file.id for update of attachment`, commentID, publicFileIDs, userID)
	if err != nil {
		return err
	}
	type attachmentCandidate struct {
		fileID   int64
		publicID string
		name     string
	}
	candidates := make([]attachmentCandidate, 0, len(publicFileIDs))
	for rows.Next() {
		var candidate attachmentCandidate
		if err = rows.Scan(&candidate.fileID, &candidate.publicID, &candidate.name); err != nil {
			break
		}
		candidates = append(candidates, candidate)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if !isCommentLogAttachmentName(candidate.name) {
			continue
		}
		tag, updateErr := tx.Exec(ctx, `update comment_attachments set kind='log',processing_status='processing'
			where comment_id=$1 and attachment_file_id=$2`, commentID, candidate.fileID)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() != 1 {
			return errCommentAttachmentUnavailable
		}
		var jobID int64
		var status string
		err = tx.QueryRow(ctx, `insert into comment_log_attachment_jobs(comment_id,attachment_file_id,requested_by)
			values($1,$2,$3) on conflict(comment_id,attachment_file_id) do nothing returning id,status`,
			commentID, candidate.fileID, userID).Scan(&jobID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `select id,status from comment_log_attachment_jobs
				where comment_id=$1 and attachment_file_id=$2`, commentID, candidate.fileID).Scan(&jobID, &status)
		}
		if err != nil {
			return err
		}
		if status == "completed" {
			continue
		}
		if _, err = queue.EnqueueTx(ctx, tx, "comment_log_attachment", "comment.log_attachment.requested",
			"comment_log_attachment_job", strconv.FormatInt(jobID, 10), "", commentLogAttachmentJobMessage{JobID: jobID}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) annotateCommentAttachments(ctx context.Context, items []commentResponse) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	byID := make(map[int64]*commentResponse, len(items))
	for index := range items {
		items[index].Attachments = []commentAttachment{}
		if items[index].Deleted {
			continue
		}
		ids = append(ids, items[index].internalID)
		byID[items[index].internalID] = &items[index]
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.Query(ctx, `select binding.comment_id,file.public_id,
		coalesce(nullif(file.source_original_name,''),file.original_name),file.content_type,file.size_bytes,
		file.status,file.scan_status,binding.kind,binding.processing_status,
		coalesce(share.public_code,''),coalesce(share.status,''),share.expires_at
		from comment_attachments binding
		join oss_files file on file.id=binding.attachment_file_id
		left join comment_log_bindings log_binding on log_binding.comment_id=binding.comment_id and log_binding.attachment_file_id=binding.attachment_file_id
		left join log_shares share on share.id=log_binding.log_share_id
		where binding.comment_id=any($1::bigint[]) order by binding.created_at,binding.attachment_file_id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var commentID int64
		var attachment commentAttachment
		var fileStatus, scanStatus, attachmentKind, processingStatus, publicCode, shareStatus string
		var expiresAt *time.Time
		if err = rows.Scan(&commentID, &attachment.FileID, &attachment.FileName, &attachment.ContentType,
			&attachment.SizeBytes, &fileStatus, &scanStatus, &attachmentKind, &processingStatus,
			&publicCode, &shareStatus, &expiresAt); err != nil {
			return err
		}
		item := byID[commentID]
		if item == nil {
			continue
		}
		if attachmentKind == "log" || isCommentLogAttachmentName(attachment.FileName) {
			attachment.Kind = "log"
			attachment.Status = defaultString(shareStatus, processingStatus)
			if shareStatus == "ready" && expiresAt != nil && expiresAt.After(time.Now()) {
				attachment.URL = "/log/s/" + publicCode
			} else if shareStatus == "ready" {
				attachment.Status = "expired"
			}
		} else {
			attachment.Kind = "file"
			switch {
			case fileStatus != "active":
				attachment.Status = "unavailable"
			case scanStatus == "clean" || scanStatus == "trusted_generated":
				attachment.Status = "ready"
				attachment.URL = "/api/v1/comments/" + item.ID + "/attachments/" + attachment.FileID + "/download"
			default:
				attachment.Status = "scanning"
			}
		}
		item.Attachments = append(item.Attachments, attachment)
	}
	return rows.Err()
}

func isCommentLogAttachmentName(fileName string) bool {
	baseName := norm.NFC.String(filepath.Base(strings.TrimSpace(fileName)))
	extension := strings.ToLower(filepath.Ext(baseName))
	return extension == ".log" || extension == ".zip" && strings.Contains(baseName, "错误报告")
}

func (s *Server) downloadCommentAttachment(w http.ResponseWriter, r *http.Request) {
	commentPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))
	filePublicID := strings.ToLower(strings.TrimSpace(r.PathValue("fileId")))
	if commentPublicID == "" || filePublicID == "" {
		writeError(w, http.StatusBadRequest, "评论附件地址不正确")
		return
	}
	visible, err := s.resolveVisibleComment(r.Context(), commentPublicID, currentClaims(r))
	if err != nil || visible.Status != "published" {
		writeError(w, http.StatusNotFound, "评论附件不存在")
		return
	}
	commentID := visible.ID
	items, err := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, currentClaims(r))
	if err != nil || len(items) != 1 || items[0].Deleted {
		writeError(w, http.StatusNotFound, "评论附件不存在")
		return
	}
	var objectKey, fileName string
	err = s.db.QueryRow(r.Context(), `select file.object_key,coalesce(nullif(file.source_original_name,''),file.original_name)
		from comment_attachments attachment
		join oss_files file on file.id=attachment.attachment_file_id
		where attachment.comment_id=$1 and file.public_id=$2 and file.status='active'
		  and file.scan_status in ('clean','trusted_generated')`, commentID, filePublicID).Scan(&objectKey, &fileName)
	if err != nil || isCommentLogAttachmentName(fileName) {
		writeError(w, http.StatusNotFound, "评论附件不存在或仍在安全扫描中")
		return
	}
	s.redirectOSSObjectAccess(w, r, objectKey, ossObjectAccessOptions{ContentDisposition: downloadContentDisposition(fileName)})
}

func queryCommentItemsWithQueryer(ctx context.Context, queryer commentQueryer, ids []int64, includeTree bool, maxDepth int, viewerID int64) ([]commentResponse, error) {
	if len(ids) == 0 {
		return []commentResponse{}, nil
	}
	where := "c.id=any($1)"
	if includeTree {
		where = "(c.id=any($1) or c.root_id=any($1)) and c.depth<=$2"
	}
	args := []any{ids}
	viewerIndex := 2
	if includeTree {
		args = append(args, maxDepth)
		viewerIndex = 3
	}
	args = append(args, viewerID)
	rows, err := queryer.Query(ctx, fmt.Sprintf(`select c.id,c.public_id,c.floor_number,coalesce(parent.public_id,''),
		coalesce(root.public_id,''),c.depth,c.body,c.status,c.child_count,c.descendant_count,c.hot_score,
		author.id,author.public_id,author.username,author.avatar_url,author.show_online_status,
		case when parent_block.blocker_id is null then coalesce(parent_author.username,'') else '' end,
		case when parent_block.blocker_id is null then coalesce(parent.body,'') else '' end,
		case when parent_block.blocker_id is null then coalesce(parent.status,'') else 'deleted' end,
		c.created_at,c.updated_at,c.pinned_at,
		coalesce(watch.public_id,''),coalesce(watch.status,''),watch.muted_until,
		coalesce(watch.muted_forever,false),coalesce(watch.unread_count,0),coalesce(watch.watched_reply_count,0),
		coalesce(reaction_summary.names,array[]::text[]),
		coalesce(reaction_summary.counts,array[]::bigint[]),
		coalesce(reaction_summary.selected,array[]::text[])
		from comments c
		join users author on author.id=c.author_id
		left join comments parent on parent.id=c.parent_id
		left join users parent_author on parent_author.id=parent.author_id
		left join user_blocks parent_block on parent_block.blocker_id=$%d and parent_block.blocked_id=parent.author_id
		left join comments root on root.id=c.root_id
		left join comment_watches watch on watch.comment_id=c.id and watch.user_id=$%d
		left join lateral (
			select array_agg(grouped.reaction order by grouped.reaction) names,
				array_agg(grouped.total order by grouped.reaction) counts,
				array_agg(grouped.reaction order by grouped.reaction) filter(where grouped.selected) selected
			from (
				select reaction.reaction,count(*) total,bool_or(reaction.user_id=$%d) selected
				from comment_reactions reaction
				where reaction.comment_id=c.id
				group by reaction.reaction
			) grouped
		) reaction_summary on true
		where %s and c.status in ('published','deleted')
		  and ($%d::bigint=0 or not exists(select 1 from user_blocks block
			where block.blocker_id=$%d and block.blocked_id=c.author_id))
		order by c.created_at,c.id`, viewerIndex, viewerIndex, viewerIndex, where, viewerIndex, viewerIndex), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]commentResponse, 0)
	for rows.Next() {
		var numericID int64
		var parentID, rootID, status, parentAuthor, parentBody, parentStatus string
		var watchID, watchStatus string
		var mutedUntil *time.Time
		var mutedForever bool
		var watchUnread, watchedReplies int
		var reactionNames, selectedReactions []string
		var reactionCounts []int64
		item := commentResponse{Reactions: map[string]int{}, UserReactions: []string{}}
		if err = rows.Scan(&numericID, &item.ID, &item.FloorNumber, &parentID, &rootID, &item.Depth, &item.Body, &status,
			&item.ChildCount, &item.DescendantCount, &item.HeatScore, &item.Author.internalID, &item.Author.ID, &item.Author.Username,
			&item.Author.AvatarURL, &item.Author.showOnline, &parentAuthor, &parentBody, &parentStatus,
			&item.CreatedAt, &item.UpdatedAt, &item.PinnedAt, &watchID, &watchStatus, &mutedUntil, &mutedForever,
			&watchUnread, &watchedReplies, &reactionNames, &reactionCounts, &selectedReactions); err != nil {
			return nil, err
		}
		item.ParentID = parentID
		item.internalID = numericID
		item.RootID = rootID
		item.Deleted = status == "deleted"
		item.Pinned = item.PinnedAt != nil
		if item.Deleted {
			item.Body = ""
		}
		item.HasMoreReplies = item.ChildCount > 0 && (!includeTree || item.Depth >= maxDepth)
		if parentID != "" {
			item.Parent = &commentParentPreview{
				ID: parentID, AuthorName: parentAuthor, BodySummary: plainCommentSummary(parentBody, 80),
				Deleted: parentStatus == "deleted",
			}
		}
		if watchID != "" {
			item.CurrentUserWatch = &commentWatchState{
				ID: watchID, Active: watchStatus == "active", MutedUntil: mutedUntil,
				MutedForever: mutedForever, UnreadCount: watchUnread, WatchedReplies: watchedReplies,
			}
		}
		for index, reaction := range reactionNames {
			if index < len(reactionCounts) {
				item.Reactions[reaction] = int(reactionCounts[index])
			}
		}
		item.UserReactions = selectedReactions
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) annotateCommentPermissions(ctx context.Context, items []commentResponse, claims security.Claims) error {
	if len(items) == 0 {
		return nil
	}
	commentIDs := make([]string, 0, len(items))
	itemsByID := make(map[string]*commentResponse, len(items))
	for index := range items {
		commentIDs = append(commentIDs, items[index].ID)
		itemsByID[items[index].ID] = &items[index]
	}

	type permissionContext struct {
		CommentID      string
		AuthorID       int64
		TargetType     string
		TargetID       int64
		TargetVersion  *int64
		ProjectID      string
		TargetAuthorID int64
		TargetKind     string
		Root           bool
		AcceptedAnswer bool
	}
	rows, err := s.db.Query(ctx, `select comment.public_id,comment.author_id,
		comment.target_type,comment.target_id,comment.target_version_id,
		coalesce(direct_mod.project_code,direct_modpack.public_id,direct_simple.public_id,resource_mod.project_code,''),coalesce(post.author_id,0),coalesce(post.kind,''),comment.parent_id is null,
		coalesce(post.accepted_comment_id=comment.id,false)
		from comments comment
		left join mods direct_mod on comment.target_type='mod' and direct_mod.id=comment.target_id
		left join modpacks direct_modpack on comment.target_type='modpack' and direct_modpack.id=comment.target_id
		left join simple_projects direct_simple on comment.target_type=direct_simple.project_type and direct_simple.id=comment.target_id
		left join mod_content_versions resource_version
			on comment.target_type='mod_resource' and resource_version.id=comment.target_version_id
		left join mods resource_mod on resource_mod.id=resource_version.mod_id
		left join community_posts post on comment.target_type='community_post' and post.id=comment.target_id
		where comment.public_id=any($1::text[])`, commentIDs)
	if err != nil {
		return err
	}
	contexts := make([]permissionContext, 0, len(items))
	authorSet := make(map[int64]struct{}, len(items))
	for rows.Next() {
		var value permissionContext
		if err = rows.Scan(&value.CommentID, &value.AuthorID, &value.TargetType, &value.TargetID, &value.TargetVersion,
			&value.ProjectID, &value.TargetAuthorID, &value.TargetKind, &value.Root, &value.AcceptedAnswer); err != nil {
			rows.Close()
			return err
		}
		contexts = append(contexts, value)
		authorSet[value.AuthorID] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	authorIDs := make([]int64, 0, len(authorSet))
	for authorID := range authorSet {
		authorIDs = append(authorIDs, authorID)
	}
	authorPermissions, err := s.resolveUsersRootPermissions(ctx, authorIDs)
	if err != nil {
		return err
	}
	targetIdentities := make([]commentTargetIdentity, 0, len(contexts))
	for _, value := range contexts {
		targetIdentities = append(targetIdentities, newCommentTargetIdentity(value.TargetType, value.TargetID, value.TargetVersion))
	}
	targetOwnerBlocks, err := s.commentTargetOwnersBlockUser(ctx, targetIdentities, claims.Subject)
	if err != nil {
		return err
	}

	for _, value := range contexts {
		item := itemsByID[value.CommentID]
		if item == nil {
			continue
		}
		if value.ProjectID != "" {
			item.Author.ProjectRole = commentProjectRoleFromPermissions(
				authorPermissions[value.AuthorID].Permissions,
				value.ProjectID,
			)
		}
		if claims.Subject == 0 {
			continue
		}
		moderator := claimsAllow(claims, "comment.moderate") ||
			(value.ProjectID != "" && claimsAllow(claims, "project.comment.moderate."+value.ProjectID))
		pinModerator := claimsAllow(claims, "comment.pin") ||
			(value.ProjectID != "" && claimsAllow(claims, "project.comment.pin."+value.ProjectID))
		postAuthorModerator := communityPostAuthorModeratesComments(value.TargetKind) && value.TargetAuthorID == claims.Subject
		own := claims.Subject == value.AuthorID
		targetOwnerBlocksViewer := targetOwnerBlocks[newCommentTargetIdentity(value.TargetType, value.TargetID, value.TargetVersion)]
		item.CanEdit = !item.Deleted && (moderator || own && claimsAllow(claims, "comment.edit.own"))
		item.CanDelete = !item.Deleted && !value.AcceptedAnswer && (moderator || postAuthorModerator || own && claimsAllow(claims, "comment.delete.own"))
		item.CanPin = value.Root && !item.Deleted && (pinModerator || postAuthorModerator)
		item.CanReply = !item.Deleted && claimsAllow(claims, "comment.create") && !targetOwnerBlocksViewer
		item.CanReact = !item.Deleted && claimsAllow(claims, "comment.react")
		item.CanReport = !item.Deleted && !own && claimsAllow(claims, "report.create")
		item.CanWatch = claimsAllow(claims, "comment.watch")
	}
	return nil
}

func communityPostAuthorModeratesComments(kind string) bool {
	return kind == "tutorial" || kind == "discussion"
}

func commentProjectRoleFromPermissions(permissions []security.PermissionRule, projectID string) string {
	identityPermissions := make([]security.PermissionRule, 0, len(permissions))
	for _, permission := range permissions {
		// Administrative wildcards authorize actions, but must not assert that an
		// administrator belongs to every project. Identity badges require an
		// explicit project identity permission or its dedicated wildcard.
		if permission.Code == "admin.*" || permission.Code == "*" {
			continue
		}
		identityPermissions = append(identityPermissions, permission)
	}
	if permissionRulesAllow(identityPermissions, "project.comment.role.developer."+projectID) {
		return "developer"
	}
	if permissionRulesAllow(identityPermissions, "project.comment.role.editor."+projectID) {
		return "editor"
	}
	return ""
}

func (s *Server) resolveCommentTarget(ctx context.Context, targetType, targetKey string, claims security.Claims) (commentTargetInfo, error) {
	return resolveCommentTargetWithQueryer(ctx, s.db, targetType, targetKey, claims)
}

type commentTargetQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type visibleCommentInfo struct {
	ID     int64
	RootID int64
	Status string
	Target commentTargetInfo
}

func (s *Server) resolveVisibleComment(ctx context.Context, publicID string, claims security.Claims) (visibleCommentInfo, error) {
	return resolveVisibleCommentWithQueryer(ctx, s.db, publicID, claims)
}

func resolveVisibleCommentWithQueryer(ctx context.Context, queryer commentTargetQueryer, publicID string, claims security.Claims) (visibleCommentInfo, error) {
	var result visibleCommentInfo
	var targetType string
	var targetID int64
	var targetVersionID *int64
	err := queryer.QueryRow(ctx, `select id,coalesce(root_id,id),status,target_type,target_id,target_version_id
		from comments where public_id=$1 and status in ('published','deleted')`,
		strings.ToLower(strings.TrimSpace(publicID))).Scan(
		&result.ID, &result.RootID, &result.Status, &targetType, &targetID, &targetVersionID,
	)
	if err != nil {
		return visibleCommentInfo{}, err
	}
	result.Target, err = resolveCommentTargetByInternalWithQueryer(ctx, queryer, targetType, targetID, targetVersionID, claims)
	if err != nil {
		return visibleCommentInfo{}, err
	}
	return result, nil
}

func (s *Server) resolveVisibleCommentWatch(ctx context.Context, watchPublicID string, userID int64, claims security.Claims) (visibleCommentInfo, error) {
	var commentPublicID string
	err := s.db.QueryRow(ctx, `select comment.public_id from comment_watches watch
		join comments comment on comment.id=watch.comment_id
		where watch.public_id=$1 and watch.user_id=$2 and watch.status='active'
		  and comment.status in ('published','deleted')`,
		strings.ToLower(strings.TrimSpace(watchPublicID)), userID).Scan(&commentPublicID)
	if err != nil {
		return visibleCommentInfo{}, err
	}
	return s.resolveVisibleComment(ctx, commentPublicID, claims)
}

func resolveCommentTargetWithQueryer(ctx context.Context, queryer commentTargetQueryer, targetType, targetKey string, claims security.Claims) (commentTargetInfo, error) {
	targetType = strings.ToLower(strings.TrimSpace(targetType))
	targetKey = strings.ToLower(strings.TrimSpace(targetKey))
	info := commentTargetInfo{Type: targetType, Key: targetKey}
	viewerID := claims.Subject
	moderator := claimsAllow(claims, "comment.moderate") || claimsAllow(claims, "admin.*")
	switch targetType {
	case "mod":
		err := queryer.QueryRow(ctx, `select mod.id,mod.primary_name,route.canonical_path
			from mods mod join public_routes route on route.public_id=mod.project_code and route.entity_type='mod'
			where mod.project_code=$1 and (mod.review_status='approved' or mod.submitted_by=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "modpack":
		err := queryer.QueryRow(ctx, `select pack.id,pack.primary_name,route.canonical_path
			from modpacks pack join public_routes route on route.public_id=pack.public_id and route.entity_type='modpack'
			where pack.public_id=$1 and (pack.review_status='approved' or pack.submitted_by=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		err := queryer.QueryRow(ctx, `select project.id,project.primary_name,route.canonical_path
			from simple_projects project join public_routes route
				on route.public_id=project.public_id and route.entity_type=project.project_type
			where project.public_id=$1 and project.project_type=$2
			  and (project.review_status='approved' or project.submitted_by=$3 or $4)`,
			targetKey, targetType, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "blueprint":
		err := queryer.QueryRow(ctx, `select blueprint.id,blueprint.title,route.canonical_path
			from blueprints blueprint join public_routes route on route.public_id=blueprint.public_id and route.entity_type='blueprint'
			where blueprint.public_id=$1 and blueprint.status<>'deleted'
			  and (blueprint.review_status in ('not_required','approved') or blueprint.owner_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "skin":
		err := queryer.QueryRow(ctx, `select asset.id,asset.display_name,route.canonical_path
			from skin_assets asset join public_routes route on route.public_id=asset.public_id and route.entity_type='skin'
			where asset.public_id=$1 and asset.status='active'
			  and ((asset.visibility in ('public','unlisted') and asset.review_status='approved') or asset.owner_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "creator":
		err := queryer.QueryRow(ctx, `select creator.id,creator.name,
			case when creator.kind='team' then '/teams/' else '/authors/' end||creator.public_id
			from creators creator where creator.public_id=$1
			  and (creator.review_status='approved' or creator.created_by=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "community_post":
		err := queryer.QueryRow(ctx, `select post.id,post.title,route.canonical_path
			from community_posts post join public_routes route on route.public_id=post.public_id and route.entity_type='community_post'
			where post.public_id=$1 and post.status='active'
			  and (post.review_status='approved' or post.author_id=$2 or $3)`, targetKey, viewerID, moderator).
			Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "ban_record":
		err := queryer.QueryRow(ctx, `select ban.id,'小黑屋 · '||ban.username_snapshot,'/site-affairs/blackroom/'||ban.public_id
			from ban_records ban where ban.public_id=$1`, targetKey).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "player_profile":
		err := queryer.QueryRow(ctx, `select profile.id,profile.name,'/players/'||profile.public_id
			from player_profiles profile where profile.public_id=$1 and profile.status='active'
			  and (profile.visibility in ('public','unlisted') or profile.user_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "tag":
		err := queryer.QueryRow(ctx, `select entity.id,coalesce(nullif(localization.name,''),'#'||definition.canonical_id),
			'/mods-tag?publicId='||entity.public_id
			from catalog_entities entity join catalog_tags definition on definition.entity_id=entity.id
			left join lateral (select name from content_localizations where catalog_entity_id=entity.id
				and name<>'' order by case locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) localization on true
			where entity.public_id=$1 and entity.entity_type='tag' and entity.status='active'`,
			targetKey).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "recipe_type":
		err := queryer.QueryRow(ctx, `select entity.id,coalesce(nullif(localization.name,''),definition.canonical_id),
			'/recipe-types?publicId='||entity.public_id
			from catalog_entities entity join recipe_types definition on definition.entity_id=entity.id
			left join lateral (select name from content_localizations where catalog_entity_id=entity.id
				and name<>'' order by case locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) localization on true
			where entity.public_id=$1 and entity.entity_type='recipe_type' and entity.status='active'`,
			targetKey).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "mod_resource":
		parts := strings.Split(targetKey, "~")
		if len(parts) != 2 || len(parts[0]) != 9 || len(parts[1]) != 9 {
			return info, pgx.ErrNoRows
		}
		var siteID, resourcePublicID, versionPublicID, canonicalID, versionLabel, localizedName string
		var versionInternalID int64
		err := queryer.QueryRow(ctx, `select resource.entity_id,version.id,mod.slug,entity.public_id,version.public_id,resource.canonical_id,
			version.label,coalesce((select name from mod_resource_version_detail_localizations localization
				where localization.resource_id=resource.entity_id and localization.version_id=version.id
				and localization.name<>'' order by case localization.locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1),'')
			from catalog_entities entity
			join game_resources resource on resource.entity_id=entity.id
			join mod_resource_bindings binding on binding.resource_id=resource.entity_id
			join mods mod on mod.id=binding.mod_id
			join mod_content_versions version on version.mod_id=mod.id and version.public_id=$2
			join mod_resource_version_details detail on detail.resource_id=resource.entity_id and detail.version_id=version.id
			where entity.public_id=$1 and entity.status='active' and detail.status='active'
			  and version.status='active' and (mod.review_status='approved' or mod.submitted_by=$3 or $4)`,
			parts[0], parts[1], viewerID, moderator).Scan(
			&info.InternalID, &versionInternalID, &siteID, &resourcePublicID, &versionPublicID, &canonicalID, &versionLabel, &localizedName,
		)
		if err != nil {
			return info, err
		}
		info.Title = localizedName
		if info.Title == "" {
			info.Title = canonicalID
		}
		if versionLabel != "" {
			info.Title += " · " + versionLabel
		}
		info.VersionID = &versionInternalID
		info.URL = "/mods/" + siteID + "/resources/" + resourcePublicID + "?version=" + versionPublicID
		return info, nil
	default:
		return info, pgx.ErrNoRows
	}
}

func (s *Server) resolveCommentTargetByInternal(ctx context.Context, targetType string, targetID int64, targetVersionID *int64, claims security.Claims) (commentTargetInfo, error) {
	return resolveCommentTargetByInternalWithQueryer(ctx, s.db, targetType, targetID, targetVersionID, claims)
}

func resolveCommentTargetByInternalWithQueryer(ctx context.Context, queryer commentTargetQueryer, targetType string, targetID int64, targetVersionID *int64, claims security.Claims) (commentTargetInfo, error) {
	var targetKey string
	if targetType == "mod_resource" {
		if targetVersionID == nil {
			return commentTargetInfo{}, pgx.ErrNoRows
		}
		var resourcePublicID, versionPublicID string
		err := queryer.QueryRow(ctx, `select entity.public_id,version.public_id
			from catalog_entities entity
			join mod_content_versions version on version.id=$2
			where entity.id=$1`, targetID, *targetVersionID).Scan(&resourcePublicID, &versionPublicID)
		if err != nil {
			return commentTargetInfo{}, err
		}
		targetKey = resourcePublicID + "~" + versionPublicID
	} else if targetType == "ban_record" {
		if err := queryer.QueryRow(ctx, `select public_id from ban_records where id=$1`, targetID).Scan(&targetKey); err != nil {
			return commentTargetInfo{}, err
		}
	} else {
		if err := queryer.QueryRow(ctx, `select public_id from public_routes
			where entity_type=$1 and internal_id=$2`, targetType, targetID).Scan(&targetKey); err != nil {
			return commentTargetInfo{}, err
		}
	}
	return resolveCommentTargetWithQueryer(ctx, queryer, targetType, targetKey, claims)
}

func (s *Server) commentIDForTarget(ctx context.Context, publicID string, target commentTargetInfo) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `select id from comments where public_id=$1 and target_type=$2 and target_id=$3
		and target_version_id is not distinct from $4 and status in ('published','deleted')`,
		publicID, target.Type, target.InternalID, target.VersionID).Scan(&id)
	return id, err
}

func (s *Server) queryCommentWatch(ctx context.Context, userID, commentID int64) (commentWatchState, error) {
	return queryCommentWatchWithQueryer(ctx, s.db, userID, commentID)
}

func queryCommentWatchWithQueryer(ctx context.Context, queryer commentTargetQueryer, userID, commentID int64) (commentWatchState, error) {
	var state commentWatchState
	var status string
	err := queryer.QueryRow(ctx, `select public_id,status,muted_until,muted_forever,unread_count,watched_reply_count
		from comment_watches where user_id=$1 and comment_id=$2`, userID, commentID).Scan(
		&state.ID, &status, &state.MutedUntil, &state.MutedForever, &state.UnreadCount, &state.WatchedReplies,
	)
	state.Active = status == "active"
	return state, err
}

const maxActiveCommentWatches = 2_000

var errCommentWatchLimitExceeded = errors.New("watch limit exceeded")

func (s *Server) ensureCommentWatch(ctx context.Context, userID, commentID int64) (commentWatchState, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return commentWatchState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(
		hashtextextended('comment-watch-quota:'||$1::bigint::text,0))`, userID); err != nil {
		return commentWatchState{}, err
	}
	state, err := queryCommentWatchWithQueryer(ctx, tx, userID, commentID)
	if err == nil && state.Active {
		if err = tx.Commit(ctx); err != nil {
			return commentWatchState{}, err
		}
		return state, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return commentWatchState{}, err
	}
	var active int
	if err = tx.QueryRow(ctx, `select count(*) from (
		select 1 from comment_watches where user_id=$1 and status='active' limit $2
	) active_watches`, userID, maxActiveCommentWatches).Scan(&active); err != nil {
		return commentWatchState{}, err
	}
	if active >= maxActiveCommentWatches {
		return commentWatchState{}, errCommentWatchLimitExceeded
	}
	_, err = tx.Exec(ctx, `insert into comment_watches(user_id,comment_id)
		values($1,$2) on conflict(user_id,comment_id) do update set status='active',
		muted_until=null,muted_forever=false,unread_count=0,watched_reply_count=0,
		last_activity_at=now(),last_read_comment_id=null,created_at=now(),updated_at=now(),cancelled_at=null`,
		userID, commentID)
	if err != nil {
		return commentWatchState{}, err
	}
	state, err = queryCommentWatchWithQueryer(ctx, tx, userID, commentID)
	if err != nil {
		return commentWatchState{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return commentWatchState{}, err
	}
	return state, nil
}

func isPureCY(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	runes := []rune(value)
	if len(runes) > 0 && strings.ContainsRune("。.!！", runes[len(runes)-1]) {
		value = strings.TrimSpace(string(runes[:len(runes)-1]))
	}
	return value == "cy"
}

func plainCommentSummary(value string, maximum int) string {
	value = strings.TrimSpace(value)
	value = commentMarkdownLinkPattern.ReplaceAllString(value, "$1")
	value = commentHTMLTagPattern.ReplaceAllString(value, "")
	for _, token := range []string{"#", "*", "_", "`", ">", "[", "]", "(", ")", "~"} {
		value = strings.ReplaceAll(value, token, "")
	}
	value = strings.Join(strings.Fields(value), " ")
	return truncateRunes(value, maximum)
}

func truncateRunes(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum]) + "…"
}

func nonNegativeInt(value string) int {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number < 0 {
		return 0
	}
	return number
}

func (s *Server) enqueueOrCreateCommentWatchNotification(ctx context.Context, recipientID, actorID int64, title, body string, data map[string]any) {
	blocked, err := s.userBlocksActor(ctx, recipientID, actorID)
	if err != nil || blocked {
		return
	}
	event := notificationEvent{
		Action: "comment_watch", RecipientID: recipientID, ActorID: actorID,
		Kind: "comment_watch_reply", Title: title, Body: body, SourceLocale: "zh-CN", Data: data,
	}
	if err = s.enqueueNotificationTask(ctx, event); err != nil {
		log.Printf("queue comment watch notification recipient_id=%d: %v", recipientID, err)
	}
}
