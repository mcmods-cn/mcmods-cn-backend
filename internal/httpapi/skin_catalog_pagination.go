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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const skinCatalogPageCursorVersion = 1

type skinCatalogPageRequest struct {
	Query     string
	Kind      string
	Model     string
	Limit     int
	Sort      catalogSort
	Direction catalogSortDirection
	Scope     string
	Cursor    *skinCatalogPageCursor
}

type skinCatalogPageCursor struct {
	Version     int                  `json:"v"`
	Scope       string               `json:"s"`
	Sort        catalogSort          `json:"sort"`
	Direction   catalogSortDirection `json:"direction"`
	ID          int64                `json:"id"`
	Name        string               `json:"name,omitempty"`
	CreatedAt   time.Time            `json:"createdAt,omitempty"`
	UpdatedAt   time.Time            `json:"updatedAt,omitempty"`
	Metric      string               `json:"metric,omitempty"`
	RatingCount int64                `json:"ratingCount,omitempty"`
}

type skinCatalogPageRow struct {
	AssetID                                   int64
	SortName                                  string
	CreatedAt, UpdatedAt                      time.Time
	Heat, Downloads, Favorites, Rating, Views string
	Comments                                  string
	RatingCount                               int64
}

func parseSkinCatalogPageRequest(values url.Values) (skinCatalogPageRequest, error) {
	allowed := map[string]bool{
		"q": true, "kind": true, "model": true, "limit": true, "sort": true, "order": true, "cursor": true,
	}
	for key, items := range values {
		if !allowed[key] {
			return skinCatalogPageRequest{}, fmt.Errorf("unsupported skin catalog query parameter %q", key)
		}
		if len(items) != 1 {
			return skinCatalogPageRequest{}, fmt.Errorf("skin catalog query parameter %q must appear exactly once", key)
		}
	}
	query := strings.TrimSpace(values.Get("q"))
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 80 || len(query) > 320 {
		return skinCatalogPageRequest{}, errors.New("invalid skin catalog query")
	}
	kind := strings.ToLower(strings.TrimSpace(values.Get("kind")))
	if kind != "" && kind != "skin" && kind != "cape" {
		return skinCatalogPageRequest{}, errors.New("invalid skin kind")
	}
	model := strings.ToLower(strings.TrimSpace(values.Get("model")))
	if model != "" && model != "default" && model != "slim" {
		return skinCatalogPageRequest{}, errors.New("invalid skin model")
	}
	limit := 24
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			return skinCatalogPageRequest{}, errors.New("invalid skin catalog page size")
		}
		limit = parsed
	}
	rawSort := strings.TrimSpace(values.Get("sort"))
	if rawSort == "" {
		rawSort = string(catalogSortPublished)
	}
	sort, validSort := parseCatalogSort(rawSort)
	direction, validDirection := parseCatalogSortDirection(values.Get("order"), sort)
	if !validSort || !validDirection {
		return skinCatalogPageRequest{}, errors.New("invalid skin catalog sort")
	}
	// The previous generic order builder treated relevance as heat and collected
	// as updated. Canonicalizing those aliases keeps old bookmark semantics while
	// ensuring every cursor has one stable tuple definition.
	if sort == catalogSortRelevance {
		sort = catalogSortHeat
	}
	if sort == catalogSortCollected {
		sort = catalogSortUpdated
	}
	request := skinCatalogPageRequest{
		Query: query, Kind: kind, Model: model, Limit: limit, Sort: sort, Direction: direction,
	}
	request.Scope = skinCatalogPageScope(request)
	cursor, err := decodeSkinCatalogPageCursor(values.Get("cursor"), request.Scope, request.Sort, request.Direction)
	if err != nil {
		return skinCatalogPageRequest{}, err
	}
	request.Cursor = cursor
	return request, nil
}

func skinCatalogPageScope(request skinCatalogPageRequest) string {
	material, _ := json.Marshal(struct {
		Version   int                  `json:"version"`
		Query     string               `json:"query"`
		Kind      string               `json:"kind"`
		Model     string               `json:"model"`
		Limit     int                  `json:"limit"`
		Sort      catalogSort          `json:"sort"`
		Direction catalogSortDirection `json:"direction"`
	}{skinCatalogPageCursorVersion, request.Query, request.Kind, request.Model, request.Limit, request.Sort, request.Direction})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeSkinCatalogPageCursor(cursor skinCatalogPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSkinCatalogPageCursor(raw, scope string, sort catalogSort, direction catalogSortDirection) (*skinCatalogPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid skin catalog cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 {
		return nil, errors.New("invalid skin catalog cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor skinCatalogPageCursor
	if err = decoder.Decode(&cursor); err != nil || requireSkinCatalogCursorEOF(decoder) != nil ||
		cursor.Version != skinCatalogPageCursorVersion || cursor.Scope != scope || cursor.Sort != sort ||
		cursor.Direction != direction || cursor.ID <= 0 || !validSkinCatalogCursor(cursor) {
		return nil, errors.New("invalid skin catalog cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	return &cursor, nil
}

func requireSkinCatalogCursorEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if errors.Is(decoder.Decode(&extra), io.EOF) {
		return nil
	}
	return errors.New("invalid skin catalog cursor")
}

func validSkinCatalogCursor(cursor skinCatalogPageCursor) bool {
	switch cursor.Sort {
	case catalogSortName:
		return strings.TrimSpace(cursor.Name) != ""
	case catalogSortPublished:
		return !cursor.CreatedAt.IsZero() && !cursor.UpdatedAt.IsZero()
	case catalogSortUpdated:
		return !cursor.UpdatedAt.IsZero()
	case catalogSortRating:
		return validNonNegativeDecimal(cursor.Metric) && cursor.RatingCount >= 0 && !cursor.UpdatedAt.IsZero()
	default:
		return validNonNegativeDecimal(cursor.Metric) && !cursor.UpdatedAt.IsZero()
	}
}

func skinCatalogPageSQL(request skinCatalogPageRequest) (string, []any) {
	arguments := make([]any, 0, 10)
	where := make([]string, 0, 4)
	if request.Kind != "" {
		arguments = append(arguments, request.Kind)
		where = append(where, fmt.Sprintf("catalog.kind=$%d", len(arguments)))
	}
	if request.Model != "" {
		arguments = append(arguments, request.Model)
		where = append(where, fmt.Sprintf("catalog.model=$%d", len(arguments)))
	}
	if request.Query != "" {
		arguments = append(arguments, request.Query)
		where = append(where, fmt.Sprintf("catalog.search_document@@websearch_to_tsquery('simple',$%d)", len(arguments)))
	}
	if request.Cursor != nil {
		predicate, cursorArguments := skinCatalogCursorPredicateSQL(request.Cursor, len(arguments)+1)
		where = append(where, predicate)
		arguments = append(arguments, cursorArguments...)
	}
	query := `select catalog.asset_id,lower(catalog.display_name),catalog.created_at,catalog.updated_at,
		catalog.heat_score::text,catalog.downloads::text,catalog.favorite_count::text,
		catalog.bayesian_rating::text,catalog.rating_count,catalog.view_count::text,catalog.comment_count::text
		from skin_public_catalog catalog`
	if len(where) != 0 {
		query += " where " + strings.Join(where, " and ")
	}
	query += " order by " + skinCatalogOrderSQL(request.Sort, request.Direction)
	arguments = append(arguments, request.Limit+1)
	query += fmt.Sprintf(" limit $%d", len(arguments))
	return query, arguments
}

func skinCatalogCursorPredicateSQL(cursor *skinCatalogPageCursor, startParameter int) (string, []any) {
	operator := ">"
	if cursor.Direction == catalogSortDescending {
		operator = "<"
	}
	switch cursor.Sort {
	case catalogSortName:
		return fmt.Sprintf("(lower(catalog.display_name),catalog.asset_id)%s($%d,$%d)", operator, startParameter, startParameter+1),
			[]any{cursor.Name, cursor.ID}
	case catalogSortPublished:
		return fmt.Sprintf("(catalog.created_at,catalog.updated_at,catalog.asset_id)%s($%d,$%d,$%d)", operator, startParameter, startParameter+1, startParameter+2),
			[]any{cursor.CreatedAt, cursor.UpdatedAt, cursor.ID}
	case catalogSortUpdated:
		return fmt.Sprintf("(catalog.updated_at,catalog.asset_id)%s($%d,$%d)", operator, startParameter, startParameter+1),
			[]any{cursor.UpdatedAt, cursor.ID}
	case catalogSortRating:
		return fmt.Sprintf("(catalog.bayesian_rating,catalog.rating_count,catalog.updated_at,catalog.asset_id)%s($%d::numeric,$%d,$%d,$%d)",
				operator, startParameter, startParameter+1, startParameter+2, startParameter+3),
			[]any{cursor.Metric, cursor.RatingCount, cursor.UpdatedAt, cursor.ID}
	default:
		expression := skinCatalogMetricExpression(cursor.Sort)
		return fmt.Sprintf("(%s,catalog.updated_at,catalog.asset_id)%s($%d::numeric,$%d,$%d)",
				expression, operator, startParameter, startParameter+1, startParameter+2),
			[]any{cursor.Metric, cursor.UpdatedAt, cursor.ID}
	}
}

func skinCatalogMetricExpression(sort catalogSort) string {
	switch sort {
	case catalogSortDownloads:
		return "catalog.downloads"
	case catalogSortFavorites:
		return "catalog.favorite_count"
	case catalogSortViews:
		return "catalog.view_count"
	case catalogSortComments:
		return "catalog.comment_count"
	default:
		return "catalog.heat_score"
	}
}

func skinCatalogOrderSQL(sort catalogSort, direction catalogSortDirection) string {
	directionSQL := string(direction)
	if directionSQL != string(catalogSortAscending) {
		directionSQL = string(catalogSortDescending)
	}
	stable := fmt.Sprintf("catalog.updated_at %s,catalog.asset_id %s", directionSQL, directionSQL)
	switch sort {
	case catalogSortName:
		return fmt.Sprintf("lower(catalog.display_name) %s,catalog.asset_id %s", directionSQL, directionSQL)
	case catalogSortPublished:
		return fmt.Sprintf("catalog.created_at %s,%s", directionSQL, stable)
	case catalogSortUpdated:
		return stable
	case catalogSortRating:
		return fmt.Sprintf("catalog.bayesian_rating %s,catalog.rating_count %s,%s", directionSQL, directionSQL, stable)
	default:
		return fmt.Sprintf("%s %s,%s", skinCatalogMetricExpression(sort), directionSQL, stable)
	}
}

func skinCatalogNextCursor(request skinCatalogPageRequest, row skinCatalogPageRow) string {
	cursor := skinCatalogPageCursor{
		Version: skinCatalogPageCursorVersion, Scope: request.Scope, Sort: request.Sort, Direction: request.Direction,
		ID: row.AssetID, Name: row.SortName, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
		RatingCount: row.RatingCount,
	}
	switch request.Sort {
	case catalogSortDownloads:
		cursor.Metric = row.Downloads
	case catalogSortFavorites:
		cursor.Metric = row.Favorites
	case catalogSortRating:
		cursor.Metric = row.Rating
	case catalogSortViews:
		cursor.Metric = row.Views
	case catalogSortComments:
		cursor.Metric = row.Comments
	case catalogSortName, catalogSortPublished, catalogSortUpdated:
	default:
		cursor.Metric = row.Heat
	}
	return encodeSkinCatalogPageCursor(cursor)
}
