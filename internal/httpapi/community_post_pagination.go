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

	"mcmods-cn-backend/internal/security"
)

const communityPostPageCursorVersion = 1

type communityPostPageRequest struct {
	Kind, Query, Category, VersionMode, ModID, ResourceID string
	Versions, ProjectFilters                              []string
	Sort                                                  catalogSort
	Direction                                             catalogSortDirection
	Limit                                                 int
	ViewerID                                              int64
	Moderator                                             bool
	Scope                                                 string
	Cursor                                                *communityPostPageCursor
}

type communityPostPageCursor struct {
	Version     int                  `json:"v"`
	Scope       string               `json:"s"`
	Sort        catalogSort          `json:"sort"`
	Direction   catalogSortDirection `json:"direction"`
	ID          int64                `json:"id"`
	Name        string               `json:"name,omitempty"`
	PublishedAt time.Time            `json:"publishedAt,omitempty"`
	UpdatedAt   time.Time            `json:"updatedAt,omitempty"`
	Metric      string               `json:"metric,omitempty"`
	RatingCount int64                `json:"ratingCount,omitempty"`
}

type communityPostPageRow struct {
	ID                int64
	Item              communityPostResponse
	AuthorInternalID  int64
	CoverKey          string
	PublishedAt       time.Time
	UpdatedAt         time.Time
	SortName          string
	Heat, Downloads   string
	Favorites, Rating string
	Views, Comments   string
	Relevance         string
	RatingCount       int64
}

func parseCommunityPostPageRequest(values url.Values, claims security.Claims) (communityPostPageRequest, error) {
	allowed := map[string]bool{
		"kind": true, "q": true, "category": true, "version": true, "versionMode": true,
		"project": true, "sort": true, "order": true, "modId": true, "resourceId": true,
		"limit": true, "cursor": true,
	}
	for key, items := range values {
		if !allowed[key] || len(items) != 1 {
			return communityPostPageRequest{}, fmt.Errorf("invalid community post query parameter %q", key)
		}
	}
	kind := normalizeCommunityPostKind(values.Get("kind"))
	if kind == "" {
		return communityPostPageRequest{}, errors.New("invalid community post kind")
	}
	query, valid := parseCatalogQuery(values.Get("q"))
	if !valid {
		return communityPostPageRequest{}, errors.New("invalid community post query")
	}
	category := strings.ToLower(strings.TrimSpace(values.Get("category")))
	if category != "" && !communityPostCategoryAllowed(kind, category) {
		return communityPostPageRequest{}, errors.New("invalid community post category")
	}
	versions, valid := parseCatalogList(values.Get("version"), 20)
	if !valid {
		return communityPostPageRequest{}, errors.New("invalid Minecraft version filter")
	}
	versionMode, valid := parseCatalogVersionMode(values.Get("versionMode"))
	if !valid {
		return communityPostPageRequest{}, errors.New("invalid version mode")
	}
	projectFilters, valid := parseCommunityProjectFilters(values.Get("project"))
	if !valid {
		return communityPostPageRequest{}, errors.New("invalid community project filter")
	}
	rawSort := strings.TrimSpace(values.Get("sort"))
	if rawSort == "" {
		rawSort = string(catalogSortPublished)
	}
	sortField, validSort := parseCatalogSort(rawSort)
	direction, validDirection := parseCatalogSortDirection(values.Get("order"), sortField)
	if !validSort || !validDirection {
		return communityPostPageRequest{}, errors.New("invalid community post sort")
	}
	limit := 24
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsed < 1 || parsed > 100 {
			return communityPostPageRequest{}, errors.New("invalid community post page size")
		}
		limit = parsed
	}
	request := communityPostPageRequest{
		Kind: kind, Query: query, Category: category, Versions: versions, VersionMode: versionMode,
		ProjectFilters: projectFilters, Sort: sortField, Direction: direction, Limit: limit,
		ModID:      strings.ToLower(strings.TrimSpace(values.Get("modId"))),
		ResourceID: strings.ToLower(strings.TrimSpace(values.Get("resourceId"))),
		ViewerID:   claims.Subject,
		Moderator:  claimsAllow(claims, "content.review") || claimsAllow(claims, "admin.*"),
	}
	request.Scope = communityPostPageScope(request)
	cursor, err := decodeCommunityPostPageCursor(values.Get("cursor"), request)
	if err != nil {
		return communityPostPageRequest{}, err
	}
	request.Cursor = cursor
	return request, nil
}

func communityPostPageScope(request communityPostPageRequest) string {
	payload, _ := json.Marshal(struct {
		Version                                               int `json:"version"`
		Kind, Query, Category, VersionMode, ModID, ResourceID string
		Versions, ProjectFilters                              []string
		Sort                                                  catalogSort
		Direction                                             catalogSortDirection
		Limit                                                 int
		ViewerID                                              int64
		Moderator                                             bool
	}{communityPostPageCursorVersion, request.Kind, request.Query, request.Category, request.VersionMode,
		request.ModID, request.ResourceID, request.Versions, request.ProjectFilters, request.Sort,
		request.Direction, request.Limit, request.ViewerID, request.Moderator})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:16])
}

func decodeCommunityPostPageCursor(raw string, request communityPostPageRequest) (*communityPostPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid community post cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(payload) == 0 || len(payload) > 2048 {
		return nil, errors.New("invalid community post cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor communityPostPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid community post cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != communityPostPageCursorVersion ||
		cursor.Scope != request.Scope || cursor.Sort != request.Sort || cursor.Direction != request.Direction ||
		cursor.ID <= 0 || !validCommunityPostPageCursor(cursor) {
		return nil, errors.New("invalid community post cursor")
	}
	cursor.PublishedAt = cursor.PublishedAt.UTC()
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	return &cursor, nil
}

func validCommunityPostPageCursor(cursor communityPostPageCursor) bool {
	switch cursor.Sort {
	case catalogSortName:
		return strings.TrimSpace(cursor.Name) != ""
	case catalogSortPublished:
		return !cursor.PublishedAt.IsZero() && !cursor.UpdatedAt.IsZero()
	case catalogSortUpdated, catalogSortCollected:
		return !cursor.UpdatedAt.IsZero()
	case catalogSortRating:
		return validNonNegativeDecimal(cursor.Metric) && cursor.RatingCount >= 0 && !cursor.UpdatedAt.IsZero()
	default:
		return validNonNegativeDecimal(cursor.Metric) && !cursor.UpdatedAt.IsZero()
	}
}

func communityPostPageSQL(request communityPostPageRequest) (string, []any) {
	arguments := []any{request.Kind, request.ViewerID, request.Moderator, request.Query, request.ModID,
		request.ResourceID, request.Category, request.Versions, request.VersionMode, request.ProjectFilters}
	predicate, cursorArguments := communityPostCursorPredicateSQL(request.Cursor, 11)
	arguments = append(arguments, cursorArguments...)
	arguments = append(arguments, request.Limit+1)
	query := `select post.id,post.public_id,post.kind,post.category,post.title,post.source_locale,post.body_summary,
		post.minecraft_versions,post.mod_version_min,post.mod_version_max,post.severity,post.has_fix,post.issue_url,
		coalesce(file.public_id,''),coalesce(file.object_key,''),author.public_id,author.username,post.review_status,
		post.created_at,post.updated_at,post.author_id,post.resolution_status,coalesce(accepted.public_id,''),post.resolved_at,
		post.published_at,lower(post.title),post.heat_score::text,post.download_count::text,post.favorite_count::text,
		post.bayesian_rating::text,post.rating_count,post.view_count::text,post.comment_count::text,
		` + communityPostRelevanceExpression() + `::text
		from community_post_catalog post join users author on author.id=post.author_id
		left join oss_files file on file.id=post.cover_file_id and file.status='active'
		left join comments accepted on accepted.id=post.accepted_comment_id
		where post.kind=$1 and ` + communityPostVisibilityPredicateSQL(request) + `
		and ($4='' or post.search_document @@ websearch_to_tsquery('simple',$4))
		and ($5='' or exists(select 1 from community_post_project_refs ref join public_routes route
			on route.internal_id=ref.target_id and route.entity_type=ref.target_type where ref.post_id=post.id
			and route.public_id=$5 and ` + followProjectTargetVisibilitySQL("route", "$2", "$3") + `))
		and ($6='' or exists(select 1 from community_post_resource_refs ref join catalog_entities entity
			on entity.id=ref.resource_id where ref.post_id=post.id and entity.public_id=$6))
		and ($7='' or post.category=$7)
		and (cardinality($8::text[])=0 or $9='all' and post.minecraft_versions @> $8::text[]
			or $9='any' and post.minecraft_versions && $8::text[])
		and (cardinality($10::text[])=0 or exists(select 1 from community_post_project_refs ref
			left join public_routes route on route.internal_id=ref.target_id and route.entity_type=ref.target_type
			where ref.post_id=post.id and (ref.target_id is null and lower(ref.target_type||':'||ref.raw_identifier)=any($10::text[])
				or ref.target_id is not null and lower(ref.target_type||':'||route.public_id)=any($10::text[])
					and ` + followProjectTargetVisibilitySQL("route", "$2", "$3") + `)))
		` + predicate + ` order by ` + communityPostOrderSQL(request) + fmt.Sprintf(" limit $%d", len(arguments))
	return query, arguments
}

func communityPostVisibilityPredicateSQL(request communityPostPageRequest) string {
	if request.Moderator {
		return "true"
	}
	if request.ViewerID > 0 {
		return "(post.review_status='approved' or post.author_id=$2)"
	}
	return "post.review_status='approved'"
}

func communityPostRelevanceExpression() string {
	return `case when $4='' then post.heat_score else ts_rank_cd(post.search_document,websearch_to_tsquery('simple',$4)) end`
}

func communityPostSortExpression(sortField catalogSort) string {
	switch sortField {
	case catalogSortDownloads:
		return "post.download_count"
	case catalogSortFavorites:
		return "post.favorite_count"
	case catalogSortRating:
		return "post.bayesian_rating"
	case catalogSortViews:
		return "post.view_count"
	case catalogSortComments:
		return "post.comment_count"
	case catalogSortRelevance:
		return communityPostRelevanceExpression()
	default:
		return "post.heat_score"
	}
}

func communityPostOrderSQL(request communityPostPageRequest) string {
	direction := "desc"
	if request.Direction == catalogSortAscending {
		direction = "asc"
	}
	stable := "post.updated_at " + direction + ",post.id " + direction
	switch request.Sort {
	case catalogSortName:
		return "lower(post.title) " + direction + ",post.id " + direction
	case catalogSortPublished:
		return "post.published_at " + direction + "," + stable
	case catalogSortUpdated, catalogSortCollected:
		return stable
	case catalogSortRating:
		return "post.bayesian_rating " + direction + ",post.rating_count " + direction + "," + stable
	default:
		return communityPostSortExpression(request.Sort) + " " + direction + "," + stable
	}
}

func communityPostCursorPredicateSQL(cursor *communityPostPageCursor, start int) (string, []any) {
	if cursor == nil {
		return "", nil
	}
	operator := ">"
	if cursor.Direction == catalogSortDescending {
		operator = "<"
	}
	switch cursor.Sort {
	case catalogSortName:
		return fmt.Sprintf("and (lower(post.title),post.id) %s ($%d,$%d)", operator, start, start+1), []any{cursor.Name, cursor.ID}
	case catalogSortPublished:
		return fmt.Sprintf("and (post.published_at,post.updated_at,post.id) %s ($%d,$%d,$%d)", operator, start, start+1, start+2), []any{cursor.PublishedAt, cursor.UpdatedAt, cursor.ID}
	case catalogSortUpdated, catalogSortCollected:
		return fmt.Sprintf("and (post.updated_at,post.id) %s ($%d,$%d)", operator, start, start+1), []any{cursor.UpdatedAt, cursor.ID}
	case catalogSortRating:
		return fmt.Sprintf("and (post.bayesian_rating,post.rating_count,post.updated_at,post.id) %s ($%d::numeric,$%d,$%d,$%d)", operator, start, start+1, start+2, start+3), []any{cursor.Metric, cursor.RatingCount, cursor.UpdatedAt, cursor.ID}
	default:
		return fmt.Sprintf("and (%s,post.updated_at,post.id) %s ($%d::numeric,$%d,$%d)", communityPostSortExpression(cursor.Sort), operator, start, start+1, start+2), []any{cursor.Metric, cursor.UpdatedAt, cursor.ID}
	}
}

func communityPostNextCursor(request communityPostPageRequest, row communityPostPageRow) string {
	cursor := communityPostPageCursor{Version: communityPostPageCursorVersion, Scope: request.Scope,
		Sort: request.Sort, Direction: request.Direction, ID: row.ID, Name: row.SortName,
		PublishedAt: row.PublishedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(), RatingCount: row.RatingCount}
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
	case catalogSortRelevance:
		cursor.Metric = row.Relevance
	case catalogSortName, catalogSortPublished, catalogSortUpdated, catalogSortCollected:
	default:
		cursor.Metric = row.Heat
	}
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}
