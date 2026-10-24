package httpapi

import (
	"context"
	"errors"
	"fmt"

	"mcmods-cn-backend/internal/security"
	"mcmods-cn-backend/internal/systemactor"
)

var errAutomationPermissionDenied = errors.New("automation actor no longer has the required project permission")

// Workers resolve current claims again at each persistence boundary. The
// transaction variant uses its existing connection, including in a one-slot
// pool, and never manufactures an administrative fallback.
func authorizeAutomationProject(ctx context.Context, query permissionQuery, expectedActorID int64, projectID string, create, publish bool) (security.Claims, error) {
	actor, err := automationActorQuery(ctx, query)
	if err != nil {
		return actor, err
	}
	if expectedActorID > 0 && actor.Subject != expectedActorID {
		return actor, errAutomationPermissionDenied
	}
	if create {
		if !claimsAllow(actor, "project.create") {
			return actor, errAutomationPermissionDenied
		}
	} else if !automationScopedPermission(actor, "project.edit", projectID) {
		return actor, errAutomationPermissionDenied
	}
	if publish && !automationScopedPermission(actor, "project.no-review", projectID) && !claimsAllow(actor, "content.no-review") {
		return actor, errAutomationPermissionDenied
	}
	return actor, nil
}

func automationScopedPermission(actor security.Claims, permission, projectID string) bool {
	if projectID == "" {
		return claimsAllow(actor, permission)
	}
	// Fold the global grant into the exact project's decision, so an explicit
	// scoped deny cannot be bypassed by checking the global permission first.
	rules := append([]security.PermissionRule(nil), actor.PermissionRules...)
	for index := range rules {
		if rules[index].Code == permission {
			rules[index].Code += ".*"
		}
	}
	return permissionRulesAllow(rules, permission+"."+projectID)
}

// automationActor resolves the seeded service account through the same RBAC
// rules as an interactive user. Workers must never manufacture privileges or
// silently fall back to an administrator account.
func (s *Server) automationActor(ctx context.Context) (security.Claims, error) {
	return automationActorQuery(ctx, s.db)
}

func automationActorQuery(ctx context.Context, query permissionQuery) (security.Claims, error) {
	var claims security.Claims
	if err := query.QueryRow(ctx, `select id,public_id,username,email,auth_version
		from users where username=$1 and lower(email)=lower($2) and status=$3`,
		systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus).Scan(
		&claims.Subject, &claims.PublicSubject, &claims.Username, &claims.Email, &claims.AuthVersion,
	); err != nil {
		return claims, fmt.Errorf("resolve %s automation account: %w", systemactor.AutobotUsername, err)
	}
	resolved, err := resolveUsersRootPermissionsQuery(ctx, query, []int64{claims.Subject})
	if err != nil {
		return claims, fmt.Errorf("resolve %s permissions: %w", systemactor.AutobotUsername, err)
	}
	claims.PermissionRules = resolved[claims.Subject].Permissions
	if claimsExplicitlyAllow(claims, "account.banned") {
		return security.Claims{}, errAutomationPermissionDenied
	}
	return claims, nil
}
