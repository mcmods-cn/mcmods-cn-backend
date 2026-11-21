package httpapi

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"mcmods-cn-backend/internal/security"
)

const authSessionCookieName = "mcmods_session"

type issuedAuthSession struct {
	token  string // only the HttpOnly cookie carries this credential
	marker string
}

func browserSessionMarker(sessionID string) string {
	// Domain-separated from the database key and realtime/presence identity.
	return base64.RawURLEncoding.EncodeToString(security.SessionFingerprint("mcmods:logout:v1\x00" + sessionID))
}

func (s *Server) writeLoginResult(w http.ResponseWriter, r *http.Request, status int, session issuedAuthSession, user authenticatedUserResponse) {
	s.setAuthSessionCookie(w, r, session)
	writeJSON(w, status, map[string]any{"user": user, "sessionMarker": session.marker})
}

func (s *Server) setAuthSessionCookie(w http.ResponseWriter, r *http.Request, session issuedAuthSession) {
	w.Header().Set("Cache-Control", "private, no-store")
	ttl := s.cfg.JWTTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	http.SetCookie(w, s.httpOnlyCookie(r, authSessionCookieName, session.token, "/api/", int(ttl.Seconds()), time.Now().Add(ttl)))
}

func (s *Server) clearAuthSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, s.httpOnlyCookie(r, authSessionCookieName, "", "/api/", -1, time.Unix(1, 0)))
}

func (s *Server) httpOnlyCookie(r *http.Request, name, value, path string, maxAge int, expires time.Time) *http.Cookie {
	return &http.Cookie{ // #nosec G124 -- this constructor always sets HttpOnly, SameSite=Lax, and Secure for TLS/production requests.
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.secureRequest(r),
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) secureRequest(r *http.Request) bool {
	if strings.EqualFold(s.cfg.Env, "production") || r.TLS != nil {
		return true
	}
	peer := normalizeIPAddress(remoteIP(r.RemoteAddr))
	return networkContainsIP(s.trustedProxies, peer) &&
		strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
