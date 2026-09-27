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
	"strings"
	"time"
)

const blueprintPageCursorVersion = 1

type blueprintPageRequest struct {
	Query     string
	Limit     int
	Offset    int
	Sort      catalogSort
	Direction catalogSortDirection
	Scope     string
	Cursor    *blueprintPageCursor
}

type blueprintPageCursor struct {
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

type blueprintPageRow struct {
	InternalID                                   int64
	PublicID, Title, Description, Format, Status string
	Username, Avatar, SortName                   string
	SizeX, SizeY, SizeZ, PaletteCount            int
	BlockCount, UploaderID                       int64
	CreatedAt, UpdatedAt                         time.Time
	Heat, Downloads, Favorites, Rating, Views    string
	Comments                                     string
	RatingCount                                  int64
}

func parseBlueprintPageRequest(values url.Values, userID int64, admin bool) (blueprintPageRequest, error) {
	query, validQuery := parseCatalogQuery(values.Get("q"))
	if !validQuery {
		return blueprintPageRequest{}, errors.New("invalid blueprint catalog query")
	}
	limit := boundedLimit(values.Get("limit"), 24, 100)
	offset := boundedOffset(values.Get("offset"))
	rawSort := strings.TrimSpace(values.Get("sort"))
	if rawSort == "" {
		rawSort = string(catalogSortUpdated)
	}
	sort, validSort := parseCatalogSort(rawSort)
	direction, validDirection := parseCatalogSortDirection(values.Get("order"), sort)
	if !validSort || !validDirection {
		return blueprintPageRequest{}, errors.New("invalid blueprint catalog sort")
	}
	scope := blueprintPageScope(query, sort, direction, limit, userID, admin)
	cursor, err := decodeBlueprintPageCursor(values.Get("cursor"), scope, sort, direction)
	if err != nil {
		return blueprintPageRequest{}, err
	}
	if cursor != nil && offset != 0 {
		return blueprintPageRequest{}, errors.New("blueprint cursor cannot be combined with offset")
	}
	return blueprintPageRequest{
		Query: query, Limit: limit, Offset: offset, Sort: sort, Direction: direction, Scope: scope, Cursor: cursor,
	}, nil
}

func blueprintPageScope(query string, sort catalogSort, direction catalogSortDirection, limit int, userID int64, admin bool) string {
	material, _ := json.Marshal(struct {
		Version   int                  `json:"version"`
		Query     string               `json:"query"`
		Sort      catalogSort          `json:"sort"`
		Direction catalogSortDirection `json:"direction"`
		Limit     int                  `json:"limit"`
		UserID    int64                `json:"userId"`
		Admin     bool                 `json:"admin"`
	}{blueprintPageCursorVersion, query, sort, direction, limit, userID, admin})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeBlueprintPageCursor(cursor blueprintPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeBlueprintPageCursor(raw, scope string, sort catalogSort, direction catalogSortDirection) (*blueprintPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid blueprint cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid blueprint cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor blueprintPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid blueprint cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != blueprintPageCursorVersion ||
		cursor.Scope != scope || cursor.Sort != sort || cursor.Direction != direction || cursor.ID <= 0 ||
		!validBlueprintSQLCursor(cursor) {
		return nil, errors.New("invalid blueprint cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	return &cursor, nil
}

func validBlueprintSQLCursor(cursor blueprintPageCursor) bool {
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

func blueprintCursorPredicateSQL(cursor *blueprintPageCursor, startParameter int) (string, []any) {
	if cursor == nil {
		return "", nil
	}
	operator := ">"
	if cursor.Direction == catalogSortDescending {
		operator = "<"
	}
	parameter := func(offset int) string { return fmt.Sprintf("$%d", startParameter+offset) }
	switch cursor.Sort {
	case catalogSortName:
		return fmt.Sprintf("and (lower(b.title),b.id) %s (%s,%s)", operator, parameter(0), parameter(1)),
			[]any{cursor.Name, cursor.ID}
	case catalogSortPublished:
		return fmt.Sprintf("and (b.created_at,b.updated_at,b.id) %s (%s,%s,%s)", operator, parameter(0), parameter(1), parameter(2)),
			[]any{cursor.CreatedAt, cursor.UpdatedAt, cursor.ID}
	case catalogSortUpdated, catalogSortCollected:
		return fmt.Sprintf("and (b.updated_at,b.id) %s (%s,%s)", operator, parameter(0), parameter(1)),
			[]any{cursor.UpdatedAt, cursor.ID}
	case catalogSortDownloads:
		return blueprintMetricCursorPredicate("coalesce(popularity.download_count,0)", cursor, operator, startParameter, false)
	case catalogSortFavorites:
		return blueprintMetricCursorPredicate("coalesce(popularity.favorite_count,0)", cursor, operator, startParameter, false)
	case catalogSortRating:
		return blueprintMetricCursorPredicate("coalesce(popularity.bayesian_rating,0)", cursor, operator, startParameter, true)
	case catalogSortViews:
		return blueprintMetricCursorPredicate("coalesce(popularity.view_count,0)", cursor, operator, startParameter, false)
	case catalogSortComments:
		return blueprintMetricCursorPredicate("coalesce(popularity.comment_count,0)", cursor, operator, startParameter, false)
	default:
		return blueprintMetricCursorPredicate("coalesce(popularity.heat_score,0)", cursor, operator, startParameter, false)
	}
}

func blueprintMetricCursorPredicate(expression string, cursor *blueprintPageCursor, operator string, startParameter int, rating bool) (string, []any) {
	if rating {
		return fmt.Sprintf("and (%s,coalesce(popularity.rating_count,0),b.updated_at,b.id) %s ($%d::numeric,$%d,$%d,$%d)",
				expression, operator, startParameter, startParameter+1, startParameter+2, startParameter+3),
			[]any{cursor.Metric, cursor.RatingCount, cursor.UpdatedAt, cursor.ID}
	}
	return fmt.Sprintf("and (%s,b.updated_at,b.id) %s ($%d::numeric,$%d,$%d)",
			expression, operator, startParameter, startParameter+1, startParameter+2),
		[]any{cursor.Metric, cursor.UpdatedAt, cursor.ID}
}

func blueprintSQLPageCursor(request blueprintPageRequest, row blueprintPageRow) string {
	cursor := blueprintPageCursor{
		Version: blueprintPageCursorVersion, Scope: request.Scope, Sort: request.Sort,
		Direction: request.Direction, ID: row.InternalID, Name: row.SortName,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, RatingCount: row.RatingCount,
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
	return encodeBlueprintPageCursor(cursor)
}
