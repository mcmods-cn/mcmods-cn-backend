package httpapi

import (
	"context"
	"fmt"

	"mcmods-cn-backend/internal/security"
	"mcmods-cn-backend/internal/systemactor"
)

// automationActor resolves the seeded service account through the same RBAC
// rules as an interactive user. Workers must never manufacture privileges or
// silently fall back to an administrator account.
func (s *Server) automationActor(ctx context.Context) (security.Claims, error) {
	var claims security.Claims
	if err := s.db.QueryRow(ctx, `select id,public_id,username,email,auth_version
		from users where username=$1 and lower(email)=lower($2) and status='active'`,
		systemactor.AutobotUsername, systemactor.AutobotEmail).Scan(
		&claims.Subject, &claims.PublicSubject, &claims.Username, &claims.Email, &claims.AuthVersion,
	); err != nil {
		return claims, fmt.Errorf("resolve %s automation account: %w", systemactor.AutobotUsername, err)
	}
	_, rules, err := s.resolveUserRootPermissionsUncached(ctx, claims.Subject)
	if err != nil {
		return claims, fmt.Errorf("resolve %s permissions: %w", systemactor.AutobotUsername, err)
	}
	claims.PermissionRules = rules
	return claims, nil
}
