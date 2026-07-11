package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/domain"
)

const directUserPermissionPriority = 1_000_000_000

type adminUserDetailsResponse struct {
	User                    domain.User            `json:"user"`
	Country                 string                 `json:"country"`
	Timezone                string                 `json:"timezone"`
	PrimaryLanguage         string                 `json:"primaryLanguage"`
	SecondaryLanguage       string                 `json:"secondaryLanguage"`
	RegistrationIP          string                 `json:"registrationIp"`
	RegistrationCountryCode string                 `json:"registrationCountryCode"`
	RegistrationCity        string                 `json:"registrationCity"`
	OAuthProviders          []string               `json:"oauthProviders"`
	LastLogin               *adminUserLoginDetails `json:"lastLogin,omitempty"`
	RootPermissions         []effectivePermission  `json:"rootPermissions"`
}

type adminUserLoginDetails struct {
	At          string `json:"at"`
	IP          string `json:"ip"`
	CountryCode string `json:"countryCode"`
	City        string `json:"city"`
	UserAgent   string `json:"userAgent"`
}

type effectivePermission struct {
	Code     string `json:"code"`
	Allow    bool   `json:"allow"`
	Priority int    `json:"priority"`
	Source   string `json:"source"`
}

type permissionRole struct {
	Code        string
	Weight      int
	Parents     []string
	Permissions []domain.RolePermissionEntry
}

type permissionCandidate struct {
	effectivePermission
	Depth int
}

func (s *Server) adminUserDetails(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return
	}

	details, err := s.loadAdminUserDetails(r.Context(), userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取用户详情失败")
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *Server) loadAdminUserDetails(ctx context.Context, userID int64) (adminUserDetailsResponse, error) {
	var result adminUserDetailsResponse
	result.OAuthProviders = []string{}
	err := s.db.QueryRow(
		ctx,
		`select id, username, email, display_name, email_verified, status, created_at, last_login_at,
		        country, timezone, preferred_content_language, preferred_ui_language,
		        registration_ip, registration_country_code, registration_city
		 from users where id = $1`,
		userID,
	).Scan(
		&result.User.ID,
		&result.User.Username,
		&result.User.Email,
		&result.User.DisplayName,
		&result.User.EmailVerified,
		&result.User.Status,
		&result.User.CreatedAt,
		&result.User.LastLoginAt,
		&result.Country,
		&result.Timezone,
		&result.PrimaryLanguage,
		&result.SecondaryLanguage,
		&result.RegistrationIP,
		&result.RegistrationCountryCode,
		&result.RegistrationCity,
	)
	if err != nil {
		return result, err
	}

	roles, permissions, err := s.resolveUserRootPermissions(ctx, userID)
	if err != nil {
		return result, err
	}
	result.User.Roles = roles
	result.RootPermissions = permissions
	grants := make([]string, 0, len(roles)+len(permissions))
	for _, role := range roles {
		grants = append(grants, "group."+role)
	}
	for _, permission := range permissions {
		if permission.Allow {
			grants = append(grants, permission.Code)
		}
	}
	result.User.Permissions = normalizeCodes(grants)

	providerRows, err := s.db.Query(ctx, `select provider from oauth_accounts where user_id = $1 order by provider`, userID)
	if err != nil {
		return result, err
	}
	for providerRows.Next() {
		var provider string
		if err := providerRows.Scan(&provider); err != nil {
			providerRows.Close()
			return result, err
		}
		result.OAuthProviders = append(result.OAuthProviders, provider)
	}
	if err := providerRows.Err(); err != nil {
		providerRows.Close()
		return result, err
	}
	providerRows.Close()

	var login adminUserLoginDetails
	var loginAt time.Time
	err = s.db.QueryRow(
		ctx,
		`select created_at, ip, country_code, city, user_agent
		 from user_login_logs
		 where user_id = $1 and success = true
		 order by created_at desc
		 limit 1`,
		userID,
	).Scan(&loginAt, &login.IP, &login.CountryCode, &login.City, &login.UserAgent)
	if err == nil {
		login.At = loginAt.Format(time.RFC3339)
		result.LastLogin = &login
	} else if err != pgx.ErrNoRows {
		return result, err
	}
	return result, nil
}

func (s *Server) resolveUserRootPermissions(ctx context.Context, userID int64) ([]string, []effectivePermission, error) {
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
				effectivePermission: effectivePermission{Code: code, Allow: entry.Allow, Priority: role.Weight, Source: "group." + roleCode},
				Depth:               depth,
			})
		}
		for _, parent := range role.Parents {
			applyRole(applyRoleVariables(parent, variables), depth+1, nextPath)
		}
	}
	for _, role := range assignedRoles {
		applyRole(role, 0, map[string]bool{})
	}

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
			effectivePermission: effectivePermission{Code: code, Allow: allow, Priority: directUserPermissionPriority, Source: "user"},
			Depth:               -1,
		})
	}
	if err := directRows.Err(); err != nil {
		directRows.Close()
		return nil, nil, err
	}
	directRows.Close()

	permissions := make([]effectivePermission, 0, len(candidates))
	for _, candidate := range candidates {
		permissions = append(permissions, candidate.effectivePermission)
	}
	sort.Slice(permissions, func(i, j int) bool { return permissions[i].Code < permissions[j].Code })
	return assignedRoles, permissions, nil
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
