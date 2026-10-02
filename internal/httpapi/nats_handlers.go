package httpapi

import (
	"errors"
	"net/http"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/queue"
)

type natsJetStreamConfigDTO struct {
	Enabled               bool   `json:"enabled"`
	Stream                string `json:"stream"`
	MaxDeliver            int    `json:"maxDeliver"`
	AckWaitSeconds        int    `json:"ackWaitSeconds"`
	PublishTimeoutSeconds int    `json:"publishTimeoutSeconds"`
}

type natsConfigResponse struct {
	Enabled       bool                    `json:"enabled"`
	URL           string                  `json:"url"`
	Username      string                  `json:"username"`
	HasPassword   bool                    `json:"hasPassword"`
	HasToken      bool                    `json:"hasToken"`
	SubjectPrefix string                  `json:"subjectPrefix"`
	Tasks         []config.NATSTaskConfig `json:"tasks"`
	OutboxEnabled bool                    `json:"outboxEnabled"`
	Realtime      bool                    `json:"realtime"`
	JetStream     natsJetStreamConfigDTO  `json:"jetStream"`
	Status        queue.Status            `json:"status"`
}

type natsJetStreamUpdateRequest struct {
	Enabled               *bool   `json:"enabled"`
	Stream                *string `json:"stream"`
	MaxDeliver            *int    `json:"maxDeliver"`
	AckWaitSeconds        *int    `json:"ackWaitSeconds"`
	PublishTimeoutSeconds *int    `json:"publishTimeoutSeconds"`
}

type natsConfigUpdateRequest struct {
	Enabled       *bool                       `json:"enabled"`
	URL           *string                     `json:"url"`
	Username      *string                     `json:"username"`
	Password      *string                     `json:"password"`
	ClearPassword bool                        `json:"clearPassword"`
	Token         *string                     `json:"token"`
	ClearToken    bool                        `json:"clearToken"`
	SubjectPrefix *string                     `json:"subjectPrefix"`
	Tasks         *[]config.NATSTaskConfig    `json:"tasks"`
	OutboxEnabled *bool                       `json:"outboxEnabled"`
	Realtime      *bool                       `json:"realtime"`
	JetStream     *natsJetStreamUpdateRequest `json:"jetStream"`
}

func (payload natsConfigUpdateRequest) config(current config.NATSConfig) (config.NATSConfig, error) {
	if payload.Enabled == nil || payload.URL == nil || payload.Username == nil || payload.SubjectPrefix == nil ||
		payload.Tasks == nil || payload.OutboxEnabled == nil || payload.Realtime == nil || payload.JetStream == nil ||
		payload.JetStream.Enabled == nil || payload.JetStream.Stream == nil || payload.JetStream.MaxDeliver == nil ||
		payload.JetStream.AckWaitSeconds == nil || payload.JetStream.PublishTimeoutSeconds == nil {
		return config.NATSConfig{}, errors.New("complete NATS configuration is required")
	}
	if payload.ClearPassword && payload.Password != nil && *payload.Password != "" {
		return config.NATSConfig{}, errors.New("password cannot be replaced and cleared together")
	}
	if payload.ClearToken && payload.Token != nil && *payload.Token != "" {
		return config.NATSConfig{}, errors.New("token cannot be replaced and cleared together")
	}
	if *payload.JetStream.MaxDeliver < 2 || *payload.JetStream.MaxDeliver > 100 ||
		*payload.JetStream.AckWaitSeconds < 1 || *payload.JetStream.AckWaitSeconds > 86400 ||
		*payload.JetStream.PublishTimeoutSeconds < 1 || *payload.JetStream.PublishTimeoutSeconds > 300 {
		return config.NATSConfig{}, errors.New("JetStream delivery and timeout settings are invalid")
	}
	if *payload.JetStream.Enabled && !*payload.Enabled {
		return config.NATSConfig{}, errors.New("NATS must be enabled when JetStream is enabled")
	}

	password := current.Password
	if payload.ClearPassword {
		password = ""
	} else if payload.Password != nil && *payload.Password != "" {
		password = *payload.Password
	}
	token := current.Token
	if payload.ClearToken {
		token = ""
	} else if payload.Token != nil && *payload.Token != "" {
		token = *payload.Token
	}
	return config.NATSConfig{
		Enabled:       *payload.Enabled,
		URL:           *payload.URL,
		Username:      *payload.Username,
		Password:      password,
		Token:         token,
		SubjectPrefix: *payload.SubjectPrefix,
		Tasks:         append([]config.NATSTaskConfig(nil), (*payload.Tasks)...),
		OutboxEnabled: *payload.OutboxEnabled,
		Realtime:      *payload.Realtime,
		JetStream: config.JetStreamConfig{
			Enabled:        *payload.JetStream.Enabled,
			Stream:         *payload.JetStream.Stream,
			MaxDeliver:     *payload.JetStream.MaxDeliver,
			AckWait:        time.Duration(*payload.JetStream.AckWaitSeconds) * time.Second,
			PublishTimeout: time.Duration(*payload.JetStream.PublishTimeoutSeconds) * time.Second,
		},
	}, nil
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
	s.natsConfigMu.Lock()
	defer s.natsConfigMu.Unlock()

	var request natsConfigUpdateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	current, err := database.LoadNATSConfig(r.Context(), s.db, s.cfg.NATS, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 NATS 配置失败")
		return
	}
	payload, err := request.config(current)
	if err != nil {
		writeError(w, http.StatusBadRequest, "NATS 配置格式不正确")
		return
	}
	payload = queue.NormalizeConfig(payload)
	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "NATS 配置格式不正确")
		return
	}
	if s.queue == nil {
		writeError(w, http.StatusServiceUnavailable, "NATS 运行时不可用，原配置未更改")
		return
	}
	var persistenceErr error
	err = s.queue.ReconfigureWithPersistence(payload, func(config.NATSConfig) error {
		_, persistenceErr = s.db.Exec(
			r.Context(),
			`insert into system_settings (key, value, updated_by, updated_at)
			 values ('nats.config', $1::jsonb, $2, now())
			 on conflict (key) do update
			 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
			raw,
			currentClaims(r).Subject,
		)
		return persistenceErr
	})
	if err != nil {
		if persistenceErr != nil {
			writeError(w, http.StatusInternalServerError, "保存 NATS 配置失败，原配置仍在使用")
		} else {
			writeError(w, http.StatusBadGateway, "应用 NATS 配置失败，原配置仍在使用")
		}
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_nats_config", "nats.config", currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{
		"enabled": payload.Enabled, "outboxEnabled": payload.OutboxEnabled, "realtime": payload.Realtime,
		"jetStreamEnabled": payload.JetStream.Enabled, "tasks": len(payload.Tasks),
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
		Enabled: cfg.Enabled, URL: cfg.URL, Username: cfg.Username,
		HasPassword: cfg.Password != "", HasToken: cfg.Token != "",
		SubjectPrefix: cfg.SubjectPrefix, Tasks: cfg.Tasks, OutboxEnabled: cfg.OutboxEnabled, Realtime: cfg.Realtime,
		JetStream: natsJetStreamConfigDTO{
			Enabled: cfg.JetStream.Enabled, Stream: cfg.JetStream.Stream, MaxDeliver: cfg.JetStream.MaxDeliver,
			AckWaitSeconds: int(cfg.JetStream.AckWait / time.Second), PublishTimeoutSeconds: int(cfg.JetStream.PublishTimeout / time.Second),
		},
		Status: status,
	}
}
