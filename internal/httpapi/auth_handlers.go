package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

var usernamePattern = regexp.MustCompile(`^[\p{Han}A-Za-z0-9_]+$`)

var reservedUsernames = map[string]struct{}{
	"admin": {}, "root": {}, "system": {}, "api": {}, "login": {}, "register": {},
	"mods": {}, "mod": {}, "users": {}, "settings": {}, "null": {}, "undefined": {},
}

type registerRequest struct {
	Username                 string `json:"username"`
	Email                    string `json:"email"`
	Password                 string `json:"password"`
	DisplayName              string `json:"displayName"`
	Country                  string `json:"country"`
	Timezone                 string `json:"timezone"`
	PreferredContentLanguage string `json:"preferredContentLanguage"`
	SecondaryContentLanguage string `json:"secondaryContentLanguage"`
	PreferredUILanguage      string `json:"preferredUILanguage"`
}

type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type emailCodeRequest struct {
	Email   string `json:"email"`
	Purpose string `json:"purpose"`
}

type emailLoginRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = normalizeEmail(req.Email)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Country = strings.TrimSpace(req.Country)
	req.Timezone = defaultString(strings.TrimSpace(req.Timezone), "Asia/Shanghai")
	req.PreferredContentLanguage = normalizeContentLocale(defaultString(strings.TrimSpace(req.PreferredContentLanguage), "zh-CN"))
	req.SecondaryContentLanguage = normalizeContentLocale(defaultString(strings.TrimSpace(req.SecondaryContentLanguage), "en-US"))
	req.PreferredUILanguage = normalizeContentLocale(defaultString(strings.TrimSpace(req.PreferredUILanguage), "en-US"))
	if !validContentLocaleTag(req.PreferredContentLanguage) || !validContentLocaleTag(req.SecondaryContentLanguage) {
		writeError(w, http.StatusBadRequest, "content language preference is invalid")
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
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

	passwordHash, err := security.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	location := requestClientLocation(r)

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "数据库事务创建失败")
		return
	}
	defer tx.Rollback(r.Context())

	var user domain.User
	err = tx.QueryRow(
		r.Context(),
		`insert into users (
		     username, email, display_name, password_hash,
		     country, timezone, preferred_content_language, secondary_content_language, preferred_ui_language,
		     registration_ip, registration_country_code, registration_city
		 )
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 returning id, username, email, display_name, email_verified, status, created_at, last_login_at`,
		req.Username,
		req.Email,
		req.DisplayName,
		passwordHash,
		req.Country,
		req.Timezone,
		req.PreferredContentLanguage,
		req.SecondaryContentLanguage,
		req.PreferredUILanguage,
		location.IP,
		location.CountryCode,
		location.City,
	).Scan(&user.ID, &user.Username, &user.Email, &user.DisplayName, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt)
	if err != nil {
		writeError(w, http.StatusConflict, "用户名或邮箱已被占用")
		return
	}
	if err := s.assignConfiguredRoleTx(r.Context(), tx, user.ID, "registered"); err != nil {
		writeError(w, http.StatusInternalServerError, "分配新用户权限组失败")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "注册失败")
		return
	}

	user.Roles, user.Permissions = s.userGrants(r.Context(), user.ID)
	token, err := s.issueToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成登录凭证失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": user})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	account := strings.TrimSpace(req.Account)
	location := requestClientLocation(r)
	user, passwordHash, err := s.findUserForLogin(r.Context(), account)
	if err != nil || !security.VerifyPassword(req.Password, passwordHash) {
		s.recordLogin(r.Context(), nil, account, location, r.UserAgent(), false, "invalid_credentials")
		writeError(w, http.StatusUnauthorized, "账号或密码不正确")
		return
	}
	if user.Status != "active" {
		s.recordLogin(r.Context(), &user.ID, account, location, r.UserAgent(), false, "disabled")
		writeError(w, http.StatusForbidden, "账号已被禁用")
		return
	}

	user.Roles, user.Permissions = s.userGrants(r.Context(), user.ID)
	token, err := s.issueToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成登录凭证失败")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update users set last_login_at = now(), updated_at = now() where id = $1`, user.ID)
	s.recordLogin(r.Context(), &user.ID, account, location, r.UserAgent(), true, "")
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

func (s *Server) requestEmailCode(w http.ResponseWriter, r *http.Request) {
	var req emailCodeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Email = normalizeEmail(req.Email)
	req.Purpose = strings.TrimSpace(req.Purpose)
	if req.Purpose == "" {
		req.Purpose = "login"
	}
	if !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}

	code, err := randomCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "验证码生成失败")
		return
	}
	codeHash, err := security.HashCode(code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "验证码处理失败")
		return
	}
	_, err = s.db.Exec(
		r.Context(),
		`insert into email_verification_codes (email, purpose, code_hash, expires_at)
		 values ($1, $2, $3, $4)`,
		req.Email,
		req.Purpose,
		codeHash,
		time.Now().Add(10*time.Minute),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "验证码保存失败")
		return
	}

	subject := "Mcmods-cn 登录验证码"
	body := fmt.Sprintf("你的验证码是：%s\n\n验证码 10 分钟内有效。如果不是你本人操作，请忽略这封邮件。", code)
	if err := s.activeMailer(r.Context()).Send(req.Email, subject, body); err != nil {
		writeError(w, http.StatusServiceUnavailable, "验证码已生成，但邮件服务尚未配置或发送失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "expiresInSeconds": 600})
}

func (s *Server) emailLogin(w http.ResponseWriter, r *http.Request) {
	var req emailLoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Email = normalizeEmail(req.Email)
	req.Code = strings.TrimSpace(req.Code)
	location := requestClientLocation(r)

	var codeID int64
	var codeHash string
	err := s.db.QueryRow(
		r.Context(),
		`select id, code_hash
		 from email_verification_codes
		 where email = $1 and purpose = 'login' and consumed_at is null and expires_at > now()
		 order by id desc
		 limit 1`,
		req.Email,
	).Scan(&codeID, &codeHash)
	if err != nil || !security.VerifyCode(req.Code, codeHash) {
		writeError(w, http.StatusUnauthorized, "验证码不正确或已过期")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update email_verification_codes set consumed_at = now() where id = $1`, codeID)

	user, err := s.findUserByEmail(r.Context(), req.Email)
	if err != nil {
		s.recordLogin(r.Context(), nil, req.Email, location, r.UserAgent(), false, "email_not_registered")
		writeError(w, http.StatusNotFound, "该邮箱尚未注册")
		return
	}
	if user.Status != "active" {
		s.recordLogin(r.Context(), &user.ID, req.Email, location, r.UserAgent(), false, "disabled")
		writeError(w, http.StatusForbidden, "账号已被禁用")
		return
	}
	user.Roles, user.Permissions = s.userGrants(r.Context(), user.ID)
	token, err := s.issueToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成登录凭证失败")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update users set last_login_at = now(), updated_at = now() where id = $1`, user.ID)
	s.recordLogin(r.Context(), &user.ID, req.Email, location, r.UserAgent(), true, "email_code")
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	user, err := s.findUserByID(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	user.Roles, user.Permissions = s.userGrants(r.Context(), user.ID)
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) evaluatePermission(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "权限名称不能为空")
		return
	}
	value := numericPermissionValue(claims.Permissions, code)
	writeJSON(w, http.StatusOK, map[string]any{
		"permission": code,
		"allowed":    hasPermission(claims.Permissions, code) || value > 0,
		"value":      value,
	})
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) issueToken(user domain.User) (string, error) {
	claims := security.NewClaims(user.ID, user.Username, user.Email, user.Roles, user.Permissions, s.cfg.JWTTTL)
	return security.SignToken(s.cfg.JWTSecret, claims)
}

func (s *Server) findUserForLogin(ctx context.Context, account string) (domain.User, string, error) {
	var user domain.User
	var passwordHash string
	query := `select id, username, email, display_name, password_hash, email_verified, status, created_at, last_login_at, avatar_url, signature
		from users
		where lower(email) = lower($1) or lower(username) = lower($1)`
	args := []any{account}
	if id, err := strconv.ParseInt(account, 10, 64); err == nil {
		query += ` or id = $2`
		args = append(args, id)
	}
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.DisplayName,
		&passwordHash,
		&user.EmailVerified,
		&user.Status,
		&user.CreatedAt,
		&user.LastLoginAt,
		&user.AvatarURL,
		&user.Signature,
	)
	return user, passwordHash, err
}

func (s *Server) findUserByEmail(ctx context.Context, email string) (domain.User, error) {
	var user domain.User
	err := s.db.QueryRow(
		ctx,
		`select id, username, email, display_name, email_verified, status, created_at, last_login_at, avatar_url, signature
		 from users where lower(email) = lower($1)`,
		email,
	).Scan(&user.ID, &user.Username, &user.Email, &user.DisplayName, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature)
	return user, err
}

func (s *Server) findUserByID(ctx context.Context, id int64) (domain.User, error) {
	var user domain.User
	err := s.db.QueryRow(
		ctx,
		`select id, username, email, display_name, email_verified, status, created_at, last_login_at, avatar_url, signature
		 from users where id = $1`,
		id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.DisplayName, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature)
	return user, err
}

func (s *Server) userGrants(ctx context.Context, userID int64) ([]string, []string) {
	roles := make([]string, 0)
	roleRows, err := s.db.Query(ctx, `select r.code from roles r join user_role_bindings b on b.role_id = r.id where b.user_id = $1 order by r.code`, userID)
	if err == nil {
		defer roleRows.Close()
		for roleRows.Next() {
			var role string
			if err := roleRows.Scan(&role); err == nil {
				roles = append(roles, role)
			}
		}
	}

	permissions := make([]string, 0)
	for _, role := range roles {
		permissions = append(permissions, "group."+role)
		permissions = append(permissions, s.rolePermissions(ctx, role)...)
	}
	permissionRows, err := s.db.Query(
		ctx,
		`select p.code
		 from permissions p
		 join user_permissions up on up.permission_id = p.id
		 where up.user_id = $1 and up.allow = true and (up.expires_at is null or up.expires_at > now())
		 order by p.code`,
		userID,
	)
	if err == nil {
		defer permissionRows.Close()
		for permissionRows.Next() {
			var permission string
			if err := permissionRows.Scan(&permission); err == nil {
				permissions = append(permissions, permission)
			}
		}
	}
	return roles, normalizeCodes(permissions)
}

func (s *Server) recordLogin(ctx context.Context, userID *int64, account string, location clientLocation, userAgent string, success bool, reason string) {
	_, _ = s.db.Exec(
		ctx,
		`insert into user_login_logs (user_id, account, ip, country_code, city, user_agent, success, reason)
		 values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userID,
		account,
		location.IP,
		location.CountryCode,
		location.City,
		userAgent,
		success,
		reason,
	)
}

func validateUsername(username string) error {
	if len([]rune(username)) < 3 || len([]rune(username)) > 24 {
		return fmt.Errorf("用户名长度需要在 3 到 24 个字符之间")
	}
	if _, exists := reservedUsernames[strings.ToLower(username)]; exists {
		return fmt.Errorf("该用户名为系统保留词")
	}
	if _, err := strconv.ParseInt(username, 10, 64); err == nil {
		return fmt.Errorf("用户名不能为纯数字")
	}
	if !usernamePattern.MatchString(username) {
		return fmt.Errorf("用户名只能包含中文、字母、数字和下划线")
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func randomCode() (string, error) {
	max := big.NewInt(1000000)
	value, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func ignoreNoRows(err error) error {
	if err == pgx.ErrNoRows {
		return nil
	}
	return err
}
