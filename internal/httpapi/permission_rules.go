package httpapi

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

const directUserPermissionPriority = 1_000_000_000

type permissionQuery interface {
	databaseQuery
	catalogResourceResolverQuery
}

type permissionRole struct {
	Code             string
	Weight           int
	Parents          []string
	Permissions      []domain.RolePermissionEntry
	hasExpiringRules bool
}

type permissionCandidate struct {
	security.PermissionRule
	Depth int
}

type resolvedUserRootPermissions struct {
	Roles               []string
	Permissions         []security.PermissionRule
	hasExpiringBindings bool
}

// resolveUserRootPermissions is the single source of truth for user
// authorization. It resolves active direct roles, inherited/template roles,
// rule priority and direct user allow/deny overrides.
func (s *Server) resolveUserRootPermissions(ctx context.Context, userID int64) ([]string, []security.PermissionRule, error) {
	if !s.cache.Config().RBACCacheEnabled {
		return s.resolveUserRootPermissionsUncached(ctx, userID)
	}
	versions, err := s.permissionVersions(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return s.resolveUserRootPermissionsAtVersion(ctx, userID, versions)
}

func (s *Server) resolveUserRootPermissionsAtVersion(ctx context.Context, userID int64, versions permissionVersions) ([]string, []security.PermissionRule, error) {
	if !s.cache.Config().RBACCacheEnabled {
		return s.resolveUserRootPermissionsUncached(ctx, userID)
	}
	key := rbacCacheKey(versions.RBAC, versions.ProjectACL, userID, versions.User)
	raw, err := s.cache.GetOrLoadCacheableTTL(ctx, key, s.cache.Config().RBACCacheTTL, func(loadCtx context.Context) ([]byte, bool, error) {
		users, loadErr := s.resolveUsersRootPermissions(loadCtx, []int64{userID})
		if loadErr != nil {
			return nil, false, loadErr
		}
		user := users[userID]
		raw, err := json.Marshal(user)
		return raw, !user.hasExpiringBindings, err
	})
	if err != nil {
		return nil, nil, err
	}
	var resolved resolvedUserRootPermissions
	if err = json.Unmarshal(raw, &resolved); err != nil {
		s.cache.Delete(ctx, key)
		return nil, nil, err
	}
	return resolved.Roles, resolved.Permissions, nil
}

func (s *Server) resolveUserRootPermissionsUncached(ctx context.Context, userID int64) ([]string, []security.PermissionRule, error) {
	resolved, err := s.resolveUsersRootPermissions(ctx, []int64{userID})
	if err != nil {
		return nil, nil, err
	}
	user := resolved[userID]
	return user.Roles, user.Permissions, nil
}

// resolveUsersRootPermissions resolves a set of users with the same priority,
// inheritance and deny semantics as the single-user authorization path. It is
// used when a response needs permission-derived metadata for several authors.
func (s *Server) resolveUsersRootPermissions(ctx context.Context, userIDs []int64) (map[int64]resolvedUserRootPermissions, error) {
	return resolveUsersRootPermissionsQuery(ctx, s.db, userIDs)
}

func resolveUsersRootPermissionsQuery(ctx context.Context, query permissionQuery, userIDs []int64) (map[int64]resolvedUserRootPermissions, error) {
	result := make(map[int64]resolvedUserRootPermissions, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	rolesByCode, templates, err := loadPermissionRolesQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	roleRows, err := query.Query(
		ctx,
		`select b.user_id, r.code, bool_and(b.expires_at is not null)
		 from user_role_bindings b
		 join roles r on r.id = b.role_id
		 where b.user_id = any($1) and r.status = 'active'
		   and (b.expires_at is null or b.expires_at > statement_timestamp())
		 group by b.user_id, r.code
		 order by b.user_id, r.code`,
		userIDs,
	)
	if err != nil {
		return nil, err
	}
	assignedRoles := make(map[int64][]string, len(userIDs))
	hasExpiry := make(map[int64]bool, len(userIDs))
	for roleRows.Next() {
		var userID int64
		var code string
		var temporary bool
		if err := roleRows.Scan(&userID, &code, &temporary); err != nil {
			roleRows.Close()
			return nil, err
		}
		assignedRoles[userID] = append(assignedRoles[userID], code)
		hasExpiry[userID] = hasExpiry[userID] || temporary
	}
	if err := roleRows.Err(); err != nil {
		roleRows.Close()
		return nil, err
	}
	roleRows.Close()

	accessRows, err := query.Query(ctx, `select distinct user_id,access_level,project_public_id
		from effective_project_access where user_id=any($1)
		order by user_id,access_level,project_public_id`, userIDs)
	if err != nil {
		return nil, err
	}
	for accessRows.Next() {
		var userID int64
		var level, projectID string
		if err = accessRows.Scan(&userID, &level, &projectID); err != nil {
			accessRows.Close()
			return nil, err
		}
		rolePrefix := "project_editor."
		if level == "developer" {
			rolePrefix = "project_developer."
		}
		assignedRoles[userID] = append(assignedRoles[userID], rolePrefix+projectID)
	}
	if err = accessRows.Err(); err != nil {
		accessRows.Close()
		return nil, err
	}
	accessRows.Close()

	candidatesByUser := make(map[int64]map[string]permissionCandidate, len(userIDs))
	for _, userID := range userIDs {
		candidates := make(map[string]permissionCandidate)
		roleExpiry := applyPermissionRoles(candidates, assignedRoles[userID], rolesByCode, templates)
		hasExpiry[userID] = hasExpiry[userID] || roleExpiry
		candidatesByUser[userID] = candidates
	}

	claimRows, err := query.Query(ctx, `select distinct claim.user_id,creator.public_id
		from creator_claims claim join creators creator on creator.id=claim.creator_id
		where claim.user_id=any($1) and claim.status='approved' and creator.kind='author'`, userIDs)
	if err != nil {
		return nil, err
	}
	for claimRows.Next() {
		var userID int64
		var creatorID string
		if err = claimRows.Scan(&userID, &creatorID); err != nil {
			claimRows.Close()
			return nil, err
		}
		applyPermissionCandidate(candidatesByUser[userID], permissionCandidate{
			PermissionRule: security.PermissionRule{Code: "creator.edit." + creatorID, Allow: true, Priority: 100, Source: "author_claim"},
		})
	}
	if err = claimRows.Err(); err != nil {
		claimRows.Close()
		return nil, err
	}
	claimRows.Close()

	directRows, err := query.Query(
		ctx,
		`select up.user_id,p.code,up.allow,up.source,up.expires_at is not null
		 from user_permissions up
		 join permissions p on p.id = up.permission_id
		 where up.user_id = any($1) and (up.expires_at is null or up.expires_at > statement_timestamp())
		 order by up.user_id, p.code`,
		userIDs,
	)
	if err != nil {
		return nil, err
	}
	for directRows.Next() {
		var userID int64
		var code, source string
		var allow, temporary bool
		if err := directRows.Scan(&userID, &code, &allow, &source, &temporary); err != nil {
			directRows.Close()
			return nil, err
		}
		hasExpiry[userID] = hasExpiry[userID] || temporary
		applyPermissionCandidate(candidatesByUser[userID], permissionCandidate{
			PermissionRule: security.PermissionRule{Code: code, Allow: allow, Priority: directUserPermissionPriority, Source: "user:" + source},
			Depth:          -1,
		})
	}
	if err := directRows.Err(); err != nil {
		directRows.Close()
		return nil, err
	}
	directRows.Close()

	for _, userID := range userIDs {
		roles := assignedRoles[userID]
		if roles == nil {
			roles = []string{}
		}
		result[userID] = resolvedUserRootPermissions{
			Roles:               roles,
			Permissions:         permissionRulesFromCandidates(candidatesByUser[userID]),
			hasExpiringBindings: hasExpiry[userID],
		}
	}
	return result, nil
}

func applyPermissionRoles(candidates map[string]permissionCandidate, roleCodes []string, rolesByCode map[string]permissionRole, templates []permissionRole) (hasExpiringRules bool) {
	// Each concrete role has the same rules and parents whichever path reached it.
	// From all assigned roots, the first visit is the shortest inheritance path.
	type pendingRole struct {
		code  string
		depth int
	}
	seen := make(map[string]bool, len(roleCodes))
	pending := make([]pendingRole, 0, len(roleCodes))
	add := func(code string, depth int) {
		if !seen[code] {
			seen[code] = true
			pending = append(pending, pendingRole{code, depth})
		}
	}
	for _, code := range roleCodes {
		add(code, 0)
	}
	for i := 0; i < len(pending); i++ {
		current := pending[i]
		role, variables, ok := resolvePermissionRole(current.code, rolesByCode, templates)
		if !ok {
			continue
		}
		hasExpiringRules = hasExpiringRules || role.hasExpiringRules
		for _, entry := range role.Permissions {
			code := applyRoleVariables(entry.Code, variables)
			applyPermissionCandidate(candidates, permissionCandidate{
				PermissionRule: security.PermissionRule{Code: code, Allow: entry.Allow, Priority: role.Weight, Source: "group." + current.code},
				Depth:          current.depth,
			})
		}
		for _, parent := range role.Parents {
			add(applyRoleVariables(parent, variables), current.depth+1)
		}
	}
	return hasExpiringRules
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
	return loadPermissionRolesQuery(ctx, s.db)
}

func loadPermissionRolesQuery(ctx context.Context, query permissionQuery) (map[string]permissionRole, []permissionRole, error) {
	rows, err := query.Query(ctx, `select code, weight, parents from roles where status = 'active'`)
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

	permissionRows, err := query.Query(
		ctx,
		`select r.code, p.code, rp.allow, rp.expires_at is not null
		 from role_permissions rp
		 join roles r on r.id = rp.role_id
		 join permissions p on p.id = rp.permission_id
		 where r.status = 'active' and (rp.expires_at is null or rp.expires_at > statement_timestamp())
		 order by r.code, p.code`,
	)
	if err != nil {
		return nil, nil, err
	}
	for permissionRows.Next() {
		var roleCode string
		var entry domain.RolePermissionEntry
		var temporary bool
		if err := permissionRows.Scan(&roleCode, &entry.Code, &entry.Allow, &temporary); err != nil {
			permissionRows.Close()
			return nil, nil, err
		}
		role := roles[roleCode]
		role.Permissions = append(role.Permissions, entry)
		role.hasExpiringRules = role.hasExpiringRules || temporary
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

func selectedPermissionRule(entries []security.PermissionRule, required string) *security.PermissionRule {
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
	return selected
}

func permissionRulesAllow(entries []security.PermissionRule, required string) bool {
	rule := selectedPermissionRule(entries, required)
	return rule != nil && rule.Allow
}

func claimsAllow(claims security.Claims, required string) bool {
	return permissionRulesAllow(claims.PermissionRules, required)
}

// claimsExplicitlyAllow is reserved for state-marker permissions whose
// presence carries meaning by itself. Administrative wildcards must not imply
// markers such as account.banned.
func claimsExplicitlyAllow(claims security.Claims, required string) bool {
	exact := make([]security.PermissionRule, 0, 1)
	for _, rule := range claims.PermissionRules {
		if rule.Code == required {
			exact = append(exact, rule)
		}
	}
	return permissionRulesAllow(exact, required)
}

func permissionRulesNumericValue(entries []security.PermissionRule, permissionPrefix string) int32 {
	permissionPrefix = strings.TrimSuffix(permissionPrefix, ".")
	prefix := permissionPrefix + "."
	// An inherited wildcard supplies the default limit, subject to the same
	// priority and specificity as every concrete permission.
	unlimited := selectedPermissionRule(entries, prefix+"*")
	if unlimited != nil && unlimited.Allow {
		node := selectedPermissionRule(entries, permissionPrefix)
		if node == nil || node.Allow || unlimited.Code == prefix+"*" {
			return maxPermissionValue
		}
	}
	best := int32(0)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Code, prefix) || !permissionRulesAllow(entries, entry.Code) {
			continue
		}
		valuePart := strings.TrimPrefix(entry.Code, prefix)
		if valuePart == "-1" {
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
