package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

type natsJetStreamConfigDTO struct {
	NewStreamMaxBytes     int64  `json:"newStreamMaxBytes"`
	Enabled               bool   `json:"enabled"`
	Stream                string `json:"stream"`
	MaxDeliver            int    `json:"maxDeliver"`
	AckWaitSeconds        int    `json:"ackWaitSeconds"`
	PublishTimeoutSeconds int    `json:"publishTimeoutSeconds"`
}

type natsConfigResponse struct {
	Revision      string                  `json:"revision"`
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
	NewStreamMaxBytes     *int64  `json:"newStreamMaxBytes,omitempty"`
	Enabled               *bool   `json:"enabled"`
	Stream                *string `json:"stream"`
	MaxDeliver            *int    `json:"maxDeliver"`
	AckWaitSeconds        *int    `json:"ackWaitSeconds"`
	PublishTimeoutSeconds *int    `json:"publishTimeoutSeconds"`
}

type natsConfigUpdateRequest struct {
	Revision      string                      `json:"revision"`
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

var errNATSCredentialDecision = errors.New("replace or clear saved credentials before changing their NATS destination")

func natsConfigRevision(setting database.NATSConfigSetting, key string) (string, error) {
	if len(setting.Encrypted) > 0 {
		return encryptedSettingRevision(setting.Encrypted), nil
	}
	// Two uninitialized replicas may have different environment settings.
	// A keyed revision distinguishes them without publishing a secret hash.
	key = strings.TrimSpace(key)
	if len(key) < 32 {
		return encryptedSettingRevision(nil), nil
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte("mcmods:nats:fallback:v1\x00"))
	if err := json.NewEncoder(mac).Encode(setting.Config); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (payload natsConfigUpdateRequest) config(current config.NATSConfig) (config.NATSConfig, error) {
	if payload.Enabled == nil || payload.URL == nil || payload.Username == nil || payload.SubjectPrefix == nil ||
		payload.Tasks == nil || payload.Realtime == nil || payload.JetStream == nil ||
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

	streamBudget := current.JetStream.NewStreamMaxBytes
	if value := payload.JetStream.NewStreamMaxBytes; value != nil {
		if *value < 1 {
			return config.NATSConfig{}, errors.New("new stream byte limit must be positive")
		}
		streamBudget = *value
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
	internalURL, err := mergeNATSURLCredentialUpdates(current, payload)
	if err != nil {
		return config.NATSConfig{}, err
	}
	if err := validateNATSCredentialTarget(current, internalURL, payload); err != nil {
		return config.NATSConfig{}, err
	}
	return config.NATSConfig{
		Enabled:       *payload.Enabled,
		URL:           internalURL,
		Username:      *payload.Username,
		Password:      password,
		Token:         token,
		SubjectPrefix: *payload.SubjectPrefix,
		Tasks:         append([]config.NATSTaskConfig(nil), (*payload.Tasks)...),
		OutboxEnabled: true,
		Realtime:      *payload.Realtime,
		JetStream: config.JetStreamConfig{
			Enabled:           *payload.JetStream.Enabled,
			Stream:            *payload.JetStream.Stream,
			NewStreamMaxBytes: streamBudget,
			MaxDeliver:        *payload.JetStream.MaxDeliver,
			AckWait:           time.Duration(*payload.JetStream.AckWaitSeconds) * time.Second,
			PublishTimeout:    time.Duration(*payload.JetStream.PublishTimeoutSeconds) * time.Second,
		},
	}, nil
}

func validateNATSCredentialTarget(current config.NATSConfig, nextURL string, payload natsConfigUpdateRequest) error {
	hasURLPassword, hasURLToken := natsURLCredentialFlags(current.URL)
	keepPassword := (current.Password != "" || hasURLPassword) &&
		!payload.ClearPassword && (payload.Password == nil || *payload.Password == "")
	keepToken := (current.Token != "" || hasURLToken) &&
		!payload.ClearToken && (payload.Token == nil || *payload.Token == "")
	if !keepPassword && !keepToken {
		return nil
	}
	if keepPassword && strings.TrimSpace(*payload.Username) != strings.TrimSpace(current.Username) {
		return errNATSCredentialDecision
	}
	// Compare every requested server. Removing a peer is safe with global
	// credentials. A new host, port, protocol or WebSocket path needs a decision.
	previous := make(map[string]bool)
	for _, raw := range strings.Split(current.URL, ",") {
		previous[natsCredentialDestination(raw)] = true
	}
	for _, raw := range strings.Split(nextURL, ",") {
		if !previous[natsCredentialDestination(raw)] {
			return errNATSCredentialDecision
		}
	}
	return nil
}

func natsCredentialDestination(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "nats://" + raw
	}
	address, err := url.Parse(raw)
	if err != nil || address.Hostname() == "" {
		return ""
	}
	address.User = nil
	host := address.Hostname()
	if !strings.ContainsAny(host, ":%") && strings.IndexFunc(host, func(r rune) bool { return r > 127 }) < 0 {
		host = strings.ToLower(host)
	}
	port := address.Port()
	if port == "" {
		switch address.Scheme {
		case "ws":
			port = "80"
		case "wss":
			port = "443"
		default:
			port = "4222"
		}
	}
	address.Host = net.JoinHostPort(host, port)
	return address.String()
}

// URL userinfo takes priority over nats.go auth options. Keep legacy per-server
// credentials on a no-change form round trip, and update the matching auth kind
// when the administrator explicitly replaces or clears a secret.
func mergeNATSURLCredentialUpdates(current config.NATSConfig, payload natsConfigUpdateRequest) (string, error) {
	raw := strings.TrimSpace(*payload.URL)
	unchanged := raw == queue.RedactURL(current.URL)
	if unchanged {
		raw = current.URL
	}
	// A cluster edit may reorder or remove peers. Restore a masked URL's own
	// userinfo only for the same retained endpoint, never on an added peer.
	type previousLogin struct {
		userinfo                         *url.Userinfo
		ambiguous, hasPassword, hasToken bool
	}
	byEndpoint := make(map[string]previousLogin)
	for _, old := range strings.Split(current.URL, ",") {
		old = strings.TrimSpace(old)
		if !strings.Contains(old, "://") {
			old = "nats://" + old
		}
		address, err := url.Parse(old)
		if err != nil || address.Hostname() == "" {
			continue
		}
		key := natsCredentialDestination(old)
		previous, exists := byEndpoint[key]
		if exists && previous.userinfo.String() != address.User.String() {
			previous.ambiguous = true
		}
		previous.userinfo = address.User
		if address.User != nil {
			if value, password := address.User.Password(); password {
				previous.hasPassword = previous.hasPassword || value != ""
			} else {
				previous.hasToken = previous.hasToken || address.User.Username() != ""
			}
		}
		byEndpoint[key] = previous
	}
	servers := strings.Split(raw, ",")
	for i, server := range servers {
		server = strings.TrimSpace(server)
		schemed := strings.Contains(server, "://")
		if !schemed {
			server = "nats://" + server
		}
		address, err := url.Parse(server)
		if err != nil || address.Hostname() == "" {
			return "", errors.New("invalid NATS endpoint")
		}
		if !unchanged && address.User == nil {
			previous := byEndpoint[natsCredentialDestination(server)]
			if previous.ambiguous {
				retainPassword := previous.hasPassword && !payload.ClearPassword && (payload.Password == nil || *payload.Password == "")
				retainToken := previous.hasToken && !payload.ClearToken && (payload.Token == nil || *payload.Token == "")
				if retainPassword || retainToken {
					return "", errNATSCredentialDecision
				}
			} else {
				address.User = previous.userinfo
			}
		}
		if address.User != nil {
			username := address.User.Username()
			password, hasPassword := address.User.Password()
			switch {
			case payload.Token != nil && *payload.Token != "":
				address.User = url.User(*payload.Token)
			case !hasPassword && payload.ClearToken:
				address.User = nil
			case hasPassword:
				if *payload.Username != current.Username {
					username = *payload.Username
				}
				if payload.ClearPassword {
					password = ""
				} else if payload.Password != nil && *payload.Password != "" {
					password = *payload.Password
				}
				// Retaining the colon is essential: username without a password
				// marker is interpreted as a token by the NATS protocol client.
				address.User = url.UserPassword(username, password)
			}
		}
		servers[i] = address.String()
		if !schemed {
			servers[i] = strings.TrimPrefix(servers[i], "nats://")
		}
	}
	return strings.Join(servers, ","), nil
}

func natsURLCredentialFlags(raw string) (hasPassword, hasToken bool) {
	for _, server := range strings.Split(raw, ",") {
		server = strings.TrimSpace(server)
		if !strings.Contains(server, "://") {
			server = "nats://" + server
		}
		address, err := url.Parse(server)
		if err != nil || address.User == nil {
			continue
		}
		if password, exists := address.User.Password(); exists {
			hasPassword = hasPassword || password != ""
		} else {
			hasToken = hasToken || address.User.Username() != ""
		}
	}
	return
}

func (s *Server) getNATSConfig(w http.ResponseWriter, r *http.Request) {
	setting, err := database.ReadNATSConfig(r.Context(), s.db, s.cfg.NATS, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 NATS 配置失败")
		return
	}
	view := redactNATSConfig(setting.Config, s.natsStatus())
	view.Revision, err = natsConfigRevision(setting, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "NATS_SETTINGS_UNAVAILABLE", "NATS settings are temporarily unavailable", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) updateNATSConfig(w http.ResponseWriter, r *http.Request) {
	var request natsConfigUpdateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if request.Revision == "" {
		writeAPIError(w, http.StatusPreconditionRequired, "NATS_CONFIG_REVISION_REQUIRED", "read the latest NATS configuration before saving", 0, nil)
		return
	}
	s.natsConfigMu.Lock()
	defer s.natsConfigMu.Unlock()
	current, err := s.readNATSCandidateBase(r.Context(), currentClaims(r))
	if err != nil {
		switch {
		case errors.Is(err, errNATSConfigWriteForbidden):
			writeError(w, http.StatusForbidden, "permission denied")
		case errors.Is(err, pgx.ErrNoRows), errors.Is(err, errWriteAccountBanned):
			writeCurrentWriteActorFailure(w, err)
		default:
			writeAPIError(w, http.StatusServiceUnavailable, "NATS_SETTINGS_UNAVAILABLE", "NATS settings are temporarily unavailable", 0, nil)
		}
		return
	}
	currentRevision, err := natsConfigRevision(current, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "NATS_SETTINGS_UNAVAILABLE", "NATS settings are temporarily unavailable", 0, nil)
		return
	}
	if request.Revision != currentRevision {
		writeNATSConfigConflict(w)
		return
	}
	payload, err := request.config(current.Config)
	if errors.Is(err, errNATSCredentialDecision) {
		writeAPIError(w, http.StatusBadRequest, "NATS_CREDENTIAL_DECISION_REQUIRED", err.Error(), 0, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "NATS 配置格式不正确")
		return
	}
	payload = queue.NormalizeConfig(payload)
	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "NATS_SETTINGS_UNAVAILABLE", "NATS settings are temporarily unavailable", 0, nil)
		return
	}
	if s.queue == nil {
		writeError(w, http.StatusServiceUnavailable, "NATS 运行时不可用，原配置未更改")
		return
	}
	var stored []byte
	var persistenceErr error
	err = s.queue.ReconfigureWithPersistence(payload, func(config.NATSConfig) error {
		stored, persistenceErr = s.persistNATSCandidate(r.Context(), current.Encrypted, raw, currentClaims(r))
		return persistenceErr
	})
	if err != nil {
		switch {
		case errors.Is(persistenceErr, errNATSConfigConflict):
			writeNATSConfigConflict(w)
		case errors.Is(persistenceErr, errNATSConfigWriteForbidden):
			writeError(w, http.StatusForbidden, "permission denied")
		case errors.Is(persistenceErr, pgx.ErrNoRows), errors.Is(persistenceErr, errWriteAccountBanned):
			writeCurrentWriteActorFailure(w, persistenceErr)
		case persistenceErr != nil:
			writeError(w, http.StatusInternalServerError, "保存 NATS 配置失败，原配置仍在使用")
		default:
			writeError(w, http.StatusBadGateway, "应用 NATS 配置失败，原配置仍在使用")
		}
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_nats_config", "nats.config", currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{
		"enabled": payload.Enabled, "outboxEnabled": payload.OutboxEnabled, "realtime": payload.Realtime,
		"jetStreamEnabled": payload.JetStream.Enabled, "tasks": len(payload.Tasks),
	})
	view := redactNATSConfig(payload, s.natsStatus())
	view.Revision = encryptedSettingRevision(stored)
	writeJSON(w, http.StatusOK, view)
}

var (
	errNATSConfigConflict       = errors.New("NATS configuration changed since it was read")
	errNATSConfigWriteForbidden = errors.New("changing NATS settings requires admin.config.write")
)

func resolveNATSWriter(ctx context.Context, tx pgx.Tx, claims security.Claims) (security.Claims, error) {
	actor, err := resolveCurrentWriteActorTx(ctx, tx, claims)
	if err != nil {
		return security.Claims{}, err
	}
	if !claimsAllow(actor, "admin.config.write") {
		return security.Claims{}, errNATSConfigWriteForbidden
	}
	return actor, nil
}

func (s *Server) readNATSCandidateBase(ctx context.Context, claims security.Claims) (database.NATSConfigSetting, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return database.NATSConfigSetting{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = resolveNATSWriter(ctx, tx, claims); err != nil {
		return database.NATSConfigSetting{}, err
	}
	return database.ReadNATSConfig(ctx, tx, s.cfg.NATS, s.cfg.SettingsEncryptionKey)
}

func (s *Server) persistNATSCandidate(ctx context.Context, previous []byte, sealed string, claims security.Claims) ([]byte, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Acquire the DB connection and lock after preparing the candidate. Queue
	// recovery loads settings under its own lock, including on one-connection pools.
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('settings:nats.config',0))`); err != nil {
		return nil, err
	}
	actor, err := resolveNATSWriter(ctx, tx, claims)
	if err != nil {
		return nil, err
	}
	var saved []byte
	if len(previous) == 0 {
		err = tx.QueryRow(ctx, `insert into system_settings(key,value,updated_by,updated_at)
			values('nats.config',$1::jsonb,$2,now()) on conflict(key) do nothing returning value`, sealed, actor.Subject).Scan(&saved)
	} else {
		err = tx.QueryRow(ctx, `update system_settings set value=$1::jsonb,updated_by=$2,updated_at=now()
			where key='nats.config' and value=$3::jsonb returning value`, sealed, actor.Subject, previous).Scan(&saved)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errNATSConfigConflict
	}
	if err != nil {
		return nil, err
	}
	return saved, tx.Commit(ctx)
}

func writeNATSConfigConflict(w http.ResponseWriter) {
	writeAPIError(w, http.StatusConflict, "NATS_CONFIG_CONFLICT", errNATSConfigConflict.Error(), 0, nil)
}

func (s *Server) natsStatus() queue.Status {
	if s.queue == nil {
		return queue.Status{}
	}
	return s.queue.Status()
}

func redactNATSConfig(cfg config.NATSConfig, status queue.Status) natsConfigResponse {
	urlPassword, urlToken := natsURLCredentialFlags(cfg.URL)
	status.URL = queue.RedactURL(status.URL)
	return natsConfigResponse{
		Enabled: cfg.Enabled, URL: queue.RedactURL(cfg.URL), Username: cfg.Username,
		HasPassword: cfg.Password != "" || urlPassword, HasToken: cfg.Token != "" || urlToken,
		SubjectPrefix: cfg.SubjectPrefix, Tasks: cfg.Tasks, OutboxEnabled: cfg.OutboxEnabled, Realtime: cfg.Realtime,
		JetStream: natsJetStreamConfigDTO{
			NewStreamMaxBytes: cfg.JetStream.NewStreamMaxBytes, Enabled: cfg.JetStream.Enabled, Stream: cfg.JetStream.Stream, MaxDeliver: cfg.JetStream.MaxDeliver,
			AckWaitSeconds: int(cfg.JetStream.AckWait / time.Second), PublishTimeoutSeconds: int(cfg.JetStream.PublishTimeout / time.Second),
		},
		Status: status,
	}
}
