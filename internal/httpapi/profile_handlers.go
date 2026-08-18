package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/queue"
)

type notificationSettingsRequest struct {
	EmailEnabled          *bool `json:"emailEnabled"`
	ProjectUpdatesEnabled *bool `json:"projectUpdatesEnabled"`
}

type userConnectionItem struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatarUrl"`
	Signature string `json:"signature"`
}

func (s *Server) userOverview(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	var followers, following, blocked int64
	err := s.db.QueryRow(
		r.Context(),
		`select
		 (select count(*) from user_follows follow join users account on account.id=follow.follower_id where follow.followed_id=$1 and account.status='active'),
		 (select count(*) from user_follows follow join users account on account.id=follow.followed_id where follow.follower_id=$1 and account.status='active'),
		 (select count(*) from user_blocks block join users account on account.id=block.blocked_id where block.blocker_id=$1 and account.status='active')`,
		claims.Subject,
	).Scan(&followers, &following, &blocked)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户主页概览失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"followers": followers,
		"following": following,
		"blocked":   blocked,
		"aiBalance": s.userAIDailyBalance(r.Context(), claims.Subject, claims),
	})
}

func (s *Server) userProfile(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	userID := identity.InternalID
	var username, status, avatarURL, signature, profileBackgroundURL string
	var showOnlineStatus bool
	var createdAt time.Time
	err := s.db.QueryRow(
		r.Context(),
		`select username,status,created_at,avatar_url,signature,profile_background_url,show_online_status
		 from users where id=$1`, userID,
	).Scan(&username, &status, &createdAt, &avatarURL, &signature, &profileBackgroundURL, &showOnlineStatus)
	if err == pgx.ErrNoRows || status == "deleted" {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户资料失败")
		return
	}

	claims := currentClaims(r)
	isOwn := claims.Subject > 0 && claims.Subject == userID
	var followers, following, blockedCount int64
	_ = s.db.QueryRow(r.Context(), `select count(*) from user_follows follow join users account on account.id=follow.follower_id where follow.followed_id=$1 and account.status='active'`, userID).Scan(&followers)
	_ = s.db.QueryRow(r.Context(), `select count(*) from user_follows follow join users account on account.id=follow.followed_id where follow.follower_id=$1 and account.status='active'`, userID).Scan(&following)
	if isOwn {
		_ = s.db.QueryRow(r.Context(), `select count(*) from user_blocks block join users account on account.id=block.blocked_id where block.blocker_id=$1 and account.status='active'`, userID).Scan(&blockedCount)
	}
	isFollowing := false
	isBlocked := false
	blockedEitherDirection := false
	if claims.Subject > 0 && !isOwn {
		_ = s.db.QueryRow(
			r.Context(),
			`select exists(select 1 from user_follows where follower_id = $1 and followed_id = $2)`,
			claims.Subject,
			userID,
		).Scan(&isFollowing)
		_ = s.db.QueryRow(r.Context(), `select
			exists(select 1 from user_blocks where blocker_id=$1 and blocked_id=$2),
			exists(select 1 from user_blocks where (blocker_id=$1 and blocked_id=$2) or (blocker_id=$2 and blocked_id=$1))`,
			claims.Subject, userID).Scan(&isBlocked, &blockedEitherDirection)
	}
	canBlock := claims.Subject > 0 && !isOwn
	canFollow := claims.Subject > 0 && !isOwn && !blockedEitherDirection && claimsAllow(claims, "user.follow.create") && s.userHasPermission(r.Context(), userID, "user.follow.receive")
	canMessage := claims.Subject > 0 && !isOwn && !blockedEitherDirection && claimsAllow(claims, "user.message.send") && s.userHasPermission(r.Context(), userID, "user.message.receive")
	ossCfg := s.ossConfigFromSettings(r.Context())
	avatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, avatarURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "生成用户头像访问链接失败")
		return
	}
	profileBackgroundURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, profileBackgroundURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "生成用户主页背景访问链接失败")
		return
	}

	onlineActive := s.cache.UsersOnline(r.Context(), []int64{userID}, time.Now(), s.cache.Config().PresenceTTL)[userID]
	writeJSON(w, http.StatusOK, map[string]any{
		"id": identity.PublicID, "username": username, "status": status,
		"createdAt": createdAt, "followers": followers, "following": following,
		"blocked":   blockedCount,
		"avatarUrl": avatarURL, "signature": signature, "profileBackgroundUrl": profileBackgroundURL,
		"onlineStatus": mapPublicOnlineVisibility(showOnlineStatus, onlineActive),
		"isOwn":        isOwn, "isFollowing": isFollowing, "isBlocked": isBlocked,
		"canBlock": canBlock, "canFollow": canFollow, "canMessage": canMessage,
	})
}

func (s *Server) userFollowers(w http.ResponseWriter, r *http.Request) {
	s.userConnections(w, r, "followers")
}

func (s *Server) userFollowing(w http.ResponseWriter, r *http.Request) {
	s.userConnections(w, r, "following")
}

func (s *Server) userConnections(w http.ResponseWriter, r *http.Request, connectionType string) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	var status string
	if err := s.db.QueryRow(r.Context(), `select status from users where id=$1`, identity.InternalID).Scan(&status); err == pgx.ErrNoRows || status == "deleted" {
		writeError(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user connections")
		return
	}

	page := boundedInt(r.URL.Query().Get("page"), 1, 1, 10000)
	pageSize := boundedInt(r.URL.Query().Get("pageSize"), 24, 1, 60)
	offset := (page - 1) * pageSize
	countQuery := `select count(*) from user_follows follow join users account on account.id=follow.follower_id where follow.followed_id=$1 and account.status='active'`
	itemsQuery := `select account.public_id,account.username,account.avatar_url,account.signature
		from user_follows follow join users account on account.id=follow.follower_id
		where follow.followed_id=$1 and account.status='active'
		order by follow.created_at desc,follow.follower_id desc limit $2 offset $3`
	if connectionType == "following" {
		countQuery = `select count(*) from user_follows follow join users account on account.id=follow.followed_id where follow.follower_id=$1 and account.status='active'`
		itemsQuery = `select account.public_id,account.username,account.avatar_url,account.signature
			from user_follows follow join users account on account.id=follow.followed_id
			where follow.follower_id=$1 and account.status='active'
			order by follow.created_at desc,follow.followed_id desc limit $2 offset $3`
	}
	var total int64
	if err := s.db.QueryRow(r.Context(), countQuery, identity.InternalID).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count user connections")
		return
	}
	rows, err := s.db.Query(r.Context(), itemsQuery, identity.InternalID, pageSize, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user connections")
		return
	}
	defer rows.Close()
	items := make([]userConnectionItem, 0)
	ossCfg := s.ossConfigFromSettings(r.Context())
	for rows.Next() {
		var item userConnectionItem
		if err = rows.Scan(&item.ID, &item.Username, &item.AvatarURL, &item.Signature); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode user connections")
			return
		}
		item.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.AvatarURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate user avatar URL")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user connections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "page": page, "pageSize": pageSize, "total": total,
	})
}

func (s *Server) followUser(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	targetID := identity.InternalID
	claims := currentClaims(r)
	if targetID == claims.Subject {
		writeError(w, http.StatusBadRequest, "不能关注自己")
		return
	}
	blocked, err := s.usersBlockEachOther(r.Context(), claims.Subject, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "检查用户关系失败")
		return
	}
	if blocked {
		writeError(w, http.StatusForbidden, "当前无法关注该用户")
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
		if s.cfg.NATS.OutboxEnabled {
			if _, err = queue.EnqueueTx(r.Context(), tx, notificationTaskCode, "user.followed", "user", identity.PublicID, r.Header.Get("X-Request-ID"),
				notificationEvent{Action: "follow", RecipientID: targetID, ActorID: claims.Subject}); err != nil {
				writeError(w, http.StatusInternalServerError, "关注通知入队失败")
				return
			}
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "关注用户失败")
		return
	}
	if tag.RowsAffected() > 0 && !s.cfg.NATS.OutboxEnabled && s.queue != nil {
		_ = s.queue.PublishTask(r.Context(), notificationTaskCode, notificationEvent{Action: "follow", RecipientID: targetID, ActorID: claims.Subject})
	}
	writeJSON(w, http.StatusOK, map[string]any{"following": true, "created": tag.RowsAffected() > 0})
}

func (s *Server) unfollowUser(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	targetID := identity.InternalID
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
	emailEnabled, projectUpdatesEnabled := false, true
	err := s.db.QueryRow(r.Context(), `select email_enabled,project_updates_enabled from user_notification_settings where user_id = $1`, claims.Subject).Scan(&emailEnabled, &projectUpdatesEnabled)
	if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "读取通知设置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"emailEnabled": emailEnabled, "projectUpdatesEnabled": projectUpdatesEnabled})
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
		`insert into user_notification_settings (user_id,email_enabled,project_updates_enabled,updated_at)
		 values ($1,coalesce($2,false),coalesce($3,true),now())
		 on conflict (user_id) do update set
		 email_enabled=coalesce($2,user_notification_settings.email_enabled),
		 project_updates_enabled=coalesce($3,user_notification_settings.project_updates_enabled),updated_at=now()`,
		claims.Subject,
		req.EmailEnabled,
		req.ProjectUpdatesEnabled,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存通知设置失败")
		return
	}
	getNotificationSettings := r.Clone(r.Context())
	s.getNotificationSettings(w, getNotificationSettings)
}

func (s *Server) pathUserIdentity(w http.ResponseWriter, r *http.Request) (publicIdentity, bool) {
	identity, err := s.resolvePublicIdentity(r.Context(), strings.TrimSpace(r.PathValue("id")), "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return publicIdentity{}, false
	}
	return identity, true
}
