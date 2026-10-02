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
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	unresolvedReferencePageCursorVersion  = 1
	unresolvedReferenceDefaultPageLimit   = 50
	unresolvedReferenceMaximumPageLimit   = 100
	unresolvedReferenceMaximumCursorSize  = 2048
	unresolvedReferenceMaximumTypeSize    = 128
	unresolvedReferenceSearchResultBudget = 10000
)

var errUnresolvedReferenceSearchTooBroad = errors.New("unresolved reference search matches too many records; enter a longer prefix")

type unresolvedReferencePageRequest struct {
	Query         string
	SearchPrefix  string
	ReferenceType string
	Status        string
	Limit         int
	Scope         string
	Cursor        *unresolvedReferencePageCursor
}

type unresolvedReferencePageCursor struct {
	Version     int       `json:"v"`
	Scope       string    `json:"scope"`
	CreatedAt   time.Time `json:"createdAt"`
	Origin      int16     `json:"origin"`
	SourceRowID int64     `json:"sourceRowId"`
}

type unresolvedReferenceListItem struct {
	ID             string     `json:"id"`
	SourceType     string     `json:"sourceType"`
	SourceID       string     `json:"sourceId"`
	FieldPath      string     `json:"fieldPath"`
	ReferenceType  string     `json:"referenceType"`
	RawIdentifier  string     `json:"rawIdentifier"`
	Status         string     `json:"status"`
	ResolvedType   string     `json:"resolvedType,omitempty"`
	ResolvedID     string     `json:"resolvedId,omitempty"`
	SourceLabel    string     `json:"sourceLabel,omitempty"`
	SourcePublicID string     `json:"sourcePublicId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
}

type unresolvedReferencePage struct {
	Items      []unresolvedReferenceListItem `json:"items"`
	Limit      int                           `json:"limit"`
	HasMore    bool                          `json:"hasMore"`
	NextCursor string                        `json:"nextCursor"`
}

func parseUnresolvedReferencePageRequest(values url.Values) (unresolvedReferencePageRequest, error) {
	allowed := map[string]bool{"q": true, "type": true, "status": true, "limit": true, "cursor": true}
	for name, entries := range values {
		if !allowed[name] || len(entries) != 1 {
			return unresolvedReferencePageRequest{}, errors.New("invalid unresolved reference pagination parameters")
		}
	}
	query := strings.ToLower(strings.TrimSpace(values.Get("q")))
	queryCharacters := utf8.RuneCountInString(query)
	if !utf8.ValidString(query) || queryCharacters == 1 || queryCharacters > 64 {
		return unresolvedReferencePageRequest{}, errors.New("unresolved reference search must contain 2 to 64 characters")
	}
	referenceType := strings.TrimSpace(values.Get("type"))
	if !validUnresolvedReferenceType(referenceType) {
		return unresolvedReferencePageRequest{}, errors.New("invalid unresolved reference type")
	}
	status := strings.TrimSpace(values.Get("status"))
	if status == "" {
		status = "pending"
	}
	if status != "all" && status != "pending" && status != "resolved" && status != "ignored" {
		return unresolvedReferencePageRequest{}, errors.New("invalid unresolved reference status")
	}
	limit := unresolvedReferenceDefaultPageLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > unresolvedReferenceMaximumPageLimit {
			return unresolvedReferencePageRequest{}, errors.New("invalid unresolved reference page limit")
		}
		limit = parsed
	}
	searchPrefix := ""
	if query != "" {
		searchPrefix = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	}
	scope := unresolvedReferencePageScope(query, referenceType, status, limit)
	cursor, err := decodeUnresolvedReferencePageCursor(values.Get("cursor"), scope)
	if err != nil {
		return unresolvedReferencePageRequest{}, err
	}
	return unresolvedReferencePageRequest{
		Query: query, SearchPrefix: searchPrefix, ReferenceType: referenceType,
		Status: status, Limit: limit, Scope: scope, Cursor: cursor,
	}, nil
}

func validUnresolvedReferenceType(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > unresolvedReferenceMaximumTypeSize || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return false
		}
	}
	return true
}

type unresolvedReferenceTypesResponse struct {
	Items []string `json:"items"`
}

func builtInUnresolvedReferenceTypes() []string {
	types := make([]string, 0, len(communityPostProjectTypes)+3)
	for projectType := range communityPostProjectTypes {
		types = append(types, projectType)
	}
	types = append(types, "enchantment", "server", "tag")
	sort.Strings(types)
	return types
}

func (s *Server) queryUnresolvedReferenceTypes(ctx context.Context) ([]string, error) {
	types := builtInUnresolvedReferenceTypes()
	seen := make(map[string]bool, len(types))
	for _, referenceType := range types {
		seen[referenceType] = true
	}
	rows, err := s.db.Query(ctx, `select code from resource_kinds order by display_order,code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var referenceType string
		if err = rows.Scan(&referenceType); err != nil {
			return nil, err
		}
		if referenceType == "" || !validUnresolvedReferenceType(referenceType) {
			return nil, fmt.Errorf("resource kind %q cannot be used as an unresolved-reference filter", referenceType)
		}
		if !seen[referenceType] {
			seen[referenceType] = true
			types = append(types, referenceType)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return types, nil
}

func (s *Server) adminUnresolvedReferenceTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.queryUnresolvedReferenceTypes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load unresolved reference types")
		return
	}
	writeJSON(w, http.StatusOK, unresolvedReferenceTypesResponse{Items: types})
}

func unresolvedReferencePageScope(query, referenceType, status string, limit int) string {
	payload, _ := json.Marshal(struct {
		Version       int    `json:"version"`
		Query         string `json:"query"`
		ReferenceType string `json:"referenceType"`
		Status        string `json:"status"`
		Limit         int    `json:"limit"`
	}{unresolvedReferencePageCursorVersion, query, referenceType, status, limit})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:16])
}

func encodeUnresolvedReferencePageCursor(cursor unresolvedReferencePageCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeUnresolvedReferencePageCursor(raw, scope string) (*unresolvedReferencePageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > unresolvedReferenceMaximumCursorSize*2 {
		return nil, errors.New("invalid unresolved reference cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(payload) == 0 || len(payload) > unresolvedReferenceMaximumCursorSize {
		return nil, errors.New("invalid unresolved reference cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor unresolvedReferencePageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid unresolved reference cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != unresolvedReferencePageCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.Origin < 0 || cursor.Origin > 1 || cursor.SourceRowID <= 0 {
		return nil, errors.New("invalid unresolved reference cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}

func appendUnresolvedReferencePageFilters(query *strings.Builder, args *[]any, request unresolvedReferencePageRequest, includeCursor bool) {
	filters := make([]string, 0, 4)
	addArgument := func(value any) string {
		*args = append(*args, value)
		return "$" + strconv.Itoa(len(*args))
	}
	if request.Status != "all" {
		filters = append(filters, "status="+addArgument(request.Status))
	}
	if request.ReferenceType != "" {
		filters = append(filters, "reference_type="+addArgument(request.ReferenceType))
	}
	if request.SearchPrefix != "" {
		placeholder := addArgument(request.SearchPrefix)
		filters = append(filters, "(lower(raw_identifier) like "+placeholder+` escape '\' or lower(source_label) like `+placeholder+` escape '\')`)
	}
	if includeCursor && request.Cursor != nil {
		createdAt := addArgument(request.Cursor.CreatedAt)
		origin := addArgument(request.Cursor.Origin)
		sourceRowID := addArgument(request.Cursor.SourceRowID)
		filters = append(filters, "(created_at,origin,source_row_id)<("+createdAt+"::timestamptz,"+origin+"::smallint,"+sourceRowID+"::bigint)")
	}
	if len(filters) > 0 {
		query.WriteString(" where ")
		query.WriteString(strings.Join(filters, " and "))
	}
}

func buildUnresolvedReferencePageQuery(request unresolvedReferencePageRequest) (string, []any) {
	var query strings.Builder
	query.WriteString(`select origin,source_row_id,source_type,source_id::text,field_path,reference_type,
		raw_identifier,status,resolved_type,resolved_id,source_label,source_public_id,created_at,resolved_at
		from unresolved_reference_catalog`)
	args := make([]any, 0, 8)
	appendUnresolvedReferencePageFilters(&query, &args, request, true)
	query.WriteString(" order by created_at desc,origin desc,source_row_id desc limit $")
	args = append(args, request.Limit+1)
	query.WriteString(strconv.Itoa(len(args)))
	return query.String(), args
}

func buildUnresolvedReferenceSearchBudgetQuery(request unresolvedReferencePageRequest) (string, []any) {
	var query strings.Builder
	query.WriteString("select count(*) from (select 1 from unresolved_reference_catalog")
	args := make([]any, 0, 4)
	appendUnresolvedReferencePageFilters(&query, &args, request, false)
	query.WriteString(" limit $")
	args = append(args, unresolvedReferenceSearchResultBudget+1)
	query.WriteString(strconv.Itoa(len(args)))
	query.WriteString(") bounded_matches")
	return query.String(), args
}

func (s *Server) queryUnresolvedReferencePage(ctx context.Context, request unresolvedReferencePageRequest) (unresolvedReferencePage, error) {
	if request.Query != "" {
		budgetSQL, budgetArgs := buildUnresolvedReferenceSearchBudgetQuery(request)
		var matches int
		if err := s.db.QueryRow(ctx, budgetSQL, budgetArgs...).Scan(&matches); err != nil {
			return unresolvedReferencePage{}, err
		}
		if matches > unresolvedReferenceSearchResultBudget {
			return unresolvedReferencePage{}, errUnresolvedReferenceSearchTooBroad
		}
	}
	pageSQL, pageArgs := buildUnresolvedReferencePageQuery(request)
	rows, err := s.db.Query(ctx, pageSQL, pageArgs...)
	if err != nil {
		return unresolvedReferencePage{}, err
	}
	defer rows.Close()
	items := make([]unresolvedReferenceListItem, 0, request.Limit+1)
	type position struct {
		Origin      int16
		SourceRowID int64
		CreatedAt   time.Time
	}
	positions := make([]position, 0, request.Limit+1)
	for rows.Next() {
		var item unresolvedReferenceListItem
		var itemPosition position
		if err = rows.Scan(&itemPosition.Origin, &itemPosition.SourceRowID, &item.SourceType, &item.SourceID,
			&item.FieldPath, &item.ReferenceType, &item.RawIdentifier, &item.Status, &item.ResolvedType,
			&item.ResolvedID, &item.SourceLabel, &item.SourcePublicID, &itemPosition.CreatedAt, &item.ResolvedAt); err != nil {
			return unresolvedReferencePage{}, err
		}
		prefix := "general"
		if itemPosition.Origin == 1 {
			prefix = "resource"
		}
		item.ID = fmt.Sprintf("%s:%d", prefix, itemPosition.SourceRowID)
		item.CreatedAt = itemPosition.CreatedAt
		items = append(items, item)
		positions = append(positions, itemPosition)
	}
	if err = rows.Err(); err != nil {
		return unresolvedReferencePage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
		positions = positions[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(positions) > 0 {
		last := positions[len(positions)-1]
		nextCursor = encodeUnresolvedReferencePageCursor(unresolvedReferencePageCursor{
			Version: unresolvedReferencePageCursorVersion, Scope: request.Scope,
			CreatedAt: last.CreatedAt, Origin: last.Origin, SourceRowID: last.SourceRowID,
		})
	}
	return unresolvedReferencePage{Items: items, Limit: request.Limit, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func (s *Server) adminUnresolvedReferences(w http.ResponseWriter, r *http.Request) {
	request, err := parseUnresolvedReferencePageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.queryUnresolvedReferencePage(r.Context(), request)
	if errors.Is(err, errUnresolvedReferenceSearchTooBroad) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load unresolved references")
		return
	}
	writeJSON(w, http.StatusOK, page)
}
