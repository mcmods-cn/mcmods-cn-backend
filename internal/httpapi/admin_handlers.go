package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

type adminCreateUserRequest struct {
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Status   string   `json:"status"`
	Roles    []string `json:"roles"`
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
	ExpiresAt string `json:"expiresAt,omitempty"`
	Source    string `json:"source,omitempty"`
	SourceKey string `json:"sourceKey,omitempty"`
	Editable  bool   `json:"editable,omitempty"`
}

type updateUserPermissionsRequest struct {
	Permissions []userPermissionEntry `json:"permissions"`
}

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-zA-Z0-9_*:\[\]<>-]+)*$`)
var permissionCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-zA-Z0-9_*:\[\]<>-]+)+$`)

func (s *Server) adminConfig(w http.ResponseWriter, r *http.Request) {
	mailCfg, err := s.mailConfigFromSettings(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "MAIL_SETTINGS_UNAVAILABLE", "mail settings are temporarily unavailable", 0, nil)
		return
	}
	general, err := s.readSiteGeneralConfig(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "SITE_SETTINGS_UNAVAILABLE", "site settings are temporarily unavailable", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"general": general,
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
		"oauth":     redactOAuthConfig(s.oauthConfig(r.Context())),
		"mail":      redactMailConfig(mailCfg),
		"oss":       redactOSSConfig(s.ossConfigFromSettings(r.Context())),
		"markdown":  s.markdownConfigFromSettings(r.Context()),
		"profile":   s.profileConfigFromSettings(r.Context()),
		"yggdrasil": s.redactedYggdrasilConfig(),
		"ai":        redactAIConfig(s.aiConfigFromSettings(r.Context())),
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
	if payload.Enabled && !mailer.New(smtpConfigFromPayload(payload)).Enabled() {
		writeError(w, http.StatusBadRequest, "启用邮件系统需要有效的服务器、端口和发件地址")
		return
	}
	payload, err := s.saveMailConfig(r.Context(), payload, currentClaims(r).Subject)
	if err != nil {
		if errors.Is(err, errMailSettingsUnavailable) {
			writeAPIError(w, http.StatusServiceUnavailable, "MAIL_SETTINGS_UNAVAILABLE", "mail settings are temporarily unavailable", 0, nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "保存邮件配置失败")
		return
	}
	s.invalidateSettingsCache(r.Context())
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
	active, err := s.activeMailer(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "MAIL_SETTINGS_UNAVAILABLE", "mail settings are temporarily unavailable", 0, nil)
		return
	}
	if err := active.Send(req.To, "Mcmods-cn 邮件系统测试", "这是一封来自后台管理界面的测试邮件。"); err != nil {
		slog.Warn("send admin test email", "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, "MAIL_DELIVERY_FAILED", "mail delivery failed", 0, nil)
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
	if !s.requireSecurityVersionRefresh(w, r, "upsert_permission_node", 0, s.refreshRBACVersion(r.Context())) {
		return
	}
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
	if err = lockRoleGraphMutationTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "锁定权限组图失败")
		return
	}

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
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "权限组代码已存在")
		} else {
			writeError(w, http.StatusInternalServerError, "创建权限组失败")
		}
		return
	}
	if err = validateRoleGraphTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	if !s.requireSecurityVersionRefresh(w, r, "create_role", 0, s.refreshRBACVersion(r.Context())) {
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
	if err = lockRoleGraphMutationTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "锁定权限组图失败")
		return
	}

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
	if err = validateRoleGraphTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	if !s.requireSecurityVersionRefresh(w, r, "update_role", 0, s.refreshRBACVersion(r.Context())) {
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
	if err = lockRoleGraphMutationTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "锁定权限组图失败")
		return
	}

	var roleID int64
	if err := tx.QueryRow(r.Context(), `select id from roles where code=$1 for update`, code).Scan(&roleID); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "权限组不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取权限组失败")
		return
	}
	blockers, err := roleDeletionBlockersTx(r.Context(), tx, roleID, code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "检查权限组依赖失败")
		return
	}
	if len(blockers) != 0 {
		writeError(w, http.StatusConflict, "权限组仍被引用: "+strings.Join(blockers, ", "))
		return
	}
	tag, err := tx.Exec(r.Context(), `delete from roles where id=$1`, roleID)
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
	if !s.requireSecurityVersionRefresh(w, r, "delete_role", 0, s.refreshRBACVersion(r.Context())) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := s.db.Query(
		r.Context(),
		`select id, public_id, username, email, email_verified, status, created_at, last_login_at
		 from users
		 where $1 = ''
		    or public_id = lower($1)
		    or username ilike '%' || $1 || '%'
		    or email ilike '%' || $1 || '%'
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
	userIDs := make([]int64, 0)
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt); err != nil {
			writeError(w, http.StatusInternalServerError, "读取用户数据失败")
			return
		}
		users = append(users, user)
		userIDs = append(userIDs, user.ID)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户列表失败")
		return
	}
	rows.Close()
	resolvedByUser, err := s.resolveUsersRootPermissions(r.Context(), userIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user permissions")
		return
	}
	for index := range users {
		users[index].RoleCodes = resolvedByUser[users[index].ID].Roles
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
	req.Status = strings.TrimSpace(req.Status)
	req.Roles = normalizeCodes(req.Roles)
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
		`insert into users (username, email, password_hash, email_verified, status)
		 values ($1, $2, $3, true, $4)
		 returning id, public_id, username, email, email_verified, status, created_at, last_login_at`,
		req.Username,
		req.Email,
		passwordHash,
		req.Status,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt)
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
			`insert into user_role_bindings (user_id, role_id, source, source_key)
			 select $1, id, $3, '' from roles where code = $2
			 on conflict do nothing`,
			user.ID,
			role,
			authorizationSourceManual,
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
	roleCodes, _, permissionErr := s.resolveUserRootPermissions(r.Context(), user.ID)
	if permissionErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user permissions")
		return
	}
	user.RoleCodes = roleCodes
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) userPermissionDetails(w http.ResponseWriter, r *http.Request) {
	identity, err := s.resolvePublicIdentity(r.Context(), r.PathValue("id"), "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return
	}
	userID := identity.InternalID
	roles, effectivePermissionRules, err := s.resolveUserRootPermissions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user permissions")
		return
	}
	roleRows, err := s.db.Query(
		r.Context(),
		`select role.code,binding.expires_at,binding.source,binding.source_key
		 from user_role_bindings binding join roles role on role.id=binding.role_id
		 where binding.user_id=$1
		 order by role.code,binding.source,binding.source_key`,
		userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户权限组失败")
		return
	}
	roleBindings := make([]userPermissionEntry, 0)
	groupPermissions := make([]string, 0)
	for roleRows.Next() {
		var code, source, sourceKey string
		var expiresAt *time.Time
		if err = roleRows.Scan(&code, &expiresAt, &source, &sourceKey); err != nil {
			roleRows.Close()
			writeError(w, http.StatusInternalServerError, "读取用户权限组数据失败")
			return
		}
		entry := userPermissionEntry{Code: "group." + code, Allow: true, Source: source,
			SourceKey: sourceKey, Editable: source == authorizationSourceManual}
		if expiresAt != nil {
			entry.ExpiresAt = expiresAt.Format(time.RFC3339)
		}
		roleBindings = append(roleBindings, entry)
		if entry.Editable {
			groupPermissions = append(groupPermissions, entry.Code)
		}
	}
	if err = roleRows.Err(); err != nil {
		roleRows.Close()
		writeError(w, http.StatusInternalServerError, "读取用户权限组数据失败")
		return
	}
	roleRows.Close()

	rows, err := s.db.Query(
		r.Context(),
		`select p.code,up.allow,up.expires_at,up.source,up.source_key
		 from user_permissions up
		 join permissions p on p.id = up.permission_id
		 where up.user_id = $1
		 order by p.code,up.source,up.source_key`,
		userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户权限失败")
		return
	}
	direct := make([]userPermissionEntry, 0)
	for rows.Next() {
		var entry userPermissionEntry
		var expiresAt *time.Time
		if err := rows.Scan(&entry.Code, &entry.Allow, &expiresAt, &entry.Source, &entry.SourceKey); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "读取用户权限数据失败")
			return
		}
		entry.Editable = entry.Source == authorizationSourceManual
		if expiresAt != nil {
			entry.ExpiresAt = expiresAt.Format(time.RFC3339)
		}
		direct = append(direct, entry)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "读取用户权限数据失败")
		return
	}
	rows.Close()
	writeJSON(w, http.StatusOK, map[string]any{
		"roles":                    roles,
		"groupPermissions":         groupPermissions,
		"roleBindings":             roleBindings,
		"directPermissions":        direct,
		"effectivePermissionRules": effectivePermissionRules,
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
	manualRoles, manualPermissions, normalizeErr := normalizeManualAuthorizationEntries(req.Permissions)
	if normalizeErr != nil {
		writeError(w, http.StatusBadRequest, normalizeErr.Error())
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库事务创建失败")
		return
	}
	defer tx.Rollback(r.Context())

	for _, role := range manualRoles {
		if err := s.ensureRoleForBinding(r.Context(), tx, role.Code); err != nil {
			if requestErr, ok := err.(*requestError); ok {
				writeError(w, http.StatusBadRequest, requestErr.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "绑定权限组失败")
			return
		}
	}
	for _, permission := range manualPermissions {
		if err := ensurePermissionNode(r.Context(), tx, permission.Code); err != nil {
			writeError(w, http.StatusInternalServerError, "保存权限节点失败")
			return
		}
	}
	if err = replaceManualUserAuthorizationTx(r.Context(), tx, userID, manualRoles, manualPermissions); err != nil {
		writeError(w, http.StatusInternalServerError, "保存用户权限失败")
		return
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
	if !s.requireSecurityVersionRefresh(w, r, "update_user_permissions", userID,
		s.refreshPermissionVersion(r.Context(), userID), s.refreshRBACVersion(r.Context())) {
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
		roles = append(roles, role)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	roleCodes := make([]string, len(roles))
	for index := range roles {
		roleCodes[index] = roles[index].Code
	}
	entriesByRole, err := s.rolePermissionEntriesForRoles(ctx, roleCodes)
	if err != nil {
		return nil, err
	}
	for index := range roles {
		roles[index].PermissionEntries = entriesByRole[roles[index].Code]
		roles[index].Permissions = rolePermissionCodes(roles[index].PermissionEntries)
	}
	return roles, nil
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
	role.PermissionEntries, err = s.rolePermissionEntries(ctx, role.Code)
	if err != nil {
		return role, err
	}
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

func (s *Server) rolePermissionEntries(ctx context.Context, roleCode string) ([]domain.RolePermissionEntry, error) {
	entriesByRole, err := s.rolePermissionEntriesForRoles(ctx, []string{roleCode})
	if err != nil {
		return nil, err
	}
	return entriesByRole[roleCode], nil
}

func (s *Server) rolePermissionEntriesForRoles(ctx context.Context, roleCodes []string) (map[string][]domain.RolePermissionEntry, error) {
	entriesByRole := make(map[string][]domain.RolePermissionEntry, len(roleCodes))
	for _, roleCode := range roleCodes {
		entriesByRole[roleCode] = []domain.RolePermissionEntry{}
	}
	if len(roleCodes) == 0 {
		return entriesByRole, nil
	}
	rows, err := s.db.Query(
		ctx,
		`select r.code,p.code,rp.allow,rp.expires_at
		 from permissions p
		 join role_permissions rp on rp.permission_id = p.id
		 join roles r on r.id = rp.role_id
		 where r.code=any($1)
		 order by r.code,p.code`,
		roleCodes,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var roleCode string
		var entry domain.RolePermissionEntry
		var expiresAt *time.Time
		if err := rows.Scan(&roleCode, &entry.Code, &entry.Allow, &expiresAt); err != nil {
			return nil, err
		}
		if expiresAt != nil {
			entry.ExpiresAt = expiresAt.Format(time.RFC3339)
		}
		entriesByRole[roleCode] = append(entriesByRole[roleCode], entry)
	}
	return entriesByRole, rows.Err()
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

func smtpConfigFromPayload(payload mailConfigPayload) config.SMTPConfig {
	return config.SMTPConfig{
		Enabled: payload.Enabled, Host: payload.Host, Port: payload.Port,
		Username: payload.Username, Password: payload.Password, From: payload.From, UseTLS: payload.UseTLS,
	}
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
