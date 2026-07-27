package httpapi

import (
	"net/http"
	"strings"
	"time"
)

const authSessionCookieName = "mcmods_session"

func (s *Server) setAuthSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	ttl := s.cfg.JWTTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	http.SetCookie(w, s.httpOnlyCookie(r, authSessionCookieName, token, "/api/", int(ttl.Seconds()), time.Now().Add(ttl)))
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
