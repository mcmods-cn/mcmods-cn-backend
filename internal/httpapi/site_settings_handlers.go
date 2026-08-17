package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

const siteGeneralSettingKey = "site.general"

var publicSiteLogoPathPattern = regexp.MustCompile(`^/site-assets/site-logo-[a-f0-9]{20}\.(?:png|jpe?g|webp|gif)$`)

type siteGeneralConfig struct {
	SiteName string `json:"siteName"`
	LogoURL  string `json:"logoUrl"`
}

func defaultSiteGeneralConfig() siteGeneralConfig {
	return siteGeneralConfig{SiteName: "Mcmods-cn"}
}

func (s *Server) publicSiteGeneralConfig(w http.ResponseWriter, r *http.Request) {
	config := s.siteGeneralConfigFromSettings(r.Context())
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) siteLogoUploadAccess(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updateSiteGeneralConfig(w http.ResponseWriter, r *http.Request) {
	var payload siteGeneralConfig
	if decodeJSON(r, &payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid site settings")
		return
	}
	if err := normalizeSiteGeneralConfig(&payload); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid site settings")
		return
	}
	_, err = s.db.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at)
		values($1,$2::jsonb,$3,now()) on conflict(key) do update set value=excluded.value,
		updated_by=excluded.updated_by,updated_at=now()`, siteGeneralSettingKey, raw, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save site settings")
		return
	}
	s.invalidateSettingsCache(r.Context())
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) siteGeneralConfigFromSettings(ctx context.Context) siteGeneralConfig {
	config := defaultSiteGeneralConfig()
	var raw []byte
	raw, err := s.loadCachedPublicSetting(ctx, siteGeneralSettingKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return config
	}
	if err != nil || json.Unmarshal(raw, &config) != nil || normalizeSiteGeneralConfig(&config) != nil {
		return defaultSiteGeneralConfig()
	}
	return config
}

func normalizeSiteGeneralConfig(config *siteGeneralConfig) error {
	config.SiteName = strings.TrimSpace(config.SiteName)
	config.LogoURL = strings.TrimSpace(config.LogoURL)
	if config.SiteName == "" || len([]rune(config.SiteName)) > 80 {
		return errors.New("siteName must contain 1 to 80 characters")
	}
	if config.LogoURL == "" {
		return nil
	}
	if !publicSiteLogoPathPattern.MatchString(config.LogoURL) {
		return errors.New("logoUrl must reference a logo stored in the frontend public directory")
	}
	return nil
}
