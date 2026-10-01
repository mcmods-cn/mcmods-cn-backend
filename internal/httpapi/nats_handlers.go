package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/queue"
)

type natsConfigResponse struct {
	Enabled       bool                    `json:"enabled"`
	URL           string                  `json:"url"`
	Username      string                  `json:"username"`
	HasPassword   bool                    `json:"hasPassword"`
	HasToken      bool                    `json:"hasToken"`
	SubjectPrefix string                  `json:"subjectPrefix"`
	Tasks         []config.NATSTaskConfig `json:"tasks"`
	Status        queue.Status            `json:"status"`
}

func (s *Server) getNATSConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := database.LoadNATSConfig(r.Context(), s.db, s.cfg.NATS, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 NATS 配置失败")
		return
	}
	writeJSON(w, http.StatusOK, redactNATSConfig(cfg, s.natsStatus()))
}

func (s *Server) updateNATSConfig(w http.ResponseWriter, r *http.Request) {
	var payload config.NATSConfig
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	current, err := database.LoadNATSConfig(r.Context(), s.db, s.cfg.NATS, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 NATS 配置失败")
		return
	}
	if payload.Password == "" {
		payload.Password = current.Password
	}
	if payload.Token == "" {
		payload.Token = current.Token
	}
	if strings.TrimSpace(payload.URL) == redactNATSConfigURLs(current.URL) {
		// An unchanged public URL is a redacted view of the stored setting.
		// Preserve legacy per-server credentials when editing other fields.
		payload.URL = current.URL
	}
	payload = queue.NormalizeConfig(payload)
	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "NATS 配置格式不正确")
		return
	}
	_, err = s.db.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ('nats.config', $1::jsonb, $2, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		raw,
		currentClaims(r).Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 NATS 配置失败")
		return
	}
	if s.queue != nil {
		err = s.queue.Reconfigure(payload)
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_nats_config", "nats.config", currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{
		"enabled": payload.Enabled,
		"tasks":   len(payload.Tasks),
	})
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "nats_connection_failed",
			"NATS configuration was saved, but its connection could not be established", 0, map[string]bool{"saved": true})
		return
	}
	writeJSON(w, http.StatusOK, redactNATSConfig(payload, s.natsStatus()))
}

func (s *Server) natsStatus() queue.Status {
	if s.queue == nil {
		return queue.Status{}
	}
	return s.queue.Status()
}

func redactNATSConfig(cfg config.NATSConfig, status queue.Status) natsConfigResponse {
	return natsConfigResponse{
		Enabled:       cfg.Enabled,
		URL:           redactNATSConfigURLs(cfg.URL),
		Username:      cfg.Username,
		HasPassword:   strings.TrimSpace(cfg.Password) != "",
		HasToken:      strings.TrimSpace(cfg.Token) != "",
		SubjectPrefix: cfg.SubjectPrefix,
		Tasks:         cfg.Tasks,
		Status:        status,
	}
}

func redactNATSConfigURLs(raw string) string {
	servers := strings.Split(raw, ",")
	for index, server := range servers {
		parsed, err := url.Parse(strings.TrimSpace(server))
		if err != nil || parsed.Host == "" {
			servers[index] = "<invalid>"
			continue
		}
		parsed.User = nil
		// NATS server URLs have no public path/query/fragment metadata; avoid
		// echoing accidentally embedded credentials in those components too.
		parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = "", "", "", ""
		servers[index] = parsed.String()
	}
	return strings.Join(servers, ",")
}
