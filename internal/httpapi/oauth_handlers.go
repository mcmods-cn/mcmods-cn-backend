package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/domain"
)

type oauthConfigPayload struct {
	Providers map[string]oauthProviderConfig `json:"providers"`
}

type oauthProviderConfig struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"`
	RedirectURI  string `json:"redirectUri"`
}

type oauthProviderProfile struct {
	Provider       string
	ProviderUserID string
	Username       string
	Email          string
	EmailVerified  bool
	AvatarURL      string
}

var supportedOAuthProviders = map[string]struct{}{
	"wechat": {}, "qq": {}, "google": {}, "github": {},
}

const maxOAuthResponseBytes = 1 << 20

var oauthHTTPClient = &http.Client{Timeout: 15 * time.Second}

func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	cfg, ok := s.oauthProviderConfig(r.Context(), provider)
	if !ok {
		writeError(w, http.StatusNotFound, "不支持的第三方登录")
		return
	}
	if !cfg.Enabled || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURI == "" {
		writeError(w, http.StatusServiceUnavailable, "第三方登录尚未配置")
		return
	}
	next, validNext := normalizeOAuthNext(r.URL.Query().Get("next"))
	if !validNext {
		writeError(w, http.StatusBadRequest, "登录后跳转地址不安全")
		return
	}
	state, err := randomState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成登录状态失败")
		return
	}
	http.SetCookie(w, s.httpOnlyCookie(r, oauthStateCookie(provider), state, "/", 600, time.Time{}))
	nextCookie := s.httpOnlyCookie(r, oauthNextCookie(provider), "", "/", 0, time.Time{}) // #nosec G124 -- helper enforces every security attribute.
	if next == "" {
		nextCookie.MaxAge = -1
	} else {
		nextCookie.Value = base64.RawURLEncoding.EncodeToString([]byte(next))
		nextCookie.MaxAge = 600
	}
	http.SetCookie(w, nextCookie)
	http.Redirect(w, r, oauthAuthorizeURL(provider, cfg, state), http.StatusFound)
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	cfg, ok := s.oauthProviderConfig(r.Context(), provider)
	if !ok {
		writeError(w, http.StatusNotFound, "不支持的第三方登录")
		return
	}
	if r.URL.Query().Get("state") == "" || !validOAuthState(r, provider) {
		writeError(w, http.StatusBadRequest, "第三方登录状态已失效")
		return
	}
	next := oauthNextFromRequest(r, provider)
	http.SetCookie(w, s.httpOnlyCookie(r, oauthStateCookie(provider), "", "/", -1, time.Unix(1, 0)))
	http.SetCookie(w, s.httpOnlyCookie(r, oauthNextCookie(provider), "", "/", -1, time.Unix(1, 0)))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "第三方登录缺少授权码")
		return
	}

	profile, err := s.fetchOAuthProfile(r.Context(), provider, cfg, code)
	if err != nil {
		log.Printf("oauth callback failed provider=%s: %v", provider, err)
		writeError(w, http.StatusBadGateway, "third-party login provider request failed")
		return
	}
	location := s.requestClientLocation(r)
	user, err := s.findOrCreateOAuthUser(r.Context(), profile, location)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "第三方账号登录失败")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update users set last_login_at = now(), updated_at = now() where id = $1`, user.ID)
	s.recordLogin(r.Context(), &user.ID, profile.Provider+":"+profile.ProviderUserID, location, r.UserAgent(), true, "oauth_"+profile.Provider)
	token, err := s.issueToken(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成登录凭证失败")
		return
	}
	s.setAuthSessionCookie(w, r, token)
	redirectURL := s.cfg.FrontendOrigin + "/login?oauth=success"
	if next != "" {
		redirectURL += "&next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (s *Server) updateOAuthConfig(w http.ResponseWriter, r *http.Request) {
	var payload oauthConfigPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	current := s.oauthConfig(r.Context())
	if current.Providers == nil {
		current.Providers = map[string]oauthProviderConfig{}
	}
	for provider, cfg := range payload.Providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if _, ok := supportedOAuthProviders[provider]; !ok {
			writeError(w, http.StatusBadRequest, "不支持的第三方登录: "+provider)
			return
		}
		cfg.ClientID = strings.TrimSpace(cfg.ClientID)
		cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
		cfg.RedirectURI = strings.TrimSpace(cfg.RedirectURI)
		if cfg.ClientSecret == "" {
			cfg.ClientSecret = current.Providers[provider].ClientSecret
		}
		if len(cfg.ClientID) > 512 || len(cfg.ClientSecret) > 4096 || len(cfg.RedirectURI) > 2048 {
			writeError(w, http.StatusBadRequest, "第三方登录配置过长")
			return
		}
		if cfg.RedirectURI != "" {
			parsed, parseErr := url.Parse(cfg.RedirectURI)
			if parseErr != nil || !validHTTPURL(cfg.RedirectURI) ||
				(parsed.Scheme != "https" && !strings.EqualFold(parsed.Hostname(), "localhost") && parsed.Hostname() != "127.0.0.1") {
				writeError(w, http.StatusBadRequest, "第三方登录回调必须使用 HTTPS；本地开发可使用 localhost")
				return
			}
		}
		if cfg.Enabled && (cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURI == "") {
			writeError(w, http.StatusBadRequest, "启用第三方登录前必须完整填写客户端和回调配置")
			return
		}
		current.Providers[provider] = cfg
	}
	raw, err := s.sealSystemSetting(current)
	if err != nil {
		writeError(w, http.StatusBadRequest, "第三方登录配置格式不正确")
		return
	}
	claims := currentClaims(r)
	_, err = s.db.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ('auth.oauth', $1::jsonb, $2, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		raw,
		claims.Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存第三方登录配置失败")
		return
	}
	writeJSON(w, http.StatusOK, redactOAuthConfig(current))
}

func (s *Server) oauthConfig(ctx context.Context) oauthConfigPayload {
	cfg := oauthConfigPayload{Providers: map[string]oauthProviderConfig{}}
	for provider := range supportedOAuthProviders {
		cfg.Providers[provider] = oauthProviderConfig{}
	}
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key = 'auth.oauth'`).Scan(&raw)
	if err != nil {
		_ = ignoreNoRows(err)
		return cfg
	}
	if err := s.openSystemSetting(raw, &cfg); err != nil {
		return cfg
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]oauthProviderConfig{}
	}
	for provider := range supportedOAuthProviders {
		if _, ok := cfg.Providers[provider]; !ok {
			cfg.Providers[provider] = oauthProviderConfig{}
		}
	}
	return cfg
}

func (s *Server) oauthProviderConfig(ctx context.Context, provider string) (oauthProviderConfig, bool) {
	if _, ok := supportedOAuthProviders[provider]; !ok {
		return oauthProviderConfig{}, false
	}
	return s.oauthConfig(ctx).Providers[provider], true
}

func redactOAuthConfig(cfg oauthConfigPayload) map[string]any {
	providers := map[string]any{}
	for provider, item := range cfg.Providers {
		providers[provider] = map[string]any{
			"enabled":         item.Enabled,
			"clientId":        item.ClientID,
			"redirectUri":     item.RedirectURI,
			"hasClientSecret": strings.TrimSpace(item.ClientSecret) != "",
		}
	}
	return map[string]any{"providers": providers}
}

func oauthAuthorizeURL(provider string, cfg oauthProviderConfig, state string) string {
	values := url.Values{}
	values.Set("state", state)
	values.Set("redirect_uri", cfg.RedirectURI)
	switch provider {
	case "github":
		values.Set("client_id", cfg.ClientID)
		values.Set("scope", "read:user user:email")
		return "https://github.com/login/oauth/authorize?" + values.Encode()
	case "google":
		values.Set("client_id", cfg.ClientID)
		values.Set("response_type", "code")
		values.Set("scope", "openid email profile")
		return "https://accounts.google.com/o/oauth2/v2/auth?" + values.Encode()
	case "qq":
		values.Set("response_type", "code")
		values.Set("client_id", cfg.ClientID)
		values.Set("scope", "get_user_info")
		return "https://graph.qq.com/oauth2.0/authorize?" + values.Encode()
	case "wechat":
		values.Set("appid", cfg.ClientID)
		values.Set("response_type", "code")
		values.Set("scope", "snsapi_login")
		return "https://open.weixin.qq.com/connect/qrconnect?" + values.Encode() + "#wechat_redirect"
	default:
		return ""
	}
}

func (s *Server) fetchOAuthProfile(ctx context.Context, provider string, cfg oauthProviderConfig, code string) (oauthProviderProfile, error) {
	switch provider {
	case "github":
		return fetchGitHubProfile(ctx, cfg, code)
	case "google":
		return fetchGoogleProfile(ctx, cfg, code)
	case "qq":
		return fetchQQProfile(ctx, cfg, code)
	case "wechat":
		return fetchWeChatProfile(ctx, cfg, code)
	default:
		return oauthProviderProfile{}, fmt.Errorf("不支持的第三方登录")
	}
}

func fetchGitHubProfile(ctx context.Context, cfg oauthProviderConfig, code string) (oauthProviderProfile, error) {
	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	values.Set("code", code)
	values.Set("redirect_uri", cfg.RedirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", bytes.NewBufferString(values.Encode()))
	if err != nil {
		return oauthProviderProfile{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return oauthProviderProfile{}, err
	}
	defer resp.Body.Close()
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := decodeOAuthJSONResponse(resp, &tokenResp); err != nil {
		return oauthProviderProfile{}, err
	}
	if tokenResp.Error != "" || tokenResp.AccessToken == "" {
		return oauthProviderProfile{}, oauthProviderError("GitHub", tokenResp.Error, tokenResp.Description)
	}
	userReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	userReq.Header.Set("Accept", "application/vnd.github+json")
	userResp, err := oauthHTTPClient.Do(userReq)
	if err != nil {
		return oauthProviderProfile{}, err
	}
	defer userResp.Body.Close()
	if userResp.StatusCode < 200 || userResp.StatusCode >= 300 {
		return oauthProviderProfile{}, fmt.Errorf("GitHub 用户信息请求失败: HTTP %d", userResp.StatusCode)
	}
	raw, err := readOAuthResponseBody(userResp)
	if err != nil {
		return oauthProviderProfile{}, err
	}
	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(raw, &user); err != nil {
		return oauthProviderProfile{}, err
	}
	user.Email = fetchGitHubPrimaryEmail(ctx, tokenResp.AccessToken)
	return oauthProviderProfile{
		Provider:       "github",
		ProviderUserID: fmt.Sprintf("%d", user.ID),
		Username:       user.Login,
		Email:          user.Email,
		EmailVerified:  user.Email != "",
		AvatarURL:      user.AvatarURL,
	}, nil
}

func fetchGitHubPrimaryEmail(ctx context.Context, accessToken string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := decodeOAuthJSONResponse(resp, &emails); err != nil {
		return ""
	}
	for _, email := range emails {
		if email.Primary && email.Verified {
			return email.Email
		}
	}
	for _, email := range emails {
		if email.Verified {
			return email.Email
		}
	}
	return ""
}

func fetchGoogleProfile(ctx context.Context, cfg oauthProviderConfig, code string) (oauthProviderProfile, error) {
	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	values.Set("code", code)
	values.Set("redirect_uri", cfg.RedirectURI)
	values.Set("grant_type", "authorization_code")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", bytes.NewBufferString(values.Encode()))
	if err != nil {
		return oauthProviderProfile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return oauthProviderProfile{}, err
	}
	defer resp.Body.Close()
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := decodeOAuthJSONResponse(resp, &tokenResp); err != nil {
		return oauthProviderProfile{}, err
	}
	if tokenResp.Error != "" || tokenResp.AccessToken == "" {
		return oauthProviderProfile{}, oauthProviderError("Google", tokenResp.Error, tokenResp.Description)
	}
	userReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	userResp, err := oauthHTTPClient.Do(userReq)
	if err != nil {
		return oauthProviderProfile{}, err
	}
	defer userResp.Body.Close()
	if userResp.StatusCode < 200 || userResp.StatusCode >= 300 {
		return oauthProviderProfile{}, fmt.Errorf("google 用户信息请求失败: HTTP %d", userResp.StatusCode)
	}
	var user struct {
		Sub           string `json:"sub"`
		Name          string `json:"name"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Picture       string `json:"picture"`
	}
	if err := decodeOAuthJSONResponse(userResp, &user); err != nil {
		return oauthProviderProfile{}, err
	}
	if user.Sub == "" {
		return oauthProviderProfile{}, fmt.Errorf("google 用户信息缺少 sub")
	}
	verifiedEmail := ""
	if user.EmailVerified {
		verifiedEmail = user.Email
	}
	return oauthProviderProfile{
		Provider:       "google",
		ProviderUserID: user.Sub,
		Username:       strings.Split(defaultString(user.Email, user.Sub), "@")[0],
		Email:          verifiedEmail,
		EmailVerified:  user.EmailVerified && verifiedEmail != "",
		AvatarURL:      user.Picture,
	}, nil
}

func fetchQQProfile(ctx context.Context, cfg oauthProviderConfig, code string) (oauthProviderProfile, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	values.Set("code", code)
	values.Set("redirect_uri", cfg.RedirectURI)
	tokenURL := "https://graph.qq.com/oauth2.0/token?" + values.Encode()
	tokenResp, err := httpGetText(ctx, tokenURL)
	if err != nil {
		return oauthProviderProfile{}, err
	}
	tokenValues, err := url.ParseQuery(tokenResp)
	if err != nil {
		return oauthProviderProfile{}, err
	}
	accessToken := tokenValues.Get("access_token")
	if accessToken == "" {
		return oauthProviderProfile{}, oauthProviderError("QQ", tokenValues.Get("error"), tokenValues.Get("error_description"))
	}
	openIDRaw, err := httpGetText(ctx, "https://graph.qq.com/oauth2.0/me?access_token="+url.QueryEscape(accessToken))
	if err != nil {
		return oauthProviderProfile{}, err
	}
	openIDRaw = trimJSONP(openIDRaw)
	var openIDResp struct {
		ClientID string `json:"client_id"`
		OpenID   string `json:"openid"`
		Error    int    `json:"error"`
		Msg      string `json:"error_description"`
	}
	if err := json.Unmarshal([]byte(openIDRaw), &openIDResp); err != nil {
		return oauthProviderProfile{}, err
	}
	if openIDResp.OpenID == "" {
		return oauthProviderProfile{}, oauthProviderError("QQ", strconv.Itoa(openIDResp.Error), openIDResp.Msg)
	}
	userURL := "https://graph.qq.com/user/get_user_info?access_token=" + url.QueryEscape(accessToken) + "&oauth_consumer_key=" + url.QueryEscape(cfg.ClientID) + "&openid=" + url.QueryEscape(openIDResp.OpenID)
	var user struct {
		Nickname string `json:"nickname"`
		Figure   string `json:"figureurl_qq_2"`
		Ret      int    `json:"ret"`
		Msg      string `json:"msg"`
	}
	if err := httpGetJSON(ctx, userURL, &user); err != nil {
		return oauthProviderProfile{}, err
	}
	if user.Ret != 0 {
		return oauthProviderProfile{}, oauthProviderError("QQ", strconv.Itoa(user.Ret), user.Msg)
	}
	return oauthProviderProfile{
		Provider:       "qq",
		ProviderUserID: openIDResp.OpenID,
		Username:       "qq_" + openIDResp.OpenID,
		AvatarURL:      user.Figure,
	}, nil
}

func fetchWeChatProfile(ctx context.Context, cfg oauthProviderConfig, code string) (oauthProviderProfile, error) {
	values := url.Values{}
	values.Set("appid", cfg.ClientID)
	values.Set("secret", cfg.ClientSecret)
	values.Set("code", code)
	values.Set("grant_type", "authorization_code")
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		OpenID      string `json:"openid"`
		UnionID     string `json:"unionid"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := httpGetJSON(ctx, "https://api.weixin.qq.com/sns/oauth2/access_token?"+values.Encode(), &tokenResp); err != nil {
		return oauthProviderProfile{}, err
	}
	if tokenResp.AccessToken == "" || tokenResp.OpenID == "" {
		return oauthProviderProfile{}, oauthProviderError("微信", strconv.Itoa(tokenResp.ErrCode), tokenResp.ErrMsg)
	}
	userURL := "https://api.weixin.qq.com/sns/userinfo?access_token=" + url.QueryEscape(tokenResp.AccessToken) + "&openid=" + url.QueryEscape(tokenResp.OpenID)
	var user struct {
		Nickname string `json:"nickname"`
		HeadImg  string `json:"headimgurl"`
		UnionID  string `json:"unionid"`
		ErrCode  int    `json:"errcode"`
		ErrMsg   string `json:"errmsg"`
	}
	if err := httpGetJSON(ctx, userURL, &user); err != nil {
		return oauthProviderProfile{}, err
	}
	if user.ErrCode != 0 {
		return oauthProviderProfile{}, oauthProviderError("微信", strconv.Itoa(user.ErrCode), user.ErrMsg)
	}
	providerID := defaultString(user.UnionID, defaultString(tokenResp.UnionID, tokenResp.OpenID))
	return oauthProviderProfile{
		Provider:       "wechat",
		ProviderUserID: providerID,
		Username:       "wechat_" + providerID,
		AvatarURL:      user.HeadImg,
	}, nil
}

func httpGetText(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := readOAuthResponseBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(raw), fmt.Errorf("第三方接口请求失败: HTTP %d", resp.StatusCode)
	}
	return string(raw), err
}

func httpGetJSON(ctx context.Context, rawURL string, target any) error {
	text, err := httpGetText(ctx, rawURL)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(text), target)
}

func readOAuthResponseBody(resp *http.Response) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxOAuthResponseBytes {
		return nil, fmt.Errorf("OAuth provider response exceeds %d bytes", maxOAuthResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OAuth provider request failed: HTTP %d", resp.StatusCode)
	}
	return raw, nil
}

func decodeOAuthJSONResponse(resp *http.Response, target any) error {
	raw, err := readOAuthResponseBody(resp)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode OAuth provider response: %w", err)
	}
	return nil
}

func (s *Server) findOrCreateOAuthUser(ctx context.Context, profile oauthProviderProfile, location clientLocation) (domain.User, error) {
	var user domain.User
	created := false
	err := s.db.QueryRow(
		ctx,
		`select u.id, u.public_id, u.username, u.email, u.email_verified, u.status, u.created_at, u.last_login_at, u.avatar_url, u.signature
		 from oauth_accounts oa
		 join users u on u.id = oa.user_id
		 where oa.provider = $1 and oa.provider_user_id = $2`,
		profile.Provider,
		profile.ProviderUserID,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature)
	if err == nil {
		return user, nil
	}
	if err != pgx.ErrNoRows {
		return user, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return user, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('oauth:'||$1||':'||$2,0))`, profile.Provider, profile.ProviderUserID); err != nil {
		return user, err
	}
	err = tx.QueryRow(
		ctx,
		`select u.id,u.public_id,u.username,u.email,u.email_verified,u.status,u.created_at,u.last_login_at,u.avatar_url,u.signature
		 from oauth_accounts oa
		 join users u on u.id=oa.user_id
		 where oa.provider=$1 and oa.provider_user_id=$2`,
		profile.Provider,
		profile.ProviderUserID,
	).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return user, err
		}
		return user, nil
	}
	if err != pgx.ErrNoRows {
		return user, err
	}
	if profile.EmailVerified && strings.TrimSpace(profile.Email) != "" {
		err = tx.QueryRow(
			ctx,
			`select id,public_id,username,email,email_verified,status,created_at,last_login_at,avatar_url,signature
			 from users where lower(email)=lower($1)`,
			profile.Email,
		).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt, &user.AvatarURL, &user.Signature)
		if err != nil && err != pgx.ErrNoRows {
			return user, err
		}
	}
	if user.ID == 0 {
		created = true
		username := normalizeOAuthUsername(profile.Provider, defaultString(profile.Username, profile.ProviderUserID)+"_"+profile.ProviderUserID)
		email := profile.Email
		if email == "" {
			email = profile.Provider + "-" + profile.ProviderUserID + "@oauth.mcmods.cn"
		}
		err = tx.QueryRow(
			ctx,
			`insert into users (
				username, email, password_hash, email_verified, status,
				registration_ip, registration_country_code, registration_city
			 )
			 values ($1, $2, 'oauth-login-disabled', $3, 'active', $4, $5, $6)
			 returning id, public_id, username, email, email_verified, status, created_at, last_login_at`,
			username,
			email,
			profile.EmailVerified,
			location.IP,
			location.CountryCode,
			location.City,
		).Scan(&user.ID, &user.PublicID, &user.Username, &user.Email, &user.EmailVerified, &user.Status, &user.CreatedAt, &user.LastLoginAt)
		if err != nil {
			return user, err
		}
	}
	if created {
		if err := s.assignConfiguredRoleTx(ctx, tx, user.ID, "registered"); err != nil {
			return user, err
		}
	}
	_, err = tx.Exec(
		ctx,
		`insert into oauth_accounts (user_id, provider, provider_user_id, username, email, avatar_url, updated_at)
		 values ($1, $2, $3, $4, $5, $6, now())
		 on conflict (provider, provider_user_id) do update
		 set username = excluded.username, email = excluded.email, avatar_url = excluded.avatar_url, updated_at = now()`,
		user.ID,
		profile.Provider,
		profile.ProviderUserID,
		profile.Username,
		profile.Email,
		profile.AvatarURL,
	)
	if err != nil {
		return user, err
	}
	return user, tx.Commit(ctx)
}

func normalizeOAuthUsername(provider string, username string) string {
	base := strings.ToLower(strings.TrimSpace(username))
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, base)
	base = strings.Trim(base, "_")
	if base == "" || len(base) < 3 {
		base = provider + "_user"
	}
	return provider + "_" + base
}

func randomState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func oauthStateCookie(provider string) string {
	return "mcmods_oauth_state_" + provider
}

func oauthNextCookie(provider string) string {
	return "mcmods_oauth_next_" + provider
}

func normalizeOAuthNext(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	if len(value) > 2048 || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, `\`) {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "", false
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil || strings.HasPrefix(decodedPath, "//") || strings.Contains(decodedPath, `\`) {
		return "", false
	}
	return value, true
}

func oauthNextFromRequest(r *http.Request, provider string) string {
	cookie, err := r.Cookie(oauthNextCookie(provider))
	if err != nil || cookie.Value == "" {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return ""
	}
	next, ok := normalizeOAuthNext(string(raw))
	if !ok {
		return ""
	}
	return next
}

func validOAuthState(r *http.Request, provider string) bool {
	cookie, err := r.Cookie(oauthStateCookie(provider))
	if err != nil {
		return false
	}
	provided := r.URL.Query().Get("state")
	return cookie.Value != "" && len(cookie.Value) == len(provided) &&
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(provided)) == 1
}

func trimJSONP(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "callback(") && strings.HasSuffix(value, ");") {
		value = strings.TrimPrefix(value, "callback(")
		value = strings.TrimSuffix(value, ");")
	}
	return strings.TrimSpace(value)
}

func oauthProviderError(provider string, code string, description string) error {
	code = strings.TrimSpace(code)
	description = strings.TrimSpace(description)
	if description == "" {
		description = "请检查 AppID/AppSecret、回调地址和平台应用状态"
	}
	if code == "" || code == "0" {
		return fmt.Errorf("%s 授权失败: %s", provider, description)
	}
	return fmt.Errorf("%s 授权失败 (%s): %s", provider, code, description)
}
