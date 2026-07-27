package httpapi

import (
	"net/http"
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
		_ = s.queue.Reconfigure(payload)
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_nats_config", "nats.config", currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{
		"enabled": payload.Enabled,
		"tasks":   len(payload.Tasks),
	})
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
		URL:           cfg.URL,
		Username:      cfg.Username,
		HasPassword:   strings.TrimSpace(cfg.Password) != "",
		HasToken:      strings.TrimSpace(cfg.Token) != "",
		SubjectPrefix: cfg.SubjectPrefix,
		Tasks:         cfg.Tasks,
		Status:        status,
	}
}
