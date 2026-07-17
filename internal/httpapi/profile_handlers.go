package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type notificationSettingsRequest struct {
	EmailEnabled bool `json:"emailEnabled"`
}

func (s *Server) userOverview(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	var followers, following int64
	err := s.db.QueryRow(
		r.Context(),
		`select
		 (select count(*) from user_follows where followed_id = $1),
		 (select count(*) from user_follows where follower_id = $1)`,
		claims.Subject,
	).Scan(&followers, &following)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户主页概览失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"followers": followers,
		"following": following,
		"aiBalance": s.userAIDailyBalance(r.Context(), claims.Subject, claims.Permissions),
	})
}

func (s *Server) userProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathUserID(w, r)
	if !ok {
		return
	}
	var username, displayName, status, avatarURL, signature, profileBackgroundURL string
	var createdAt time.Time
	err := s.db.QueryRow(
		r.Context(),
		`select username, display_name, status, created_at, avatar_url, signature, profile_background_url
		 from users where id = $1`,
		userID,
	).Scan(&username, &displayName, &status, &createdAt, &avatarURL, &signature, &profileBackgroundURL)
	if err == pgx.ErrNoRows || status == "deleted" {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户资料失败")
		return
	}

	var followers, following int64
	_ = s.db.QueryRow(r.Context(), `select count(*) from user_follows where followed_id = $1`, userID).Scan(&followers)
	_ = s.db.QueryRow(r.Context(), `select count(*) from user_follows where follower_id = $1`, userID).Scan(&following)
	claims := currentClaims(r)
	isOwn := claims.Subject > 0 && claims.Subject == userID
	isFollowing := false
	if claims.Subject > 0 && !isOwn {
		_ = s.db.QueryRow(
			r.Context(),
			`select exists(select 1 from user_follows where follower_id = $1 and followed_id = $2)`,
			claims.Subject,
			userID,
		).Scan(&isFollowing)
	}
	canFollow := claims.Subject > 0 && !isOwn && hasPermission(claims.Permissions, "user.follow.create") && s.userHasPermission(r.Context(), userID, "user.follow.receive")
	canMessage := claims.Subject > 0 && !isOwn && hasPermission(claims.Permissions, "user.message.send") && s.userHasPermission(r.Context(), userID, "user.message.receive")

	writeJSON(w, http.StatusOK, map[string]any{
		"id": userID, "username": username, "displayName": displayName, "status": status,
		"createdAt": createdAt, "followers": followers, "following": following,
		"avatarUrl": avatarURL, "signature": signature, "profileBackgroundUrl": profileBackgroundURL,
		"isOwn": isOwn, "isFollowing": isFollowing, "canFollow": canFollow, "canMessage": canMessage,
	})
}

func (s *Server) followUser(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathUserID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	if targetID == claims.Subject {
		writeError(w, http.StatusBadRequest, "不能关注自己")
		return
	}
	if !s.userHasPermission(r.Context(), targetID, "user.follow.receive") {
		writeError(w, http.StatusForbidden, "对方没有允许被关注的权限")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "关注用户失败")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(
		r.Context(),
		`insert into user_follows (follower_id, followed_id)
		 select $1, id from users where id = $2 and status = 'active'
		 on conflict do nothing`,
		claims.Subject,
		targetID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "关注用户失败")
		return
	}
	if tag.RowsAffected() > 0 {
		if s.queue == nil {
			writeError(w, http.StatusServiceUnavailable, "通知任务队列不可用")
			return
		}
		if err := s.queue.PublishTask(r.Context(), notificationTaskCode, notificationEvent{Action: "follow", RecipientID: targetID, ActorID: claims.Subject}); err != nil {
			writeError(w, http.StatusServiceUnavailable, "通知任务队列不可用")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "关注用户失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"following": true, "created": tag.RowsAffected() > 0})
}

func (s *Server) unfollowUser(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathUserID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	_, err := s.db.Exec(r.Context(), `delete from user_follows where follower_id = $1 and followed_id = $2`, claims.Subject, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "取消关注失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"following": false})
}

func (s *Server) getNotificationSettings(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	emailEnabled := false
	err := s.db.QueryRow(r.Context(), `select email_enabled from user_notification_settings where user_id = $1`, claims.Subject).Scan(&emailEnabled)
	if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "读取通知设置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"emailEnabled": emailEnabled})
}

func (s *Server) updateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	var req notificationSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	claims := currentClaims(r)
	_, err := s.db.Exec(
		r.Context(),
		`insert into user_notification_settings (user_id, email_enabled, updated_at)
		 values ($1, $2, now())
		 on conflict (user_id) do update set email_enabled = excluded.email_enabled, updated_at = now()`,
		claims.Subject,
		req.EmailEnabled,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存通知设置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"emailEnabled": req.EmailEnabled})
}

func pathUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.PathValue("id"))
	userID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return 0, false
	}
	return userID, true
}
