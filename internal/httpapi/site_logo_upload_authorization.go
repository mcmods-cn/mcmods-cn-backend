package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

const siteLogoCredentialLifetime = 60 * time.Second
const siteLogoCredentialPurpose = "site-logo-upload-v1"
const siteLogoCredentialScheme = "SiteLogo "

type siteLogoCredential struct {
	Purpose string          `json:"purpose"`
	Claims  security.Claims `json:"claims"`
}

// This short delegation is opaque, purpose-bound and never accepted by the
// general JWT middleware. It transports authorization across split hosts
// without returning the HttpOnly session JWT to browser JavaScript.
func (s *Server) siteLogoCredentialKey() string {
	return s.cfg.JWTSecret + "/" + siteLogoCredentialPurpose
}

func (s *Server) issueSiteLogoUploadAuthorization(w http.ResponseWriter, r *http.Request) {
	parent := currentClaims(r)
	now := time.Now().Unix()
	claims := security.Claims{
		PublicSubject: parent.PublicSubject, SessionID: parent.SessionID, AuthVersion: parent.AuthVersion,
		IssuedAt: now, ExpiresAt: min(parent.ExpiresAt, now+int64(siteLogoCredentialLifetime/time.Second)),
	}
	raw, err := json.Marshal(siteLogoCredential{Purpose: siteLogoCredentialPurpose, Claims: claims})
	if err == nil {
		raw, err = security.EncryptSetting(s.siteLogoCredentialKey(), raw)
	}
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "SITE_LOGO_AUTH_UNAVAILABLE", "logo upload authorization is unavailable", 0, nil)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"authorization": siteLogoCredentialScheme + base64.RawURLEncoding.EncodeToString(raw),
		"expiresAt":     claims.ExpiresAt,
	})
}

func (s *Server) parseSiteLogoUploadAuthorization(header string) (security.Claims, error) {
	var credential siteLogoCredential
	if !strings.HasPrefix(header, siteLogoCredentialScheme) || len(header) > 4096 {
		return credential.Claims, errors.New("invalid logo upload authorization")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(header, siteLogoCredentialScheme))
	if err == nil {
		raw, err = security.DecryptSetting(s.siteLogoCredentialKey(), raw)
	}
	if err == nil {
		err = json.Unmarshal(raw, &credential)
	}
	if err != nil {
		return security.Claims{}, err
	}
	claims, now := credential.Claims, time.Now().Unix()
	if credential.Purpose != siteLogoCredentialPurpose || !validCatalogPublicID(claims.PublicSubject) || claims.SessionID == "" || claims.AuthVersion <= 0 ||
		claims.IssuedAt <= 0 || claims.IssuedAt > now || claims.ExpiresAt <= now || claims.ExpiresAt <= claims.IssuedAt || claims.ExpiresAt-claims.IssuedAt > int64(siteLogoCredentialLifetime/time.Second) {
		return security.Claims{}, errors.New("expired or invalid logo upload authorization")
	}
	return claims, nil
}

func (s *Server) requireSiteLogoUploadPermission(next http.HandlerFunc) http.HandlerFunc {
	ordinary := s.requirePermission("admin.config.write", next)
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), siteLogoCredentialScheme) {
			ordinary(w, r)
			return
		}
		claims, err := s.parseSiteLogoUploadAuthorization(r.Header.Get("Authorization"))
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "SITE_LOGO_AUTH_INVALID", "logo upload authorization is invalid or expired", 0, nil)
			return
		}
		// A short cross-host delegation checks the parent session directly on
		// every use, so an out-of-process revocation cannot survive a warm TTL.
		record, err := s.loadSessionSubject(r.Context(), claims)
		if err == nil {
			err = s.resolveClaimsAuthorization(r.Context(), &claims, record.UserID)
		}
		if err != nil {
			status, code := http.StatusServiceUnavailable, "SITE_LOGO_AUTH_UNAVAILABLE"
			if errors.Is(err, pgx.ErrNoRows) {
				status, code = http.StatusUnauthorized, "SITE_LOGO_AUTH_INVALID"
			}
			writeAPIError(w, status, code, "logo upload session is unavailable", 0, nil)
			return
		}
		if !claimsAllow(claims, "admin.config.write") {
			writeAPIError(w, http.StatusForbidden, "SITE_LOGO_AUTH_DENIED", "logo upload permission is required", 0, nil)
			return
		}
		w.Header().Set(permissionVersionHeader, strconv.FormatInt(claims.PermissionVersion, 10))
		w.Header().Set(rbacVersionHeader, strconv.FormatInt(claims.RBACVersion, 10))
		markActivityUser(r, claims.Subject)
		s.serveProtectedMutation(w, r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims)), next)
	}
}
