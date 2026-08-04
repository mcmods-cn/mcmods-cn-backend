package httpapi

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

const directUserPermissionPriority = 1_000_000_000

type permissionRole struct {
	Code        string
	Weight      int
	Parents     []string
	Permissions []domain.RolePermissionEntry
}

type permissionCandidate struct {
	security.PermissionRule
	Depth int
}

// resolveUserRootPermissions is the single source of truth for user
// authorization. It resolves active direct roles, inherited/template roles,
// rule priority and direct user allow/deny overrides.
func (s *Server) resolveUserRootPermissions(ctx context.Context, userID int64) ([]string, []security.PermissionRule, error) {
	rolesByCode, templates, err := s.loadPermissionRoles(ctx)
	if err != nil {
		return nil, nil, err
	}

	roleRows, err := s.db.Query(
		ctx,
		`select r.code
		 from user_role_bindings b
		 join roles r on r.id = b.role_id
		 where b.user_id = $1 and r.status = 'active'
		 order by r.code`,
		userID,
	)
	if err != nil {
		return nil, nil, err
	}
	assignedRoles := make([]string, 0)
	for roleRows.Next() {
		var code string
		if err := roleRows.Scan(&code); err != nil {
			roleRows.Close()
			return nil, nil, err
		}
		assignedRoles = append(assignedRoles, code)
	}
	if err := roleRows.Err(); err != nil {
		roleRows.Close()
		return nil, nil, err
	}
	roleRows.Close()

	candidates := make(map[string]permissionCandidate)
	applyPermissionRoles(candidates, assignedRoles, rolesByCode, templates)

	directRows, err := s.db.Query(
		ctx,
		`select p.code, up.allow
		 from user_permissions up
		 join permissions p on p.id = up.permission_id
		 where up.user_id = $1 and (up.expires_at is null or up.expires_at > now())
		 order by p.code`,
		userID,
	)
	if err != nil {
		return nil, nil, err
	}
	for directRows.Next() {
		var code string
		var allow bool
		if err := directRows.Scan(&code, &allow); err != nil {
			directRows.Close()
			return nil, nil, err
		}
		applyPermissionCandidate(candidates, permissionCandidate{
			PermissionRule: security.PermissionRule{Code: code, Allow: allow, Priority: directUserPermissionPriority, Source: "user"},
			Depth:          -1,
		})
	}
	if err := directRows.Err(); err != nil {
		directRows.Close()
		return nil, nil, err
	}
	directRows.Close()

	return assignedRoles, permissionRulesFromCandidates(candidates), nil
}

func applyPermissionRoles(candidates map[string]permissionCandidate, roleCodes []string, rolesByCode map[string]permissionRole, templates []permissionRole) {
	var applyRole func(string, int, map[string]bool)
	applyRole = func(roleCode string, depth int, path map[string]bool) {
		if path[roleCode] {
			return
		}
		role, variables, ok := resolvePermissionRole(roleCode, rolesByCode, templates)
		if !ok {
			return
		}
		nextPath := make(map[string]bool, len(path)+1)
		for code := range path {
			nextPath[code] = true
		}
		nextPath[roleCode] = true
		for _, entry := range role.Permissions {
			code := applyRoleVariables(entry.Code, variables)
			applyPermissionCandidate(candidates, permissionCandidate{
				PermissionRule: security.PermissionRule{Code: code, Allow: entry.Allow, Priority: role.Weight, Source: "group." + roleCode},
				Depth:          depth,
			})
		}
		for _, parent := range role.Parents {
			applyRole(applyRoleVariables(parent, variables), depth+1, nextPath)
		}
	}
	for _, roleCode := range roleCodes {
		applyRole(roleCode, 0, map[string]bool{})
	}
}

func permissionRulesFromCandidates(candidates map[string]permissionCandidate) []security.PermissionRule {
	permissions := make([]security.PermissionRule, 0, len(candidates))
	for _, candidate := range candidates {
		permissions = append(permissions, candidate.PermissionRule)
	}
	sort.Slice(permissions, func(i, j int) bool { return permissions[i].Code < permissions[j].Code })
	return permissions
}

func (s *Server) loadPermissionRoles(ctx context.Context) (map[string]permissionRole, []permissionRole, error) {
	rows, err := s.db.Query(ctx, `select code, weight, parents from roles where status = 'active'`)
	if err != nil {
		return nil, nil, err
	}
	roles := make(map[string]permissionRole)
	for rows.Next() {
		var role permissionRole
		if err := rows.Scan(&role.Code, &role.Weight, &role.Parents); err != nil {
			rows.Close()
			return nil, nil, err
		}
		role.Permissions = []domain.RolePermissionEntry{}
		roles[role.Code] = role
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	permissionRows, err := s.db.Query(
		ctx,
		`select r.code, p.code, rp.allow
		 from role_permissions rp
		 join roles r on r.id = rp.role_id
		 join permissions p on p.id = rp.permission_id
		 where r.status = 'active' and (rp.expires_at is null or rp.expires_at > now())
		 order by r.code, p.code`,
	)
	if err != nil {
		return nil, nil, err
	}
	for permissionRows.Next() {
		var roleCode string
		var entry domain.RolePermissionEntry
		if err := permissionRows.Scan(&roleCode, &entry.Code, &entry.Allow); err != nil {
			permissionRows.Close()
			return nil, nil, err
		}
		role := roles[roleCode]
		role.Permissions = append(role.Permissions, entry)
		roles[roleCode] = role
	}
	if err := permissionRows.Err(); err != nil {
		permissionRows.Close()
		return nil, nil, err
	}
	permissionRows.Close()

	templates := make([]permissionRole, 0)
	for _, role := range roles {
		if _, ok := templateRoleVariables(role.Code); ok {
			templates = append(templates, role)
		}
	}
	sort.Slice(templates, func(i, j int) bool {
		if len(templates[i].Code) == len(templates[j].Code) {
			return templates[i].Code < templates[j].Code
		}
		return len(templates[i].Code) > len(templates[j].Code)
	})
	return roles, templates, nil
}

func resolvePermissionRole(code string, roles map[string]permissionRole, templates []permissionRole) (permissionRole, map[string]string, bool) {
	if role, ok := roles[code]; ok {
		return role, nil, true
	}
	for _, template := range templates {
		if variables, ok := matchRoleTemplateCode(template.Code, code); ok {
			return template, variables, true
		}
	}
	return permissionRole{}, nil, false
}

func templateRoleVariables(code string) ([]string, bool) {
	variables := make([]string, 0)
	for _, segment := range strings.Split(code, ".") {
		if variable, ok := templateVariableName(segment); ok {
			variables = append(variables, variable)
		}
	}
	return variables, len(variables) > 0
}

func applyPermissionCandidate(target map[string]permissionCandidate, candidate permissionCandidate) {
	current, exists := target[candidate.Code]
	if !exists || permissionCandidateWins(candidate, current) {
		target[candidate.Code] = candidate
	}
}

func permissionCandidateWins(candidate permissionCandidate, current permissionCandidate) bool {
	if candidate.Priority != current.Priority {
		return candidate.Priority > current.Priority
	}
	if candidate.Depth != current.Depth {
		return candidate.Depth < current.Depth
	}
	if candidate.Allow != current.Allow {
		return !candidate.Allow
	}
	return candidate.Source < current.Source
}

func permissionRulesAllow(entries []security.PermissionRule, required string) bool {
	var selected *security.PermissionRule
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

func claimsAllow(claims security.Claims, required string) bool {
	return permissionRulesAllow(claims.PermissionRules, required)
}

func permissionRulesNumericValue(entries []security.PermissionRule, permissionPrefix string) int32 {
	if permissionRulesAllow(entries, "admin.*") || permissionRulesAllow(entries, strings.TrimSuffix(permissionPrefix, ".")+".*") {
		return maxPermissionValue
	}
	best := int32(0)
	prefix := strings.TrimSuffix(permissionPrefix, ".") + "."
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Code, prefix) || !permissionRulesAllow(entries, entry.Code) {
			continue
		}
		valuePart := strings.TrimPrefix(entry.Code, prefix)
		if valuePart == "*" || valuePart == "-1" {
			return maxPermissionValue
		}
		value, err := strconv.ParseInt(valuePart, 10, 32)
		if err == nil && int32(value) > best {
			best = int32(value)
		}
	}
	return best
}

func claimsNumericPermissionValue(claims security.Claims, permissionPrefix string) int32 {
	return permissionRulesNumericValue(claims.PermissionRules, permissionPrefix)
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
