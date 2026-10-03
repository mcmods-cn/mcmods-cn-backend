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

const (
	authStateHeader         = "X-MCMods-Auth-State"
	permissionVersionHeader = "X-MCMods-Permission-Version"
	rbacVersionHeader       = "X-MCMods-RBAC-Version"
	authStateInvalid        = "invalid"
)

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
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusUnauthorized, "authentication account no longer exists")
				return
			}
			log.Printf("protected authentication lookup failed: %v", err)
			writeError(w, http.StatusServiceUnavailable, "authentication service is temporarily unavailable")
			return
		}
		w.Header().Set(permissionVersionHeader, strconv.FormatInt(claims.PermissionVersion, 10))
		w.Header().Set(rbacVersionHeader, strconv.FormatInt(claims.RBACVersion, 10))
		markActivityUser(r, claims.Subject)
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		s.serveProtectedMutation(w, r.WithContext(ctx), next)
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
		w.Header().Set(permissionVersionHeader, strconv.FormatInt(claims.PermissionVersion, 10))
		w.Header().Set(rbacVersionHeader, strconv.FormatInt(claims.RBACVersion, 10))
		markActivityUser(r, claims.Subject)
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		s.serveProtectedMutation(w, r.WithContext(ctx), next)
	}
}

func (s *Server) continueOptionalAuthAsGuest(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	// Optional authentication must never turn a public endpoint into a protected
	// one. Expire an invalid cookie so subsequent public requests do not repeat
	// the lookup; an explicit Authorization header is owned by the API client.
	w.Header().Set(authStateHeader, authStateInvalid)
	if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		if _, err := r.Cookie(authSessionCookieName); err == nil {
			s.clearAuthSessionCookie(w, r)
		}
	}
	next.ServeHTTP(w, r)
}

func (s *Server) resolveClaimsSubject(ctx context.Context, claims *security.Claims) error {
	record, err := s.resolveCachedSessionSubject(ctx, *claims)
	if err != nil {
		return err
	}
	return s.resolveClaimsAuthorization(ctx, claims, record.UserID)
}

func (s *Server) resolveClaimsAuthorization(ctx context.Context, claims *security.Claims, userID int64) error {
	claims.Subject = userID
	permissionVersion, err := s.loadPermissionVersion(ctx, claims.Subject)
	if err != nil {
		return err
	}
	rbacVersion, err := s.loadRBACVersion(ctx)
	if err != nil {
		return err
	}
	_, permissionRules, err := s.resolveUserRootPermissionsAtVersion(ctx, claims.Subject, permissionVersion, rbacVersion)
	if err != nil {
		return err
	}
	claims.PermissionRules = permissionRules
	claims.PermissionVersion = permissionVersion
	claims.RBACVersion = rbacVersion
	return nil
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
		if !claimsAllow(claims, permission) {
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

func (s *Server) userHasPermission(ctx context.Context, userID int64, permission string) bool {
	_, permissions, err := s.resolveUserRootPermissions(ctx, userID)
	return err == nil && permissionRulesAllow(permissions, permission)
}
