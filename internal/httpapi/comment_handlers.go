package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/security"
)

var allowedCommentReactions = stringSet("thumbs_up", "thumbs_down", "laugh", "hooray", "confused", "heart", "rocket", "eyes")
var commentMarkdownLinkPattern = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]+\)`)
var commentHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)

const (
	maxCommentMarkdownRunes   = 10_000
	commentIdempotencyLockSQL = `select pg_advisory_xact_lock(
		hashtextextended($1::text,0))`
)

type commentTargetInfo struct {
	Type       string `json:"type"`
	Key        string `json:"key"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	InternalID int64  `json:"-"`
	VersionID  *int64 `json:"-"`
}

type commentAuthor struct {
	ID           string             `json:"id"`
	Username     string             `json:"username"`
	AvatarURL    string             `json:"avatarUrl"`
	OnlineStatus publicOnlineStatus `json:"onlineStatus"`
	ProjectRole  string             `json:"projectRole,omitempty"`
	internalID   int64
	showOnline   bool
}

type commentParentPreview struct {
	ID          string `json:"id"`
	AuthorName  string `json:"authorName"`
	BodySummary string `json:"bodySummary"`
	Deleted     bool   `json:"deleted"`
}

type commentWatchState struct {
	ID             string     `json:"id"`
	Active         bool       `json:"active"`
	MutedUntil     *time.Time `json:"mutedUntil,omitempty"`
	MutedForever   bool       `json:"mutedForever"`
	UnreadCount    int        `json:"unreadCount"`
	WatchedReplies int        `json:"watchedReplies"`
}

type commentResponse struct {
	ID               string                `json:"id"`
	FloorNumber      *int64                `json:"floorNumber"`
	ParentID         string                `json:"parentId,omitempty"`
	RootID           string                `json:"rootId,omitempty"`
	Depth            int                   `json:"depth"`
	Body             string                `json:"body"`
	Deleted          bool                  `json:"deleted"`
	Author           commentAuthor         `json:"author"`
	Parent           *commentParentPreview `json:"parent,omitempty"`
	Reactions        map[string]int        `json:"reactions"`
	UserReactions    []string              `json:"userReactions"`
	ChildCount       int                   `json:"childCount"`
	DescendantCount  int                   `json:"descendantCount"`
	HeatScore        float64               `json:"heatScore"`
	HasMoreReplies   bool                  `json:"hasMoreReplies"`
	CurrentUserWatch *commentWatchState    `json:"currentUserWatch,omitempty"`
	Pinned           bool                  `json:"pinned"`
	PinnedAt         *time.Time            `json:"pinnedAt,omitempty"`
	CanEdit          bool                  `json:"canEdit"`
	CanDelete        bool                  `json:"canDelete"`
	CanPin           bool                  `json:"canPin"`
	CanReply         bool                  `json:"canReply"`
	CanReact         bool                  `json:"canReact"`
	CanReport        bool                  `json:"canReport"`
	CanWatch         bool                  `json:"canWatch"`
	CreatedAt        time.Time             `json:"createdAt"`
	UpdatedAt        time.Time             `json:"updatedAt"`
	Attachments      []commentAttachment   `json:"attachments"`
	internalID       int64
}

type commentAttachment struct {
	FileID      string `json:"fileId"`
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	URL         string `json:"url,omitempty"`
}

type createCommentRequest struct {
	Body              string   `json:"body"`
	ParentID          string   `json:"parentId"`
	IdempotencyKey    string   `json:"idempotencyKey"`
	Status            string   `json:"-"`
	AttachmentFileIDs []string `json:"attachmentFileIds,omitempty"`
}

type updateCommentRequest struct {
	Body          string    `json:"body"`
	BaseUpdatedAt time.Time `json:"baseUpdatedAt"`
}

type commentEditConflict struct {
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updatedAt"`
	Deleted   bool      `json:"deleted"`
}

type commentEditConflictError struct {
	Current commentEditConflict
}

func (err *commentEditConflictError) Error() string {
	return "comment changed after the editor baseline"
}

type commentEditQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type reactToCommentRequest struct {
	Reaction string `json:"reaction"`
}

type updateCommentWatchRequest struct {
	Mute string `json:"mute"`
}

type commentWatchListItem struct {
	ID             string            `json:"id"`
	Comment        commentResponse   `json:"comment"`
	Target         commentTargetInfo `json:"target"`
	MutedUntil     *time.Time        `json:"mutedUntil,omitempty"`
	MutedForever   bool              `json:"mutedForever"`
	UnreadCount    int               `json:"unreadCount"`
	WatchedReplies int               `json:"watchedReplies"`
	CreatedAt      time.Time         `json:"createdAt"`
	LastActivityAt time.Time         `json:"lastActivityAt"`
}

type pendingWatchNotification struct {
	WatchID     int64
	RecipientID int64
	Muted       bool
}

type insertedCommentTree struct {
	ID                int64
	PublicID          string
	ParentID          *int64
	RootID            *int64
	Depth             int
	DirectRecipientID int64
}

type commentQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s *Server) visibleCommentForRequest(w http.ResponseWriter, r *http.Request, claims security.Claims) (visibleCommentInfo, bool) {
	comment, err := s.resolveVisibleComment(r.Context(), r.PathValue("commentId"), claims)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论不存在或当前不可见")
		return visibleCommentInfo{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return visibleCommentInfo{}, false
	}
	return comment, true
}

func (s *Server) commentsForTarget(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	target, err := s.resolveCommentTarget(r.Context(), r.PathValue("targetType"), r.PathValue("targetKey"), claims)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论目标不存在或当前不可见")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论目标失败")
		return
	}
	if r.Method == http.MethodPost {
		s.createComment(w, r, target)
		return
	}
	s.listTargetComments(w, r, target)
}

func (s *Server) commentFloor(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	floor, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("floor")), 10, 64)
	if err != nil || floor <= 0 || floor > 1_000_000_000 {
		writeError(w, http.StatusBadRequest, "楼层必须是有效的正整数")
		return
	}
	target, err := s.resolveCommentTarget(r.Context(), r.PathValue("targetType"), r.PathValue("targetKey"), claims)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论目标不存在或当前不可见")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论目标失败")
		return
	}
	var commentID int64
	err = s.db.QueryRow(r.Context(), `select comment.id from comments comment
		where comment.target_type=$1 and comment.target_id=$2
		  and comment.target_version_id is not distinct from $3::bigint
		  and comment.parent_id is null and comment.floor_number=$4
		  and comment.status in ('published','deleted')
		  and ($5::bigint=0 or not exists(select 1 from user_blocks block
			where block.blocker_id=$5 and block.blocked_id=comment.author_id))`,
		target.Type, target.InternalID, target.VersionID, floor, claims.Subject).Scan(&commentID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "该楼层不存在或当前不可见")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取楼层失败")
		return
	}
	items, err := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, claims)
	if err != nil || len(items) != 1 {
		writeError(w, http.StatusInternalServerError, "读取楼层失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comment": items[0], "target": target})
}

func (s *Server) listTargetComments(w http.ResponseWriter, r *http.Request, target commentTargetInfo) {
	claims := currentClaims(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 20, 50)
	sortName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	switch sortName {
	case "", "latest":
		sortName = "latest"
	case "oldest":
	case "hot":
	case "replies":
	default:
		writeError(w, http.StatusBadRequest, "评论排序方式不正确")
		return
	}
	scope := commentRootCursorScope(target, claims.Subject)
	var cursor *commentRootPageCursor
	var err error
	if rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor")); rawCursor != "" {
		cursor, err = decodeCommentRootPageCursor(rawCursor, scope, sortName)
		if err != nil {
			writeError(w, http.StatusBadRequest, "评论游标不正确")
			return
		}
	}
	query, args := buildCommentRootPageQuery(target, claims.Subject, limit, sortName, cursor)
	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	type rootPageRow struct {
		id              int64
		pinnedAt        *time.Time
		createdAt       time.Time
		hotScore        string
		descendantCount int
	}
	pageRows := make([]rootPageRow, 0, limit+1)
	for rows.Next() {
		var value rootPageRow
		if err = rows.Scan(&value.id, &value.pinnedAt, &value.createdAt, &value.hotScore, &value.descendantCount); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "读取评论失败")
			return
		}
		pageRows = append(pageRows, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	hasMore := len(pageRows) > limit
	if hasMore {
		pageRows = pageRows[:limit]
	}
	rootIDs := make([]int64, len(pageRows))
	for index := range pageRows {
		rootIDs[index] = pageRows[index].id
	}
	items, err := s.queryCommentItems(r.Context(), rootIDs, true, 3, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	total, err := s.queryCommentTargetVisibleTotal(r.Context(), target, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论总数失败")
		return
	}
	ownerBlocks, err := s.commentTargetOwnerBlocksUser(r.Context(), target, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论权限失败")
		return
	}
	nextCursor := ""
	if hasMore && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		nextCursor = encodeCommentRootPageCursor(commentRootPageCursor{
			Version: commentPageCursorVersion, Scope: scope, Sort: sortName, Pinned: last.pinnedAt != nil,
			PinnedAt: last.pinnedAt, CreatedAt: last.createdAt, HotScore: last.hotScore,
			DescendantCount: last.descendantCount, ID: last.id,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "target": target, "nextCursor": nextCursor,
		"capabilities": map[string]bool{"canCreate": claimsAllow(claims, "comment.create") && !ownerBlocks},
	})
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request, target commentTargetInfo) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.create") {
		writeError(w, http.StatusForbidden, "无权发表评论")
		return
	}
	ownerBlocks, err := s.commentTargetOwnerBlocksUser(r.Context(), target, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "检查评论权限失败")
		return
	}
	if ownerBlocks {
		writeError(w, http.StatusForbidden, "当前无法在该内容下发表评论")
		return
	}
	var request createCommentRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Body = strings.TrimSpace(request.Body)
	request.ParentID = strings.ToLower(strings.TrimSpace(request.ParentID))
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = fmt.Sprintf("comment-%d-%d", claims.Subject, time.Now().UnixNano())
	}
	if len(request.IdempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "幂等键过长")
		return
	}
	if !validCommentMarkdownSize(request.Body) {
		writeError(w, http.StatusBadRequest, "评论不能为空且不能超过 10000 个字符")
		return
	}
	request.AttachmentFileIDs = uniqueNonEmpty(request.AttachmentFileIDs)
	for index := range request.AttachmentFileIDs {
		request.AttachmentFileIDs[index] = strings.ToLower(strings.TrimSpace(request.AttachmentFileIDs[index]))
	}
	request.AttachmentFileIDs = uniqueNonEmpty(request.AttachmentFileIDs)
	if len(request.AttachmentFileIDs) > 5 {
		writeError(w, http.StatusBadRequest, "评论最多附加 5 个文件")
		return
	}
	request.Status = "published"
	if antiAbuseModerationRequired(r) {
		request.Status = "pending"
	}
	if request.ParentID != "" && isPureCY(request.Body) {
		// “CY” is a private watch command rather than a public comment action.
		if !claimsAllow(claims, "comment.watch") {
			writeError(w, http.StatusForbidden, "无权插眼评论")
			return
		}
		skipRequestActivity(r)
		commentID, err := s.commentIDForTarget(r.Context(), request.ParentID, target)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "回复目标不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取回复目标失败")
			return
		}
		state, err := s.ensureCommentWatch(r.Context(), claims.Subject, commentID)
		if err != nil {
			if errors.Is(err, errCommentWatchLimitExceeded) {
				writeError(w, http.StatusBadRequest, "最多只能同时插眼 2000 条评论")
			} else {
				writeError(w, http.StatusInternalServerError, "插眼失败")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"watchOnly": true, "watch": state})
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}
	defer tx.Rollback(r.Context())
	lockKey := commentIdempotencyLockKey(claims.Subject, request.IdempotencyKey)
	if _, err = tx.Exec(r.Context(), commentIdempotencyLockSQL, lockKey); err != nil {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}
	var existingID int64
	var existingTargetMatches bool
	if err = tx.QueryRow(r.Context(), `select coalesce(existing.id,0),coalesce(
		existing.target_type=$3::text and existing.target_id=$4::bigint
		and existing.target_version_id is not distinct from $5::bigint,false)
		from (values (1)) seed(value) left join comments existing
		  on existing.author_id=$1 and existing.idempotency_key=$2`,
		claims.Subject, request.IdempotencyKey, target.Type, target.InternalID, target.VersionID).Scan(&existingID, &existingTargetMatches); err != nil {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}
	if existingID > 0 {
		if !existingTargetMatches {
			writeAPIError(w, http.StatusConflict, "COMMENT_IDEMPOTENCY_SCOPE_CONFLICT", "幂等键已用于其他评论目标", 0, nil)
			return
		}
		// A retried idempotent request must not create a duplicate activity record.
		skipRequestActivity(r)
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "发布评论失败")
			return
		}
		items, _ := s.queryCommentItems(r.Context(), []int64{existingID}, false, 0, claims)
		if len(items) > 0 {
			writeJSON(w, http.StatusOK, items[0])
			return
		}
		var existingPublicID, existingStatus string
		if s.db.QueryRow(r.Context(), `select public_id,status from comments where id=$1`, existingID).Scan(&existingPublicID, &existingStatus) == nil {
			writeJSON(w, http.StatusOK, map[string]any{"id": existingPublicID, "status": existingStatus, "moderation": existingStatus == "pending"})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load idempotent comment result")
		return
	}
	inserted, err := insertCommentTree(r.Context(), tx, target, claims.Subject, request)
	if errors.Is(err, pgx.ErrNoRows) && request.ParentID != "" {
		writeError(w, http.StatusBadRequest, "回复目标不存在")
		return
	}
	if err != nil {
		log.Printf("create comment tree target_type=%s target_id=%d parent_public_id=%s: %v",
			target.Type, target.InternalID, request.ParentID, err)
		writeError(w, http.StatusInternalServerError, "建立评论树失败")
		return
	}
	commentID := inserted.ID
	publicID := inserted.PublicID
	parentID := inserted.ParentID
	directRecipientID := inserted.DirectRecipientID
	if err = bindCommentAttachmentsTx(r.Context(), tx, commentID, claims.Subject, request.AttachmentFileIDs); err != nil {
		if errors.Is(err, errCommentAttachmentUnavailable) {
			writeError(w, http.StatusBadRequest, "评论附件不存在、尚未上传完成或不属于当前用户")
			return
		}
		writeError(w, http.StatusInternalServerError, "绑定评论附件失败")
		return
	}
	if err = enqueueCommentLogAttachmentJobsTx(r.Context(), tx, commentID, claims.Subject, request.AttachmentFileIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "创建评论日志附件处理任务失败")
		return
	}

	watchNotifications := make([]pendingWatchNotification, 0)
	if parentID != nil {
		watchNotifications, err = recordCommentWatchReplies(r.Context(), tx, *parentID, commentID, claims.Subject)
		if err != nil {
			log.Printf("record comment watch replies comment_id=%d parent_id=%d: %v", commentID, *parentID, err)
			writeError(w, http.StatusInternalServerError, "更新插眼状态失败")
			return
		}
	}
	if request.Status == "published" {
		if err = enqueueCommentDeliveryNotificationsTx(r.Context(), tx, claims.Subject, publicID, request.Body, target, parentID, directRecipientID, watchNotifications); err != nil {
			log.Printf("persist comment delivery comment_id=%d: %v", commentID, err)
			writeError(w, http.StatusInternalServerError, "保存评论通知失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}
	annotateActivity(r, activity.ActionCreate, activity.ObjectComment, publicID, len(request.Body))
	if request.Status == "pending" {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": publicID, "status": "pending", "moderation": true})
		return
	}

	items, _ := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, claims)
	if len(items) > 0 {
		writeJSON(w, http.StatusCreated, items[0])
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": publicID})
}

func commentIdempotencyLockKey(userID int64, idempotencyKey string) string {
	return fmt.Sprintf("comment-idempotency:%d:%s", userID, idempotencyKey)
}

func insertCommentTree(ctx context.Context, tx pgx.Tx, target commentTargetInfo, authorID int64, request createCommentRequest) (insertedCommentTree, error) {
	var inserted insertedCommentTree
	err := tx.QueryRow(ctx, `with parent as materialized (
			select comment.id,coalesce(comment.root_id,comment.id) root_id,comment.depth+1 depth,comment.author_id
			from comments comment
			where $5::text<>'' and comment.public_id=$5::text
			  and comment.target_type=$1::text and comment.target_id=$2::bigint
			  and comment.target_version_id is not distinct from $3::bigint
			  and comment.status in ('published','deleted')
			for update
		), floor_counter as (
			insert into comment_floor_counters(target_type,target_id,target_version_key,last_floor)
			select $1::text,$2::bigint,coalesce($3::bigint,0),1 where $5::text=''
			on conflict(target_type,target_id,target_version_key) do update
			set last_floor=comment_floor_counters.last_floor+1,updated_at=now()
			returning last_floor
		), inserted_comment as (
			insert into comments
				(target_type,target_id,target_version_id,author_id,parent_id,root_id,depth,floor_number,body,idempotency_key,status)
			select $1::text,$2::bigint,$3::bigint,$4::bigint,
				parent.id,parent.root_id,coalesce(parent.depth,0),
				case when $5::text='' then (select last_floor from floor_counter) end,
				$6::text,$7::text,$8::text
			from (values (1)) seed(value)
			left join parent on true
			where $5::text='' or parent.id is not null
			returning id,public_id,parent_id,root_id,depth
		), inserted_path as (
			insert into comment_closure(ancestor_id,descendant_id,depth)
			select path.ancestor_id,inserted.id,path.depth+1
			from inserted_comment inserted
			join comment_closure path on path.descendant_id=inserted.parent_id
			union all
			select inserted.id,inserted.id,0 from inserted_comment inserted
			returning ancestor_id,descendant_id,depth
		), updated_counts as (
			update comments comment
			set descendant_count=comment.descendant_count+1,
				child_count=comment.child_count+case when comment.id=inserted.parent_id then 1 else 0 end
			from inserted_comment inserted
			where inserted.parent_id is not null
			  and comment.id in (select ancestor_id from inserted_path where depth>0)
			returning comment.id
		)
		select inserted.id,inserted.public_id,inserted.parent_id,inserted.root_id,inserted.depth,
			coalesce(parent.author_id,0)
		from inserted_comment inserted
		left join parent on parent.id=inserted.parent_id`,
		target.Type, target.InternalID, target.VersionID, authorID, request.ParentID, request.Body, request.IdempotencyKey, request.Status).
		Scan(&inserted.ID, &inserted.PublicID, &inserted.ParentID, &inserted.RootID, &inserted.Depth, &inserted.DirectRecipientID)
	return inserted, err
}

func recordCommentWatchReplies(ctx context.Context, tx pgx.Tx, parentID, commentID, authorID int64) ([]pendingWatchNotification, error) {
	rows, err := tx.Query(ctx, `with relevant as materialized (
			select watch.id
			from comment_watches watch
			join comment_closure path on path.ancestor_id=watch.comment_id
			where path.descendant_id=$1::bigint and watch.status='active' and watch.user_id<>$3::bigint
			  and not exists(select 1 from user_blocks block
				where block.blocker_id=watch.user_id and block.blocked_id=$3::bigint)
		), inserted_replies as (
			insert into comment_watch_replies(watch_id,comment_id)
			select relevant.id,$2::bigint from relevant
			on conflict do nothing
			returning watch_id
		), updated_watches as (
			update comment_watches watch
			set unread_count=watch.unread_count+1,
				watched_reply_count=watch.watched_reply_count+1,
				last_activity_at=now(),updated_at=now()
			from inserted_replies reply
			where watch.id=reply.watch_id
			returning watch.id,watch.user_id,
				(watch.muted_forever or (watch.muted_until is not null and watch.muted_until>now())) muted
		)
		select id,user_id,muted from updated_watches`,
		parentID, commentID, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]pendingWatchNotification, 0)
	for rows.Next() {
		var item pendingWatchNotification
		if err = rows.Scan(&item.WatchID, &item.RecipientID, &item.Muted); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) commentThread(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	visible, ok := s.visibleCommentForRequest(w, r, claims)
	if !ok {
		return
	}
	focusPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))
	scope := commentReplyCursorScope(visible.ID, claims.Subject)
	var cursor *commentReplyPageCursor
	var err error
	if rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor")); rawCursor != "" {
		cursor, err = decodeCommentReplyPageCursor(rawCursor, scope)
		if err != nil {
			writeError(w, http.StatusBadRequest, "评论分支游标不正确")
			return
		}
	}
	page, ancestorCount, replyRows, hasMore, err := s.loadCommentThreadPage(
		r.Context(), visible, focusPublicID, claims, cursor,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	payload, err := marshalBoundedCommentThreadResponse(page, ancestorCount, replyRows, hasMore, scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "评论分支响应超过安全上限")
		return
	}
	writeJSONBytes(w, http.StatusOK, payload)
}

func (s *Server) commentReplies(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	visible, ok := s.visibleCommentForRequest(w, r, claims)
	if !ok {
		return
	}
	commentID := visible.ID
	limit := boundedLimit(r.URL.Query().Get("limit"), 50, 100)
	scope := commentReplyCursorScope(commentID, claims.Subject)
	var cursor *commentReplyPageCursor
	var err error
	if rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor")); rawCursor != "" {
		cursor, err = decodeCommentReplyPageCursor(rawCursor, scope)
		if err != nil {
			writeError(w, http.StatusBadRequest, "回复游标不正确")
			return
		}
	}
	query, args := buildCommentReplyPageQuery(commentID, claims.Subject, limit, cursor)
	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取回复失败")
		return
	}
	type replyPageRow struct {
		id        int64
		createdAt time.Time
	}
	pageRows := make([]replyPageRow, 0, limit+1)
	for rows.Next() {
		var value replyPageRow
		if err = rows.Scan(&value.id, &value.createdAt); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "读取回复失败")
			return
		}
		pageRows = append(pageRows, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取回复失败")
		return
	}
	hasMore := len(pageRows) > limit
	if hasMore {
		pageRows = pageRows[:limit]
	}
	ids := make([]int64, len(pageRows))
	for index := range pageRows {
		ids[index] = pageRows[index].id
	}
	items, err := s.queryCommentItems(r.Context(), ids, false, 0, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取回复失败")
		return
	}
	nextCursor := ""
	if hasMore && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		nextCursor = encodeCommentReplyPageCursor(commentReplyPageCursor{
			Version: commentPageCursorVersion, Scope: scope, CreatedAt: last.createdAt, ID: last.id,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func (s *Server) commentItem(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	visible, ok := s.visibleCommentForRequest(w, r, claims)
	if !ok {
		return
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))
	var commentID, authorID int64
	var projectID, targetKind, previousBody string
	var targetAuthorID int64
	var acceptedAnswer bool
	if err := s.db.QueryRow(r.Context(), `select comment.id,comment.author_id,comment.body,
		coalesce(direct_mod.project_code,direct_modpack.public_id,direct_simple.public_id,resource_mod.project_code,''),coalesce(post.author_id,0),coalesce(post.kind,''),
		coalesce(post.accepted_comment_id=comment.id,false)
		from comments comment
		left join mods direct_mod on comment.target_type='mod' and direct_mod.id=comment.target_id
		left join modpacks direct_modpack on comment.target_type='modpack' and direct_modpack.id=comment.target_id
		left join simple_projects direct_simple on comment.target_type=direct_simple.project_type and direct_simple.id=comment.target_id
		left join mod_content_versions resource_version
			on comment.target_type='mod_resource' and resource_version.id=comment.target_version_id
		left join mods resource_mod on resource_mod.id=resource_version.mod_id
		left join community_posts post on comment.target_type='community_post' and post.id=comment.target_id
		where comment.id=$1`, visible.ID).Scan(&commentID, &authorID, &previousBody, &projectID, &targetAuthorID, &targetKind, &acceptedAnswer); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "评论不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "读取评论失败")
		}
		return
	}
	moderator := claimsAllow(claims, "comment.moderate") ||
		(projectID != "" && claimsAllow(claims, "project.comment.moderate."+projectID))
	postAuthorModerator := communityPostAuthorModeratesComments(targetKind) && targetAuthorID == claims.Subject
	if r.Method == http.MethodPatch && !moderator &&
		(authorID != claims.Subject || !claimsAllow(claims, "comment.edit.own")) {
		writeError(w, http.StatusForbidden, "无权修改这条评论")
		return
	}
	if r.Method == http.MethodDelete && !moderator && !postAuthorModerator &&
		(authorID != claims.Subject || !claimsAllow(claims, "comment.delete.own")) {
		writeError(w, http.StatusForbidden, "无权删除这条评论")
		return
	}
	if r.Method == http.MethodDelete && acceptedAnswer {
		writeError(w, http.StatusConflict, "an accepted answer cannot be deleted")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var request updateCommentRequest
		if decodeJSON(r, &request) != nil {
			writeError(w, http.StatusBadRequest, "请求格式不正确")
			return
		}
		request.Body = strings.TrimSpace(request.Body)
		if !validCommentMarkdownSize(request.Body) || isPureCY(request.Body) || request.BaseUpdatedAt.IsZero() {
			writeError(w, http.StatusBadRequest, "评论内容不正确")
			return
		}
		previousBody, _, err := updateCommentBodyAtVersion(r.Context(), s.db, commentID, request.Body, request.BaseUpdatedAt)
		var conflict *commentEditConflictError
		if errors.As(err, &conflict) {
			writeAPIError(w, http.StatusConflict, "COMMENT_EDIT_CONFLICT",
				"comment changed after the editor baseline", 0, conflict.Current)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "修改评论失败")
			return
		}
		addedBytes, deletedBytes := activity.MarkdownDeltaBytes(previousBody, request.Body)
		annotateActivityDelta(r, activity.ActionEdit, activity.ObjectComment, publicID, addedBytes, deletedBytes)
		items, _ := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, claims)
		if len(items) > 0 {
			writeJSON(w, http.StatusOK, items[0])
			return
		}
	case http.MethodDelete:
		tx, err := s.db.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "删除评论失败")
			return
		}
		defer tx.Rollback(r.Context())
		result, err := tx.Exec(r.Context(), `update comments set body='',status='deleted',deleted_at=now(),
			pinned_at=null,pinned_by=null,updated_at=now()
			where id=$1 and status<>'deleted'
			and not exists(select 1 from community_posts where accepted_comment_id=$1)`, commentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "删除评论失败")
			return
		}
		if result.RowsAffected() == 0 {
			writeError(w, http.StatusConflict, "已选为最佳回答的评论不能删除")
			return
		}
		if _, err = tx.Exec(r.Context(), `delete from comment_log_bindings where comment_id=$1`, commentID); err != nil {
			writeError(w, http.StatusInternalServerError, "删除评论失败")
			return
		}
		if _, err = tx.Exec(r.Context(), `delete from comment_attachments where comment_id=$1`, commentID); err != nil {
			writeError(w, http.StatusInternalServerError, "删除评论失败")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "删除评论失败")
			return
		}
		annotateActivity(r, activity.ActionDelete, activity.ObjectComment, publicID, 0)
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return
	}
}

func validCommentMarkdownSize(body string) bool {
	return body != "" && utf8.RuneCountInString(body) <= maxCommentMarkdownRunes
}

func updateCommentBodyAtVersion(ctx context.Context, queryer commentEditQueryer, commentID int64,
	body string, baseUpdatedAt time.Time) (string, time.Time, error) {
	var previousBody string
	var updatedAt time.Time
	err := queryer.QueryRow(ctx, `with current as (
		select id,body from comments
		where id=$1 and status='published' and updated_at=$3
		for update
	), updated as (
		update comments comment set body=$2,
			updated_at=greatest(clock_timestamp(),comment.updated_at+interval '1 microsecond')
		from current where comment.id=current.id
		returning current.body,comment.updated_at
	)
	select body,updated_at from updated`, commentID, body, baseUpdatedAt).Scan(&previousBody, &updatedAt)
	if err == nil {
		return previousBody, updatedAt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, err
	}
	var current commentEditConflict
	if err = queryer.QueryRow(ctx, `select body,updated_at,status='deleted' from comments where id=$1`, commentID).
		Scan(&current.Body, &current.UpdatedAt, &current.Deleted); err != nil {
		return "", time.Time{}, err
	}
	return "", time.Time{}, &commentEditConflictError{Current: current}
}

func (s *Server) commentPin(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	visible, ok := s.visibleCommentForRequest(w, r, claims)
	if !ok {
		return
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))
	var commentID int64
	var parentID *int64
	var status, projectID, targetKind string
	var targetAuthorID int64
	if err := s.db.QueryRow(r.Context(), `select comment.id,comment.parent_id,comment.status,
		coalesce(direct_mod.project_code,direct_modpack.public_id,direct_simple.public_id,resource_mod.project_code,''),coalesce(post.author_id,0),coalesce(post.kind,'')
		from comments comment
		left join mods direct_mod on comment.target_type='mod' and direct_mod.id=comment.target_id
		left join modpacks direct_modpack on comment.target_type='modpack' and direct_modpack.id=comment.target_id
		left join simple_projects direct_simple on comment.target_type=direct_simple.project_type and direct_simple.id=comment.target_id
		left join mod_content_versions resource_version
			on comment.target_type='mod_resource' and resource_version.id=comment.target_version_id
		left join mods resource_mod on resource_mod.id=resource_version.mod_id
		left join community_posts post on comment.target_type='community_post' and post.id=comment.target_id
		where comment.id=$1`, visible.ID).Scan(&commentID, &parentID, &status, &projectID, &targetAuthorID, &targetKind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "评论不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "读取评论失败")
		}
		return
	}
	if parentID != nil {
		writeError(w, http.StatusBadRequest, "只能置顶根评论")
		return
	}
	if status != "published" {
		writeError(w, http.StatusBadRequest, "当前评论不可置顶")
		return
	}
	allowed := claimsAllow(claims, "comment.pin") ||
		(projectID != "" && claimsAllow(claims, "project.comment.pin."+projectID)) ||
		(communityPostAuthorModeratesComments(targetKind) && targetAuthorID == claims.Subject)
	if !allowed {
		writeError(w, http.StatusForbidden, "无权置顶该评论")
		return
	}

	pinned := r.Method == http.MethodPut
	if pinned {
		_, err := s.db.Exec(r.Context(), `update comments set pinned_at=now(),pinned_by=$2,updated_at=now()
			where id=$1 and status='published'`, commentID, claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "置顶评论失败")
			return
		}
	} else {
		_, err := s.db.Exec(r.Context(), `update comments set pinned_at=null,pinned_by=null,updated_at=now()
			where id=$1`, commentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "取消置顶失败")
			return
		}
	}
	annotateActivity(r, activity.ActionEdit, activity.ObjectComment, publicID, 0)
	items, err := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, claims)
	if err != nil || len(items) == 0 {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	writeJSON(w, http.StatusOK, items[0])
}

func (s *Server) commentReaction(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.react") {
		writeError(w, http.StatusForbidden, "无权对评论添加表态")
		return
	}
	visible, ok := s.visibleCommentForRequest(w, r, claims)
	if !ok {
		return
	}
	commentID := visible.ID
	var err error
	reaction := strings.TrimSpace(r.URL.Query().Get("reaction"))
	if r.Method == http.MethodPut {
		var request reactToCommentRequest
		if decodeJSON(r, &request) == nil && strings.TrimSpace(request.Reaction) != "" {
			reaction = strings.TrimSpace(request.Reaction)
		}
	}
	if !allowedCommentReactions[reaction] {
		writeError(w, http.StatusBadRequest, "表态类型不正确")
		return
	}
	if r.Method == http.MethodDelete {
		_, err = s.db.Exec(r.Context(), `delete from comment_reactions where comment_id=$1 and user_id=$2 and reaction=$3`, commentID, claims.Subject, reaction)
	} else {
		_, err = s.db.Exec(r.Context(), `insert into comment_reactions(comment_id,user_id,reaction)
			values($1,$2,$3) on conflict do nothing`, commentID, claims.Subject, reaction)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新评论表态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"active": r.Method != http.MethodDelete})
}

func (s *Server) commentWatch(w http.ResponseWriter, r *http.Request) {
	// Watch ownership and state are private to the current user.
	skipRequestActivity(r)
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.watch") {
		writeError(w, http.StatusForbidden, "无权插眼评论")
		return
	}
	visible, ok := s.visibleCommentForRequest(w, r, claims)
	if !ok {
		return
	}
	commentID := visible.ID
	switch r.Method {
	case http.MethodGet:
		state, err := s.queryCommentWatch(r.Context(), claims.Subject, commentID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, commentWatchState{})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取插眼状态失败")
			return
		}
		writeJSON(w, http.StatusOK, state)
	case http.MethodPut:
		state, err := s.ensureCommentWatch(r.Context(), claims.Subject, commentID)
		if err != nil {
			if errors.Is(err, errCommentWatchLimitExceeded) {
				writeError(w, http.StatusBadRequest, "最多只能同时插眼 2000 条评论")
			} else {
				writeError(w, http.StatusInternalServerError, "插眼失败")
			}
			return
		}
		writeJSON(w, http.StatusOK, state)
	case http.MethodDelete:
		_, err := s.db.Exec(r.Context(), `update comment_watches set status='cancelled',cancelled_at=now(),
			muted_until=null,muted_forever=false,updated_at=now()
			where user_id=$1 and comment_id=$2 and status='active'`, claims.Subject, commentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "取消插眼失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"active": false})
	}
}

func (s *Server) myCommentWatches(w http.ResponseWriter, r *http.Request) {
	skipRequestActivity(r)
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.watch") {
		writeError(w, http.StatusForbidden, "无权查看评论插眼")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 30, 100)
	filter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("filter")))
	switch filter {
	case "", "all":
		filter = "all"
	case "unread":
	case "muted":
	default:
		writeError(w, http.StatusBadRequest, "插眼筛选方式不正确")
		return
	}
	sortName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	switch sortName {
	case "", "activity":
		sortName = "activity"
	case "created":
	case "unread":
	default:
		writeError(w, http.StatusBadRequest, "插眼排序方式不正确")
		return
	}
	scope := commentWatchCursorScope(claims.Subject, filter)
	var cursor *commentWatchPageCursor
	var err error
	if rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor")); rawCursor != "" {
		cursor, err = decodeCommentWatchPageCursor(rawCursor, scope, sortName)
		if err != nil {
			writeError(w, http.StatusBadRequest, "插眼游标不正确")
			return
		}
	}
	query, args := buildCommentWatchPageQuery(claims.Subject, limit, filter, sortName, cursor)
	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取我的插眼失败")
		return
	}
	type watchRow struct {
		id, commentID, targetID        int64
		publicID, targetType           string
		targetVersionID                *int64
		mutedUntil                     *time.Time
		mutedForever                   bool
		unreadCount, watchedReplyCount int
		createdAt, lastActivityAt      time.Time
	}
	values := make([]watchRow, 0, limit+1)
	for rows.Next() {
		var value watchRow
		if err = rows.Scan(&value.id, &value.publicID, &value.commentID, &value.mutedUntil, &value.mutedForever,
			&value.unreadCount, &value.watchedReplyCount, &value.createdAt, &value.lastActivityAt,
			&value.targetType, &value.targetID, &value.targetVersionID); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "读取我的插眼失败")
			return
		}
		values = append(values, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取我的插眼失败")
		return
	}
	hasMore := len(values) > limit
	if hasMore {
		values = values[:limit]
	}
	targetIdentities := make([]commentTargetIdentity, 0, len(values))
	for _, value := range values {
		targetIdentities = append(targetIdentities, newCommentTargetIdentity(value.targetType, value.targetID, value.targetVersionID))
	}
	targetsByIdentity, err := s.queryCommentTargetsByInternal(r.Context(), targetIdentities, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取插眼目标失败")
		return
	}
	commentIDs := make([]int64, 0, len(values))
	for _, value := range values {
		identity := newCommentTargetIdentity(value.targetType, value.targetID, value.targetVersionID)
		if _, visible := targetsByIdentity[identity]; visible {
			commentIDs = append(commentIDs, value.commentID)
		}
	}
	comments, err := s.queryCommentItems(r.Context(), commentIDs, false, 0, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取插眼评论失败")
		return
	}
	commentsByID := make(map[int64]commentResponse, len(comments))
	for _, comment := range comments {
		commentsByID[comment.internalID] = comment
	}
	items := make([]commentWatchListItem, 0, len(values))
	for _, value := range values {
		identity := newCommentTargetIdentity(value.targetType, value.targetID, value.targetVersionID)
		target, visible := targetsByIdentity[identity]
		if !visible {
			continue
		}
		comment, visible := commentsByID[value.commentID]
		if !visible {
			continue
		}
		items = append(items, commentWatchListItem{
			ID: value.publicID, Comment: comment, Target: target, MutedUntil: value.mutedUntil,
			MutedForever: value.mutedForever, UnreadCount: value.unreadCount,
			WatchedReplies: value.watchedReplyCount, CreatedAt: value.createdAt, LastActivityAt: value.lastActivityAt,
		})
	}
	nextCursor := ""
	if hasMore && len(values) > 0 {
		last := values[len(values)-1]
		nextCursor = encodeCommentWatchPageCursor(commentWatchPageCursor{
			Version: commentPageCursorVersion, Scope: scope, Sort: sortName, UnreadCount: last.unreadCount,
			LastActivityAt: last.lastActivityAt, CreatedAt: last.createdAt, ID: last.id,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}
