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
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	creatorPageCursorVersion = 1
	creatorPageModeSQL       = "sql"
	creatorPageModeIndex     = "index"
)

var creatorDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

type creatorPageRequest struct {
	Kind      string
	Query     string
	Limit     int
	Sort      catalogSort
	Direction catalogSortDirection
	Scope     string
	Cursor    *creatorPageCursor
}

type creatorPageCursor struct {
	Version     int                  `json:"v"`
	Scope       string               `json:"s"`
	Sort        catalogSort          `json:"sort"`
	Direction   catalogSortDirection `json:"direction"`
	Mode        string               `json:"mode"`
	Offset      int                  `json:"offset,omitempty"`
	ID          int64                `json:"id,omitempty"`
	Name        string               `json:"name,omitempty"`
	CreatedAt   time.Time            `json:"createdAt,omitempty"`
	UpdatedAt   time.Time            `json:"updatedAt,omitempty"`
	Metric      string               `json:"metric,omitempty"`
	RatingCount int64                `json:"ratingCount,omitempty"`
}

type creatorPageRow struct {
	Summary     creatorSummary
	InternalID  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	SortName    string
	Heat        string
	Downloads   string
	Favorites   string
	Rating      string
	RatingCount int64
	Views       string
	Comments    string
}

func parseCreatorPageRequest(values url.Values, userID int64, admin bool) (creatorPageRequest, error) {
	kind := strings.TrimSpace(strings.ToLower(values.Get("kind")))
	if kind != "" && kind != "author" && kind != "team" {
		return creatorPageRequest{}, errors.New("invalid creator kind")
	}
	query, validQuery := parseCatalogQuery(values.Get("query"))
	if !validQuery {
		return creatorPageRequest{}, errors.New("invalid creator query")
	}
	limit := boundedInt(values.Get("limit"), 40, 1, 100)
	rawSort := strings.TrimSpace(values.Get("sort"))
	if rawSort == "" {
		rawSort = string(catalogSortName)
	}
	sort, validSort := parseCatalogSort(rawSort)
	direction, validDirection := parseCatalogSortDirection(values.Get("order"), sort)
	if !validSort || !validDirection {
		return creatorPageRequest{}, errors.New("invalid creator catalog sort")
	}
	scope := creatorPageScope(kind, query, sort, direction, limit, userID, admin)
	cursor, err := decodeCreatorPageCursor(values.Get("cursor"), scope, sort, direction, limit)
	if err != nil {
		return creatorPageRequest{}, err
	}
	if cursor != nil && cursor.Mode == creatorPageModeIndex && (query == "" || !catalogSortUsesSearchIndex(sort)) {
		return creatorPageRequest{}, errors.New("invalid creator cursor mode")
	}
	return creatorPageRequest{
		Kind: kind, Query: query, Limit: limit, Sort: sort, Direction: direction, Scope: scope, Cursor: cursor,
	}, nil
}

func creatorPageScope(kind, query string, sort catalogSort, direction catalogSortDirection, limit int, userID int64, admin bool) string {
	material, _ := json.Marshal(struct {
		Version   int                  `json:"version"`
		Kind      string               `json:"kind"`
		Query     string               `json:"query"`
		Sort      catalogSort          `json:"sort"`
		Direction catalogSortDirection `json:"direction"`
		Limit     int                  `json:"limit"`
		UserID    int64                `json:"userId"`
		Admin     bool                 `json:"admin"`
	}{creatorPageCursorVersion, kind, query, sort, direction, limit, userID, admin})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeCreatorPageCursor(cursor creatorPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCreatorPageCursor(raw, scope string, sort catalogSort, direction catalogSortDirection, limit int) (*creatorPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid creator cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid creator cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor creatorPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid creator cursor")
	}
	if err = requireCreatorCursorEOF(decoder); err != nil || cursor.Version != creatorPageCursorVersion ||
		cursor.Scope != scope || cursor.Sort != sort || cursor.Direction != direction {
		return nil, errors.New("invalid creator cursor")
	}
	switch cursor.Mode {
	case creatorPageModeIndex:
		if cursor.Offset <= 0 || cursor.Offset%limit != 0 || cursor.Offset > 1_000_000 {
			return nil, errors.New("invalid creator cursor")
		}
	case creatorPageModeSQL:
		if cursor.ID <= 0 || !validCreatorSQLCursor(cursor) {
			return nil, errors.New("invalid creator cursor")
		}
	default:
		return nil, errors.New("invalid creator cursor")
	}
	return &cursor, nil
}

func requireCreatorCursorEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("invalid creator cursor")
}

func validCreatorSQLCursor(cursor creatorPageCursor) bool {
	switch cursor.Sort {
	case catalogSortName:
		return strings.TrimSpace(cursor.Name) != ""
	case catalogSortPublished:
		return !cursor.CreatedAt.IsZero() && !cursor.UpdatedAt.IsZero()
	case catalogSortUpdated, catalogSortCollected:
		return !cursor.UpdatedAt.IsZero()
	case catalogSortRating:
		return validNonNegativeDecimal(cursor.Metric) && cursor.RatingCount >= 0 && !cursor.UpdatedAt.IsZero()
	default:
		return validNonNegativeDecimal(cursor.Metric) && !cursor.UpdatedAt.IsZero()
	}
}

func validNonNegativeDecimal(value string) bool {
	if !creatorDecimalPattern.MatchString(value) {
		return false
	}
	parsed, ok := new(big.Rat).SetString(value)
	return ok && parsed.Sign() >= 0
}

func creatorCursorPredicateSQL(cursor *creatorPageCursor, startParameter int) (string, []any) {
	if cursor == nil || cursor.Mode != creatorPageModeSQL {
		return "", nil
	}
	operator := ">"
	if cursor.Direction == catalogSortDescending {
		operator = "<"
	}
	parameter := func(offset int) string { return fmt.Sprintf("$%d", startParameter+offset) }
	switch cursor.Sort {
	case catalogSortName:
		return fmt.Sprintf("and (lower(creator.name),creator.id) %s (%s,%s)", operator, parameter(0), parameter(1)),
			[]any{cursor.Name, cursor.ID}
	case catalogSortPublished:
		return fmt.Sprintf("and (creator.created_at,creator.updated_at,creator.id) %s (%s,%s,%s)", operator, parameter(0), parameter(1), parameter(2)),
			[]any{cursor.CreatedAt, cursor.UpdatedAt, cursor.ID}
	case catalogSortUpdated, catalogSortCollected:
		return fmt.Sprintf("and (creator.updated_at,creator.id) %s (%s,%s)", operator, parameter(0), parameter(1)),
			[]any{cursor.UpdatedAt, cursor.ID}
	case catalogSortDownloads:
		return creatorMetricCursorPredicate("coalesce(popularity.download_count,0)", cursor, operator, startParameter, false)
	case catalogSortFavorites:
		return creatorMetricCursorPredicate("coalesce(popularity.favorite_count,0)", cursor, operator, startParameter, false)
	case catalogSortRating:
		return creatorMetricCursorPredicate("coalesce(popularity.bayesian_rating,0)", cursor, operator, startParameter, true)
	case catalogSortViews:
		return creatorMetricCursorPredicate("coalesce(popularity.view_count,0)", cursor, operator, startParameter, false)
	case catalogSortComments:
		return creatorMetricCursorPredicate("coalesce(popularity.comment_count,0)", cursor, operator, startParameter, false)
	default:
		return creatorMetricCursorPredicate("coalesce(popularity.heat_score,0)", cursor, operator, startParameter, false)
	}
}

func creatorMetricCursorPredicate(expression string, cursor *creatorPageCursor, operator string, startParameter int, rating bool) (string, []any) {
	if rating {
		return fmt.Sprintf("and (%s,coalesce(popularity.rating_count,0),creator.updated_at,creator.id) %s ($%d::numeric,$%d,$%d,$%d)",
				expression, operator, startParameter, startParameter+1, startParameter+2, startParameter+3),
			[]any{cursor.Metric, cursor.RatingCount, cursor.UpdatedAt, cursor.ID}
	}
	return fmt.Sprintf("and (%s,creator.updated_at,creator.id) %s ($%d::numeric,$%d,$%d)",
			expression, operator, startParameter, startParameter+1, startParameter+2),
		[]any{cursor.Metric, cursor.UpdatedAt, cursor.ID}
}

func creatorSQLPageCursor(request creatorPageRequest, row creatorPageRow) string {
	cursor := creatorPageCursor{
		Version: creatorPageCursorVersion, Scope: request.Scope, Sort: request.Sort,
		Direction: request.Direction, Mode: creatorPageModeSQL, ID: row.InternalID,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Name: row.SortName,
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
	case catalogSortName, catalogSortPublished, catalogSortUpdated, catalogSortCollected:
	default:
		cursor.Metric = row.Heat
	}
	return encodeCreatorPageCursor(cursor)
}

func creatorIndexPageCursor(request creatorPageRequest, offset int) string {
	return encodeCreatorPageCursor(creatorPageCursor{
		Version: creatorPageCursorVersion, Scope: request.Scope, Sort: request.Sort,
		Direction: request.Direction, Mode: creatorPageModeIndex, Offset: offset,
	})
}
