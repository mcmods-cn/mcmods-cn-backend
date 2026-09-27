package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const adminProjectPageCursorVersion = 1

var adminProjectHeatPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,9})\.[0-9]{6}$`)

type adminProjectPageRequest struct {
	Query       string
	ProjectType string
	Limit       int
	Scope       string
	Cursor      *adminProjectPageCursor
}

type adminProjectPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"scope"`
	Heat      string    `json:"heat"`
	UpdatedAt time.Time `json:"updatedAt"`
	RouteID   int64     `json:"routeId"`
}

type adminProjectPageRow struct {
	Summary  adminProjectSummary
	HeatSort string
	RouteID  int64
}

func parseAdminProjectPageRequest(values url.Values) (adminProjectPageRequest, error) {
	allowed := map[string]bool{"q": true, "type": true, "limit": true, "cursor": true}
	for key, items := range values {
		if !allowed[key] {
			return adminProjectPageRequest{}, fmt.Errorf("unsupported project query parameter %q", key)
		}
		if len(items) != 1 {
			return adminProjectPageRequest{}, fmt.Errorf("project query parameter %q must appear exactly once", key)
		}
	}

	request := adminProjectPageRequest{
		Query: strings.ToLower(strings.TrimSpace(values.Get("q"))), Limit: 30,
	}
	if utf8.RuneCountInString(request.Query) > 200 || len(request.Query) > 800 {
		return adminProjectPageRequest{}, errors.New("project search query is too long")
	}
	rawType := strings.TrimSpace(values.Get("type"))
	request.ProjectType = normalizeAdminProjectType(rawType)
	if rawType != "" && request.ProjectType == "" {
		return adminProjectPageRequest{}, errors.New("invalid project type")
	}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			return adminProjectPageRequest{}, errors.New("invalid project page size")
		}
		request.Limit = parsed
	}
	request.Scope = adminProjectPageScope(request)
	cursor, err := decodeAdminProjectPageCursor(values.Get("cursor"), request.Scope)
	if err != nil {
		return adminProjectPageRequest{}, err
	}
	request.Cursor = cursor
	return request, nil
}

func adminProjectPageScope(request adminProjectPageRequest) string {
	material, _ := json.Marshal(struct {
		Version     int    `json:"version"`
		Query       string `json:"query"`
		ProjectType string `json:"type"`
		Limit       int    `json:"limit"`
	}{adminProjectPageCursorVersion, request.Query, request.ProjectType, request.Limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeAdminProjectPageCursor(cursor adminProjectPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeAdminProjectPageCursor(raw, scope string) (*adminProjectPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid project cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid project cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor adminProjectPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid project cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) ||
		cursor.Version != adminProjectPageCursorVersion || cursor.Scope != scope ||
		!adminProjectHeatPattern.MatchString(cursor.Heat) || cursor.UpdatedAt.IsZero() || cursor.RouteID <= 0 {
		return nil, errors.New("invalid project cursor")
	}
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	return &cursor, nil
}

const adminProjectPageSelect = `select project.public_id,project.entity_type,project.name,project.canonical_path,
	project.review_status,project.view_count,project.edit_count,project.heat_score,
	project.rating_average,project.favorite_count,project.comment_count,project.download_count,
	project.created_at,project.updated_at,project.last_edited_at,project.heat_score::text,project.object_route_id
	from admin_project_catalog project`

func adminProjectPageSQL(request adminProjectPageRequest) (string, []any) {
	where := make([]string, 0, 3)
	arguments := make([]any, 0, 7)
	if request.ProjectType != "" {
		arguments = append(arguments, request.ProjectType)
		where = append(where, fmt.Sprintf("project.entity_type=$%d", len(arguments)))
	}
	if request.Query != "" {
		arguments = append(arguments, request.Query)
		parameter := len(arguments)
		where = append(where, fmt.Sprintf("(project.public_id=$%d or project.search_document @@ websearch_to_tsquery('simple',$%d))", parameter, parameter))
	}
	if request.Cursor != nil {
		arguments = append(arguments, request.Cursor.Heat, request.Cursor.UpdatedAt, request.Cursor.RouteID)
		where = append(where, fmt.Sprintf(
			"(project.heat_score,project.updated_at,project.object_route_id)<($%d::numeric,$%d,$%d)",
			len(arguments)-2, len(arguments)-1, len(arguments),
		))
	}
	query := adminProjectPageSelect
	if len(where) != 0 {
		query += " where " + strings.Join(where, " and ")
	}
	arguments = append(arguments, request.Limit+1)
	query += fmt.Sprintf(" order by project.heat_score desc,project.updated_at desc,project.object_route_id desc limit $%d", len(arguments))
	return query, arguments
}

func adminProjectNextCursor(request adminProjectPageRequest, row adminProjectPageRow) string {
	return encodeAdminProjectPageCursor(adminProjectPageCursor{
		Version: adminProjectPageCursorVersion, Scope: request.Scope, Heat: row.HeatSort,
		UpdatedAt: row.Summary.UpdatedAt.UTC(), RouteID: row.RouteID,
	})
}
