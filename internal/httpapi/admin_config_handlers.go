package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/mailer"
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

func (s *Server) adminFeatures() map[string]bool {
	return map[string]bool{
		"contentReview":  true,
		"emailSystem":    true,
		"permissionRBAC": true,
		"oss":            true,
		"logSystem":      true,
		"ai":             true,
		"redis":          s.cfg.Redis.Enabled,
		"crawler":        false,
	}
}

func (s *Server) adminFeatureConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.adminFeatures())
}

func (s *Server) adminAuthMethods() map[string]any {
	return map[string]any{
		"allowRegistration":        true,
		"emailPasswordLogin":       true,
		"usernamePasswordLogin":    true,
		"userIDPasswordLogin":      true,
		"emailCodeLogin":           true,
		"requireEmailVerification": false,
		"passwordMinLength":        8,
		"reservedUsernames":        []string{"admin", "root", "system", "api", "login", "register", "mods", "mod", "users", "settings"},
		"tokenTTLHours":            int(s.cfg.JWTTTL.Hours()),
	}
}

func (s *Server) adminAuthConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.oauthConfig(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "OAUTH_SETTINGS_UNAVAILABLE", "third-party login settings are temporarily unavailable", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auth": s.adminAuthMethods(), "oauth": redactOAuthConfig(cfg)})
}

func (s *Server) adminMailConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.mailConfigFromSettings(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "MAIL_SETTINGS_UNAVAILABLE", "mail settings are temporarily unavailable", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, redactMailConfig(cfg))
}

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
	oauthCfg, err := s.oauthConfig(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "OAUTH_SETTINGS_UNAVAILABLE", "third-party login settings are temporarily unavailable", 0, nil)
		return
	}
	aiCfg, err := readAIConfigFromSettings(r.Context(), s.db, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI_SETTINGS_UNAVAILABLE", "AI settings are temporarily unavailable", 0, nil)
		return
	}
	markdown := defaultMarkdownConfig()
	if err = s.readEditableSetting(r.Context(), markdownConfigSettingKey, &markdown); err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "MARKDOWN_SETTINGS_UNAVAILABLE", "Markdown settings are temporarily unavailable", 0, nil)
		return
	}
	profile := defaultProfileConfig()
	if err = s.readEditableSetting(r.Context(), profileConfigSettingKey, &profile); err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "PROFILE_SETTINGS_UNAVAILABLE", "profile settings are temporarily unavailable", 0, nil)
		return
	}
	oss, err := s.ossConfigFromSettingsStrictWithQueryer(r.Context(), s.db)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "OSS_SETTINGS_UNAVAILABLE", "OSS settings are temporarily unavailable", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"general":   general,
		"auth":      s.adminAuthMethods(),
		"oauth":     redactOAuthConfig(oauthCfg),
		"mail":      redactMailConfig(mailCfg),
		"oss":       redactOSSConfig(oss),
		"markdown":  normalizeMarkdownConfig(markdown),
		"profile":   normalizeProfileConfig(profile),
		"yggdrasil": s.redactedYggdrasilConfig(),
		"ai":        redactAIConfig(aiCfg),
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
		"features": s.adminFeatures(),
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
