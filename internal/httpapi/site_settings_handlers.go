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

var publicSiteLogoPathPattern = regexp.MustCompile(`^/site-assets/site-logo-([a-z0-9]{9})\.png$`)

type siteGeneralConfig struct {
	SiteName string `json:"siteName"`
	LogoURL  string `json:"logoUrl"`
}

func defaultSiteGeneralConfig() siteGeneralConfig {
	return siteGeneralConfig{SiteName: "Mcmods-cn"}
}

func (s *Server) publicSiteGeneralConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.readSiteGeneralConfig(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "SITE_SETTINGS_UNAVAILABLE", "site settings are temporarily unavailable", 0, nil)
		return
	}
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
	err = s.saveSiteGeneralConfig(r.Context(), payload, raw, currentClaims(r).Subject)
	if err != nil {
		if errors.Is(err, errSiteLogoUnavailable) {
			writeAPIError(w, http.StatusUnprocessableEntity, "SITE_LOGO_UNAVAILABLE", "logo must reference an active safe shared site asset", 0, nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save site settings")
		return
	}
	s.invalidateSettingsCache(r.Context())
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) readSiteGeneralConfig(ctx context.Context) (siteGeneralConfig, error) {
	config := defaultSiteGeneralConfig()
	var raw []byte
	// An infrequently changed brand is one small authoritative read. A cached
	// old version on another replica must not point at a tombstoned old logo.
	err := s.db.QueryRow(ctx, `select value from system_settings where key=$1`, siteGeneralSettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		return config, err
	}
	// Retain the configured name, but never use an obsolete instance-local
	// filename or an arbitrary external URL as a shared asset authority.
	if config.LogoURL != "" && !publicSiteLogoPathPattern.MatchString(strings.TrimSpace(config.LogoURL)) {
		config.LogoURL = ""
	}
	if err = normalizeSiteGeneralConfig(&config); err != nil {
		return defaultSiteGeneralConfig(), err
	}
	return config, nil
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
		return errors.New("logoUrl must reference a versioned shared site logo")
	}
	return nil
}
