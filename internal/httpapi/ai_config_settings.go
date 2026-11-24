package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

type aiConfigQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type aiConfigSetting struct {
	config    aiConfigPayload
	encrypted []byte
}

type aiConfigView struct {
	aiConfigPayload
	Revision string `json:"revision"`
}

type aiConfigUpdate struct {
	aiConfigPayload
	Revision     string   `json:"revision"`
	ClearAPIKeys []string `json:"clearApiKeys,omitempty"`
}

func readAIConfigSetting(ctx context.Context, queryer aiConfigQueryer, encryptionKey string) (aiConfigSetting, error) {
	setting := aiConfigSetting{config: defaultAIConfig()}
	err := queryer.QueryRow(ctx, `select value from system_settings where key = 'ai.config'`).Scan(&setting.encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return setting, nil
	}
	if err != nil {
		return aiConfigSetting{}, err
	}
	raw, err := security.DecryptSetting(encryptionKey, setting.encrypted)
	if err != nil {
		return aiConfigSetting{}, err
	}
	document, err := decodeStoredJSONObject(raw, "AI settings")
	if err != nil {
		return aiConfigSetting{}, err
	}
	if err = validateAIQuotaNumbers(document); err != nil {
		return aiConfigSetting{}, err
	}
	if err = json.Unmarshal(raw, &setting.config); err != nil {
		return aiConfigSetting{}, err
	}
	setting.config = normalizeAIConfig(setting.config)
	return setting, nil
}

func readAIConfigFromSettings(ctx context.Context, queryer aiConfigQueryer, encryptionKey string) (aiConfigPayload, error) {
	setting, err := readAIConfigSetting(ctx, queryer, encryptionKey)
	return setting.config, err
}

func aiConfigFromDatabase(ctx context.Context, queryer aiConfigQueryer, encryptionKey string) aiConfigPayload {
	payload, err := readAIConfigFromSettings(ctx, queryer, encryptionKey)
	if err != nil {
		// Execution paths fail closed with disabled providers. Admin reads and
		// writes must handle the error instead of displaying defaults.
		return defaultAIConfig()
	}
	return payload
}

func (s *Server) aiConfigFromSettings(ctx context.Context) aiConfigPayload {
	return aiConfigFromDatabase(ctx, s.db, s.cfg.SettingsEncryptionKey)
}

func (setting aiConfigSetting) revision() string {
	return encryptedSettingRevision(setting.encrypted)
}

func (setting aiConfigSetting) view() aiConfigView {
	return aiConfigView{aiConfigPayload: redactAIConfig(setting.config), Revision: setting.revision()}
}

func (s *Server) getAIConfig(w http.ResponseWriter, r *http.Request) {
	setting, err := readAIConfigSetting(r.Context(), s.db, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeAIConfigUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, setting.view())
}

func writeAIConfigUnavailable(w http.ResponseWriter) {
	writeAPIError(w, http.StatusServiceUnavailable, "AI_SETTINGS_UNAVAILABLE", "AI settings are temporarily unavailable", 0, nil)
}

func aiCredentialOrigin(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return parsed.Scheme + "://" + net.JoinHostPort(host, port)
}

func validateAIQuotaNumbers(document map[string]any) error {
	for field, value := range document {
		if !strings.EqualFold(field, "quotas") {
			continue
		}
		quotas, _ := value.([]any)
		for _, item := range quotas {
			if item == nil {
				return errors.New("AI quota entries must be objects")
			}
			quota, _ := item.(map[string]any)
			for key, limit := range quota {
				if limit != nil {
					continue
				}
				switch strings.ToLower(key) {
				case "requestlimit", "tokenlimit", "costlimitcny":
					return errors.New("AI quota limits must be numbers")
				}
			}
		}
	}
	return nil
}

func (s *Server) updateAIConfig(w http.ResponseWriter, r *http.Request) {
	var body json.RawMessage
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	document, err := decodeStoredJSONObject(body, "AI settings")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if err = validateAIQuotaNumbers(document); err != nil {
		writeAPIError(w, http.StatusBadRequest, "AI_QUOTA_VALUE_INVALID", err.Error(), 0, nil)
		return
	}
	var update aiConfigUpdate
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if update.Revision == "" {
		writeAPIError(w, http.StatusPreconditionRequired, "AI_CONFIG_REVISION_REQUIRED", "read the latest AI configuration before saving", 0, nil)
		return
	}
	payload := normalizeAIConfig(update.aiConfigPayload)
	if err := validateAIQuotas(payload.Quotas); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	providers := make(map[string]int, len(payload.Providers))
	for index, provider := range payload.Providers {
		if _, exists := providers[provider.Code]; provider.Code == "" || exists {
			writeAPIError(w, http.StatusBadRequest, "AI_PROVIDER_CODE_INVALID", "provider codes must be nonempty and unique", 0, nil)
			return
		}
		providers[provider.Code] = index
		if err := validateAIProviderEndpoint(provider.BaseURL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	clearKeys := make(map[string]bool, len(update.ClearAPIKeys))
	for _, code := range update.ClearAPIKeys {
		code = normalizeAIToken(code)
		index, found := providers[code]
		if !found || clearKeys[code] || payload.Providers[index].APIKey != "" {
			writeAPIError(w, http.StatusBadRequest, "AI_PROVIDER_KEY_UPDATE_INVALID", "choose one key action per configured provider", 0, nil)
			return
		}
		clearKeys[code] = true
	}
	for _, binding := range payload.TaskModels {
		if binding.TimeoutSeconds > 600 {
			writeError(w, http.StatusBadRequest, "AI task timeout cannot exceed 600 seconds")
			return
		}
	}

	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeAIConfigUnavailable(w)
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('ai-config-settings-write'))`); err != nil {
		writeAIConfigUnavailable(w)
		return
	}
	current, err := readAIConfigSetting(ctx, tx, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeAIConfigUnavailable(w)
		return
	}
	if update.Revision != current.revision() {
		writeAPIError(w, http.StatusConflict, "AI_CONFIG_CONFLICT", "AI configuration changed since it was read", 0, nil)
		return
	}
	previousProviders := make(map[string]aiProviderConfig, len(current.config.Providers))
	for _, provider := range current.config.Providers {
		if _, found := previousProviders[provider.Code]; !found {
			previousProviders[provider.Code] = provider
		}
	}
	for index := range payload.Providers {
		provider := &payload.Providers[index]
		provider.HasAPIKey = false
		previous := previousProviders[provider.Code]
		if provider.APIKey != "" || clearKeys[provider.Code] || previous.APIKey == "" {
			continue
		}
		if aiCredentialOrigin(provider.BaseURL) != aiCredentialOrigin(previous.BaseURL) {
			writeAPIError(w, http.StatusBadRequest, "AI_PROVIDER_KEY_ACTION_REQUIRED", "replace or clear the key when changing its provider origin", 0,
				map[string]string{"provider": provider.Code})
			return
		}
		provider.APIKey = previous.APIKey
	}
	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "AI 配置格式不正确")
		return
	}
	actorID := currentClaims(r).Subject
	var committed aiConfigSetting
	err = tx.QueryRow(ctx, `insert into system_settings (key, value, updated_by, updated_at)
		values ('ai.config', $1::jsonb, $2, now())
		on conflict (key) do update
		set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()
		returning value`, raw, actorID).Scan(&committed.encrypted)
	if err != nil || tx.Commit(ctx) != nil {
		writeError(w, http.StatusInternalServerError, "保存 AI 配置失败")
		return
	}
	s.writeAppLog(ctx, "admin_operation", "info", "update_ai_config", "ai.config", actorID, r, http.StatusOK, 0, map[string]any{
		"providers": len(payload.Providers),
		"models":    len(payload.Models),
	})
	committed.config = payload
	writeJSON(w, http.StatusOK, committed.view())
}
