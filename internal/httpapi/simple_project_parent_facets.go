package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const simpleProjectParentFacetCursorVersion = 1

type simpleProjectParentFacetRequest struct {
	ProjectType string
	ViewerID    int64
	Limit       int
	Scope       string
	Selected    []string
	Cursor      *simpleProjectParentFacetCursor
}

type simpleProjectParentFacetCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	Label   string `json:"label"`
	Key     string `json:"key"`
}

type simpleProjectParentFacetItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type simpleProjectParentFacetPage struct {
	Items         []simpleProjectParentFacetItem `json:"items"`
	SelectedItems []simpleProjectParentFacetItem `json:"selectedItems"`
	HasMore       bool                           `json:"hasMore"`
	NextCursor    string                         `json:"nextCursor"`
}

func parseSimpleProjectParentFacetRequest(values url.Values, projectType string, viewerID int64) (simpleProjectParentFacetRequest, error) {
	limit := 50
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			return simpleProjectParentFacetRequest{}, errors.New("parent facet page size is invalid")
		}
		limit = parsed
	}
	selected, validSelected := parseCatalogList(values.Get("selected"), 20)
	if !validSelected {
		return simpleProjectParentFacetRequest{}, errors.New("selected parent facets are invalid")
	}
	scope := simpleProjectParentFacetScope(projectType, viewerID, limit)
	cursor, err := decodeSimpleProjectParentFacetCursor(values.Get("cursor"), scope)
	if err != nil {
		return simpleProjectParentFacetRequest{}, err
	}
	return simpleProjectParentFacetRequest{
		ProjectType: projectType,
		ViewerID:    viewerID,
		Limit:       limit,
		Scope:       scope,
		Selected:    selected,
		Cursor:      cursor,
	}, nil
}

func simpleProjectParentFacetScope(projectType string, viewerID int64, limit int) string {
	material, _ := json.Marshal(struct {
		Version     int    `json:"version"`
		ProjectType string `json:"projectType"`
		ViewerID    int64  `json:"viewerId"`
		Limit       int    `json:"limit"`
	}{simpleProjectParentFacetCursorVersion, projectType, viewerID, limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeSimpleProjectParentFacetCursor(cursor simpleProjectParentFacetCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSimpleProjectParentFacetCursor(raw, scope string) (*simpleProjectParentFacetCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("parent facet cursor is invalid")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("parent facet cursor is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor simpleProjectParentFacetCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("parent facet cursor is invalid")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) ||
		cursor.Version != simpleProjectParentFacetCursorVersion || cursor.Scope != scope ||
		strings.TrimSpace(cursor.Label) == "" || strings.TrimSpace(cursor.Key) == "" {
		return nil, errors.New("parent facet cursor is invalid")
	}
	return &cursor, nil
}

const simpleProjectParentFacetOptionsSQL = `with parent_options as (
	select distinct
		ref.target_type||':'||coalesce(nullif(parent_mod.slug,''),nullif(parent_pack.slug,''),nullif(parent_project.slug,''),ref.raw_identifier) facet_key,
		coalesce(nullif(btrim(parent_mod.primary_name),''),nullif(btrim(parent_pack.primary_name),''),nullif(btrim(parent_project.primary_name),''),nullif(btrim(ref.raw_identifier),''),ref.target_type) facet_label
	from simple_projects project
	join simple_project_parent_refs ref on ref.project_id=project.id
	left join mods parent_mod on ref.target_type='mod' and parent_mod.id=ref.target_id
	left join modpacks parent_pack on ref.target_type='modpack' and parent_pack.id=ref.target_id
	left join simple_projects parent_project on ref.target_type=parent_project.project_type and parent_project.id=ref.target_id
	where project.project_type=$1 and (project.review_status='approved' or project.submitted_by=$2)
)
select facet_key,facet_label from parent_options
where facet_key<>'' %s
order by lower(facet_label),facet_key limit $%d`

func simpleProjectParentFacetPageSQL(request simpleProjectParentFacetRequest) (string, []any) {
	arguments := []any{request.ProjectType, request.ViewerID}
	cursorPredicate := ""
	if request.Cursor != nil {
		cursorPredicate = `and (lower(facet_label),facet_key)>($3,$4)`
		arguments = append(arguments, request.Cursor.Label, request.Cursor.Key)
	}
	arguments = append(arguments, request.Limit+1)
	return fmt.Sprintf(simpleProjectParentFacetOptionsSQL, cursorPredicate, len(arguments)), arguments
}

func simpleProjectSelectedParentFacetsSQL(request simpleProjectParentFacetRequest) (string, []any) {
	arguments := []any{request.ProjectType, request.ViewerID, request.Selected}
	return fmt.Sprintf(simpleProjectParentFacetOptionsSQL, `and facet_key=any($3::text[])`, 4), append(arguments, len(request.Selected))
}

func (s *Server) simpleProjectParentFacets(w http.ResponseWriter, r *http.Request) {
	projectType := normalizeSimpleProjectType(r.PathValue("projectType"))
	if projectType == "" {
		writeError(w, http.StatusNotFound, "project type not found")
		return
	}
	claims := currentClaims(r)
	request, err := parseSimpleProjectParentFacetRequest(r.URL.Query(), projectType, claims.Subject)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.loadSimpleProjectParentFacetPage(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load parent project facets")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Authorization")
	w.Header().Add("Vary", "Cookie")
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) loadSimpleProjectParentFacetPage(ctx context.Context, request simpleProjectParentFacetRequest) (simpleProjectParentFacetPage, error) {
	query, arguments := simpleProjectParentFacetPageSQL(request)
	rows, err := s.db.Query(ctx, query, arguments...)
	if err != nil {
		return simpleProjectParentFacetPage{}, err
	}
	items := make([]simpleProjectParentFacetItem, 0, request.Limit+1)
	for rows.Next() {
		var item simpleProjectParentFacetItem
		if err = rows.Scan(&item.Key, &item.Label); err != nil {
			rows.Close()
			return simpleProjectParentFacetPage{}, err
		}
		items = append(items, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return simpleProjectParentFacetPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	page := simpleProjectParentFacetPage{Items: items, SelectedItems: []simpleProjectParentFacetItem{}, HasMore: hasMore}
	if hasMore {
		last := items[len(items)-1]
		page.NextCursor = encodeSimpleProjectParentFacetCursor(simpleProjectParentFacetCursor{
			Version: simpleProjectParentFacetCursorVersion,
			Scope:   request.Scope,
			Label:   strings.ToLower(last.Label),
			Key:     last.Key,
		})
	}
	if len(request.Selected) == 0 {
		return page, nil
	}
	query, arguments = simpleProjectSelectedParentFacetsSQL(request)
	rows, err = s.db.Query(ctx, query, arguments...)
	if err != nil {
		return simpleProjectParentFacetPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item simpleProjectParentFacetItem
		if err = rows.Scan(&item.Key, &item.Label); err != nil {
			return simpleProjectParentFacetPage{}, err
		}
		page.SelectedItems = append(page.SelectedItems, item)
	}
	return page, rows.Err()
}
