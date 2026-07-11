package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var allowedCommentReactions = stringSet("thumbs_up", "thumbs_down", "laugh", "hooray", "confused", "heart", "rocket", "eyes")

type createModCommentRequest struct {
	Body     string `json:"body"`
	ParentID *int64 `json:"parentId"`
}

type reactToModCommentRequest struct {
	Reaction string `json:"reaction"`
}

type modCommentAuthor struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
	ProjectRole string `json:"projectRole,omitempty"`
}

type modCommentResponse struct {
	ID            int64            `json:"id"`
	ParentID      *int64           `json:"parentId,omitempty"`
	RootID        *int64           `json:"rootId,omitempty"`
	Body          string           `json:"body"`
	Author        modCommentAuthor `json:"author"`
	Reactions     map[string]int   `json:"reactions"`
	UserReactions []string         `json:"userReactions"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

func (s *Server) modComments(w http.ResponseWriter, r *http.Request) {
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
	if r.Method == http.MethodGet {
		items, listErr := s.queryModComments(r, identity.ID, identity.UniqueID, claims.Subject)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "读取评论失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
		return
	}
	var req createModCommentRequest
	if err = decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" || utf8.RuneCountInString(req.Body) > 10000 {
		writeError(w, http.StatusBadRequest, "评论不能为空且不能超过 10000 个字符")
		return
	}
	var rootID *int64
	var recipientID int64
	if req.ParentID != nil {
		var parentRoot *int64
		err = s.db.QueryRow(r.Context(), `select root_id,user_id from mod_comments where id=$1 and mod_id=$2 and status='visible'`, *req.ParentID, identity.ID).Scan(&parentRoot, &recipientID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "回复的评论不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取父评论失败")
			return
		}
		rootID = parentRoot
		if rootID == nil {
			rootID = req.ParentID
		}
	}
	var id int64
	err = s.db.QueryRow(
		r.Context(),
		`insert into mod_comments (mod_id,user_id,parent_id,root_id,body) values ($1,$2,$3,$4,$5) returning id`,
		identity.ID, claims.Subject, req.ParentID, rootID, req.Body,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发布评论失败")
		return
	}
	if req.ParentID != nil && recipientID != claims.Subject {
		s.enqueueOrCreateDirectNotification(r.Context(), recipientID, claims.Subject, "reply_mention", "评论收到回复", truncateRunes(req.Body, 160), map[string]any{"modId": identity.ID, "modSiteId": identity.SiteID, "commentId": id, "parentId": *req.ParentID})
	}
	items, _ := s.queryModComments(r, identity.ID, identity.UniqueID, claims.Subject)
	for _, item := range items {
		if item.ID == id {
			writeJSON(w, http.StatusCreated, item)
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *Server) reactToModComment(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	commentID, err := strconv.ParseInt(r.PathValue("commentId"), 10, 64)
	if err != nil || commentID <= 0 {
		writeError(w, http.StatusBadRequest, "评论编号不正确")
		return
	}
	var req reactToModCommentRequest
	if err = decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Reaction = strings.TrimSpace(req.Reaction)
	if !allowedCommentReactions[req.Reaction] {
		writeError(w, http.StatusBadRequest, "表态类型不正确")
		return
	}
	claims := currentClaims(r)
	var authorID, modID int64
	var storedSiteID string
	err = s.db.QueryRow(r.Context(), `select c.user_id,c.mod_id,m.slug from mod_comments c join mods m on m.id=c.mod_id where c.id=$1 and m.slug=$2 and c.status='visible'`, commentID, siteID).Scan(&authorID, &modID, &storedSiteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "评论不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论失败")
		return
	}
	var exists bool
	_ = s.db.QueryRow(r.Context(), `select exists(select 1 from mod_comment_reactions where comment_id=$1 and user_id=$2 and reaction=$3)`, commentID, claims.Subject, req.Reaction).Scan(&exists)
	active := !exists
	if exists {
		_, err = s.db.Exec(r.Context(), `delete from mod_comment_reactions where comment_id=$1 and user_id=$2 and reaction=$3`, commentID, claims.Subject, req.Reaction)
	} else {
		_, err = s.db.Exec(r.Context(), `insert into mod_comment_reactions (comment_id,user_id,reaction) values ($1,$2,$3)`, commentID, claims.Subject, req.Reaction)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新评论表态失败")
		return
	}
	if active && authorID != claims.Subject {
		s.enqueueOrCreateDirectNotification(r.Context(), authorID, claims.Subject, "review", "评论收到新表态", fmt.Sprintf("有人对你的评论作出了 %s 表态。", req.Reaction), map[string]any{"modId": modID, "modSiteId": storedSiteID, "commentId": commentID, "reaction": req.Reaction})
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": active})
}

func (s *Server) queryModComments(r *http.Request, modID int64, uniqueID string, viewerID int64) ([]modCommentResponse, error) {
	rows, err := s.db.Query(r.Context(), `select c.id,c.parent_id,c.root_id,c.body,c.user_id,u.username,u.display_name,u.avatar_url,c.created_at,c.updated_at from mod_comments c join users u on u.id=c.user_id where c.mod_id=$1 and c.status='visible' order by c.created_at,c.id limit 1000`, modID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modCommentResponse, 0)
	byID := map[int64]int{}
	for rows.Next() {
		var item modCommentResponse
		item.Reactions = map[string]int{}
		item.UserReactions = []string{}
		if err = rows.Scan(&item.ID, &item.ParentID, &item.RootID, &item.Body, &item.Author.ID, &item.Author.Username, &item.Author.DisplayName, &item.Author.AvatarURL, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
		byID[item.ID] = len(items) - 1
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	reactionRows, err := s.db.Query(r.Context(), `select r.comment_id,r.reaction,count(*),bool_or(r.user_id=$2) from mod_comment_reactions r join mod_comments c on c.id=r.comment_id where c.mod_id=$1 group by r.comment_id,r.reaction`, modID, viewerID)
	if err != nil {
		return nil, err
	}
	defer reactionRows.Close()
	for reactionRows.Next() {
		var commentID int64
		var reaction string
		var count int
		var selected bool
		if err = reactionRows.Scan(&commentID, &reaction, &count, &selected); err != nil {
			return nil, err
		}
		if index, ok := byID[commentID]; ok {
			items[index].Reactions[reaction] = count
			if selected {
				items[index].UserReactions = append(items[index].UserReactions, reaction)
			}
		}
	}
	if err = reactionRows.Err(); err != nil {
		return nil, err
	}
	projectRoles := make(map[int64]string)
	for index := range items {
		userID := items[index].Author.ID
		role, resolved := projectRoles[userID]
		if !resolved {
			switch {
			case s.userHasPermission(r.Context(), userID, "project.owner."+uniqueID):
				role = "owner"
			case s.userHasPermission(r.Context(), userID, "project.editor."+uniqueID):
				role = "editor"
			}
			projectRoles[userID] = role
		}
		items[index].Author.ProjectRole = role
	}
	return items, nil
}

func truncateRunes(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum]) + "…"
}
