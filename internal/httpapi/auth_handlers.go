package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

var verificationCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

var errInvalidEmailVerificationCode = errors.New("invalid email verification code")

const loginDummyPasswordHash = "argon2id$v=19$m=65536,t=3,p=2$bWNtb2RzLWxvZ2luLXBhZA$qQ5NxyYK2yue8JX3kj4JZleY7qpvnj43TJm5qJKxDAE"

var reservedUsernames = map[string]struct{}{
	"admin": {}, "root": {}, "system": {}, "api": {}, "login": {}, "register": {},
	"mods": {}, "mod": {}, "users": {}, "settings": {}, "null": {}, "undefined": {},
}

type registerRequest struct {
	Username                 string `json:"username"`
	Email                    string `json:"email"`
	Code                     string `json:"code"`
	Password                 string `json:"password"`
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

type loginAccount struct {
	domain.User
	authVersion int64
}

type authenticatedUserResponse struct {
	domain.User
	PermissionRules   []security.PermissionRule `json:"permissionRules"`
	PermissionVersion int64                     `json:"permissionVersion"`
	RBACVersion       int64                     `json:"rbacVersion"`
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
	req.Country = strings.TrimSpace(req.Country)
	req.Timezone = defaultString(strings.TrimSpace(req.Timezone), "Asia/Shanghai")
	var timezoneErr error
	req.Timezone, timezoneErr = normalizeTimezone(req.Timezone)
	if timezoneErr != nil {
		writeError(w, http.StatusBadRequest, timezoneErr.Error())
		return
	}
	req.PreferredContentLanguage = normalizeContentLocale(defaultString(strings.TrimSpace(req.PreferredContentLanguage), "zh-CN"))
	req.SecondaryContentLanguage = normalizeContentLocale(defaultString(strings.TrimSpace(req.SecondaryContentLanguage), "en-US"))
	req.PreferredUILanguage = normalizeContentLocale(defaultString(strings.TrimSpace(req.PreferredUILanguage), "en-US"))
	if !validContentLocaleTag(req.PreferredContentLanguage) || !isEditableContentLocale(req.SecondaryContentLanguage) {
		writeError(w, http.StatusBadRequest, "content language preference is invalid")
		return
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
	if !security.ValidPasswordLength(req.Password) {
		writeError(w, http.StatusBadRequest, "密码长度需要在 8 到 1024 字节之间")
		return
	}
	if len(req.Country) > 64 {
		writeError(w, http.StatusBadRequest, "注册资料过长")
		return
	}

	location := s.requestClientLocation(r)
	rateLimited, err := s.registrationRateLimited(r.Context(), location.IP, r.Header.Get("X-Client-ID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registration rate-limit check failed")
		return
	}
	if rateLimited {
		writeAPIError(w, http.StatusTooManyRequests, "auth_rate_limited", "too many registration requests; please try again later", 3600, nil)
		return
	}
	codeID, err := verifiedEmailCodeID(r.Context(), s.db, req.Email, "register", req.Code)
	if err != nil {
		writeEmailCodeError(w, err)
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
	if err := claimVerifiedEmailCode(r.Context(), tx, codeID); err != nil {
		writeEmailCodeError(w, err)
		return
	}

	var user loginAccount
	err = tx.QueryRow(
		r.Context(),
		`insert into users (
		     username, email, password_hash,
		     country, timezone, preferred_content_language, secondary_content_language, preferred_ui_language,
		     registration_ip, registration_country_code, registration_city, email_verified
		 )
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, true)
		 returning id, public_id, username, email, email_verified, status, created_at, last_login_at, auth_version`,
		req.Username,
		req.Email,
		passwordHash,
		req.Country,
		req.Timezone,
		req.PreferredContentLanguage,
		req.SecondaryContentLanguage,
		req.PreferredUILanguage,
		location.IP,
		location.CountryCode,
		location.City,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.authVersion)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "用户名或邮箱已被占用")
		} else {
			writeError(w, http.StatusInternalServerError, "注册失败")
		}
		return
	}
	if err := s.assignConfiguredRoleTx(r.Context(), tx, user.ID, "registered"); err != nil {
		writeError(w, http.StatusInternalServerError, "分配新用户权限组失败")
		return
	}

	versions, err := readPermissionVersions(r.Context(), tx, user.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取新用户权限失败")
		return
	}
	resolved, err := resolveUsersRootPermissionsQuery(r.Context(), tx, []int64{user.ID})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取新用户权限失败")
		return
	}
	grants := resolved[user.ID]
	authenticatedUser := authenticatedResponse(user.User, versions, grants.Roles, grants.Permissions)
	session, err := s.issueToken(r.Context(), tx, user)
	if err != nil {
		writeSessionIssueError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "注册失败")
		return
	}
	s.writeLoginResult(w, r, http.StatusCreated, session, authenticatedUser)
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
	rateLimited, err := s.authAttemptRateLimited(r.Context(), account, location.IP, r.Header.Get("X-Client-ID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login rate-limit check failed")
		return
	}
	if rateLimited {
		writeAPIError(w, http.StatusTooManyRequests, "auth_rate_limited", "too many login attempts; please try again later", 900, nil)
		return
	}
	user, passwordHash, err := s.findUserForLogin(r.Context(), account)
	userFound := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		passwordHash = loginDummyPasswordHash
	} else if err != nil {
		log.Print(database.RedactedError("login account lookup", err))
		writeError(w, http.StatusInternalServerError, "登录服务暂时不可用")
		return
	}
	passwordMatches := security.VerifyPassword(req.Password, passwordHash)
	if !userFound || !passwordMatches {
		s.recordLogin(r.Context(), nil, account, location, r.UserAgent(), false, "invalid_credentials")
		writeError(w, http.StatusUnauthorized, "账号或密码不正确")
		return
	}
	if user.Status != "active" {
		s.recordLogin(r.Context(), &user.ID, account, location, r.UserAgent(), false, "disabled")
		writeError(w, http.StatusForbidden, "账号已被禁用")
		return
	}

	authenticatedUser, err := s.authenticatedUser(r.Context(), user.User)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user permissions")
		return
	}
	session, err := s.issueToken(r.Context(), s.db, user)
	if err != nil {
		writeSessionIssueError(w, err)
		return
	}
	_, _ = s.db.Exec(r.Context(), `update users set last_login_at = now(), updated_at = now() where id = $1`, user.ID)
	s.recordLogin(r.Context(), &user.ID, account, location, r.UserAgent(), true, "")
	s.writeLoginResult(w, r, http.StatusOK, session, authenticatedUser)
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
		r.Header.Get("X-Client-ID"),
		time.Now().Add(10*time.Minute),
	)
	if err != nil {
		if err == errAuthRateLimited {
			writeAPIError(w, http.StatusTooManyRequests, "auth_rate_limited", "too many verification-code requests; please try again later", 600, nil)
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
	active, err := s.activeMailer(r.Context())
	if err == nil {
		err = active.Send(req.Email, subject, body)
	}
	if err != nil {
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

	rateLimited, err := s.authAttemptRateLimited(r.Context(), req.Email, location.IP, r.Header.Get("X-Client-ID"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login rate-limit check failed")
		return
	}
	if rateLimited {
		writeAPIError(w, http.StatusTooManyRequests, "auth_rate_limited", "too many login attempts; please try again later", 900, nil)
		return
	}
	codeID, err := verifiedEmailCodeID(r.Context(), s.db, req.Email, "login", req.Code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errInvalidEmailVerificationCode) {
			s.recordLogin(r.Context(), nil, req.Email, location, r.UserAgent(), false, "invalid_email_code")
		}
		writeEmailCodeError(w, err)
		return
	}
	user, err := s.findUserByEmail(r.Context(), req.Email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusServiceUnavailable, "登录服务暂时不可用")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) || user.Status != "active" {
		if claimErr := claimVerifiedEmailCode(r.Context(), s.db, codeID); claimErr != nil {
			writeEmailCodeError(w, claimErr)
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			s.recordLogin(r.Context(), nil, req.Email, location, r.UserAgent(), false, "email_not_registered")
			writeError(w, http.StatusNotFound, "该邮箱尚未注册")
		} else {
			s.recordLogin(r.Context(), &user.ID, req.Email, location, r.UserAgent(), false, "disabled")
			writeError(w, http.StatusForbidden, "账号已被禁用")
		}
		return
	}
	authenticatedUser, err := s.authenticatedUser(r.Context(), user.User)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to resolve user permissions")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "登录服务暂时不可用")
		return
	}
	defer tx.Rollback(r.Context())
	if err = claimVerifiedEmailCode(r.Context(), tx, codeID); err != nil {
		writeEmailCodeError(w, err)
		return
	}
	session, err := s.issueToken(r.Context(), tx, user)
	if err != nil {
		writeSessionIssueError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "登录服务暂时不可用")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update users set last_login_at = now(), updated_at = now() where id = $1`, user.ID)
	s.recordLogin(r.Context(), &user.ID, req.Email, location, r.UserAgent(), true, "email_code")
	s.writeLoginResult(w, r, http.StatusOK, session, authenticatedUser)
}

func writeEmailCodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errInvalidEmailVerificationCode) {
		writeError(w, http.StatusUnauthorized, "验证码不正确或已过期")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "邮箱验证服务暂时不可用")
}

func verifiedEmailCodeID(ctx context.Context, db databaseQuery, email, purpose, code string) (int64, error) {
	var codeID int64
	var codeHash string
	err := db.QueryRow(ctx, `select id, code_hash from email_verification_codes
		where email=$1 and purpose=$2 and consumed_at is null and expires_at>clock_timestamp()
		order by id desc limit 1`, email, purpose).Scan(&codeID, &codeHash)
	if err != nil {
		return 0, err
	}
	if !security.VerifyCode(code, codeHash) {
		return 0, errInvalidEmailVerificationCode
	}
	return codeID, nil
}

func claimVerifiedEmailCode(ctx context.Context, db databaseQuery, id int64) error {
	// Check wall time after acquiring the row, even if another claimant held it past expiry.
	var claimed int64
	return db.QueryRow(ctx, `with candidate as materialized (
			select id,expires_at from email_verification_codes where id=$1 and consumed_at is null for update
		) update email_verification_codes e set consumed_at=clock_timestamp() from candidate c
		where e.id=c.id and c.expires_at>clock_timestamp() returning e.id`, id).Scan(&claimed)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	user, err := s.findUserByID(r.Context(), claims.Subject)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusServiceUnavailable, "用户资料暂时不可用")
			return
		}
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	authenticatedUser, err := s.authenticatedUser(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve user permissions")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, struct {
		authenticatedUserResponse
		SessionMarker string `json:"sessionMarker"`
	}{authenticatedUser, browserSessionMarker(claims.SessionID)})
}

func (s *Server) evaluatePermission(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "权限名称不能为空")
		return
	}
	value := claimsNumericPermissionValue(claims, code)
	allowed := claimsAllow(claims, code) || value > 0
	if allowed && value == 0 {
		if child := selectedPermissionRule(claims.PermissionRules, code+".*"); child != nil && !child.Allow {
			// A parent boolean can remain usable even if its children are denied.
			// A declared quantity with this rule combination has zero allowance.
			var numeric bool
			if err := s.db.QueryRow(r.Context(), `select exists(select 1 from permissions where code=$1)`, code+".<num>").Scan(&numeric); err != nil {
				writeError(w, http.StatusServiceUnavailable, "permission catalog unavailable")
				return
			}
			allowed = !numeric
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"permission": code,
		"allowed":    allowed,
		"value":      value,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.finishLogout(w, r, true)
}

func (s *Server) logoutIfCurrent(w http.ResponseWriter, r *http.Request) {
	var condition struct {
		SessionMarker string `json:"sessionMarker"`
	}
	if r.Body == nil || decodeJSON(r, &condition) != nil || strings.TrimSpace(condition.SessionMarker) == "" {
		writeError(w, http.StatusBadRequest, "a session marker is required")
		return
	}
	if condition.SessionMarker != browserSessionMarker(currentClaims(r).SessionID) {
		writeAPIError(w, http.StatusConflict, "AUTH_SESSION_CHANGED", "the active session has changed", 0, nil)
		return
	}
	// A browser may receive this response after a different session logged
	// in. Expiring the shared cookie here could erase that newer credential.
	s.finishLogout(w, r, false)
}

func (s *Server) finishLogout(w http.ResponseWriter, r *http.Request, expireCookie bool) {
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "logout is temporarily unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `update auth_sessions set revoked_at=coalesce(revoked_at,now())
		where session_hash=$1 and user_id=$2`, security.SessionFingerprint(claims.SessionID), claims.Subject); err != nil {
		writeError(w, http.StatusServiceUnavailable, "logout is temporarily unavailable")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from user_presence_sessions where session_hash=$1 and user_id=$2`, security.SessionFingerprint(claims.SessionID), claims.Subject); err != nil {
		writeError(w, http.StatusServiceUnavailable, "logout is temporarily unavailable")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "logout is temporarily unavailable")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "logout", claims.Subject,
		s.revokeSessionCache(r.Context(), claims)) {
		return
	}
	s.cache.RemoveUserPresence(r.Context(), claims.Subject, hex.EncodeToString(security.SessionFingerprint(claims.SessionID)), time.Now(), s.cache.Config().PresenceTTL)
	if expireCookie {
		s.clearAuthSessionCookie(w, r)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeSessionIssueError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "账号状态已变化，请重新登录")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "生成登录凭证失败")
}

func (s *Server) issueToken(ctx context.Context, db databaseQuery, user loginAccount) (issuedAuthSession, error) {
	claims, err := security.NewClaims(user.PublicID, user.Username, user.Email, user.authVersion, s.cfg.JWTTTL)
	if err != nil {
		return issuedAuthSession{}, err
	}
	token, err := security.SignToken(s.cfg.JWTSecret, claims)
	if err != nil {
		return issuedAuthSession{}, err
	}
	// Bind the session to the account revision that supplied the credential proof.
	var inserted bool
	err = db.QueryRow(ctx, `with current_account as materialized (
			select id,auth_version from users where id=$2 and auth_version=$3 and status='active' for share
		) insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		select $1,id,auth_version,$4 from current_account returning true`,
		security.SessionFingerprint(claims.SessionID), user.ID, user.authVersion, time.Unix(claims.ExpiresAt, 0)).Scan(&inserted)
	if err != nil {
		return issuedAuthSession{}, err
	}
	return issuedAuthSession{token: token, marker: browserSessionMarker(claims.SessionID)}, nil
}

func (s *Server) findUserForLogin(ctx context.Context, account string) (loginAccount, string, error) {
	var user loginAccount
	var passwordHash string
	query := `select id, public_id, username, email, password_hash, email_verified, status, created_at, last_login_at, avatar_url, signature, auth_version
		from users
		where lower(email) = lower($1) or lower(username) = lower($1) or public_id = lower($1)`
	args := []any{account}
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&user.ID,
		&user.PublicID,
		&user.Username,
		&user.Email,
		&passwordHash,
		&user.EmailVerified,
		&user.Status,
		&user.CreatedAt,
		&user.LastLoginAt,
		&user.AvatarURL,
		&user.Signature,
		&user.authVersion,
	)
	return user, passwordHash, err
}

func (s *Server) findUserByEmail(ctx context.Context, email string) (loginAccount, error) {
	var user loginAccount
	err := s.db.QueryRow(
		ctx,
		`select id, public_id, username, email, email_verified, status, created_at, last_login_at, avatar_url, signature, auth_version
		 from users where lower(email) = lower($1)`,
		email,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature, &user.authVersion)
	return user, err
}

func (s *Server) findUserByID(ctx context.Context, id int64) (domain.User, error) {
	var user domain.User
	err := s.db.QueryRow(
		ctx,
		`select id, public_id, username, email, email_verified, status, created_at, last_login_at, avatar_url, signature
		 from users where id = $1`,
		id,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature)
	return user, err
}

func (s *Server) authenticatedUser(ctx context.Context, user domain.User) (authenticatedUserResponse, error) {
	versions, err := readPermissionVersions(ctx, s.db, user.ID)
	if err != nil {
		return authenticatedUserResponse{}, err
	}
	roleCodes, permissionRules, err := s.resolveUserRootPermissionsAtVersion(ctx, user.ID, versions)
	if err != nil {
		return authenticatedUserResponse{}, err
	}
	user.AvatarURL, err = s.resolveStoredOSSObjectAccessURL(ctx, user.AvatarURL)
	if err != nil {
		return authenticatedUserResponse{}, err
	}
	return authenticatedResponse(user, versions, roleCodes, permissionRules), nil
}

func authenticatedResponse(user domain.User, versions permissionVersions, roleCodes []string, permissionRules []security.PermissionRule) authenticatedUserResponse {
	user.RoleCodes = roleCodes
	return authenticatedUserResponse{User: user, PermissionRules: permissionRules, PermissionVersion: versions.User, RBACVersion: versions.RBAC}
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
	if len([]rune(username)) < 1 || len([]rune(username)) > 32 {
		return fmt.Errorf("用户名长度需要在 1 到 32 个字符之间")
	}
	if _, exists := reservedUsernames[strings.ToLower(username)]; exists {
		return fmt.Errorf("该用户名为系统保留词")
	}
	for _, character := range username {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return fmt.Errorf("用户名不能包含空白或控制字符")
		}
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
