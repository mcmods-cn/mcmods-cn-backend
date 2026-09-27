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

const (
	favoritePageCursorVersion = 1
	defaultFavoritePageLimit  = 20
	maximumFavoritePageLimit  = 100
)

type favoriteCollectionPageRequest struct {
	OwnerID        int64
	IncludePrivate bool
	ViewerID       int64
	Moderator      bool
	Limit          int
	Scope          string
	Cursor         *favoriteCollectionPageCursor
}

type favoriteCollectionPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"scope"`
	IsDefault bool      `json:"isDefault"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

type favoriteCollectionPageRow struct {
	ID        int64
	CreatedAt time.Time
	Summary   favoriteCollectionSummary
}

type favoriteItemPageRequest struct {
	OwnerID            int64
	IncludePrivate     bool
	CollectionPublicID string
	ViewerID           int64
	Moderator          bool
	Limit              int
	Scope              string
	Cursor             *favoriteItemPageCursor
}

type favoriteItemPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

type favoriteItemPageRow struct {
	ID        int64
	CreatedAt time.Time
	Item      favoriteCollectionItemRow
}

type favoriteRowScanner interface {
	Scan(destinations ...any) error
}

type favoriteRows interface {
	favoriteRowScanner
	Next() bool
	Err() error
	Close()
}

type favoriteMembershipSummaryRows struct {
	PublicIDs             []string
	CollectionIDsByEntity map[string][]string
}

type favoriteMembershipSummaryRequest struct {
	EntityType      string   `json:"entityType"`
	EntityPublicIDs []string `json:"entityPublicIds"`
	CollectionIDs   []string `json:"collectionIds,omitempty"`
}

type favoriteMembershipDeltaRequest struct {
	EntityType          string   `json:"entityType"`
	EntityPublicID      string   `json:"entityPublicId"`
	AddCollectionIDs    []string `json:"addCollectionIds"`
	RemoveCollectionIDs []string `json:"removeCollectionIds"`
}

func parseFavoriteCollectionPageRequest(values url.Values, ownerID int64, includePrivate bool, claims security.Claims) (favoriteCollectionPageRequest, error) {
	if ownerID <= 0 {
		return favoriteCollectionPageRequest{}, errors.New("invalid favorite collection owner")
	}
	limit, rawCursor, err := parseFavoritePageQuery(values)
	if err != nil {
		return favoriteCollectionPageRequest{}, err
	}
	request := favoriteCollectionPageRequest{
		OwnerID: ownerID, IncludePrivate: includePrivate, ViewerID: claims.Subject,
		Moderator: favoriteClaimsModerator(claims), Limit: limit,
	}
	request.Scope = favoriteCollectionPageScope(request)
	request.Cursor, err = decodeFavoriteCollectionPageCursor(rawCursor, request.Scope)
	return request, err
}

func parseFavoriteItemPageRequest(values url.Values, ownerID int64, includePrivate bool, collectionPublicID string, claims security.Claims) (favoriteItemPageRequest, error) {
	collectionPublicID = strings.ToLower(strings.TrimSpace(collectionPublicID))
	if ownerID <= 0 || !validCatalogPublicID(collectionPublicID) {
		return favoriteItemPageRequest{}, errors.New("invalid favorite collection")
	}
	limit, rawCursor, err := parseFavoritePageQuery(values)
	if err != nil {
		return favoriteItemPageRequest{}, err
	}
	request := favoriteItemPageRequest{
		OwnerID: ownerID, IncludePrivate: includePrivate, CollectionPublicID: collectionPublicID,
		ViewerID: claims.Subject, Moderator: favoriteClaimsModerator(claims), Limit: limit,
	}
	request.Scope = favoriteItemPageScope(request)
	request.Cursor, err = decodeFavoriteItemPageCursor(rawCursor, request.Scope)
	return request, err
}

func parseFavoritePageQuery(values url.Values) (int, string, error) {
	allowed := map[string]bool{"limit": true, "cursor": true}
	for key, items := range values {
		if !allowed[key] {
			return 0, "", fmt.Errorf("unsupported favorite query parameter %q", key)
		}
		if len(items) != 1 {
			return 0, "", fmt.Errorf("favorite query parameter %q must appear exactly once", key)
		}
	}
	limit := defaultFavoritePageLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > maximumFavoritePageLimit {
			return 0, "", errors.New("invalid favorite page size")
		}
		limit = parsed
	}
	return limit, values.Get("cursor"), nil
}

func favoriteClaimsModerator(claims security.Claims) bool {
	return claimsAllow(claims, "admin.*") || claimsAllow(claims, "project.review")
}

func favoriteCollectionPageScope(request favoriteCollectionPageRequest) string {
	return favoritePageScope(struct {
		Version        int   `json:"version"`
		OwnerID        int64 `json:"ownerId"`
		IncludePrivate bool  `json:"includePrivate"`
		ViewerID       int64 `json:"viewerId"`
		Moderator      bool  `json:"moderator"`
		Limit          int   `json:"limit"`
	}{favoritePageCursorVersion, request.OwnerID, request.IncludePrivate, request.ViewerID, request.Moderator, request.Limit})
}

func favoriteItemPageScope(request favoriteItemPageRequest) string {
	return favoritePageScope(struct {
		Version            int    `json:"version"`
		OwnerID            int64  `json:"ownerId"`
		IncludePrivate     bool   `json:"includePrivate"`
		CollectionPublicID string `json:"collectionPublicId"`
		ViewerID           int64  `json:"viewerId"`
		Moderator          bool   `json:"moderator"`
		Limit              int    `json:"limit"`
	}{
		favoritePageCursorVersion, request.OwnerID, request.IncludePrivate, request.CollectionPublicID,
		request.ViewerID, request.Moderator, request.Limit,
	})
}

func favoritePageScope(value any) string {
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:16])
}

func encodeFavoritePageCursor(value any) string {
	payload, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeFavoriteCollectionPageCursor(raw, scope string) (*favoriteCollectionPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var cursor favoriteCollectionPageCursor
	if err := decodeFavoritePageCursor(raw, &cursor); err != nil || cursor.Version != favoritePageCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid favorite collection cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}

func decodeFavoriteItemPageCursor(raw, scope string) (*favoriteItemPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var cursor favoriteItemPageCursor
	if err := decodeFavoritePageCursor(raw, &cursor); err != nil || cursor.Version != favoritePageCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid favorite item cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}

func decodeFavoritePageCursor(raw string, destination any) error {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return errors.New("invalid favorite cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(payload) == 0 || len(payload) > 2048 {
		return errors.New("invalid favorite cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		return err
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid favorite cursor")
	}
	return nil
}

func favoriteCollectionPageSQL(request favoriteCollectionPageRequest) (string, []any) {
	arguments := []any{request.OwnerID, request.IncludePrivate}
	query := `select collection.id,collection.public_id,collection.name,collection.is_default,collection.is_public,collection.created_at
		from favorite_collections collection
		where collection.user_id=$1 and ($2 or collection.is_public)`
	if request.Cursor != nil {
		arguments = append(arguments, request.Cursor.IsDefault, request.Cursor.CreatedAt, request.Cursor.ID)
		query += fmt.Sprintf(` and (collection.is_default<$%d or (collection.is_default=$%d and
			(collection.created_at>$%d or (collection.created_at=$%d and collection.id>$%d))))`,
			len(arguments)-2, len(arguments)-2, len(arguments)-1, len(arguments)-1, len(arguments))
	}
	arguments = append(arguments, request.Limit+1)
	query += fmt.Sprintf(` order by collection.is_default desc,collection.created_at,collection.id limit $%d`, len(arguments))
	return query, arguments
}

func favoriteItemPageSQL(request favoriteItemPageRequest) (string, []any) {
	arguments := []any{request.OwnerID, request.IncludePrivate, request.ViewerID, request.Moderator, request.CollectionPublicID}
	query := `select item.id,item.created_at,item.entity_type,route.public_id,
		coalesce(mods.primary_name,''),coalesce(mods.secondary_name,''),coalesce(mods.icon_url,''),coalesce(mods.slug,''),
		coalesce(modpack.primary_name,''),coalesce(modpack.secondary_name,''),coalesce(modpack.icon_url,''),coalesce(modpack.slug,''),
		coalesce(blueprint.title,''),coalesce(blueprint.public_id,''),coalesce(blueprint.cover_object_key,'')
		from favorite_collection_items item
		` + favoriteTargetJoinsSQL + `
		where item.collection_id=(select collection.id from favorite_collections collection
			where collection.user_id=$1 and ($2 or collection.is_public) and collection.public_id=$5)
		  and ` + favoriteTargetVisibilitySQL("$3", "$4")
	if request.Cursor != nil {
		arguments = append(arguments, request.Cursor.CreatedAt, request.Cursor.ID)
		query += fmt.Sprintf(` and (item.created_at<$%d or (item.created_at=$%d and item.id<$%d))`,
			len(arguments)-1, len(arguments)-1, len(arguments))
	}
	arguments = append(arguments, request.Limit+1)
	query += fmt.Sprintf(` order by item.created_at desc,item.id desc limit $%d`, len(arguments))
	return query, arguments
}

func favoriteCollectionNextCursor(request favoriteCollectionPageRequest, row favoriteCollectionPageRow) string {
	return encodeFavoritePageCursor(favoriteCollectionPageCursor{
		Version: favoritePageCursorVersion, Scope: request.Scope, IsDefault: row.Summary.IsDefault,
		CreatedAt: row.CreatedAt.UTC(), ID: row.ID,
	})
}

func favoriteItemNextCursor(request favoriteItemPageRequest, row favoriteItemPageRow) string {
	return encodeFavoritePageCursor(favoriteItemPageCursor{
		Version: favoritePageCursorVersion, Scope: request.Scope, CreatedAt: row.CreatedAt.UTC(), ID: row.ID,
	})
}

func scanFavoriteCollectionPageRow(scanner favoriteRowScanner) (favoriteCollectionPageRow, error) {
	var row favoriteCollectionPageRow
	err := scanner.Scan(&row.ID, &row.Summary.ID, &row.Summary.Name, &row.Summary.IsDefault,
		&row.Summary.IsPublic, &row.CreatedAt)
	return row, err
}

func scanFavoriteItemPageRow(scanner favoriteRowScanner) (favoriteItemPageRow, error) {
	var row favoriteItemPageRow
	err := scanner.Scan(&row.ID, &row.CreatedAt, &row.Item.entityType, &row.Item.entityPublicID,
		&row.Item.primaryName, &row.Item.secondaryName, &row.Item.iconURL, &row.Item.slug,
		&row.Item.modpackPrimaryName, &row.Item.modpackSecondaryName, &row.Item.modpackIconURL, &row.Item.modpackSlug,
		&row.Item.title, &row.Item.publicID, &row.Item.blueprintIconURL)
	return row, err
}

func collectFavoriteCollectionPageRows(rows favoriteRows, capacity int) ([]favoriteCollectionPageRow, error) {
	defer rows.Close()
	result := make([]favoriteCollectionPageRow, 0, capacity)
	for rows.Next() {
		row, err := scanFavoriteCollectionPageRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func collectFavoriteItemPageRows(rows favoriteRows, capacity int) ([]favoriteItemPageRow, error) {
	defer rows.Close()
	result := make([]favoriteItemPageRow, 0, capacity)
	for rows.Next() {
		row, err := scanFavoriteItemPageRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func collectFavoriteMembershipSummaryRows(rows favoriteRows, withCollections bool, capacity int) (favoriteMembershipSummaryRows, error) {
	defer rows.Close()
	result := favoriteMembershipSummaryRows{
		PublicIDs:             make([]string, 0, capacity),
		CollectionIDsByEntity: make(map[string][]string),
	}
	for rows.Next() {
		var publicID string
		if withCollections {
			var collectionID string
			if err := rows.Scan(&publicID, &collectionID); err != nil {
				return favoriteMembershipSummaryRows{}, err
			}
			result.CollectionIDsByEntity[publicID] = append(result.CollectionIDsByEntity[publicID], collectionID)
		} else if err := rows.Scan(&publicID); err != nil {
			return favoriteMembershipSummaryRows{}, err
		}
		if len(result.PublicIDs) == 0 || result.PublicIDs[len(result.PublicIDs)-1] != publicID {
			result.PublicIDs = append(result.PublicIDs, publicID)
		}
	}
	if err := rows.Err(); err != nil {
		return favoriteMembershipSummaryRows{}, err
	}
	return result, nil
}

func normalizeFavoriteMembershipSummaryRequest(request favoriteMembershipSummaryRequest) (favoriteMembershipSummaryRequest, error) {
	request.EntityType = strings.ToLower(strings.TrimSpace(request.EntityType))
	if _, ok := favoriteTargetTypes[request.EntityType]; !ok {
		return favoriteMembershipSummaryRequest{}, errors.New("invalid favorite target type")
	}
	if len(request.EntityPublicIDs) < 1 || len(request.EntityPublicIDs) > maximumFavoritePageLimit {
		return favoriteMembershipSummaryRequest{}, errors.New("favorite membership summary requires 1 to 100 targets")
	}
	seen := make(map[string]struct{}, len(request.EntityPublicIDs))
	publicIDs := make([]string, 0, len(request.EntityPublicIDs))
	for _, rawPublicID := range request.EntityPublicIDs {
		publicID := strings.ToLower(strings.TrimSpace(rawPublicID))
		if !validCatalogPublicID(publicID) {
			return favoriteMembershipSummaryRequest{}, errors.New("invalid favorite target public ID")
		}
		if _, duplicate := seen[publicID]; duplicate {
			continue
		}
		seen[publicID] = struct{}{}
		publicIDs = append(publicIDs, publicID)
	}
	request.EntityPublicIDs = publicIDs
	collectionIDs, err := normalizeFavoriteCollectionIDs(request.CollectionIDs, true)
	if err != nil {
		return favoriteMembershipSummaryRequest{}, err
	}
	if len(collectionIDs) > 0 && len(publicIDs)*len(collectionIDs) > maximumFavoritePageLimit {
		return favoriteMembershipSummaryRequest{}, errors.New("favorite membership summary matrix is too large")
	}
	request.CollectionIDs = collectionIDs
	return request, nil
}

func favoriteMembershipSummarySQL(request favoriteMembershipSummaryRequest, userID int64, moderator bool) (string, []any) {
	arguments := []any{userID, request.EntityType, request.EntityPublicIDs, moderator}
	if len(request.CollectionIDs) == 0 {
		query := `select target.public_id
			from public_routes target
			join lateral (select 1 from favorite_collection_items item
				join favorite_collections collection on collection.id=item.collection_id
				` + favoriteTargetJoinsSQL + `
				where collection.user_id=$1 and item.entity_type=target.entity_type and item.entity_id=target.internal_id
				  and ` + favoriteTargetVisibilitySQL("$1", "$4") + ` limit 1) membership on true
			where target.entity_type=$2 and target.public_id=any($3::text[])
			order by target.public_id`
		return query, arguments
	}
	arguments = append(arguments, request.CollectionIDs)
	query := `select distinct target.public_id,collection.public_id
		from public_routes target
		join favorite_collection_items item on item.entity_type=target.entity_type and item.entity_id=target.internal_id
		join favorite_collections collection on collection.id=item.collection_id
		` + favoriteTargetJoinsSQL + `
		where collection.user_id=$1 and target.entity_type=$2 and target.public_id=any($3::text[])
		  and collection.public_id=any($5::text[]) and ` + favoriteTargetVisibilitySQL("$1", "$4") + `
		order by target.public_id,collection.public_id`
	return query, arguments
}

func normalizeFavoriteMembershipDeltaRequest(request favoriteMembershipDeltaRequest) (favoriteMembershipDeltaRequest, error) {
	request.EntityType = strings.ToLower(strings.TrimSpace(request.EntityType))
	request.EntityPublicID = strings.ToLower(strings.TrimSpace(request.EntityPublicID))
	if _, ok := favoriteTargetTypes[request.EntityType]; !ok || !validCatalogPublicID(request.EntityPublicID) {
		return favoriteMembershipDeltaRequest{}, errors.New("invalid favorite target")
	}
	add, err := normalizeFavoriteCollectionIDs(request.AddCollectionIDs, true)
	if err != nil {
		return favoriteMembershipDeltaRequest{}, err
	}
	remove, err := normalizeFavoriteCollectionIDs(request.RemoveCollectionIDs, true)
	if err != nil {
		return favoriteMembershipDeltaRequest{}, err
	}
	if len(add)+len(remove) > maximumFavoritePageLimit {
		return favoriteMembershipDeltaRequest{}, errors.New("favorite membership update is too large")
	}
	removed := make(map[string]struct{}, len(remove))
	for _, collectionID := range remove {
		removed[collectionID] = struct{}{}
	}
	for _, collectionID := range add {
		if _, conflict := removed[collectionID]; conflict {
			return favoriteMembershipDeltaRequest{}, errors.New("favorite collection cannot be both added and removed")
		}
	}
	request.AddCollectionIDs = add
	request.RemoveCollectionIDs = remove
	return request, nil
}

func normalizeFavoriteCollectionIDs(values []string, allowEmpty bool) ([]string, error) {
	if len(values) > maximumFavoritePageLimit || (!allowEmpty && len(values) == 0) {
		return nil, errors.New("invalid favorite collection list")
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, rawValue := range values {
		value := strings.ToLower(strings.TrimSpace(rawValue))
		if !validCatalogPublicID(value) {
			return nil, errors.New("invalid favorite collection public ID")
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}
