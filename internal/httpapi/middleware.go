package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

const maxPermissionValue int32 = 2147483647

type contextKey string

const claimsContextKey contextKey = "claims"

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := authTokenFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		claims, err := security.ParseToken(s.cfg.JWTSecret, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication session has expired")
			return
		}
		if err := s.resolveClaimsSubject(r.Context(), &claims); err != nil {
			writeError(w, http.StatusUnauthorized, "authentication account no longer exists")
			return
		}
		markActivityUser(r, claims.Subject)
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) optionalAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requestHasAuthToken(r) {
			next.ServeHTTP(w, r)
			return
		}
		token, err := authTokenFromRequest(r)
		if err != nil {
			s.continueOptionalAuthAsGuest(w, r, next)
			return
		}
		claims, err := security.ParseToken(s.cfg.JWTSecret, token)
		if err != nil {
			s.continueOptionalAuthAsGuest(w, r, next)
			return
		}
		if err := s.resolveClaimsSubject(r.Context(), &claims); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				s.continueOptionalAuthAsGuest(w, r, next)
				return
			}
			log.Printf("optional authentication lookup failed: %v", err)
			writeError(w, http.StatusServiceUnavailable, "authentication service is temporarily unavailable")
			return
		}
		markActivityUser(r, claims.Subject)
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) continueOptionalAuthAsGuest(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	// Optional authentication must never turn a public endpoint into a protected
	// one. Expire an invalid cookie so subsequent public requests do not repeat
	// the lookup; an explicit Authorization header is owned by the API client.
	if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		if _, err := r.Cookie(authSessionCookieName); err == nil {
			s.clearAuthSessionCookie(w, r)
		}
	}
	next.ServeHTTP(w, r)
}

func (s *Server) resolveClaimsSubject(ctx context.Context, claims *security.Claims) error {
	return s.db.QueryRow(
		ctx,
		`select users.id
		 from auth_sessions session
		 join users on users.id=session.user_id
		 where session.session_hash=$1
		   and session.revoked_at is null
		   and session.expires_at>now()
		   and session.auth_version=$2
		   and users.auth_version=$2
		   and users.public_id=$3
		   and users.status='active'`,
		security.SessionFingerprint(claims.SessionID),
		claims.AuthVersion,
		claims.PublicSubject,
	).Scan(&claims.Subject)
}

func authTokenFromRequest(r *http.Request) (string, error) {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return security.BearerToken(r.Header.Get("Authorization"))
	}
	cookie, err := r.Cookie(authSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return security.BearerToken("")
	}
	return strings.TrimSpace(cookie.Value), nil
}

func requestHasAuthToken(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return true
	}
	cookie, err := r.Cookie(authSessionCookieName)
	return err == nil && strings.TrimSpace(cookie.Value) != ""
}

func (s *Server) requirePermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	s.registerDeclaredPermission(permission)
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		claims := currentClaims(r)
		if !hasPermission(claims.Permissions, permission) {
			writeError(w, http.StatusForbidden, "permission denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) registerDeclaredPermission(permission string) {
	permission = normalizeCode(permission)
	if permission == "" || s.db == nil {
		return
	}
	module := permission
	if index := strings.Index(permission, "."); index > 0 {
		module = permission[:index]
	}
	_, _ = s.db.Exec(
		context.Background(),
		`insert into permissions (code, module, name, description)
		 values ($1, $2, $1, 'Permission declared by application code')
		 on conflict (code) do nothing`,
		permission,
		module,
	)
}

func currentClaims(r *http.Request) security.Claims {
	claims, _ := r.Context().Value(claimsContextKey).(security.Claims)
	return claims
}

func hasPermission(grants []string, required string) bool {
	for _, grant := range grants {
		if grant == "*" || grant == "admin.*" {
			return true
		}
		if grant == required {
			return true
		}
		if strings.HasSuffix(grant, ".*") {
			prefix := strings.TrimSuffix(grant, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}
	}
	return false
}

func numericPermissionValue(grants []string, permissionPrefix string) int32 {
	if hasPermission(grants, "admin.*") {
		return maxPermissionValue
	}
	if hasPermission(grants, permissionPrefix+".*") {
		return maxPermissionValue
	}
	best := int32(0)
	prefix := strings.TrimSuffix(permissionPrefix, ".") + "."
	for _, grant := range grants {
		if !strings.HasPrefix(grant, prefix) {
			continue
		}
		valuePart := strings.TrimPrefix(grant, prefix)
		if valuePart == "*" || valuePart == "-1" {
			return maxPermissionValue
		}
		value, err := strconv.ParseInt(valuePart, 10, 32)
		if err != nil {
			continue
		}
		if int32(value) > best {
			best = int32(value)
		}
	}
	return best
}

func (s *Server) userHasPermission(ctx context.Context, userID int64, permission string) bool {
	if _, permissions, err := s.resolveUserRootPermissions(ctx, userID); err == nil {
		return resolvedPermissionAllows(permissions, permission)
	}
	_, grants := s.userGrants(ctx, userID)
	return hasPermission(grants, permission)
}

func resolvedPermissionAllows(entries []effectivePermission, required string) bool {
	var selected *effectivePermission
	selectedSpecificity := -1
	for index := range entries {
		entry := &entries[index]
		matches, specificity := permissionEntryMatches(entry.Code, required)
		if !matches {
			continue
		}
		if selected == nil || entry.Priority > selected.Priority ||
			(entry.Priority == selected.Priority && specificity > selectedSpecificity) ||
			(entry.Priority == selected.Priority && specificity == selectedSpecificity && !entry.Allow && selected.Allow) {
			selected = entry
			selectedSpecificity = specificity
		}
	}
	return selected != nil && selected.Allow
}

func permissionEntryMatches(grant string, required string) (bool, int) {
	if grant == "*" || grant == "admin.*" {
		return true, 0
	}
	if grant == required {
		return true, len(grant) + 10000
	}
	if strings.HasSuffix(grant, ".*") {
		prefix := strings.TrimSuffix(grant, "*")
		if strings.HasPrefix(required, prefix) {
			return true, len(prefix)
		}
	}
	return false, -1
}
