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

type commentTargetInfo struct {
	Type       string `json:"type"`
	Key        string `json:"key"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	InternalID int64  `json:"-"`
	VersionID  *int64 `json:"-"`
}

type commentAuthor struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	AvatarURL   string `json:"avatarUrl"`
	ProjectRole string `json:"projectRole,omitempty"`
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
}

type createCommentRequest struct {
	Body           string `json:"body"`
	ParentID       string `json:"parentId"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type updateCommentRequest struct {
	Body string `json:"body"`
}

type reactToCommentRequest struct {
	Reaction string `json:"reaction"`
}

type reportCommentRequest struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
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

func (s *Server) listTargetComments(w http.ResponseWriter, r *http.Request, target commentTargetInfo) {
	claims := currentClaims(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 20, 50)
	offset := nonNegativeInt(r.URL.Query().Get("cursor"))
	sortName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	order := "c.created_at desc,c.id desc"
	switch sortName {
	case "", "latest":
	case "oldest":
		order = "c.created_at,c.id"
	case "hot":
		order = "(c.descendant_count + coalesce((select count(*) from comment_reactions reaction where reaction.comment_id=c.id),0)*2) desc,c.created_at desc,c.id desc"
	case "replies":
		order = "c.descendant_count desc,c.created_at desc,c.id desc"
	default:
		writeError(w, http.StatusBadRequest, "评论排序方式不正确")
		return
	}
	rows, err := s.db.Query(r.Context(), `select c.id from comments c
		where c.target_type=$1 and c.target_id=$2 and c.target_version_id is not distinct from $3 and c.parent_id is null
		  and c.status in ('published','deleted')
		order by (c.pinned_at is not null) desc,c.pinned_at desc,`+order+` limit $4 offset $5`, target.Type, target.InternalID, target.VersionID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	rootIDs := make([]int64, 0, limit+1)
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			rootIDs = append(rootIDs, id)
		}
	}
	rows.Close()
	hasMore := len(rootIDs) > limit
	if hasMore {
		rootIDs = rootIDs[:limit]
	}
	items, err := s.queryCommentItems(r.Context(), rootIDs, true, 3, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	var total int
	_ = s.db.QueryRow(r.Context(), `select count(*) from comments
		where target_type=$1 and target_id=$2 and target_version_id is not distinct from $3
		  and status in ('published','deleted')`, target.Type, target.InternalID, target.VersionID).Scan(&total)
	nextCursor := ""
	if hasMore {
		nextCursor = strconv.Itoa(offset + limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "target": target, "nextCursor": nextCursor,
		"capabilities": map[string]bool{"canCreate": claimsAllow(claims, "comment.create")},
	})
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request, target commentTargetInfo) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.create") {
		writeError(w, http.StatusForbidden, "无权发表评论")
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
	if request.Body == "" || utf8.RuneCountInString(request.Body) > 10000 {
		writeError(w, http.StatusBadRequest, "评论不能为空且不能超过 10000 个字符")
		return
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
			writeError(w, http.StatusInternalServerError, "插眼失败")
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
	var existingID int64
	var recent int
	if err = tx.QueryRow(r.Context(), `select
		coalesce((select id from comments where author_id=$1 and idempotency_key=$2 limit 1),0),
		(select count(*) from comments where author_id=$1 and created_at>now()-interval '1 minute')`,
		claims.Subject, request.IdempotencyKey).Scan(&existingID, &recent); err != nil {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}
	if existingID > 0 {
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
	}
	if recent >= 20 {
		writeError(w, http.StatusTooManyRequests, "评论发布过于频繁，请稍后再试")
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

	watchNotifications := make([]pendingWatchNotification, 0)
	if parentID != nil {
		watchNotifications, err = recordCommentWatchReplies(r.Context(), tx, *parentID, commentID, claims.Subject)
		if err != nil {
			log.Printf("record comment watch replies comment_id=%d parent_id=%d: %v", commentID, *parentID, err)
			writeError(w, http.StatusInternalServerError, "更新插眼状态失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}
	annotateActivity(r, activity.ActionCreate, activity.ObjectComment, publicID, len(request.Body), map[string]any{
		"targetType": target.Type,
		"targetKey":  target.Key,
		"parentId":   request.ParentID,
	})

	notificationData := map[string]any{
		"commentId": publicID, "targetType": target.Type, "targetKey": target.Key,
		"targetLabel": target.Title, "url": target.URL + "#comment-" + publicID,
	}
	if parentID != nil && directRecipientID != claims.Subject {
		s.enqueueOrCreateDirectNotification(r.Context(), directRecipientID, claims.Subject, "reply_mention",
			"评论收到回复", truncateRunes(request.Body, 160), notificationData)
	}
	notifiedWatchUsers := make(map[int64]bool)
	for _, pending := range watchNotifications {
		if pending.RecipientID == directRecipientID || pending.Muted {
			continue
		}
		if notifiedWatchUsers[pending.RecipientID] {
			continue
		}
		notifiedWatchUsers[pending.RecipientID] = true
		watchData := make(map[string]any, len(notificationData)+2)
		for key, value := range notificationData {
			watchData[key] = value
		}
		watchData["watchId"] = pending.WatchID
		watchData["replyCount"] = 1
		s.enqueueOrCreateCommentWatchNotification(r.Context(), pending.RecipientID, claims.Subject,
			"插眼的评论有了新回复", truncateRunes(request.Body, 160), watchData)
	}
	items, _ := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, claims)
	if len(items) > 0 {
		writeJSON(w, http.StatusCreated, items[0])
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": publicID})
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
		), inserted_comment as (
			insert into comments
				(target_type,target_id,target_version_id,author_id,parent_id,root_id,depth,body,idempotency_key)
			select $1::text,$2::bigint,$3::bigint,$4::bigint,
				parent.id,parent.root_id,coalesce(parent.depth,0),$6::text,$7::text
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
		target.Type, target.InternalID, target.VersionID, authorID, request.ParentID, request.Body, request.IdempotencyKey).
		Scan(&inserted.ID, &inserted.PublicID, &inserted.ParentID, &inserted.RootID, &inserted.Depth, &inserted.DirectRecipientID)
	return inserted, err
}

func recordCommentWatchReplies(ctx context.Context, tx pgx.Tx, parentID, commentID, authorID int64) ([]pendingWatchNotification, error) {
	rows, err := tx.Query(ctx, `with relevant as materialized (
			select watch.id
			from comment_watches watch
			join comment_closure path on path.ancestor_id=watch.comment_id
			where path.descendant_id=$1::bigint and watch.status='active' and watch.user_id<>$3::bigint
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
	var id, rootID int64
	var targetType string
	var targetID int64
	var targetVersionID *int64
	err := s.db.QueryRow(r.Context(), `select id,coalesce(root_id,id),target_type,target_id,target_version_id from comments
		where public_id=$1 and status in ('published','deleted')`, strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))).
		Scan(&id, &rootID, &targetType, &targetID, &targetVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	items, err := s.queryCommentItems(r.Context(), []int64{rootID}, true, 256, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	target, err := s.resolveCommentTargetByInternal(r.Context(), targetType, targetID, targetVersionID, claims)
	if err != nil {
		writeError(w, http.StatusNotFound, "评论目标不存在或当前不可见")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "focusId": r.PathValue("commentId"), "target": target})
}

func (s *Server) commentReplies(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	commentID, err := s.numericCommentID(r.Context(), r.PathValue("commentId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 50, 100)
	offset := nonNegativeInt(r.URL.Query().Get("cursor"))
	rows, err := s.db.Query(r.Context(), `select id from comments where parent_id=$1
		and status in ('published','deleted') order by created_at,id limit $2 offset $3`, commentID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取回复失败")
		return
	}
	ids := make([]int64, 0, limit+1)
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	items, err := s.queryCommentItems(r.Context(), ids, false, 0, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取回复失败")
		return
	}
	nextCursor := ""
	if hasMore {
		nextCursor = strconv.Itoa(offset + limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func (s *Server) commentItem(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))
	var commentID, authorID int64
	var projectID, targetKind string
	var targetAuthorID int64
	var acceptedAnswer bool
	if err := s.db.QueryRow(r.Context(), `select comment.id,comment.author_id,
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
		where comment.public_id=$1`, publicID).Scan(&commentID, &authorID, &projectID, &targetAuthorID, &targetKind, &acceptedAnswer); err != nil {
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
		if request.Body == "" || utf8.RuneCountInString(request.Body) > 10000 || isPureCY(request.Body) {
			writeError(w, http.StatusBadRequest, "评论内容不正确")
			return
		}
		if _, err := s.db.Exec(r.Context(), `update comments set body=$2,updated_at=now()
			where id=$1 and status='published'`, commentID, request.Body); err != nil {
			writeError(w, http.StatusInternalServerError, "修改评论失败")
			return
		}
		annotateActivity(r, activity.ActionEdit, activity.ObjectComment, publicID, len(request.Body), nil)
		items, _ := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, claims)
		if len(items) > 0 {
			writeJSON(w, http.StatusOK, items[0])
			return
		}
	case http.MethodDelete:
		result, err := s.db.Exec(r.Context(), `update comments set body='',status='deleted',deleted_at=now(),
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
		annotateActivity(r, activity.ActionDelete, activity.ObjectComment, publicID, 0, nil)
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return
	}
}

func (s *Server) commentPin(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
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
		where comment.public_id=$1`, publicID).Scan(&commentID, &parentID, &status, &projectID, &targetAuthorID, &targetKind); err != nil {
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
	annotateActivity(r, activity.ActionEdit, activity.ObjectComment, publicID, 0, map[string]any{"pinned": pinned})
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
	commentID, err := s.numericCommentID(r.Context(), r.PathValue("commentId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
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

func (s *Server) reportComment(w http.ResponseWriter, r *http.Request) {
	// Reports are private moderation input and must not appear in user activity.
	skipRequestActivity(r)
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.report") {
		writeError(w, http.StatusForbidden, "无权举报评论")
		return
	}
	commentID, err := s.numericCommentID(r.Context(), r.PathValue("commentId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	var request reportCommentRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	request.Detail = strings.TrimSpace(request.Detail)
	if request.Reason == "" || utf8.RuneCountInString(request.Detail) > 2000 {
		writeError(w, http.StatusBadRequest, "举报内容不正确")
		return
	}
	_, err = s.db.Exec(r.Context(), `insert into comment_reports(comment_id,reporter_id,reason,detail)
		values($1,$2,$3,$4) on conflict(comment_id,reporter_id)
		do update set reason=excluded.reason,detail=excluded.detail,status='pending',reviewer_id=null,
			resolution_note='',created_at=now(),updated_at=now(),resolved_at=null`,
		commentID, claims.Subject, request.Reason, request.Detail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "提交举报失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"reported": true})
}

func (s *Server) commentWatch(w http.ResponseWriter, r *http.Request) {
	// Watch ownership and state are private to the current user.
	skipRequestActivity(r)
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.watch") {
		writeError(w, http.StatusForbidden, "无权插眼评论")
		return
	}
	commentID, err := s.numericCommentID(r.Context(), r.PathValue("commentId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
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
			if strings.Contains(err.Error(), "watch limit") {
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
	offset := nonNegativeInt(r.URL.Query().Get("cursor"))
	filter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("filter")))
	where := "watch.user_id=$1 and watch.status='active'"
	switch filter {
	case "", "all":
	case "unread":
		where += " and watch.unread_count>0"
	case "muted":
		where += " and (watch.muted_forever or watch.muted_until>now())"
	default:
		writeError(w, http.StatusBadRequest, "插眼筛选方式不正确")
		return
	}
	sortName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	order := "watch.last_activity_at desc,watch.id desc"
	switch sortName {
	case "", "activity":
	case "created":
		order = "watch.created_at desc,watch.id desc"
	case "unread":
		order = "watch.unread_count desc,watch.last_activity_at desc,watch.id desc"
	default:
		writeError(w, http.StatusBadRequest, "插眼排序方式不正确")
		return
	}
	rows, err := s.db.Query(r.Context(), `select watch.id,watch.public_id,watch.comment_id,watch.muted_until,
		watch.muted_forever,watch.unread_count,watch.watched_reply_count,watch.created_at,watch.last_activity_at,
		comment.target_type,comment.target_id,comment.target_version_id
		from comment_watches watch join comments comment on comment.id=watch.comment_id
		where `+where+` order by `+order+` limit $2 offset $3`, claims.Subject, limit+1, offset)
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
		if rows.Scan(&value.id, &value.publicID, &value.commentID, &value.mutedUntil, &value.mutedForever,
			&value.unreadCount, &value.watchedReplyCount, &value.createdAt, &value.lastActivityAt,
			&value.targetType, &value.targetID, &value.targetVersionID) == nil {
			values = append(values, value)
		}
	}
	rows.Close()
	hasMore := len(values) > limit
	if hasMore {
		values = values[:limit]
	}
	items := make([]commentWatchListItem, 0, len(values))
	for _, value := range values {
		comments, queryErr := s.queryCommentItems(r.Context(), []int64{value.commentID}, false, 0, claims)
		if queryErr != nil || len(comments) == 0 {
			continue
		}
		target, queryErr := s.resolveCommentTargetByInternal(r.Context(), value.targetType, value.targetID, value.targetVersionID, claims)
		if queryErr != nil {
			target = commentTargetInfo{Type: value.targetType, InternalID: value.targetID}
		}
		items = append(items, commentWatchListItem{
			ID: value.publicID, Comment: comments[0], Target: target, MutedUntil: value.mutedUntil,
			MutedForever: value.mutedForever, UnreadCount: value.unreadCount,
			WatchedReplies: value.watchedReplyCount, CreatedAt: value.createdAt, LastActivityAt: value.lastActivityAt,
		})
	}
	nextCursor := ""
	if hasMore {
		nextCursor = strconv.Itoa(offset + limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func (s *Server) commentWatchItem(w http.ResponseWriter, r *http.Request) {
	skipRequestActivity(r)
	claims := currentClaims(r)
	if !claimsAllow(claims, "comment.watch") {
		writeError(w, http.StatusForbidden, "无权管理评论插眼")
		return
	}
	watchID := strings.ToLower(strings.TrimSpace(r.PathValue("watchId")))
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
	ossCfg := s.ossConfigFromSettings(ctx)
	for index := range items {
		items[index].Author.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, items[index].Author.AvatarURL)
		if err != nil {
			return nil, err
		}
	}
	if err = s.annotateCommentPermissions(ctx, items, claims); err != nil {
		return nil, err
	}
	return items, nil
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
	rows, err := queryer.Query(ctx, fmt.Sprintf(`select c.id,c.public_id,coalesce(parent.public_id,''),
		coalesce(root.public_id,''),c.depth,c.body,c.status,c.child_count,c.descendant_count,
		author.public_id,author.username,author.avatar_url,
		coalesce(parent_author.username,''),coalesce(parent.body,''),coalesce(parent.status,''),
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
		order by c.created_at,c.id`, viewerIndex, viewerIndex, where), args...)
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
		if err = rows.Scan(&numericID, &item.ID, &parentID, &rootID, &item.Depth, &item.Body, &status,
			&item.ChildCount, &item.DescendantCount, &item.Author.ID, &item.Author.Username,
			&item.Author.AvatarURL, &parentAuthor, &parentBody, &parentStatus,
			&item.CreatedAt, &item.UpdatedAt, &item.PinnedAt, &watchID, &watchStatus, &mutedUntil, &mutedForever,
			&watchUnread, &watchedReplies, &reactionNames, &reactionCounts, &selectedReactions); err != nil {
			return nil, err
		}
		item.ParentID = parentID
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
		ProjectID      string
		TargetAuthorID int64
		TargetKind     string
		Root           bool
		AcceptedAnswer bool
	}
	rows, err := s.db.Query(ctx, `select comment.public_id,comment.author_id,
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
		if err = rows.Scan(&value.CommentID, &value.AuthorID, &value.ProjectID, &value.TargetAuthorID, &value.TargetKind, &value.Root, &value.AcceptedAnswer); err != nil {
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
		item.CanEdit = !item.Deleted && (moderator || own && claimsAllow(claims, "comment.edit.own"))
		item.CanDelete = !item.Deleted && !value.AcceptedAnswer && (moderator || postAuthorModerator || own && claimsAllow(claims, "comment.delete.own"))
		item.CanPin = value.Root && !item.Deleted && (pinModerator || postAuthorModerator)
		item.CanReply = !item.Deleted && claimsAllow(claims, "comment.create")
		item.CanReact = !item.Deleted && claimsAllow(claims, "comment.react")
		item.CanReport = !item.Deleted && !own && claimsAllow(claims, "comment.report")
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
	if permissionRulesAllow(identityPermissions, "project.comment.role.owner."+projectID) {
		return "owner"
	}
	if permissionRulesAllow(identityPermissions, "project.comment.role.editor."+projectID) {
		return "editor"
	}
	return ""
}

func (s *Server) resolveCommentTarget(ctx context.Context, targetType, targetKey string, claims security.Claims) (commentTargetInfo, error) {
	targetType = strings.ToLower(strings.TrimSpace(targetType))
	targetKey = strings.ToLower(strings.TrimSpace(targetKey))
	info := commentTargetInfo{Type: targetType, Key: targetKey}
	viewerID := claims.Subject
	moderator := claimsAllow(claims, "comment.moderate") || claimsAllow(claims, "admin.*")
	switch targetType {
	case "mod":
		err := s.db.QueryRow(ctx, `select mod.id,mod.primary_name,route.canonical_path
			from mods mod join public_routes route on route.public_id=mod.project_code and route.entity_type='mod'
			where mod.project_code=$1 and (mod.review_status='approved' or mod.created_by=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "modpack":
		err := s.db.QueryRow(ctx, `select pack.id,pack.primary_name,route.canonical_path
			from modpacks pack join public_routes route on route.public_id=pack.public_id and route.entity_type='modpack'
			where pack.public_id=$1 and (pack.review_status='approved' or pack.created_by=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		err := s.db.QueryRow(ctx, `select project.id,project.primary_name,route.canonical_path
			from simple_projects project join public_routes route
				on route.public_id=project.public_id and route.entity_type=project.project_type
			where project.public_id=$1 and project.project_type=$2
			  and (project.review_status='approved' or project.created_by=$3 or $4)`,
			targetKey, targetType, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "blueprint":
		err := s.db.QueryRow(ctx, `select blueprint.id,blueprint.title,route.canonical_path
			from blueprints blueprint join public_routes route on route.public_id=blueprint.public_id and route.entity_type='blueprint'
			where blueprint.public_id=$1 and blueprint.status<>'deleted'
			  and (blueprint.review_status in ('not_required','approved') or blueprint.owner_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "skin":
		err := s.db.QueryRow(ctx, `select asset.id,asset.display_name,route.canonical_path
			from skin_assets asset join public_routes route on route.public_id=asset.public_id and route.entity_type='skin'
			where asset.public_id=$1 and asset.status='active'
			  and ((asset.visibility in ('public','unlisted') and asset.review_status='approved') or asset.owner_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "creator":
		err := s.db.QueryRow(ctx, `select creator.id,creator.name,
			case when creator.kind='team' then '/teams/' else '/authors/' end||creator.public_id
			from creators creator where creator.public_id=$1
			  and (creator.review_status='approved' or creator.created_by=$2 or creator.claimed_by=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "community_post":
		err := s.db.QueryRow(ctx, `select post.id,post.title,route.canonical_path
			from community_posts post join public_routes route on route.public_id=post.public_id and route.entity_type='community_post'
			where post.public_id=$1 and post.status='active'
			  and (post.review_status='approved' or post.author_id=$2 or $3)`, targetKey, viewerID, moderator).
			Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "player_profile":
		err := s.db.QueryRow(ctx, `select profile.id,profile.name,'/players/'||profile.public_id
			from player_profiles profile where profile.public_id=$1 and profile.status='active'
			  and (profile.visibility in ('public','unlisted') or profile.user_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "tag":
		err := s.db.QueryRow(ctx, `select entity.id,coalesce(nullif(localization.name,''),'#'||definition.canonical_id),
			'/mods-tag?publicId='||entity.public_id
			from catalog_entities entity join catalog_tags definition on definition.entity_id=entity.id
			left join lateral (select name from content_localizations where catalog_entity_id=entity.id
				and name<>'' order by case locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) localization on true
			where entity.public_id=$1 and entity.entity_type='tag' and entity.status='active'`,
			targetKey).Scan(&info.InternalID, &info.Title, &info.URL)
		return info, err
	case "recipe_type":
		err := s.db.QueryRow(ctx, `select entity.id,coalesce(nullif(localization.name,''),definition.canonical_id),
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
		err := s.db.QueryRow(ctx, `select resource.entity_id,version.id,mod.slug,entity.public_id,version.public_id,resource.canonical_id,
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
			  and version.status='active' and (mod.review_status='approved' or mod.created_by=$3 or $4)`,
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
	var targetKey string
	if targetType == "mod_resource" {
		if targetVersionID == nil {
			return commentTargetInfo{}, pgx.ErrNoRows
		}
		var resourcePublicID, versionPublicID string
		err := s.db.QueryRow(ctx, `select entity.public_id,version.public_id
			from catalog_entities entity
			join mod_content_versions version on version.id=$2
			where entity.id=$1`, targetID, *targetVersionID).Scan(&resourcePublicID, &versionPublicID)
		if err != nil {
			return commentTargetInfo{}, err
		}
		targetKey = resourcePublicID + "~" + versionPublicID
	} else {
		if err := s.db.QueryRow(ctx, `select public_id from public_routes
			where entity_type=$1 and internal_id=$2`, targetType, targetID).Scan(&targetKey); err != nil {
			return commentTargetInfo{}, err
		}
	}
	return s.resolveCommentTarget(ctx, targetType, targetKey, claims)
}

func (s *Server) numericCommentID(ctx context.Context, publicID string) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `select id from comments where public_id=$1 and status in ('published','deleted')`,
		strings.ToLower(strings.TrimSpace(publicID))).Scan(&id)
	return id, err
}

func (s *Server) commentIDForTarget(ctx context.Context, publicID string, target commentTargetInfo) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `select id from comments where public_id=$1 and target_type=$2 and target_id=$3
		and target_version_id is not distinct from $4 and status in ('published','deleted')`,
		publicID, target.Type, target.InternalID, target.VersionID).Scan(&id)
	return id, err
}

func (s *Server) queryCommentWatch(ctx context.Context, userID, commentID int64) (commentWatchState, error) {
	var state commentWatchState
	var status string
	err := s.db.QueryRow(ctx, `select public_id,status,muted_until,muted_forever,unread_count,watched_reply_count
		from comment_watches where user_id=$1 and comment_id=$2`, userID, commentID).Scan(
		&state.ID, &status, &state.MutedUntil, &state.MutedForever, &state.UnreadCount, &state.WatchedReplies,
	)
	state.Active = status == "active"
	return state, err
}

func (s *Server) ensureCommentWatch(ctx context.Context, userID, commentID int64) (commentWatchState, error) {
	var active int
	if err := s.db.QueryRow(ctx, `select count(*) from comment_watches where user_id=$1 and status='active'`, userID).Scan(&active); err != nil {
		return commentWatchState{}, err
	}
	state, err := s.queryCommentWatch(ctx, userID, commentID)
	if err == nil && state.Active {
		return state, nil
	}
	if active >= 2000 {
		return commentWatchState{}, errors.New("watch limit exceeded")
	}
	_, err = s.db.Exec(ctx, `insert into comment_watches(user_id,comment_id)
		values($1,$2) on conflict(user_id,comment_id) do update set status='active',
		muted_until=null,muted_forever=false,unread_count=0,watched_reply_count=0,
		last_activity_at=now(),last_read_comment_id=null,created_at=now(),updated_at=now(),cancelled_at=null`,
		userID, commentID)
	if err != nil {
		return commentWatchState{}, err
	}
	return s.queryCommentWatch(ctx, userID, commentID)
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
	event := notificationEvent{
		Action: "comment_watch", RecipientID: recipientID, ActorID: actorID,
		Kind: "comment_watch_reply", Title: title, Body: body, SourceLocale: "zh-CN", Data: data,
	}
	if s.queue != nil && s.queue.PublishTask(ctx, notificationTaskCode, event) == nil {
		return
	}
	s.enqueueOrCreateDirectNotification(ctx, recipientID, actorID, "comment_watch_reply", title, body, data)
}
