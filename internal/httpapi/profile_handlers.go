package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
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

type profileRelationships struct {
	Followers, Following, Blocked              int64
	IsFollowing, BlocksTarget, BlockedByTarget bool
}

func loadProfileRelationships(ctx context.Context, db databaseQuery, target, viewer int64) (profileRelationships, error) {
	var links profileRelationships
	err := db.QueryRow(ctx, `select
		(select count(*) from user_follows f join users u on u.id=f.follower_id where f.followed_id=$1 and u.status='active'),
		(select count(*) from user_follows f join users u on u.id=f.followed_id where f.follower_id=$1 and u.status='active'),
		case when $1=$2 then (select count(*) from user_blocks b join users u on u.id=b.blocked_id where b.blocker_id=$1 and u.status='active') else 0 end,
		exists(select 1 from user_follows where $2>0 and $2<>$1 and follower_id=$2 and followed_id=$1),
		exists(select 1 from user_blocks where $2>0 and $2<>$1 and blocker_id=$2 and blocked_id=$1),
		exists(select 1 from user_blocks where $2>0 and $2<>$1 and blocker_id=$1 and blocked_id=$2)`,
		target, viewer).Scan(&links.Followers, &links.Following, &links.Blocked, &links.IsFollowing, &links.BlocksTarget, &links.BlockedByTarget)
	return links, err
}

func (s *Server) userOverview(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	links, err := loadProfileRelationships(r.Context(), s.db, claims.Subject, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户主页概览失败")
		return
	}
	balance, err := s.userAIDailyBalance(r.Context(), claims.Subject, claims)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to load AI token balance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"followers": links.Followers,
		"following": links.Following,
		"blocked":   links.Blocked,
		"aiBalance": balance,
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
	links, err := loadProfileRelationships(r.Context(), s.db, userID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取用户关系失败")
		return
	}
	canBlock := claims.Subject > 0 && !isOwn && status == "active"
	canInteract := canBlock && !links.BlocksTarget && !links.BlockedByTarget
	canFollow := canInteract && claimsAllow(claims, "user.follow.create")
	canMessage := canInteract && claimsAllow(claims, "user.message.send")
	if canFollow || canMessage {
		_, permissions, err := s.resolveUserRootPermissions(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "读取互动权限失败")
			return
		}
		canFollow = canFollow && permissionRulesAllow(permissions, "user.follow.receive")
		canMessage = canMessage && permissionRulesAllow(permissions, "user.message.receive")
	}
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
		"createdAt": createdAt, "followers": links.Followers, "following": links.Following,
		"blocked":   links.Blocked,
		"avatarUrl": avatarURL, "signature": signature, "profileBackgroundUrl": profileBackgroundURL,
		"onlineStatus": mapPublicOnlineVisibility(showOnlineStatus, onlineActive),
		"isOwn":        isOwn, "isFollowing": links.IsFollowing, "isBlocked": links.BlocksTarget,
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

	request, err := parseUserConnectionPageRequest(r.URL.Query(), identity.InternalID, connectionType)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "USER_CONNECTION_CURSOR_INVALID", "invalid user connection cursor", 0, nil)
		return
	}
	pageRows, hasMore, nextCursor, err := loadUserConnectionPage(r.Context(), s.db, identity.InternalID, connectionType, request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user connections")
		return
	}
	items := make([]userConnectionItem, len(pageRows))
	for i, row := range pageRows {
		items[i] = row.Item
	}
	if err = s.resolveUserConnectionAvatars(r.Context(), items); err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate user avatar URL")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "limit": request.Limit, "hasMore": hasMore, "nextCursor": nextCursor,
	})
}

func (s *Server) resolveUserConnectionAvatars(ctx context.Context, items []userConnectionItem) error {
	if len(items) == 0 {
		return nil
	}
	urls := make([]string, len(items))
	for i := range items {
		urls[i] = items[i].AvatarURL
	}
	resolved, err := s.resolveStoredOSSImageURLsWithConfig(ctx, s.ossConfigFromSettings(ctx), urls)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].AvatarURL = resolved[i]
	}
	return nil
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "关注用户失败")
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockUserRelationshipPairTx(r.Context(), tx, claims.Subject, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "检查用户关系失败")
		return
	}
	var activeID int64
	if err = tx.QueryRow(r.Context(), `select id from users where id=$1 and status='active' for share`, targetID).Scan(&activeID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusServiceUnavailable, "检查用户状态失败")
		return
	}
	var blocked bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from user_blocks
		where (blocker_id=$1 and blocked_id=$2) or (blocker_id=$2 and blocked_id=$1))`, claims.Subject, targetID).Scan(&blocked); err != nil {
		writeError(w, http.StatusInternalServerError, "检查用户关系失败")
		return
	}
	if blocked {
		writeError(w, http.StatusForbidden, "当前无法关注该用户")
		return
	}
	permissions, err := resolveUsersRootPermissionsQuery(r.Context(), tx, []int64{claims.Subject, targetID})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "检查关注权限失败")
		return
	}
	if !permissionRulesAllow(permissions[claims.Subject].Permissions, "user.follow.create") {
		writeError(w, http.StatusForbidden, "没有关注权限")
		return
	}
	if !permissionRulesAllow(permissions[targetID].Permissions, "user.follow.receive") {
		writeError(w, http.StatusForbidden, "对方没有允许被关注的权限")
		return
	}
	tag, err := tx.Exec(
		r.Context(),
		`insert into user_follows (follower_id, followed_id)
		 values($1,$2)
		 on conflict do nothing`,
		claims.Subject,
		targetID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "关注用户失败")
		return
	}
	if tag.RowsAffected() > 0 {
		if err = enqueueNotificationTaskTx(r.Context(), tx, "user.followed", "user", identity.PublicID, r.Header.Get("X-Request-ID"),
			notificationEvent{Action: "follow", RecipientID: targetID, ActorID: claims.Subject}); err != nil {
			writeError(w, http.StatusInternalServerError, "关注通知入队失败")
			return
		}
	}
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist user activity")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "关注用户失败")
		return
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "取消关注失败")
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `delete from user_follows where follower_id = $1 and followed_id = $2`, claims.Subject, targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "取消关注失败")
		return
	}
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist user preference activity")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存通知设置失败")
		return
	}
	defer tx.Rollback(r.Context())
	var emailEnabled, projectUpdatesEnabled bool
	err = tx.QueryRow(
		r.Context(),
		`insert into user_notification_settings (user_id,email_enabled,project_updates_enabled,updated_at)
		 values ($1,coalesce($2,false),coalesce($3,true),now())
		 on conflict (user_id) do update set
		 email_enabled=coalesce($2,user_notification_settings.email_enabled),
		 project_updates_enabled=coalesce($3,user_notification_settings.project_updates_enabled),updated_at=now()
		 returning email_enabled,project_updates_enabled`,
		claims.Subject,
		req.EmailEnabled,
		req.ProjectUpdatesEnabled,
	).Scan(&emailEnabled, &projectUpdatesEnabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存通知设置失败")
		return
	}
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist user preference activity")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存通知设置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"emailEnabled": emailEnabled, "projectUpdatesEnabled": projectUpdatesEnabled})
}
