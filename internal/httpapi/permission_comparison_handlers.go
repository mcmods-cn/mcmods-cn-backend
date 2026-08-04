package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"mcmods-cn-backend/internal/domain"
	"mcmods-cn-backend/internal/security"
)

type permissionComparisonOption struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type permissionComparisonRequest struct {
	Left  permissionComparisonSelection `json:"left"`
	Right permissionComparisonSelection `json:"right"`
}

type permissionComparisonSelection struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
}

type permissionComparisonSubject struct {
	Kind        string                    `json:"kind"`
	Code        string                    `json:"code"`
	Name        string                    `json:"name"`
	Groups      []string                  `json:"groups"`
	Permissions []security.PermissionRule `json:"permissions"`
}

type permissionComparisonRow struct {
	Code         string                   `json:"code"`
	Name         string                   `json:"name"`
	Description  string                   `json:"description"`
	Translations domain.LocalizedTexts    `json:"translations"`
	Left         *security.PermissionRule `json:"left,omitempty"`
	Right        *security.PermissionRule `json:"right,omitempty"`
}

func (s *Server) permissionComparisonOptions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(
		r.Context(),
		`select code, coalesce(nullif(name, ''), code)
		 from roles where status = 'active' order by weight desc, code`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限组列表失败")
		return
	}
	defer rows.Close()
	options := []permissionComparisonOption{{Kind: "me", Code: "me", Name: "me"}}
	for rows.Next() {
		var option permissionComparisonOption
		option.Kind = "role"
		if err := rows.Scan(&option.Code, &option.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "读取权限组列表失败")
			return
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限组列表失败")
		return
	}
	writeJSON(w, http.StatusOK, options)
}

func (s *Server) comparePermissions(w http.ResponseWriter, r *http.Request) {
	var request permissionComparisonRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	left, err := s.resolvePermissionComparisonSubject(r.Context(), currentClaims(r).Subject, request.Left)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	right, err := s.resolvePermissionComparisonSubject(r.Context(), currentClaims(r).Subject, request.Right)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows := mergePermissionComparisonRows(left.Permissions, right.Permissions)
	if err := s.attachPermissionComparisonMetadata(r.Context(), rows); err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限说明失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"left": left, "right": right, "rows": rows})
}

func (s *Server) attachPermissionComparisonMetadata(ctx context.Context, rows []permissionComparisonRow) error {
	catalog, err := s.permissions(ctx)
	if err != nil {
		return err
	}
	for index := range rows {
		permission := matchPermissionCatalog(rows[index].Code, catalog)
		if permission == nil {
			rows[index].Translations = domain.LocalizedTexts{}
			continue
		}
		rows[index].Name = permission.Name
		rows[index].Description = permission.Description
		rows[index].Translations = permission.Translations
	}
	return nil
}

func matchPermissionCatalog(code string, catalog []domain.Permission) *domain.Permission {
	for index := range catalog {
		if catalog[index].Code == code {
			return &catalog[index]
		}
	}
	actualParts := strings.Split(code, ".")
	bestIndex := -1
	bestLiteralCount := -1
	for index := range catalog {
		templateParts := strings.Split(catalog[index].Code, ".")
		if len(templateParts) != len(actualParts) {
			continue
		}
		literalCount := 0
		hasVariable := false
		matches := true
		for partIndex, templatePart := range templateParts {
			if _, ok := templateVariableName(templatePart); ok {
				hasVariable = true
				continue
			}
			if templatePart != actualParts[partIndex] {
				matches = false
				break
			}
			literalCount++
		}
		if matches && hasVariable && literalCount > bestLiteralCount {
			bestIndex = index
			bestLiteralCount = literalCount
		}
	}
	if bestIndex < 0 {
		return nil
	}
	return &catalog[bestIndex]
}

func mergePermissionComparisonRows(left []security.PermissionRule, right []security.PermissionRule) []permissionComparisonRow {
	byCode := make(map[string]*permissionComparisonRow, len(left)+len(right))
	for index := range left {
		entry := left[index]
		row := byCode[entry.Code]
		if row == nil {
			row = &permissionComparisonRow{Code: entry.Code}
			byCode[entry.Code] = row
		}
		row.Left = &entry
	}
	for index := range right {
		entry := right[index]
		row := byCode[entry.Code]
		if row == nil {
			row = &permissionComparisonRow{Code: entry.Code}
			byCode[entry.Code] = row
		}
		row.Right = &entry
	}
	rows := make([]permissionComparisonRow, 0, len(byCode))
	for _, row := range byCode {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Code < rows[j].Code })
	return rows
}

func (s *Server) resolvePermissionComparisonSubject(ctx context.Context, userID int64, selection permissionComparisonSelection) (permissionComparisonSubject, error) {
	selection.Kind = strings.ToLower(strings.TrimSpace(selection.Kind))
	selection.Code = strings.TrimSpace(selection.Code)
	if selection.Kind == "me" {
		groups, permissions, err := s.resolveUserRootPermissions(ctx, userID)
		if err != nil {
			return permissionComparisonSubject{}, err
		}
		var name string
		if err := s.db.QueryRow(ctx, `select username from users where id = $1`, userID).Scan(&name); err != nil {
			return permissionComparisonSubject{}, err
		}
		return permissionComparisonSubject{Kind: "me", Code: "me", Name: name, Groups: groups, Permissions: permissions}, nil
	}
	if selection.Kind != "role" || selection.Code == "" {
		return permissionComparisonSubject{}, &requestError{message: "请选择要对比的权限组"}
	}
	permissions, err := s.resolveRoleRootPermissions(ctx, selection.Code)
	if err != nil {
		return permissionComparisonSubject{}, err
	}
	var name string
	if err := s.db.QueryRow(ctx, `select coalesce(nullif(name, ''), code) from roles where code = $1 and status = 'active'`, selection.Code).Scan(&name); err != nil {
		return permissionComparisonSubject{}, &requestError{message: "权限组不存在: " + selection.Code}
	}
	return permissionComparisonSubject{Kind: "role", Code: selection.Code, Name: name, Groups: []string{selection.Code}, Permissions: permissions}, nil
}

func (s *Server) resolveRoleRootPermissions(ctx context.Context, roleCode string) ([]security.PermissionRule, error) {
	rolesByCode, templates, err := s.loadPermissionRoles(ctx)
	if err != nil {
		return nil, err
	}
	if _, _, ok := resolvePermissionRole(roleCode, rolesByCode, templates); !ok {
		return nil, &requestError{message: "权限组不存在: " + roleCode}
	}
	candidates := make(map[string]permissionCandidate)
	applyPermissionRoles(candidates, []string{roleCode}, rolesByCode, templates)
	return permissionRulesFromCandidates(candidates), nil
}
