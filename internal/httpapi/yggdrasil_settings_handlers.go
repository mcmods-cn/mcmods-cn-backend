package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/config"
)

const yggdrasilSettingKey = "minecraft.yggdrasil"

type yggdrasilSettingsPayload struct {
	Enabled           bool     `json:"enabled"`
	PublicBaseURL     string   `json:"publicBaseUrl"`
	TextureBaseURL    string   `json:"textureBaseUrl"`
	ServerName        string   `json:"serverName"`
	PrivateKeyBase64  string   `json:"privateKeyBase64,omitempty"`
	TrustedProxyCIDRs []string `json:"trustedProxyCidrs"`
	TokenTTLHours     int      `json:"tokenTtlHours"`
	MaxTokens         int      `json:"maxTokens"`
	JoinTTLSeconds    int      `json:"joinTtlSeconds"`
	TextureMaxBytes   int64    `json:"textureMaxBytes"`
}

type updateYggdrasilSettingsRequest struct {
	yggdrasilSettingsPayload
	RotatePrivateKey bool `json:"rotatePrivateKey"`
}

func (s *Server) getYggdrasilConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.redactedYggdrasilConfig())
}

func (s *Server) updateYggdrasilConfig(w http.ResponseWriter, r *http.Request) {
	var request updateYggdrasilSettingsRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid Yggdrasil settings")
		return
	}

	current, service := s.yggdrasilRuntimeSnapshot()
	next := yggdrasilConfigFromPayload(request.yggdrasilSettingsPayload)
	if request.RotatePrivateKey {
		encoded, err := generateYggdrasilPrivateKey()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate Yggdrasil signing key")
			return
		}
		next.PrivateKeyBase64 = encoded
	} else if strings.TrimSpace(next.PrivateKeyBase64) == "" {
		next.PrivateKeyBase64 = current.PrivateKeyBase64
		if next.PrivateKeyBase64 == "" && service != nil && service.privateKey != nil {
			encoded, err := encodeYggdrasilPrivateKey(service.privateKey)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to preserve Yggdrasil signing key")
				return
			}
			next.PrivateKeyBase64 = encoded
		}
	}
	if err := normalizeYggdrasilConfig(&next, s.cfg.Env); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	serverConfig := config.Config{Env: s.cfg.Env, Yggdrasil: next}
	nextService := newYggdrasilService(serverConfig)
	if next.Enabled && (nextService == nil || nextService.disabledReason != nil) {
		reason := "invalid Yggdrasil settings"
		if nextService != nil && nextService.disabledReason != nil {
			reason = nextService.disabledReason.Error()
		}
		writeError(w, http.StatusUnprocessableEntity, reason)
		return
	}
	if next.Enabled && next.PrivateKeyBase64 == "" && nextService.privateKey != nil {
		encoded, encodeErr := encodeYggdrasilPrivateKey(nextService.privateKey)
		if encodeErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist Yggdrasil signing key")
			return
		}
		next.PrivateKeyBase64 = encoded
	}

	sealed, err := s.sealSystemSetting(yggdrasilPayloadFromConfig(next))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid Yggdrasil settings")
		return
	}
	if _, err = s.db.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at)
		values($1,$2::jsonb,$3,now()) on conflict(key) do update set value=excluded.value,
		updated_by=excluded.updated_by,updated_at=now()`, yggdrasilSettingKey, sealed, currentClaims(r).Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save Yggdrasil settings")
		return
	}

	s.replaceYggdrasilRuntime(next, nextService)
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_yggdrasil_config", next.ServerName, currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{
		"enabled":           next.Enabled,
		"rotatedSigningKey": request.RotatePrivateKey,
	})
	writeJSON(w, http.StatusOK, s.redactedYggdrasilConfig())
}

func (s *Server) loadYggdrasilConfig(ctx context.Context) (config.YggdrasilConfig, error) {
	fallback := cloneYggdrasilConfig(s.cfg.Yggdrasil)
	var stored []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key=$1`, yggdrasilSettingKey).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	var payload yggdrasilSettingsPayload
	if err = s.openSystemSetting(stored, &payload); err != nil {
		return fallback, fmt.Errorf("decrypt Yggdrasil settings: %w", err)
	}
	loaded := yggdrasilConfigFromPayload(payload)
	if err = normalizeYggdrasilConfig(&loaded, s.cfg.Env); err != nil {
		return fallback, err
	}
	return loaded, nil
}

func (s *Server) redactedYggdrasilConfig() map[string]any {
	cfg, service := s.yggdrasilRuntimeSnapshot()
	trustedProxyCIDRs := append([]string{}, cfg.TrustedProxyCIDRs...)
	available := cfg.Enabled && service != nil && service.privateKey != nil && service.disabledReason == nil
	disabledReason := ""
	if cfg.Enabled && service != nil && service.disabledReason != nil {
		disabledReason = service.disabledReason.Error()
	}
	return map[string]any{
		"enabled":              cfg.Enabled,
		"publicBaseUrl":        cfg.PublicBaseURL,
		"textureBaseUrl":       cfg.TextureBaseURL,
		"serverName":           cfg.ServerName,
		"trustedProxyCidrs":    trustedProxyCIDRs,
		"tokenTtlHours":        int(cfg.TokenTTL / time.Hour),
		"maxTokens":            cfg.MaxTokens,
		"joinTtlSeconds":       int(cfg.JoinTTL / time.Second),
		"textureMaxBytes":      cfg.TextureMaxBytes,
		"hasPrivateKey":        service != nil && service.privateKey != nil,
		"persistentPrivateKey": strings.TrimSpace(cfg.PrivateKeyBase64) != "",
		"available":            available,
		"disabledReason":       disabledReason,
	}
}

func yggdrasilConfigFromPayload(payload yggdrasilSettingsPayload) config.YggdrasilConfig {
	return config.YggdrasilConfig{
		Enabled:           payload.Enabled,
		PublicBaseURL:     payload.PublicBaseURL,
		TextureBaseURL:    payload.TextureBaseURL,
		ServerName:        payload.ServerName,
		PrivateKeyBase64:  payload.PrivateKeyBase64,
		TrustedProxyCIDRs: payload.TrustedProxyCIDRs,
		TokenTTL:          time.Duration(payload.TokenTTLHours) * time.Hour,
		MaxTokens:         payload.MaxTokens,
		JoinTTL:           time.Duration(payload.JoinTTLSeconds) * time.Second,
		TextureMaxBytes:   payload.TextureMaxBytes,
	}
}

func yggdrasilPayloadFromConfig(cfg config.YggdrasilConfig) yggdrasilSettingsPayload {
	return yggdrasilSettingsPayload{
		Enabled:           cfg.Enabled,
		PublicBaseURL:     cfg.PublicBaseURL,
		TextureBaseURL:    cfg.TextureBaseURL,
		ServerName:        cfg.ServerName,
		PrivateKeyBase64:  cfg.PrivateKeyBase64,
		TrustedProxyCIDRs: cfg.TrustedProxyCIDRs,
		TokenTTLHours:     int(cfg.TokenTTL / time.Hour),
		MaxTokens:         cfg.MaxTokens,
		JoinTTLSeconds:    int(cfg.JoinTTL / time.Second),
		TextureMaxBytes:   cfg.TextureMaxBytes,
	}
}

func normalizeYggdrasilConfig(cfg *config.YggdrasilConfig, environment string) error {
	cfg.PublicBaseURL = normalizedYggdrasilBaseURL(cfg.PublicBaseURL)
	cfg.TextureBaseURL = normalizedYggdrasilBaseURL(cfg.TextureBaseURL)
	cfg.ServerName = strings.TrimSpace(cfg.ServerName)
	cfg.PrivateKeyBase64 = strings.TrimSpace(cfg.PrivateKeyBase64)
	if cfg.ServerName == "" || utf8.RuneCountInString(cfg.ServerName) > 80 {
		return errors.New("yggdrasil server name must contain 1 to 80 characters")
	}
	proxies, err := normalizeYggdrasilProxyCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return err
	}
	cfg.TrustedProxyCIDRs = proxies
	production := strings.EqualFold(strings.TrimSpace(environment), "production")
	if err = validateYggdrasilEndpoint(cfg.PublicBaseURL, "public API", production); err != nil {
		return err
	}
	if err = validateYggdrasilEndpoint(cfg.TextureBaseURL, "texture", production); err != nil {
		return err
	}
	if err = validateYggdrasilLimits(cfg.TokenTTL, cfg.JoinTTL, cfg.MaxTokens, cfg.TextureMaxBytes); err != nil {
		return err
	}
	if cfg.PrivateKeyBase64 != "" {
		if _, err = parseYggdrasilPrivateKey(cfg.PrivateKeyBase64); err != nil {
			return err
		}
	} else if cfg.Enabled && production {
		return errors.New("yggdrasil signing key is required in production")
	}
	return nil
}

func normalizeYggdrasilProxyCIDRs(values []string) ([]string, error) {
	unique := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, err := parseYggdrasilTrustedProxies([]string{value}); err != nil {
			return nil, err
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func generateYggdrasilPrivateKey() (string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	return encodeYggdrasilPrivateKey(key)
}

func encodeYggdrasilPrivateKey(key *rsa.PrivateKey) (string, error) {
	if key == nil {
		return "", errors.New("missing RSA private key")
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

func cloneYggdrasilConfig(cfg config.YggdrasilConfig) config.YggdrasilConfig {
	cfg.TrustedProxyCIDRs = append([]string(nil), cfg.TrustedProxyCIDRs...)
	return cfg
}

func (s *Server) yggdrasilRuntimeSnapshot() (config.YggdrasilConfig, *yggdrasilService) {
	s.yggMu.RLock()
	defer s.yggMu.RUnlock()
	return cloneYggdrasilConfig(s.cfg.Yggdrasil), s.ygg
}

func (s *Server) replaceYggdrasilRuntime(cfg config.YggdrasilConfig, service *yggdrasilService) {
	s.yggMu.Lock()
	defer s.yggMu.Unlock()
	s.cfg.Yggdrasil = cloneYggdrasilConfig(cfg)
	s.ygg = service
}
