package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"mcmods-cn-backend/internal/security"
)

const maxPermissionValue int32 = 2147483647

type contextKey string

const claimsContextKey contextKey = "claims"

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := security.BearerToken(r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		claims, err := security.ParseToken(s.cfg.JWTSecret, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication session has expired")
			return
		}
		markActivityUser(r, claims.Subject)
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) optionalAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			next.ServeHTTP(w, r)
			return
		}
		token, err := security.BearerToken(r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication session is invalid")
			return
		}
		claims, err := security.ParseToken(s.cfg.JWTSecret, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication session has expired")
			return
		}
		markActivityUser(r, claims.Subject)
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
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
