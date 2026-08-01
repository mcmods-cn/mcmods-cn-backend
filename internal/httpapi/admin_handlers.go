package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/mailer"
	"mcmods-cn-backend/internal/security"
)

type mailConfigPayload struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
	UseTLS   bool   `json:"useTLS"`
}

type testMailRequest struct {
	To string `json:"to"`
}

type updateRolesRequest struct {
	Roles []string `json:"roles"`
}

type adminCreateUserRequest struct {
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	Password    string   `json:"password"`
	DisplayName string   `json:"displayName"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
}

type roleRequest struct {
	Code              string                       `json:"code"`
	Name              string                       `json:"name"`
	Description       string                       `json:"description"`
	Translations      domain.LocalizedTexts        `json:"translations"`
	Weight            int                          `json:"weight"`
	Parents           []string                     `json:"parents"`
	Permissions       []string                     `json:"permissions"`
	PermissionEntries []domain.RolePermissionEntry `json:"permissionEntries"`
}

type permissionNodeRequest struct {
	Code         string                `json:"code"`
	Module       string                `json:"module"`
	Name         string                `json:"name"`
	Description  string                `json:"description"`
	Translations domain.LocalizedTexts `json:"translations"`
}

type userPermissionEntry struct {
	Code      string `json:"code"`
	Allow     bool   `json:"allow"`
	ExpiresAt string `json:"expiresAt"`
	Context   string `json:"context"`
}

type updateUserPermissionsRequest struct {
	Permissions []userPermissionEntry `json:"permissions"`
}

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-zA-Z0-9_*:\[\]<>-]+)*$`)
var permissionCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-zA-Z0-9_*:\[\]<>-]+)+$`)

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	var users, roles, permissions, loginSuccess, loginFailed int64
	_ = s.db.QueryRow(r.Context(), `select count(*) from users`).Scan(&users)
	_ = s.db.QueryRow(r.Context(), `select count(*) from roles`).Scan(&roles)
	_ = s.db.QueryRow(r.Context(), `select count(*) from permissions`).Scan(&permissions)
	_ = s.db.QueryRow(r.Context(), `select count(*) from user_login_logs where success = true`).Scan(&loginSuccess)
	_ = s.db.QueryRow(r.Context(), `select count(*) from user_login_logs where success = false`).Scan(&loginFailed)

	writeJSON(w, http.StatusOK, map[string]any{
		"cards": []map[string]any{
			{"label": "users", "value": users, "tone": "green"},
			{"label": "roles", "value": roles, "tone": "blue"},
			{"label": "permissions", "value": permissions, "tone": "violet"},
			{"label": "loginSuccess", "value": loginSuccess, "tone": "green"},
			{"label": "loginFailed", "value": loginFailed, "tone": "red"},
		},
	})
}

func (s *Server) adminNav(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, adminNavigation())
}

func (s *Server) adminConfig(w http.ResponseWriter, r *http.Request) {
	mailCfg := s.mailConfigFromSettings(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"auth": map[string]any{
			"allowRegistration":        true,
			"emailPasswordLogin":       true,
			"usernamePasswordLogin":    true,
			"userIDPasswordLogin":      true,
			"emailCodeLogin":           true,
			"requireEmailVerification": false,
			"passwordMinLength":        8,
			"reservedUsernames":        []string{"admin", "root", "system", "api", "login", "register", "mods", "mod", "users", "settings"},
			"tokenTTLHours":            int(s.cfg.JWTTTL.Hours()),
		},
		"oauth":    redactOAuthConfig(s.oauthConfig(r.Context())),
		"mail":     redactMailConfig(mailCfg),
		"oss":      redactOSSConfig(s.ossConfigFromSettings(r.Context())),
		"markdown": s.markdownConfigFromSettings(r.Context()),
		"profile":  s.profileConfigFromSettings(r.Context()),
		"ai":       redactAIConfig(s.aiConfigFromSettings(r.Context())),
		"permissions": map[string]any{
			"mode":              "RBAC + user override",
			"temporaryGrant":    true,
			"auditLog":          true,
			"adminAccessPolicy": "require admin.access permission",
		},
		"database": map[string]any{
			"driver":  "PostgreSQL",
			"host":    s.cfg.DB.Host,
			"port":    s.cfg.DB.Port,
			"name":    s.cfg.DB.Name,
			"user":    s.cfg.DB.User,
			"sslMode": s.cfg.DB.SSLMode,
		},
		"features": map[string]bool{
			"contentReview":  true,
			"emailSystem":    true,
			"permissionRBAC": true,
			"oss":            true,
			"logSystem":      true,
			"ai":             true,
			"redis":          s.cfg.Redis.Enabled,
			"crawler":        false,
		},
	})
}

func (s *Server) updateMailConfig(w http.ResponseWriter, r *http.Request) {
	var payload mailConfigPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if payload.Port <= 0 {
		payload.Port = 587
	}
	current := s.mailConfigFromSettings(r.Context())
	if strings.TrimSpace(payload.Password) == "" {
		payload.Password = current.Password
	}
	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "邮件配置格式不正确")
		return
	}
	claims := currentClaims(r)
	_, err = s.db.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ('mail.smtp', $1::jsonb, $2, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		raw,
		claims.Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存邮件配置失败")
		return
	}
	s.mailer = mailer.New(config.SMTPConfig{
		Host:     payload.Host,
		Port:     payload.Port,
		Username: payload.Username,
		Password: payload.Password,
		From:     payload.From,
		UseTLS:   payload.UseTLS,
	})
	writeJSON(w, http.StatusOK, redactMailConfig(payload))
}

func (s *Server) sendTestMail(w http.ResponseWriter, r *http.Request) {
	var req testMailRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.To = strings.TrimSpace(req.To)
	if !strings.Contains(req.To, "@") {
		writeError(w, http.StatusBadRequest, "测试邮箱格式不正确")
		return
	}
	if err := s.activeMailer(r.Context()).Send(req.To, "Mcmods-cn 邮件系统测试", "这是一封来自后台管理界面的测试邮件。"); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

func (s *Server) permissionCatalog(w http.ResponseWriter, r *http.Request) {
	roles, err := s.roles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取角色失败")
		return
	}
	permissions, err := s.permissions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles, "permissions": permissions})
}

func (s *Server) createPermission(w http.ResponseWriter, r *http.Request) {
	var req permissionNodeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Code = normalizeCode(req.Code)
	req.Module = strings.TrimSpace(req.Module)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.Translations = normalizeLocalizedTexts(req.Translations)
	if err := validatePermissionNode(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	translationsJSON, err := localizedTextsJSON(req.Translations)
	if err != nil {
		writeError(w, http.StatusBadRequest, "权限翻译格式不正确")
		return
	}
	_, err = s.db.Exec(
		r.Context(),
		`insert into permissions (code, module, name, description, translations)
		 values ($1, $2, $3, $4, $5)
		 on conflict (code) do update
		 set module = excluded.module,
		     name = excluded.name,
		     description = excluded.description,
		     translations = excluded.translations`,
		req.Code,
		req.Module,
		req.Name,
		req.Description,
		translationsJSON,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存权限节点失败")
		return
	}
	s.auditPermissionChange(r.Context(), currentClaims(r).Subject, nil, "upsert_permission_node", req)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) createRole(w http.ResponseWriter, r *http.Request) {
	var req roleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	normalizeRoleRequest(&req)
	if err := validateRoleRequest(req, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库事务创建失败")
		return
	}
	defer tx.Rollback(r.Context())

	translationsJSON, err := localizedTextsJSON(req.Translations)
	if err != nil {
		writeError(w, http.StatusBadRequest, "权限组翻译格式不正确")
		return
	}
	_, err = tx.Exec(
		r.Context(),
		`insert into roles (code, name, description, weight, parents, translations)
		 values ($1, $2, $3, $4, $5, $6)`,
		req.Code,
		req.Name,
		req.Description,
		req.Weight,
		req.Parents,
		translationsJSON,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "权限组代码已存在")
		return
	}
	if err := s.replaceRolePermissions(r.Context(), tx, req.Code, rolePermissionEntries(req.Permissions, req.PermissionEntries)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.auditPermissionChangeTx(r.Context(), tx, currentClaims(r).Subject, nil, "create_role", req)
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存权限组失败")
		return
	}
	role, err := s.roleByCode(r.Context(), req.Code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限组失败")
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

func (s *Server) updateRole(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.PathValue("code"))
	var req roleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Code = code
	normalizeRoleRequest(&req)
	if err := validateRoleRequest(req, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库事务创建失败")
		return
	}
	defer tx.Rollback(r.Context())

	translationsJSON, err := localizedTextsJSON(req.Translations)
	if err != nil {
		writeError(w, http.StatusBadRequest, "权限组翻译格式不正确")
		return
	}
	tag, err := tx.Exec(
		r.Context(),
		`update roles
		 set name = $2,
		     description = $3,
		     weight = $4,
		     parents = $5,
		     translations = $6
		 where code = $1`,
		req.Code,
		req.Name,
		req.Description,
		req.Weight,
		req.Parents,
		translationsJSON,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新权限组失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "权限组不存在")
		return
	}
	if err := s.replaceRolePermissions(r.Context(), tx, req.Code, rolePermissionEntries(req.Permissions, req.PermissionEntries)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.auditPermissionChangeTx(r.Context(), tx, currentClaims(r).Subject, nil, "update_role", req)
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存权限组失败")
		return
	}
	role, err := s.roleByCode(r.Context(), req.Code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限组失败")
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (s *Server) deleteRole(w http.ResponseWriter, r *http.Request) {
	code := normalizeCode(r.PathValue("code"))
	if code == "" || !roleCodePattern.MatchString(code) {
		writeError(w, http.StatusBadRequest, "权限组代码格式不正确")
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库事务创建失败")
		return
	}
	defer tx.Rollback(r.Context())

	var roleID int64
	if err := tx.QueryRow(r.Context(), `select id from roles where code = $1`, code).Scan(&roleID); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "权限组不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取权限组失败")
		return
	}
	if _, err := tx.Exec(r.Context(), `delete from user_role_bindings where role_id = $1`, roleID); err != nil {
		writeError(w, http.StatusInternalServerError, "清理用户权限组绑定失败")
		return
	}
	if _, err := tx.Exec(r.Context(), `delete from role_permissions where role_id = $1`, roleID); err != nil {
		writeError(w, http.StatusInternalServerError, "清理权限组节点失败")
		return
	}
	tag, err := tx.Exec(r.Context(), `delete from roles where id = $1`, roleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除权限组失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "权限组不存在")
		return
	}
	s.auditPermissionChangeTx(r.Context(), tx, currentClaims(r).Subject, nil, "delete_role", map[string]string{"code": code})
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "删除权限组失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := s.db.Query(
		r.Context(),
		`select id, public_id, username, email, display_name, email_verified, status, created_at, last_login_at
		 from users
		 where $1 = ''
		    or public_id = lower($1)
		    or username ilike '%' || $1 || '%'
		    or email ilike '%' || $1 || '%'
		    or display_name ilike '%' || $1 || '%'
		 order by id desc
		 limit 100`,
		query,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户列表失败")
		return
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.DisplayName, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt); err != nil {
			writeError(w, http.StatusInternalServerError, "读取用户数据失败")
			return
		}
		user.Roles, user.Permissions = s.userGrants(r.Context(), user.ID)
		users = append(users, user)
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) createAdminUser(w http.ResponseWriter, r *http.Request) {
	var req adminCreateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = normalizeEmail(req.Email)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Status = strings.TrimSpace(req.Status)
	req.Roles = normalizeCodes(req.Roles)
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if err := validateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "密码至少需要 8 位")
		return
	}
	if req.Status != "active" && req.Status != "disabled" && req.Status != "banned" && req.Status != "deleted" {
		writeError(w, http.StatusBadRequest, "用户状态不正确")
		return
	}
	for _, role := range req.Roles {
		if !roleCodePattern.MatchString(role) {
			writeError(w, http.StatusBadRequest, "权限组代码格式不正确: "+role)
			return
		}
	}

	passwordHash, err := security.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库事务创建失败")
		return
	}
	defer tx.Rollback(r.Context())

	var user domain.User
	err = tx.QueryRow(
		r.Context(),
		`insert into users (username, email, display_name, password_hash, email_verified, status)
		 values ($1, $2, $3, $4, true, $5)
		 returning id, public_id, username, email, display_name, email_verified, status, created_at, last_login_at`,
		req.Username,
		req.Email,
		req.DisplayName,
		passwordHash,
		req.Status,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.DisplayName, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt)
	if err != nil {
		writeError(w, http.StatusConflict, "用户名或邮箱已被占用")
		return
	}
	for _, role := range req.Roles {
		if err := s.ensureRoleForBinding(r.Context(), tx, role); err != nil {
			if requestErr, ok := err.(*requestError); ok {
				writeError(w, http.StatusBadRequest, requestErr.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "绑定权限组失败")
			return
		}
		tag, err := tx.Exec(
			r.Context(),
			`insert into user_role_bindings (user_id, role_id)
			 select $1, id from roles where code = $2
			 on conflict do nothing`,
			user.ID,
			role,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "绑定权限组失败")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusBadRequest, "权限组不存在: "+role)
			return
		}
	}
	defaultRoleKind := "registered"
	if req.Status == "banned" {
		defaultRoleKind = "banned"
	}
	if err := s.assignConfiguredRoleTx(r.Context(), tx, user.ID, defaultRoleKind); err != nil {
		writeError(w, http.StatusInternalServerError, "分配默认权限组失败")
		return
	}
	s.auditPermissionChangeTx(r.Context(), tx, currentClaims(r).Subject, &user.ID, "create_user", map[string]any{
		"username": user.Username,
		"email":    user.Email,
		"status":   user.Status,
		"roles":    req.Roles,
	})
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	user.Roles, user.Permissions = s.userGrants(r.Context(), user.ID)
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) updateUserRoles(w http.ResponseWriter, r *http.Request) {
	identity, err := s.resolvePublicIdentity(r.Context(), r.PathValue("id"), "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return
	}
	userID := identity.InternalID
	var req updateRolesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库事务创建失败")
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(), `delete from user_role_bindings where user_id = $1`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "清理旧角色失败")
		return
	}
	for _, role := range req.Roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if _, err := tx.Exec(
			r.Context(),
			`insert into user_role_bindings (user_id, role_id)
			 select $1, id from roles where code = $2
			 on conflict do nothing`,
			userID,
			role,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "绑定角色失败")
			return
		}
	}
	claims := currentClaims(r)
	s.auditPermissionChangeTx(r.Context(), tx, claims.Subject, &userID, "update_user_roles", map[string]any{"roles": req.Roles})
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存角色失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) userPermissionDetails(w http.ResponseWriter, r *http.Request) {
	identity, err := s.resolvePublicIdentity(r.Context(), r.PathValue("id"), "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return
	}
	userID := identity.InternalID
	roles, effective := s.userGrants(r.Context(), userID)
	rows, err := s.db.Query(
		r.Context(),
		`select p.code, up.allow, up.expires_at, up.context
		 from user_permissions up
		 join permissions p on p.id = up.permission_id
		 where up.user_id = $1
		 order by p.code`,
		userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户权限失败")
		return
	}
	defer rows.Close()
	direct := make([]map[string]any, 0)
	for rows.Next() {
		var code, contextValue string
		var allow bool
		var expiresAt *time.Time
		if err := rows.Scan(&code, &allow, &expiresAt, &contextValue); err != nil {
			writeError(w, http.StatusInternalServerError, "读取用户权限数据失败")
			return
		}
		item := map[string]any{"code": code, "allow": allow, "context": contextValue}
		if expiresAt != nil {
			item["expiresAt"] = expiresAt.Format(time.RFC3339)
		}
		direct = append(direct, item)
	}
	groupPermissions := make([]string, 0, len(roles))
	for _, role := range roles {
		groupPermissions = append(groupPermissions, "group."+role)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"roles":                roles,
		"groupPermissions":     groupPermissions,
		"directPermissions":    direct,
		"effectivePermissions": effective,
	})
}

func (s *Server) updateUserPermissions(w http.ResponseWriter, r *http.Request) {
	identity, err := s.resolvePublicIdentity(r.Context(), r.PathValue("id"), "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return
	}
	userID := identity.InternalID
	var req updateUserPermissionsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库事务创建失败")
		return
	}
	defer tx.Rollback(r.Context())

	groupRoles := make([]string, 0)
	directPermissions := make([]userPermissionEntry, 0)
	for _, entry := range req.Permissions {
		entry.Code = normalizeCode(entry.Code)
		entry.Context = strings.TrimSpace(entry.Context)
		if entry.Code == "" {
			continue
		}
		if strings.HasPrefix(entry.Code, "group.") {
			role := strings.TrimPrefix(entry.Code, "group.")
			if !roleCodePattern.MatchString(role) {
				writeError(w, http.StatusBadRequest, "权限组代码格式不正确: "+role)
				return
			}
			groupRoles = append(groupRoles, role)
			continue
		}
		if !permissionCodePattern.MatchString(entry.Code) {
			writeError(w, http.StatusBadRequest, "权限节点格式不正确: "+entry.Code)
			return
		}
		directPermissions = append(directPermissions, entry)
	}

	if _, err := tx.Exec(r.Context(), `delete from user_role_bindings where user_id = $1`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "清理旧权限组失败")
		return
	}
	for _, role := range normalizeCodes(groupRoles) {
		if err := s.ensureRoleForBinding(r.Context(), tx, role); err != nil {
			if requestErr, ok := err.(*requestError); ok {
				writeError(w, http.StatusBadRequest, requestErr.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "绑定权限组失败")
			return
		}
		tag, err := tx.Exec(
			r.Context(),
			`insert into user_role_bindings (user_id, role_id)
			 select $1, id from roles where code = $2
			 on conflict do nothing`,
			userID,
			role,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "绑定权限组失败")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusBadRequest, "权限组不存在: "+role)
			return
		}
	}

	if _, err := tx.Exec(r.Context(), `delete from user_permissions where user_id = $1`, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "清理旧用户权限失败")
		return
	}
	for _, entry := range directPermissions {
		expiresAt, err := parseOptionalTime(entry.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "有效期格式不正确: "+entry.Code)
			return
		}
		if err := ensurePermissionNode(r.Context(), tx, entry.Code); err != nil {
			writeError(w, http.StatusInternalServerError, "保存权限节点失败")
			return
		}
		_, err = tx.Exec(
			r.Context(),
			`insert into user_permissions (user_id, permission_id, allow, expires_at, context, updated_at)
			 select $1, id, $3, $4, $5, now() from permissions where code = $2
			 on conflict (user_id, permission_id) do update
			 set allow = excluded.allow, expires_at = excluded.expires_at, context = excluded.context, updated_at = now()`,
			userID,
			entry.Code,
			entry.Allow,
			expiresAt,
			entry.Context,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存用户权限失败")
			return
		}
	}
	claims := currentClaims(r)
	s.auditPermissionChangeTx(r.Context(), tx, claims.Subject, &userID, "update_user_permissions", map[string]any{
		"permissions": req.Permissions,
		"ip":          s.requestClientLocation(r).IP,
		"userAgent":   r.UserAgent(),
	})
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存用户权限失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) roles(ctx context.Context) ([]domain.Role, error) {
	rows, err := s.db.Query(ctx, `select code, name, description, translations, weight, parents from roles order by weight desc, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]domain.Role, 0)
	for rows.Next() {
		var role domain.Role
		var translations []byte
		if err := rows.Scan(&role.Code, &role.Name, &role.Description, &translations, &role.Weight, &role.Parents); err != nil {
			return nil, err
		}
		role.Translations = parseLocalizedTexts(translations)
		role.PermissionEntries = s.rolePermissionEntries(ctx, role.Code)
		role.Permissions = rolePermissionCodes(role.PermissionEntries)
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (s *Server) roleByCode(ctx context.Context, code string) (domain.Role, error) {
	var role domain.Role
	var translations []byte
	err := s.db.QueryRow(
		ctx,
		`select code, name, description, translations, weight, parents from roles where code = $1`,
		code,
	).Scan(&role.Code, &role.Name, &role.Description, &translations, &role.Weight, &role.Parents)
	if err != nil {
		return role, err
	}
	role.Translations = parseLocalizedTexts(translations)
	role.PermissionEntries = s.rolePermissionEntries(ctx, role.Code)
	role.Permissions = rolePermissionCodes(role.PermissionEntries)
	return role, nil
}

func (s *Server) replaceRolePermissions(ctx context.Context, tx pgx.Tx, roleCode string, permissionEntries []domain.RolePermissionEntry) error {
	var roleID int64
	if err := tx.QueryRow(ctx, `select id from roles where code = $1`, roleCode).Scan(&roleID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from role_permissions where role_id = $1`, roleID); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(permissionEntries))
	for _, entry := range permissionEntries {
		permissionCode := normalizeCode(entry.Code)
		if permissionCode == "" {
			continue
		}
		if _, exists := seen[permissionCode]; exists {
			continue
		}
		seen[permissionCode] = struct{}{}
		if !permissionCodePattern.MatchString(permissionCode) {
			return &requestError{message: "权限节点格式不正确: " + permissionCode}
		}
		expiresAt, err := parseOptionalTime(entry.ExpiresAt)
		if err != nil {
			return &requestError{message: "权限节点有效期格式不正确: " + permissionCode}
		}
		if err := ensurePermissionNode(ctx, tx, permissionCode); err != nil {
			return err
		}
		tag, err := tx.Exec(
			ctx,
			`insert into role_permissions (role_id, permission_id, allow, expires_at, updated_at)
			 select $1, id, $3, $4, now() from permissions where code = $2
			 on conflict (role_id, permission_id) do update
			 set allow = excluded.allow, expires_at = excluded.expires_at, updated_at = now()`,
			roleID,
			permissionCode,
			entry.Allow,
			expiresAt,
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return &requestError{message: "权限节点不存在: " + permissionCode}
		}
	}
	return nil
}

func (s *Server) permissions(ctx context.Context) ([]domain.Permission, error) {
	rows, err := s.db.Query(ctx, `select code, module, name, description, translations from permissions order by module, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permissions := make([]domain.Permission, 0)
	for rows.Next() {
		var permission domain.Permission
		var translations []byte
		if err := rows.Scan(&permission.Code, &permission.Module, &permission.Name, &permission.Description, &translations); err != nil {
			return nil, err
		}
		permission.Translations = parseLocalizedTexts(translations)
		permissions = append(permissions, permission)
	}
	return permissions, rows.Err()
}

func (s *Server) rolePermissionEntries(ctx context.Context, roleCode string) []domain.RolePermissionEntry {
	rows, err := s.db.Query(
		ctx,
		`select p.code, rp.allow, rp.expires_at
		 from permissions p
		 join role_permissions rp on rp.permission_id = p.id
		 join roles r on r.id = rp.role_id
		 where r.code = $1
		 order by p.code`,
		roleCode,
	)
	if err != nil {
		return []domain.RolePermissionEntry{}
	}
	defer rows.Close()
	entries := make([]domain.RolePermissionEntry, 0)
	for rows.Next() {
		var entry domain.RolePermissionEntry
		var expiresAt *time.Time
		if err := rows.Scan(&entry.Code, &entry.Allow, &expiresAt); err == nil {
			if expiresAt != nil {
				entry.ExpiresAt = expiresAt.Format(time.RFC3339)
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

func rolePermissionCodes(entries []domain.RolePermissionEntry) []string {
	codes := make([]string, 0, len(entries))
	for _, entry := range entries {
		codes = append(codes, entry.Code)
	}
	return codes
}

func (s *Server) ensureRoleForBinding(ctx context.Context, tx pgx.Tx, roleCode string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from roles where code = $1)`, roleCode).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	template, variables, ok, err := matchRoleTemplateTx(ctx, tx, roleCode)
	if err != nil {
		return err
	}
	if !ok {
		return &requestError{message: "权限组不存在: " + roleCode}
	}
	parents := applyRoleVariablesToCodes(template.Parents, variables)
	if len(parents) == 0 {
		parents = []string{template.Code}
	}
	if _, err := tx.Exec(
		ctx,
		`insert into roles (code, name, description, weight, parents, status, updated_at)
		 values ($1, $2, $3, $4, $5, 'active', now())
		 on conflict (code) do nothing`,
		roleCode,
		template.Name+" "+roleTemplateSuffix(variables),
		template.Description,
		template.Weight,
		parents,
	); err != nil {
		return err
	}

	entries, err := rolePermissionEntriesTx(ctx, tx, template.Code)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		entry.Code = applyRoleVariables(entry.Code, variables)
		if err := addRolePermissionTx(ctx, tx, roleCode, entry); err != nil {
			return err
		}
	}
	return nil
}

func matchRoleTemplateTx(ctx context.Context, tx pgx.Tx, roleCode string) (domain.Role, map[string]string, bool, error) {
	rows, err := tx.Query(
		ctx,
		`select code, name, description, weight, parents
		 from roles
		 where code like '%[%' or code like '%<%'
		 order by length(code) desc, code`,
	)
	if err != nil {
		return domain.Role{}, nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role.Code, &role.Name, &role.Description, &role.Weight, &role.Parents); err != nil {
			return domain.Role{}, nil, false, err
		}
		if variables, ok := matchRoleTemplateCode(role.Code, roleCode); ok {
			return role, variables, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return domain.Role{}, nil, false, err
	}
	return domain.Role{}, nil, false, nil
}

func matchRoleTemplateCode(templateCode string, roleCode string) (map[string]string, bool) {
	templateParts := strings.Split(templateCode, ".")
	roleParts := strings.Split(roleCode, ".")
	if len(templateParts) != len(roleParts) {
		return nil, false
	}
	variables := make(map[string]string)
	for index, templatePart := range templateParts {
		rolePart := roleParts[index]
		if name, ok := templateVariableName(templatePart); ok {
			if rolePart == "" {
				return nil, false
			}
			if existing, exists := variables[name]; exists && existing != rolePart {
				return nil, false
			}
			variables[name] = rolePart
			continue
		}
		if templatePart != rolePart {
			return nil, false
		}
	}
	if len(variables) == 0 {
		return nil, false
	}
	return variables, true
}

func templateVariableName(segment string) (string, bool) {
	if len(segment) < 3 {
		return "", false
	}
	if (segment[0] == '[' && segment[len(segment)-1] == ']') || (segment[0] == '<' && segment[len(segment)-1] == '>') {
		name := segment[1 : len(segment)-1]
		if name != "" {
			return name, true
		}
	}
	return "", false
}

func applyRoleVariables(permissionCode string, variables map[string]string) string {
	parts := strings.Split(permissionCode, ".")
	for index, part := range parts {
		name, ok := templateVariableName(part)
		if !ok {
			continue
		}
		for variable, value := range variables {
			if strings.EqualFold(name, variable) {
				parts[index] = value
				break
			}
		}
	}
	result := strings.Join(parts, ".")
	for name, value := range variables {
		result = strings.NewReplacer(
			"["+name+"]", value,
			"<"+name+">", value,
			"{"+name+"}", value,
		).Replace(result)
	}
	return result
}

func applyRoleVariablesToCodes(codes []string, variables map[string]string) []string {
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		result = append(result, applyRoleVariables(code, variables))
	}
	return normalizeCodes(result)
}

func roleTemplateSuffix(variables map[string]string) string {
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]string, 0, len(variables))
	for _, name := range names {
		values = append(values, variables[name])
	}
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ".")
}

func rolePermissionEntriesTx(ctx context.Context, tx pgx.Tx, roleCode string) ([]domain.RolePermissionEntry, error) {
	rows, err := tx.Query(
		ctx,
		`select p.code, rp.allow, rp.expires_at
		 from permissions p
		 join role_permissions rp on rp.permission_id = p.id
		 join roles r on r.id = rp.role_id
		 where r.code = $1
		 order by p.code`,
		roleCode,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]domain.RolePermissionEntry, 0)
	for rows.Next() {
		var entry domain.RolePermissionEntry
		var expiresAt *time.Time
		if err := rows.Scan(&entry.Code, &entry.Allow, &expiresAt); err != nil {
			return nil, err
		}
		if expiresAt != nil {
			entry.ExpiresAt = expiresAt.Format(time.RFC3339)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func addRolePermissionTx(ctx context.Context, tx pgx.Tx, roleCode string, entry domain.RolePermissionEntry) error {
	permissionCode := normalizeCode(entry.Code)
	if permissionCode == "" {
		return nil
	}
	if !permissionCodePattern.MatchString(permissionCode) {
		return &requestError{message: "权限节点格式不正确: " + permissionCode}
	}
	expiresAt, err := parseOptionalTime(entry.ExpiresAt)
	if err != nil {
		return &requestError{message: "权限节点有效期格式不正确: " + permissionCode}
	}
	if err := ensurePermissionNode(ctx, tx, permissionCode); err != nil {
		return err
	}
	tag, err := tx.Exec(
		ctx,
		`insert into role_permissions (role_id, permission_id, allow, expires_at, updated_at)
		 select r.id, p.id, $3, $4, now()
		 from roles r, permissions p
		 where r.code = $1 and p.code = $2
		 on conflict (role_id, permission_id) do update
		 set allow = excluded.allow, expires_at = excluded.expires_at, updated_at = now()`,
		roleCode,
		permissionCode,
		entry.Allow,
		expiresAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return &requestError{message: "权限组或权限节点不存在: " + roleCode + " / " + permissionCode}
	}
	return nil
}

type requestError struct {
	message string
}

func (err *requestError) Error() string {
	return err.message
}

func normalizeRoleRequest(req *roleRequest) {
	req.Code = normalizeCode(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.Translations = normalizeLocalizedTexts(req.Translations)
	req.Parents = normalizeCodes(req.Parents)
	req.Permissions = normalizeCodes(req.Permissions)
	req.PermissionEntries = rolePermissionEntries(req.Permissions, req.PermissionEntries)
}

func normalizeLocalizedTexts(values domain.LocalizedTexts) domain.LocalizedTexts {
	if len(values) == 0 {
		return domain.LocalizedTexts{}
	}
	normalized := make(domain.LocalizedTexts, len(values))
	for locale, text := range values {
		locale = strings.TrimSpace(locale)
		text.Name = strings.TrimSpace(text.Name)
		text.Description = strings.TrimSpace(text.Description)
		if locale == "" || (text.Name == "" && text.Description == "") {
			continue
		}
		normalized[locale] = text
	}
	return normalized
}

func localizedTextsJSON(values domain.LocalizedTexts) ([]byte, error) {
	if values == nil {
		values = domain.LocalizedTexts{}
	}
	return json.Marshal(values)
}

func parseLocalizedTexts(raw []byte) domain.LocalizedTexts {
	if len(raw) == 0 {
		return domain.LocalizedTexts{}
	}
	var values domain.LocalizedTexts
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return domain.LocalizedTexts{}
	}
	return normalizeLocalizedTexts(values)
}

func rolePermissionEntries(permissionCodes []string, entries []domain.RolePermissionEntry) []domain.RolePermissionEntry {
	if len(entries) == 0 {
		entries = make([]domain.RolePermissionEntry, 0, len(permissionCodes))
		for _, code := range normalizeCodes(permissionCodes) {
			entries = append(entries, domain.RolePermissionEntry{Code: code, Allow: true})
		}
		return entries
	}
	normalized := make([]domain.RolePermissionEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Code = normalizeCode(entry.Code)
		entry.ExpiresAt = strings.TrimSpace(entry.ExpiresAt)
		normalized = append(normalized, entry)
	}
	return normalized
}

func validateRoleRequest(req roleRequest, creating bool) error {
	if creating && req.Code == "" {
		return &requestError{message: "权限组代码不能为空"}
	}
	if !roleCodePattern.MatchString(req.Code) {
		return &requestError{message: "权限组代码格式不正确"}
	}
	if req.Name == "" {
		return &requestError{message: "权限组名称不能为空"}
	}
	if req.Weight < -10000 || req.Weight > 10000 {
		return &requestError{message: "权重需要在 -10000 到 10000 之间"}
	}
	for _, parent := range req.Parents {
		if parent == req.Code {
			return &requestError{message: "父权限组不能是自己"}
		}
		if !roleCodePattern.MatchString(parent) {
			return &requestError{message: "父权限组代码格式不正确: " + parent}
		}
	}
	for _, permission := range req.PermissionEntries {
		if permission.Code == "" {
			continue
		}
		if !permissionCodePattern.MatchString(permission.Code) {
			return &requestError{message: "权限节点格式不正确: " + permission.Code}
		}
		if _, err := parseOptionalTime(permission.ExpiresAt); err != nil {
			return &requestError{message: "权限节点有效期格式不正确: " + permission.Code}
		}
	}
	return nil
}

func validatePermissionNode(req permissionNodeRequest) error {
	if req.Code == "" {
		return &requestError{message: "权限节点不能为空"}
	}
	if !permissionCodePattern.MatchString(req.Code) {
		return &requestError{message: "权限节点建议使用 module.action 或 module.scope.action 格式"}
	}
	if req.Module == "" {
		return &requestError{message: "模块不能为空"}
	}
	if req.Name == "" {
		return &requestError{message: "显示名称不能为空"}
	}
	return nil
}

func normalizeCodes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		code := normalizeCode(value)
		if code == "" {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	return result
}

func normalizeCode(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	builder.Grow(len(value))
	inTemplate := false
	for _, char := range value {
		switch char {
		case '[', '<':
			inTemplate = true
			builder.WriteRune(char)
		case ']', '>':
			inTemplate = false
			builder.WriteRune(char)
		default:
			if inTemplate {
				builder.WriteRune(char)
			} else {
				builder.WriteRune(unicode.ToLower(char))
			}
		}
	}
	return builder.String()
}

func ensurePermissionNode(ctx context.Context, tx pgx.Tx, code string) error {
	module := code
	if index := strings.Index(code, "."); index > 0 {
		module = code[:index]
	}
	_, err := tx.Exec(
		ctx,
		`insert into permissions (code, module, name, description)
		 values ($1, $2, $1, '自动创建的变量/数值权限节点')
		 on conflict (code) do nothing`,
		code,
		module,
	)
	return err
}

func parseOptionalTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return &parsed, nil
	}
	parsed, err = time.Parse("2006-01-02", value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (s *Server) auditPermissionChange(ctx context.Context, operatorID int64, targetUserID *int64, action string, payload any) {
	raw, _ := json.Marshal(payload)
	_, _ = s.db.Exec(
		ctx,
		`insert into permission_audit_logs (operator_id, target_user_id, action, payload)
		 values ($1, $2, $3, $4::jsonb)`,
		operatorID,
		targetUserID,
		action,
		string(raw),
	)
}

func (s *Server) auditPermissionChangeTx(ctx context.Context, tx pgx.Tx, operatorID int64, targetUserID *int64, action string, payload any) {
	raw, _ := json.Marshal(payload)
	_, _ = tx.Exec(
		ctx,
		`insert into permission_audit_logs (operator_id, target_user_id, action, payload)
		 values ($1, $2, $3, $4::jsonb)`,
		operatorID,
		targetUserID,
		action,
		string(raw),
	)
}

func (s *Server) mailConfigFromSettings(ctx context.Context) mailConfigPayload {
	payload := mailConfigPayload{
		Enabled:  s.mailer.Enabled(),
		Host:     s.cfg.SMTP.Host,
		Port:     s.cfg.SMTP.Port,
		Username: s.cfg.SMTP.Username,
		Password: s.cfg.SMTP.Password,
		From:     s.cfg.SMTP.From,
		UseTLS:   s.cfg.SMTP.UseTLS,
	}
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key = 'mail.smtp'`).Scan(&raw)
	if err != nil {
		_ = ignoreNoRows(err)
		return payload
	}
	if err := s.openSystemSetting(raw, &payload); err != nil {
		return payload
	}
	payload.Enabled = strings.TrimSpace(payload.Host) != "" && strings.TrimSpace(payload.From) != ""
	return payload
}

func (s *Server) activeMailer(ctx context.Context) mailer.Mailer {
	payload := s.mailConfigFromSettings(ctx)
	return mailer.New(config.SMTPConfig{
		Host:     payload.Host,
		Port:     payload.Port,
		Username: payload.Username,
		Password: payload.Password,
		From:     payload.From,
		UseTLS:   payload.UseTLS,
	})
}

func redactMailConfig(payload mailConfigPayload) map[string]any {
	return map[string]any{
		"enabled":     payload.Enabled,
		"host":        payload.Host,
		"port":        payload.Port,
		"username":    payload.Username,
		"from":        payload.From,
		"useTLS":      payload.UseTLS,
		"hasPassword": strings.TrimSpace(payload.Password) != "",
	}
}

func adminNavigation() []map[string]any {
	return []map[string]any{
		{"id": "overview", "label": "统计", "items": []string{"总览", "用户统计", "上传统计", "搜索统计", "AI 调用统计"}},
		{"id": "content", "label": "内容管理", "items": []string{"模组 Mod", "模组导入数据源", "整合包", "插件 Plugin", "衍生资源", "教程", "新闻", "问题 / 讨论"}},
		{"id": "users", "label": "用户", "items": []string{"用户列表", "登录记录", "设备记录", "账号安全", "用户封禁"}},
		{"id": "notifications", "label": "通知系统", "items": []string{"系统通知", "通知模板"}},
		{"id": "permissions", "label": "权限", "items": []string{"权限组", "用户权限", "权限列表", "权限模板", "临时权限", "权限审计日志"}},
		{"id": "oss", "label": "OSS 管理", "items": []string{"OSS 链接设置", "OSS 文件目录", "文件上传记录", "文件查杀记录", "下载统计"}},
		{"id": "logs", "label": "日志", "items": []string{"系统运行日志", "用户交互日志", "管理员操作日志", "权限变更日志", "登录安全日志", "API 访问日志", "文件上传日志", "AI 调用日志"}},
		{"id": "infrastructure", "label": "基础设施", "items": []string{"NATS 设置"}},
		{"id": "review", "label": "审核", "items": []string{"待审核项", "新建内容审核", "编辑审核", "文件审核", "申请审核", "举报审核", "申诉审核"}},
		{"id": "security", "label": "安全", "items": []string{"被封禁用户列表", "IP 黑名单", "设备黑名单", "风险账号"}},
		{"id": "mail", "label": "邮件系统", "items": []string{"SMTP 配置", "验证码模板", "安全通知模板", "测试发送", "发送日志"}},
		{"id": "settings", "label": "系统设置", "items": []string{"系统信息", "标签管理", "防御模式", "内容安全", "主题", "备份", "功能开关", "维护模式"}},
	}
}
