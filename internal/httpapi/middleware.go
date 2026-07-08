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
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		claims, err := security.ParseToken(s.cfg.JWTSecret, token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "登录状态已失效")
			return
		}
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) requirePermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	s.registerDeclaredPermission(permission)
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		claims := currentClaims(r)
		if !hasPermission(claims.Permissions, permission) {
			writeError(w, http.StatusForbidden, "权限不足")
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
		 values ($1, $2, $1, '代码声明的权限节点')
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
