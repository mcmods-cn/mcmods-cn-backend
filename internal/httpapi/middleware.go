package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/database"
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
			log.Print(database.RedactedError("protected authentication lookup", err))
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
			log.Print(database.RedactedError("optional authentication lookup", err))
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
	// one. Do not mutate its session cookie: this response can arrive after a
	// newer login, and browsers apply Set-Cookie before a client generation guard
	// can reject the old response. Explicit logout owns cookie deletion.
	w.Header().Set(authStateHeader, authStateInvalid)
	next.ServeHTTP(w, r)
}

func (s *Server) resolveClaimsSubject(ctx context.Context, claims *security.Claims) error {
	var versions permissionVersions
	userID, err := s.resolveSessionSubject(ctx, *claims, &versions)
	if err != nil {
		return err
	}
	_, rules, err := s.resolveUserRootPermissionsAtVersion(ctx, userID, versions)
	if err != nil {
		return err
	}
	claims.Subject = userID
	claims.PermissionRules = rules
	claims.PermissionVersion = versions.User
	claims.RBACVersion = versions.RBAC
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
	if s.db == nil {
		return
	}
	_ = registerDeclaredPermissionWithQueryer(context.Background(), s.db, permission)
}

func currentClaims(r *http.Request) security.Claims {
	claims, _ := r.Context().Value(claimsContextKey).(security.Claims)
	return claims
}

func (s *Server) userHasPermission(ctx context.Context, userID int64, permission string) bool {
	_, permissions, err := s.resolveUserRootPermissions(ctx, userID)
	return err == nil && permissionRulesAllow(permissions, permission)
}
