package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

const modImportConfigSettingKey = "mods.import_sources"

var modImportCredentialOrigins = map[string]string{
	"modrinth":   "https://api.modrinth.com",
	"curseforge": "https://api.curseforge.com",
	"github":     "https://api.github.com",
}

type modImportProviderConfig struct {
	Enabled     bool   `json:"enabled"`
	BaseURL     string `json:"baseUrl"`
	Token       string `json:"token,omitempty"`
	APIKey      string `json:"apiKey,omitempty"`
	HasToken    bool   `json:"hasToken,omitempty"`
	HasAPIKey   bool   `json:"hasApiKey,omitempty"`
	ClearToken  bool   `json:"clearToken,omitempty"`
	ClearAPIKey bool   `json:"clearApiKey,omitempty"`
}

type modImportConfig struct {
	UserAgent             string                  `json:"userAgent"`
	RequestTimeoutSeconds int                     `json:"requestTimeoutSeconds"`
	Modrinth              modImportProviderConfig `json:"modrinth"`
	CurseForge            modImportProviderConfig `json:"curseforge"`
	GitHub                modImportProviderConfig `json:"github"`
}

func defaultModImportConfig() modImportConfig {
	return modImportConfig{
		UserAgent:             "mcmods-cn/1.0 (https://mcmods.cn)",
		RequestTimeoutSeconds: 30,
		Modrinth: modImportProviderConfig{
			Enabled: true,
			BaseURL: "https://api.modrinth.com/v2",
			Token:   strings.TrimSpace(os.Getenv("MODRINTH_TOKEN")),
		},
		CurseForge: modImportProviderConfig{
			Enabled: false,
			BaseURL: "https://api.curseforge.com/v1",
			APIKey:  strings.TrimSpace(os.Getenv("CURSEFORGE_API_KEY")),
		},
		GitHub: modImportProviderConfig{
			Enabled: true,
			BaseURL: "https://api.github.com",
			Token:   strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		},
	}
}

func (s *Server) getModImportConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.modImportConfigFromSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组数据源配置失败")
		return
	}
	writeJSON(w, http.StatusOK, redactModImportConfig(cfg))
}

func (s *Server) publicModImportProviders(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.modImportConfigFromSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "模组数据源配置不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modrinth":   map[string]bool{"enabled": cfg.Modrinth.Enabled, "configured": true},
		"curseforge": map[string]bool{"enabled": cfg.CurseForge.Enabled, "configured": cfg.CurseForge.APIKey != ""},
		"github":     map[string]bool{"enabled": cfg.GitHub.Enabled, "configured": true},
	})
}

func (s *Server) updateModImportConfig(w http.ResponseWriter, r *http.Request) {
	var payload modImportConfig
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	current, err := s.modImportConfigFromSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组数据源配置失败")
		return
	}
	if err = prepareModImportConfigUpdate(&payload, current); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "模组数据源配置格式不正确")
		return
	}
	if _, err = s.db.Exec(r.Context(),
		`insert into system_settings(key,value,updated_by,updated_at) values($1,$2::jsonb,$3,now())
		 on conflict(key) do update set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`,
		modImportConfigSettingKey, raw, currentClaims(r).Subject,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "保存模组数据源配置失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_mod_import_config", modImportConfigSettingKey, currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{
		"modrinth": payload.Modrinth.Enabled, "curseforge": payload.CurseForge.Enabled, "github": payload.GitHub.Enabled,
	})
	writeJSON(w, http.StatusOK, redactModImportConfig(payload))
}

func (s *Server) modImportConfigFromSettings(ctx context.Context) (modImportConfig, error) {
	cfg := defaultModImportConfig()
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key=$1`, modImportConfigSettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = normalizeModImportConfig(&cfg)
		return cfg, nil
	}
	if err != nil {
		return modImportConfig{}, err
	}
	if err = s.openSystemSetting(raw, &cfg); err != nil {
		return modImportConfig{}, err
	}
	if err = normalizeModImportConfig(&cfg); err != nil {
		return modImportConfig{}, err
	}
	return cfg, nil
}

func normalizeModImportConfig(cfg *modImportConfig) error {
	defaults := defaultModImportConfig()
	cfg.UserAgent = strings.TrimSpace(cfg.UserAgent)
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaults.UserAgent
	}
	if cfg.RequestTimeoutSeconds < 5 {
		cfg.RequestTimeoutSeconds = 5
	}
	if cfg.RequestTimeoutSeconds > 120 {
		cfg.RequestTimeoutSeconds = 120
	}
	providers := []struct {
		name     string
		code     string
		provider *modImportProviderConfig
		fallback string
	}{
		{"Modrinth", "modrinth", &cfg.Modrinth, defaults.Modrinth.BaseURL},
		{"CurseForge", "curseforge", &cfg.CurseForge, defaults.CurseForge.BaseURL},
		{"GitHub", "github", &cfg.GitHub, defaults.GitHub.BaseURL},
	}
	for _, item := range providers {
		item.provider.Token = strings.TrimSpace(item.provider.Token)
		item.provider.APIKey = strings.TrimSpace(item.provider.APIKey)
		item.provider.HasToken = false
		item.provider.HasAPIKey = false
		item.provider.ClearToken = false
		item.provider.ClearAPIKey = false
		baseURL, err := normalizeProviderBaseURL(item.provider.BaseURL, item.fallback)
		if err != nil {
			return errors.New(item.name + " API 地址不正确")
		}
		item.provider.BaseURL = baseURL
		if (item.provider.Token != "" || item.provider.APIKey != "") && !providerCredentialOriginAllowed(item.code, baseURL) {
			return errors.New(item.name + " 凭据只能发送到官方 API 地址")
		}
	}
	if cfg.CurseForge.Enabled && cfg.CurseForge.APIKey == "" {
		return errors.New("启用 CurseForge 前必须填写 API Key")
	}
	return nil
}

func prepareModImportConfigUpdate(next *modImportConfig, current modImportConfig) error {
	defaults := defaultModImportConfig()
	providers := []struct {
		next     *modImportProviderConfig
		current  modImportProviderConfig
		fallback string
	}{
		{&next.Modrinth, current.Modrinth, defaults.Modrinth.BaseURL},
		{&next.CurseForge, current.CurseForge, defaults.CurseForge.BaseURL},
		{&next.GitHub, current.GitHub, defaults.GitHub.BaseURL},
	}
	for _, item := range providers {
		baseURL, err := normalizeProviderBaseURL(item.next.BaseURL, item.fallback)
		if err != nil {
			return errors.New("API 地址不正确")
		}
		item.next.BaseURL = baseURL
		preserveModImportSecrets(item.next, item.current)
	}
	return normalizeModImportConfig(next)
}

func normalizeProviderBaseURL(value, fallback string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		value = fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid base URL")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost")) {
		return "", errors.New("API 地址必须使用 HTTPS")
	}
	return value, nil
}

func preserveModImportSecrets(next *modImportProviderConfig, current modImportProviderConfig) {
	if next.ClearToken {
		next.Token = ""
	} else if strings.TrimSpace(next.Token) == "" && sameProviderOrigin(next.BaseURL, current.BaseURL) {
		next.Token = current.Token
	}
	if next.ClearAPIKey {
		next.APIKey = ""
	} else if strings.TrimSpace(next.APIKey) == "" && sameProviderOrigin(next.BaseURL, current.BaseURL) {
		next.APIKey = current.APIKey
	}
}

func sameProviderOrigin(left, right string) bool {
	leftOrigin, leftOK := providerOrigin(left)
	rightOrigin, rightOK := providerOrigin(right)
	return leftOK && rightOK && leftOrigin == rightOrigin
}

func providerCredentialOriginAllowed(provider, baseURL string) bool {
	expected, ok := modImportCredentialOrigins[strings.ToLower(strings.TrimSpace(provider))]
	if !ok {
		return false
	}
	origin, valid := providerOrigin(baseURL)
	return valid && origin == expected
}

func providerOrigin(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	if port != "" {
		hostname += ":" + port
	}
	return scheme + "://" + hostname, true
}

func redactModImportConfig(cfg modImportConfig) modImportConfig {
	redact := func(provider modImportProviderConfig) modImportProviderConfig {
		provider.HasToken = provider.Token != ""
		provider.HasAPIKey = provider.APIKey != ""
		provider.Token = ""
		provider.APIKey = ""
		provider.ClearToken = false
		provider.ClearAPIKey = false
		return provider
	}
	cfg.Modrinth = redact(cfg.Modrinth)
	cfg.CurseForge = redact(cfg.CurseForge)
	cfg.GitHub = redact(cfg.GitHub)
	return cfg
}
