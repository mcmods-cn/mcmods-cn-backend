package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

var usernamePattern = regexp.MustCompile(`^[\p{Han}A-Za-z0-9_]+$`)
var verificationCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

var reservedUsernames = map[string]struct{}{
	"admin": {}, "root": {}, "system": {}, "api": {}, "login": {}, "register": {},
	"mods": {}, "mod": {}, "users": {}, "settings": {}, "null": {}, "undefined": {},
}

type registerRequest struct {
	Username                 string `json:"username"`
	Email                    string `json:"email"`
	Code                     string `json:"code"`
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
	req.Code = strings.TrimSpace(req.Code)
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
	if !validEmailAddress(req.Email) {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	if !verificationCodePattern.MatchString(req.Code) {
		writeError(w, http.StatusBadRequest, "邮箱验证码格式不正确")
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 1024 {
		writeError(w, http.StatusBadRequest, "密码长度需要在 8 到 1024 字节之间")
		return
	}
	if len([]rune(req.DisplayName)) > 64 || len(req.Country) > 64 || len(req.Timezone) > 128 {
		writeError(w, http.StatusBadRequest, "注册资料过长")
		return
	}

	location := s.requestClientLocation(r)
	rateLimited, err := s.registrationRateLimited(r.Context(), location.IP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registration rate-limit check failed")
		return
	}
	if rateLimited {
		writeError(w, http.StatusTooManyRequests, "too many registration requests; please try again later")
		return
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
	if err := consumeEmailVerificationCodeTx(r.Context(), tx, req.Email, "register", req.Code); err != nil {
		writeError(w, http.StatusUnauthorized, "验证码不正确或已过期")
		return
	}

	var user domain.User
	err = tx.QueryRow(
		r.Context(),
		`insert into users (
		     username, email, display_name, password_hash,
		     country, timezone, preferred_content_language, secondary_content_language, preferred_ui_language,
		     registration_ip, registration_country_code, registration_city, email_verified
		 )
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, true)
		 returning id, public_id, username, email, display_name, email_verified, status, created_at, last_login_at`,
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
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.DisplayName, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt)
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
	token, err := s.issueToken(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成登录凭证失败")
		return
	}
	s.setAuthSessionCookie(w, r, token)
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	account := strings.TrimSpace(req.Account)
	if account == "" || len(account) > 320 || len(req.Password) > 1024 {
		writeError(w, http.StatusBadRequest, "账号或密码格式不正确")
		return
	}
	location := s.requestClientLocation(r)
	rateLimited, err := s.authAttemptRateLimited(r.Context(), account, location.IP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login rate-limit check failed")
		return
	}
	if rateLimited {
		writeError(w, http.StatusTooManyRequests, "too many failed login attempts; please try again later")
		return
	}
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
	token, err := s.issueToken(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成登录凭证失败")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update users set last_login_at = now(), updated_at = now() where id = $1`, user.ID)
	s.recordLogin(r.Context(), &user.ID, account, location, r.UserAgent(), true, "")
	s.setAuthSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
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
	if req.Purpose != "login" && req.Purpose != "register" {
		writeError(w, http.StatusBadRequest, "unsupported verification-code purpose")
		return
	}
	if !validEmailAddress(req.Email) {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	if req.Purpose == "register" {
		var exists bool
		if err := s.db.QueryRow(r.Context(), `select exists(select 1 from users where lower(email)=lower($1))`, req.Email).Scan(&exists); err != nil {
			writeError(w, http.StatusInternalServerError, "邮箱检查失败")
			return
		}
		if exists {
			writeError(w, http.StatusConflict, "该邮箱已被注册")
			return
		}
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
	location := s.requestClientLocation(r)
	codeID, err := s.storeEmailVerificationCode(
		r.Context(),
		req.Email,
		req.Purpose,
		codeHash,
		location.IP,
		time.Now().Add(10*time.Minute),
	)
	if err != nil {
		if err == errAuthRateLimited {
			writeError(w, http.StatusTooManyRequests, "too many verification-code requests; please try again later")
			return
		}
		writeError(w, http.StatusInternalServerError, "验证码保存失败")
		return
	}

	subject := "Mcmods-cn 登录验证码"
	if req.Purpose == "register" {
		subject = "Mcmods-cn 注册验证码"
	}
	body := fmt.Sprintf("你的验证码是：%s\n\n验证码 10 分钟内有效。如果不是你本人操作，请忽略这封邮件。", code)
	if err := s.activeMailer(r.Context()).Send(req.Email, subject, body); err != nil {
		_, _ = s.db.Exec(r.Context(), `update email_verification_codes set consumed_at=now() where id=$1`, codeID)
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
	location := s.requestClientLocation(r)
	if !validEmailAddress(req.Email) || !verificationCodePattern.MatchString(req.Code) {
		s.recordLogin(r.Context(), nil, req.Email, location, r.UserAgent(), false, "invalid_email_code_format")
		writeError(w, http.StatusUnauthorized, "验证码不正确或已过期")
		return
	}

	rateLimited, err := s.authAttemptRateLimited(r.Context(), req.Email, location.IP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login rate-limit check failed")
		return
	}
	if rateLimited {
		writeError(w, http.StatusTooManyRequests, "too many failed login attempts; please try again later")
		return
	}
	if err := s.consumeEmailLoginCode(r.Context(), req.Email, req.Code); err != nil {
		s.recordLogin(r.Context(), nil, req.Email, location, r.UserAgent(), false, "invalid_email_code")
		writeError(w, http.StatusUnauthorized, "验证码不正确或已过期")
		return
	}
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
	token, err := s.issueToken(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成登录凭证失败")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update users set last_login_at = now(), updated_at = now() where id = $1`, user.ID)
	s.recordLogin(r.Context(), &user.ID, req.Email, location, r.UserAgent(), true, "email_code")
	s.setAuthSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) consumeEmailLoginCode(ctx context.Context, email, code string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := consumeEmailVerificationCodeTx(ctx, tx, email, "login", code); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func consumeEmailVerificationCodeTx(ctx context.Context, tx pgx.Tx, email, purpose, code string) error {
	var codeID int64
	var codeHash string
	if err := tx.QueryRow(
		ctx,
		`select id,code_hash
		 from email_verification_codes
		 where email=$1 and purpose=$2 and consumed_at is null and expires_at>now()
		 order by id desc
		 limit 1
		 for update`,
		email,
		purpose,
	).Scan(&codeID, &codeHash); err != nil {
		return err
	}
	if !security.VerifyCode(code, codeHash) {
		return fmt.Errorf("invalid email verification code")
	}
	if _, err := tx.Exec(ctx, `update email_verification_codes set consumed_at=now() where id=$1 and consumed_at is null`, codeID); err != nil {
		return err
	}
	return nil
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

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	_, _ = s.db.Exec(r.Context(), `update auth_sessions set revoked_at=coalesce(revoked_at,now())
		where session_hash=$1 and user_id=$2`, security.SessionFingerprint(claims.SessionID), claims.Subject)
	s.clearAuthSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) issueToken(ctx context.Context, user domain.User) (string, error) {
	var authVersion int64
	if err := s.db.QueryRow(ctx, `select auth_version from users where id=$1 and status='active'`, user.ID).Scan(&authVersion); err != nil {
		return "", err
	}
	claims, err := security.NewClaims(user.PublicID, user.Username, user.Email, user.Roles, user.Permissions, authVersion, s.cfg.JWTTTL)
	if err != nil {
		return "", err
	}
	token, err := security.SignToken(s.cfg.JWTSecret, claims)
	if err != nil {
		return "", err
	}
	if _, err = s.db.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		values($1,$2,$3,$4)`, security.SessionFingerprint(claims.SessionID), user.ID, authVersion, time.Unix(claims.ExpiresAt, 0)); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Server) findUserForLogin(ctx context.Context, account string) (domain.User, string, error) {
	var user domain.User
	var passwordHash string
	query := `select id, public_id, username, email, display_name, password_hash, email_verified, status, created_at, last_login_at, avatar_url, signature
		from users
		where lower(email) = lower($1) or lower(username) = lower($1) or public_id = lower($1)`
	args := []any{account}
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&user.ID,
		&user.PublicID,
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
		`select id, public_id, username, email, display_name, email_verified, status, created_at, last_login_at, avatar_url, signature
		 from users where lower(email) = lower($1)`,
		email,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.DisplayName, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature)
	return user, err
}

func (s *Server) findUserByID(ctx context.Context, id int64) (domain.User, error) {
	var user domain.User
	err := s.db.QueryRow(
		ctx,
		`select id, public_id, username, email, display_name, email_verified, status, created_at, last_login_at, avatar_url, signature
		 from users where id = $1`,
		id,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.DisplayName, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature)
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

	permissions := make([]string, 0, len(roles)*2)
	for _, role := range roles {
		permissions = append(permissions, "group."+role)
	}
	permissionRows, err := s.db.Query(
		ctx,
		`select p.code
		 from permissions p
		 join role_permissions rp on rp.permission_id=p.id
		 join user_role_bindings urb on urb.role_id=rp.role_id
		 where urb.user_id=$1 and rp.allow=true and (rp.expires_at is null or rp.expires_at>now())
		 union all
		 select p.code
		 from permissions p
		 join user_permissions up on up.permission_id=p.id
		 where up.user_id=$1 and up.allow=true and (up.expires_at is null or up.expires_at>now())`,
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
	templateRows, err := s.db.Query(
		ctx,
		`select r.code,p.code
		 from roles r
		 join role_permissions rp on rp.role_id=r.id
		 join permissions p on p.id=rp.permission_id
		 where (r.code like '%[%' or r.code like '%<%')
		   and rp.allow=true
		   and (rp.expires_at is null or rp.expires_at>now())
		 order by length(r.code) desc,r.code,p.code`,
	)
	if err == nil {
		defer templateRows.Close()
		templatePermissions := make(map[string][]string)
		for templateRows.Next() {
			var templateCode, permission string
			if scanErr := templateRows.Scan(&templateCode, &permission); scanErr == nil {
				templatePermissions[templateCode] = append(templatePermissions[templateCode], permission)
			}
		}
		for _, role := range roles {
			for templateCode, codes := range templatePermissions {
				variables, matches := matchRoleTemplateCode(templateCode, role)
				if !matches {
					continue
				}
				for _, code := range codes {
					permissions = append(permissions, applyRoleVariables(code, variables))
				}
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

func validEmailAddress(email string) bool {
	if len(email) == 0 || len(email) > 320 {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(address.Address, email)
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
