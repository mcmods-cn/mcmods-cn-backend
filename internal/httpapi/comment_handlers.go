package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
)

var allowedCommentReactions = stringSet("thumbs_up", "thumbs_down", "laugh", "hooray", "confused", "heart", "rocket", "eyes")
var commentMarkdownLinkPattern = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]+\)`)
var commentHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)

type commentTargetInfo struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type commentAuthor struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
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

func (s *Server) commentsForTarget(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	target, err := s.resolveCommentTarget(r.Context(), r.PathValue("targetType"), r.PathValue("targetKey"), claims.Subject, claims.Permissions)
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
		where c.target_type=$1 and c.target_key=$2 and c.parent_id is null
		  and c.status in ('published','deleted')
		order by `+order+` limit $3 offset $4`, target.Type, target.Key, limit+1, offset)
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
	items, err := s.queryCommentItems(r.Context(), rootIDs, true, 3, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	var total int
	_ = s.db.QueryRow(r.Context(), `select count(*) from comments
		where target_type=$1 and target_key=$2 and status in ('published','deleted')`, target.Type, target.Key).Scan(&total)
	nextCursor := ""
	if hasMore {
		nextCursor = strconv.Itoa(offset + limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "target": target, "nextCursor": nextCursor,
	})
}

func (s *Server) createComment(w http.ResponseWriter, r *http.Request, target commentTargetInfo) {
	claims := currentClaims(r)
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
	var recent int
	_ = s.db.QueryRow(r.Context(), `select count(*) from comments where author_id=$1 and created_at>now()-interval '1 minute'`, claims.Subject).Scan(&recent)
	if recent >= 20 {
		writeError(w, http.StatusTooManyRequests, "评论发布过于频繁，请稍后再试")
		return
	}
	if request.ParentID != "" && isPureCY(request.Body) {
		// “CY” is a private watch command rather than a public comment action.
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
	if err = tx.QueryRow(r.Context(), `select id from comments where author_id=$1 and idempotency_key=$2`, claims.Subject, request.IdempotencyKey).Scan(&existingID); err == nil {
		// A retried idempotent request must not create a duplicate activity record.
		skipRequestActivity(r)
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "发布评论失败")
			return
		}
		items, _ := s.queryCommentItems(r.Context(), []int64{existingID}, false, 0, claims.Subject)
		if len(items) > 0 {
			writeJSON(w, http.StatusOK, items[0])
			return
		}
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}

	var parentID, rootID *int64
	var depth int
	var directRecipientID int64
	if request.ParentID != "" {
		var parentNumericID int64
		var parentRootID *int64
		if err = tx.QueryRow(r.Context(), `select id,root_id,depth,author_id from comments
			where public_id=$1 and target_type=$2 and target_key=$3 and status in ('published','deleted') for update`,
			request.ParentID, target.Type, target.Key).Scan(&parentNumericID, &parentRootID, &depth, &directRecipientID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, "回复目标不存在")
			} else {
				writeError(w, http.StatusInternalServerError, "读取回复目标失败")
			}
			return
		}
		parentID = &parentNumericID
		if parentRootID != nil {
			rootID = parentRootID
		} else {
			rootID = &parentNumericID
		}
		depth++
	}

	var commentID int64
	var publicID string
	if err = tx.QueryRow(r.Context(), `insert into comments
		(target_type,target_key,author_id,parent_id,root_id,depth,body,idempotency_key)
		values($1,$2,$3,$4,$5,$6,$7,$8) returning id,public_id`,
		target.Type, target.Key, claims.Subject, parentID, rootID, depth, request.Body, request.IdempotencyKey,
	).Scan(&commentID, &publicID); err != nil {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}
	if parentID == nil {
		_, err = tx.Exec(r.Context(), `insert into comment_closure(ancestor_id,descendant_id,depth) values($1,$1,0)`, commentID)
	} else {
		_, err = tx.Exec(r.Context(), `insert into comment_closure(ancestor_id,descendant_id,depth)
			select ancestor_id,$1,depth+1 from comment_closure where descendant_id=$2
			union all select $1,$1,0`, commentID, *parentID)
		if err == nil {
			_, err = tx.Exec(r.Context(), `update comments set descendant_count=descendant_count+1
				where id in (select ancestor_id from comment_closure where descendant_id=$1 and depth>0)`, commentID)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `update comments set child_count=child_count+1 where id=$1`, *parentID)
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "建立评论树失败")
		return
	}

	watchNotifications := make([]pendingWatchNotification, 0)
	if parentID != nil {
		rows, queryErr := tx.Query(r.Context(), `select watch.id,watch.user_id,
			(watch.muted_forever or (watch.muted_until is not null and watch.muted_until>now()))
			from comment_watches watch
			join comment_closure path on path.ancestor_id=watch.comment_id
			where path.descendant_id=$1 and watch.status='active' and watch.user_id<>$2`,
			*parentID, claims.Subject)
		if queryErr != nil {
			writeError(w, http.StatusInternalServerError, "更新插眼状态失败")
			return
		}
		for rows.Next() {
			var pending pendingWatchNotification
			if rows.Scan(&pending.WatchID, &pending.RecipientID, &pending.Muted) == nil {
				watchNotifications = append(watchNotifications, pending)
			}
		}
		rows.Close()
		for _, pending := range watchNotifications {
			if _, err = tx.Exec(r.Context(), `insert into comment_watch_replies(watch_id,comment_id)
				values($1,$2) on conflict do nothing`, pending.WatchID, commentID); err != nil {
				writeError(w, http.StatusInternalServerError, "更新插眼状态失败")
				return
			}
			if _, err = tx.Exec(r.Context(), `update comment_watches set unread_count=unread_count+1,
				watched_reply_count=watched_reply_count+1,last_activity_at=now(),updated_at=now()
				where id=$1`, pending.WatchID); err != nil {
				writeError(w, http.StatusInternalServerError, "更新插眼状态失败")
				return
			}
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
		"targetTitle": target.Title, "url": target.URL + "#comment-" + publicID,
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
	items, _ := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, claims.Subject)
	if len(items) > 0 {
		writeJSON(w, http.StatusCreated, items[0])
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": publicID})
}

func (s *Server) commentThread(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	var id, rootID int64
	var targetType, targetKey string
	err := s.db.QueryRow(r.Context(), `select id,coalesce(root_id,id),target_type,target_key from comments
		where public_id=$1 and status in ('published','deleted')`, strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))).
		Scan(&id, &rootID, &targetType, &targetKey)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	items, err := s.queryCommentItems(r.Context(), []int64{rootID}, true, 256, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	target, err := s.resolveCommentTarget(r.Context(), targetType, targetKey, claims.Subject, claims.Permissions)
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
	items, err := s.queryCommentItems(r.Context(), ids, false, 0, claims.Subject)
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
	if err := s.db.QueryRow(r.Context(), `select id,author_id from comments where public_id=$1`, publicID).Scan(&commentID, &authorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "评论不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "读取评论失败")
		}
		return
	}
	if authorID != claims.Subject && !hasPermission(claims.Permissions, "comment.moderate") {
		writeError(w, http.StatusForbidden, "无权修改这条评论")
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
		items, _ := s.queryCommentItems(r.Context(), []int64{commentID}, false, 0, claims.Subject)
		if len(items) > 0 {
			writeJSON(w, http.StatusOK, items[0])
			return
		}
	case http.MethodDelete:
		if _, err := s.db.Exec(r.Context(), `update comments set body='',status='deleted',deleted_at=now(),updated_at=now()
			where id=$1 and status<>'deleted'`, commentID); err != nil {
			writeError(w, http.StatusInternalServerError, "删除评论失败")
			return
		}
		annotateActivity(r, activity.ActionDelete, activity.ObjectComment, publicID, 0, nil)
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return
	}
}

func (s *Server) commentReaction(w http.ResponseWriter, r *http.Request) {
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
		do update set reason=excluded.reason,detail=excluded.detail,status='pending',created_at=now(),resolved_at=null`,
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
		comment.target_type,comment.target_key
		from comment_watches watch join comments comment on comment.id=watch.comment_id
		where `+where+` order by `+order+` limit $2 offset $3`, claims.Subject, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取我的插眼失败")
		return
	}
	type watchRow struct {
		id, commentID                   int64
		publicID, targetType, targetKey string
		mutedUntil                      *time.Time
		mutedForever                    bool
		unreadCount, watchedReplyCount  int
		createdAt, lastActivityAt       time.Time
	}
	values := make([]watchRow, 0, limit+1)
	for rows.Next() {
		var value watchRow
		if rows.Scan(&value.id, &value.publicID, &value.commentID, &value.mutedUntil, &value.mutedForever,
			&value.unreadCount, &value.watchedReplyCount, &value.createdAt, &value.lastActivityAt,
			&value.targetType, &value.targetKey) == nil {
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
		comments, queryErr := s.queryCommentItems(r.Context(), []int64{value.commentID}, false, 0, claims.Subject)
		if queryErr != nil || len(comments) == 0 {
			continue
		}
		target, queryErr := s.resolveCommentTarget(r.Context(), value.targetType, value.targetKey, claims.Subject, claims.Permissions)
		if queryErr != nil {
			target = commentTargetInfo{Type: value.targetType, Key: value.targetKey, Title: value.targetKey}
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

func (s *Server) queryCommentItems(ctx context.Context, ids []int64, includeTree bool, maxDepth int, viewerID int64) ([]commentResponse, error) {
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
	rows, err := s.db.Query(ctx, fmt.Sprintf(`select c.id,c.public_id,coalesce(parent.public_id,''),
		coalesce(root.public_id,''),c.depth,c.body,c.status,c.child_count,c.descendant_count,
		author.id,author.username,author.display_name,author.avatar_url,
		coalesce(parent_author.display_name,parent_author.username,''),coalesce(parent.body,''),coalesce(parent.status,''),
		c.created_at,c.updated_at,
		coalesce(watch.public_id,''),coalesce(watch.status,''),watch.muted_until,
		coalesce(watch.muted_forever,false),coalesce(watch.unread_count,0),coalesce(watch.watched_reply_count,0)
		from comments c
		join users author on author.id=c.author_id
		left join comments parent on parent.id=c.parent_id
		left join users parent_author on parent_author.id=parent.author_id
		left join comments root on root.id=c.root_id
		left join comment_watches watch on watch.comment_id=c.id and watch.user_id=$%d
		where %s and c.status in ('published','deleted')
		order by c.created_at,c.id`, viewerIndex, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]commentResponse, 0)
	numericIDs := make([]int64, 0)
	indexByID := make(map[int64]int)
	for rows.Next() {
		var numericID int64
		var parentID, rootID, status, parentAuthor, parentBody, parentStatus string
		var watchID, watchStatus string
		var mutedUntil *time.Time
		var mutedForever bool
		var watchUnread, watchedReplies int
		item := commentResponse{Reactions: map[string]int{}, UserReactions: []string{}}
		if err = rows.Scan(&numericID, &item.ID, &parentID, &rootID, &item.Depth, &item.Body, &status,
			&item.ChildCount, &item.DescendantCount, &item.Author.ID, &item.Author.Username,
			&item.Author.DisplayName, &item.Author.AvatarURL, &parentAuthor, &parentBody, &parentStatus,
			&item.CreatedAt, &item.UpdatedAt, &watchID, &watchStatus, &mutedUntil, &mutedForever,
			&watchUnread, &watchedReplies); err != nil {
			return nil, err
		}
		item.ParentID = parentID
		item.RootID = rootID
		item.Deleted = status == "deleted"
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
		indexByID[numericID] = len(items)
		numericIDs = append(numericIDs, numericID)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	reactionRows, err := s.db.Query(ctx, `select comment_id,reaction,count(*),bool_or(user_id=$2)
		from comment_reactions where comment_id=any($1) group by comment_id,reaction`, numericIDs, viewerID)
	if err != nil {
		return nil, err
	}
	defer reactionRows.Close()
	for reactionRows.Next() {
		var commentID int64
		var reaction string
		var count int
		var selected bool
		if reactionRows.Scan(&commentID, &reaction, &count, &selected) == nil {
			if index, ok := indexByID[commentID]; ok {
				items[index].Reactions[reaction] = count
				if selected {
					items[index].UserReactions = append(items[index].UserReactions, reaction)
				}
			}
		}
	}
	return items, reactionRows.Err()
}

func (s *Server) resolveCommentTarget(ctx context.Context, targetType, targetKey string, viewerID int64, permissions []string) (commentTargetInfo, error) {
	targetType = strings.ToLower(strings.TrimSpace(targetType))
	targetKey = strings.ToLower(strings.TrimSpace(targetKey))
	info := commentTargetInfo{Type: targetType, Key: targetKey}
	moderator := hasPermission(permissions, "comment.moderate") || hasPermission(permissions, "admin.*")
	switch targetType {
	case "mod":
		return info, s.db.QueryRow(ctx, `select mod.primary_name,route.canonical_path
			from mods mod join public_routes route on route.public_id=mod.project_code and route.entity_type='mod'
			where mod.project_code=$1 and (mod.review_status='approved' or mod.created_by=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.Title, &info.URL)
	case "blueprint":
		return info, s.db.QueryRow(ctx, `select blueprint.title,route.canonical_path
			from blueprints blueprint join public_routes route on route.public_id=blueprint.public_id and route.entity_type='blueprint'
			where blueprint.public_id=$1 and blueprint.status<>'deleted'
			  and (blueprint.review_status in ('not_required','approved') or blueprint.owner_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.Title, &info.URL)
	case "skin":
		return info, s.db.QueryRow(ctx, `select asset.display_name,route.canonical_path
			from skin_assets asset join public_routes route on route.public_id=asset.public_id and route.entity_type='skin'
			where asset.public_id=$1 and asset.status='active'
			  and ((asset.visibility in ('public','unlisted') and asset.review_status='approved') or asset.owner_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.Title, &info.URL)
	case "creator":
		return info, s.db.QueryRow(ctx, `select creator.name,
			case when creator.kind='team' then '/teams/' else '/authors/' end||creator.public_id
			from creators creator where creator.public_id=$1
			  and (creator.review_status='approved' or creator.created_by=$2 or creator.claimed_by=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.Title, &info.URL)
	case "player_profile":
		return info, s.db.QueryRow(ctx, `select profile.name,'/players/'||profile.public_id
			from player_profiles profile where profile.public_id=$1 and profile.status='active'
			  and (profile.visibility in ('public','unlisted') or profile.user_id=$2 or $3)`,
			targetKey, viewerID, moderator).Scan(&info.Title, &info.URL)
	case "tag":
		return info, s.db.QueryRow(ctx, `select coalesce(nullif(localization.name,''),'#'||definition.canonical_id),
			'/mods-tag?publicId='||entity.public_id
			from catalog_entities entity join catalog_tags definition on definition.entity_id=entity.id
			left join lateral (select name from content_localizations where catalog_entity_id=entity.id
				and name<>'' order by case locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) localization on true
			where entity.public_id=$1 and entity.entity_type='tag' and entity.status='active'`,
			targetKey).Scan(&info.Title, &info.URL)
	case "recipe_type":
		return info, s.db.QueryRow(ctx, `select coalesce(nullif(localization.name,''),definition.canonical_id),
			'/recipe-types?publicId='||entity.public_id
			from catalog_entities entity join recipe_types definition on definition.entity_id=entity.id
			left join lateral (select name from content_localizations where catalog_entity_id=entity.id
				and name<>'' order by case locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) localization on true
			where entity.public_id=$1 and entity.entity_type='recipe_type' and entity.status='active'`,
			targetKey).Scan(&info.Title, &info.URL)
	case "mod_resource":
		parts := strings.Split(targetKey, "~")
		if len(parts) != 2 || len(parts[0]) != 9 || len(parts[1]) != 9 {
			return info, pgx.ErrNoRows
		}
		var siteID, resourceID, versionID, canonicalID, versionLabel, localizedName string
		err := s.db.QueryRow(ctx, `select mod.slug,entity.public_id,version.public_id,resource.canonical_id,
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
			&siteID, &resourceID, &versionID, &canonicalID, &versionLabel, &localizedName,
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
		info.URL = "/mods/" + siteID + "/resources/" + resourceID + "?version=" + versionID
		return info, nil
	default:
		return info, pgx.ErrNoRows
	}
}

func (s *Server) numericCommentID(ctx context.Context, publicID string) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `select id from comments where public_id=$1 and status in ('published','deleted')`,
		strings.ToLower(strings.TrimSpace(publicID))).Scan(&id)
	return id, err
}

func (s *Server) commentIDForTarget(ctx context.Context, publicID string, target commentTargetInfo) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `select id from comments where public_id=$1 and target_type=$2 and target_key=$3
		and status in ('published','deleted')`, publicID, target.Type, target.Key).Scan(&id)
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

func marshalCommentData(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
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
