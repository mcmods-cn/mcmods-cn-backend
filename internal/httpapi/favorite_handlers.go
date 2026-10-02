package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

type favoriteCollectionSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
	IsPublic  bool   `json:"isPublic"`
}

type favoriteCollectionPageResponse struct {
	Items      []favoriteCollectionSummary `json:"items"`
	Limit      int                         `json:"limit"`
	HasMore    bool                        `json:"hasMore"`
	NextCursor string                      `json:"nextCursor"`
}

type favoriteCollectionItemPageResponse struct {
	Items      []favoriteCollectionItem `json:"items"`
	Limit      int                      `json:"limit"`
	HasMore    bool                     `json:"hasMore"`
	NextCursor string                   `json:"nextCursor"`
}

type favoriteCollectionItem struct {
	EntityType     string         `json:"entityType"`
	EntityPublicID string         `json:"entityPublicId"`
	Metadata       map[string]any `json:"metadata"`
}

type favoriteCollectionItemRow struct {
	entityType, entityPublicID, primaryName, secondaryName, iconURL, slug string
	modpackPrimaryName, modpackSecondaryName, modpackIconURL              string
	modpackSlug, title, publicID, blueprintIconURL                        string
}

type favoriteQueryer interface {
	followProjectQueryer
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

var favoriteTargetTypes = map[string]struct{}{
	"mod": {}, "modpack": {}, "blueprint": {},
}

var errFavoriteCollectionNotOwned = errors.New("favorite collection does not exist")

const favoriteTargetJoinsSQL = `
	left join public_routes route on route.entity_type=item.entity_type and route.internal_id=item.entity_id
	left join mods on item.entity_type='mod' and mods.id=item.entity_id
	left join modpacks modpack on item.entity_type='modpack' and modpack.id=item.entity_id
	left join blueprints blueprint on item.entity_type='blueprint' and blueprint.id=item.entity_id`

// Collection ownership never substitutes for target visibility. Callers bind
// the current viewer and their moderator flag explicitly in every read query.
const favoriteTargetVisibilityTemplate = `item.entity_type in ('mod','modpack','blueprint') and case item.entity_type
	when 'mod' then mods.id is not null and (mods.review_status='approved' or mods.submitted_by={viewer} or {moderator})
	when 'modpack' then modpack.id is not null and (modpack.review_status='approved' or modpack.submitted_by={viewer} or {moderator})
	when 'blueprint' then blueprint.id is not null and blueprint.status<>'deleted' and (
		(blueprint.status in ('ready','partial') and blueprint.review_status in ('not_required','approved')) or blueprint.owner_id={viewer} or {moderator})
	else false end`

func favoriteTargetVisibilitySQL(viewerParameter, moderatorParameter string) string {
	return strings.NewReplacer("{viewer}", viewerParameter, "{moderator}", moderatorParameter).Replace(favoriteTargetVisibilityTemplate)
}

func (s *Server) ensureDefaultFavoriteCollection(ctx context.Context, userID int64) (int64, error) {
	var existingID int64
	err := s.db.QueryRow(ctx, `select id from favorite_collections where user_id=$1 and is_default`, userID).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	id, err := ensureDefaultFavoriteCollectionWithQueryer(ctx, tx, userID, s.favoriteQuotaPolicy())
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

func ensureDefaultFavoriteCollectionWithQueryer(ctx context.Context, tx pgx.Tx, userID int64, policy favoriteQuotaPolicy) (int64, error) {
	if err := lockFavoriteStockQuotaTx(ctx, tx, userID); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(ctx, `select id from favorite_collections where user_id=$1 and is_default`, userID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if err = enforceFavoriteCollectionGrowthTx(ctx, tx, userID, 1, policy); err != nil {
		return 0, err
	}
	err = tx.QueryRow(ctx, `insert into favorite_collections(user_id,name,is_default)
		values($1,'default',true)
		on conflict(user_id) where is_default do update set updated_at=favorite_collections.updated_at
		returning id`, userID).Scan(&id)
	return id, err
}

func validateFavoriteCollectionsTx(ctx context.Context, tx pgx.Tx, userID int64, collectionIDs []string) error {
	if len(collectionIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `select public_id from favorite_collections
		where user_id=$1 and public_id=any($2::text[]) order by id for update`, userID, collectionIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	validated := 0
	for rows.Next() {
		var publicID string
		if err = rows.Scan(&publicID); err != nil {
			return err
		}
		validated++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if validated != len(collectionIDs) {
		return errFavoriteCollectionNotOwned
	}
	return nil
}

func (s *Server) favoriteCollections(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	userID := claims.Subject
	if _, err := s.ensureDefaultFavoriteCollection(r.Context(), userID); err != nil {
		if writeFavoriteQuotaError(w, err, s.favoriteQuotaPolicy()) {
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create the default favorite collection")
		return
	}
	s.writeFavoriteCollectionPage(w, r, userID, true, claims)
}

func (s *Server) publicFavoriteCollections(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	s.writeFavoriteCollectionPage(w, r, identity.InternalID, false, currentClaims(r))
}

func (s *Server) writeFavoriteCollectionPage(w http.ResponseWriter, r *http.Request, ownerID int64, includePrivate bool, claims security.Claims) {
	page, err := parseFavoriteCollectionPageRequest(r.URL.Query(), ownerID, includePrivate, claims)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query, arguments := favoriteCollectionPageSQL(page)
	rows, err := s.db.Query(r.Context(), query, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite collections")
		return
	}
	pageRows, err := collectFavoriteCollectionPageRows(rows, page.Limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite collections")
		return
	}
	hasMore := len(pageRows) > page.Limit
	if hasMore {
		pageRows = pageRows[:page.Limit]
	}
	items := make([]favoriteCollectionSummary, 0, len(pageRows))
	for _, row := range pageRows {
		items = append(items, row.Summary)
	}
	nextCursor := ""
	if hasMore && len(pageRows) > 0 {
		nextCursor = favoriteCollectionNextCursor(page, pageRows[len(pageRows)-1])
	}
	writeJSON(w, http.StatusOK, favoriteCollectionPageResponse{
		Items: items, Limit: page.Limit, HasMore: hasMore, NextCursor: nextCursor,
	})
}

func logFavoriteCollectionWriteFailure(operation, publicID string, userID int64, err error) {
	slog.Error("favorite collection write failed",
		"module", "favorite",
		"operation", operation,
		"collection_id", publicID,
		"user_id", userID,
		"error", err,
	)
}

func (s *Server) createFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name     string `json:"name"`
		IsPublic bool   `json:"isPublic"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid favorite collection")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len([]rune(request.Name)) > 60 {
		writeError(w, http.StatusBadRequest, "favorite collection name must contain 1 to 60 characters")
		return
	}
	userID := currentClaims(r).Subject
	policy := s.favoriteQuotaPolicy()
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		logFavoriteCollectionWriteFailure("create", "", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to create favorite collection")
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockFavoriteStockQuotaTx(r.Context(), tx, userID); err == nil {
		err = enforceFavoriteCollectionGrowthTx(r.Context(), tx, userID, 1, policy)
	}
	if err != nil {
		if writeFavoriteQuotaError(w, err, policy) {
			return
		}
		logFavoriteCollectionWriteFailure("create", "", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to create favorite collection")
		return
	}
	var item favoriteCollectionSummary
	err = tx.QueryRow(r.Context(), `insert into favorite_collections(user_id,name,is_public)
		values($1,$2,$3) returning public_id,name,is_default,is_public`,
		userID, request.Name, request.IsPublic).
		Scan(&item.ID, &item.Name, &item.IsDefault, &item.IsPublic)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a favorite collection with this name already exists")
			return
		}
		logFavoriteCollectionWriteFailure("create", "", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to create favorite collection")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		logFavoriteCollectionWriteFailure("create", item.ID, userID, err)
		writeError(w, http.StatusInternalServerError, "failed to create favorite collection")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	publicID, ok := publicPathID(w, r, "id")
	if !ok {
		return
	}
	var request struct {
		Name     *string `json:"name"`
		IsPublic *bool   `json:"isPublic"`
	}
	if decodeJSON(r, &request) != nil || (request.Name == nil && request.IsPublic == nil) {
		writeError(w, http.StatusBadRequest, "invalid favorite collection update")
		return
	}
	name := ""
	if request.Name != nil {
		name = strings.TrimSpace(*request.Name)
		if name == "" || len([]rune(name)) > 60 {
			writeError(w, http.StatusBadRequest, "favorite collection name must contain 1 to 60 characters")
			return
		}
	}
	var visibility any
	if request.IsPublic != nil {
		visibility = *request.IsPublic
	}
	claims := currentClaims(r)
	var item favoriteCollectionSummary
	err := s.db.QueryRow(r.Context(), `update favorite_collections set
		name=case when is_default or $3='' then name else $3 end,
		is_public=coalesce($4::boolean,is_public),updated_at=now()
		where public_id=$1 and user_id=$2
		returning public_id,name,is_default,is_public`,
		publicID, claims.Subject, name, visibility).
		Scan(&item.ID, &item.Name, &item.IsDefault, &item.IsPublic)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a favorite collection with this name already exists")
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "favorite collection was not found")
			return
		}
		logFavoriteCollectionWriteFailure("update", publicID, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "failed to update favorite collection")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	publicID, ok := publicPathID(w, r, "id")
	if !ok {
		return
	}
	claims := currentClaims(r)
	membershipTx, err := s.beginFavoriteMembershipTx(r.Context(), claims.Subject)
	if err != nil {
		logFavoriteCollectionWriteFailure("delete", publicID, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "failed to delete favorite collection")
		return
	}
	defer membershipTx.close()
	tx := membershipTx.tx
	var collectionID int64
	err = tx.QueryRow(r.Context(), `select id from favorite_collections
		where public_id=$1 and user_id=$2 and not is_default for update`, publicID, claims.Subject).Scan(&collectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "the default collection cannot be deleted, or the collection does not exist")
		return
	}
	if err != nil {
		logFavoriteCollectionWriteFailure("delete", publicID, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "failed to delete favorite collection")
		return
	}
	if _, err = tx.Exec(r.Context(), `update favorite_modpack_export_tasks set status='cancelled',stage='cancelled',
		error_code='SOURCE_COLLECTION_DELETED',error_detail='source favorite collection was deleted',finished_at=now(),
		lease_token='',lease_expires_at=null,updated_at=now()
		where collection_id=$1 and status in ('pending','processing')`, collectionID); err != nil {
		logFavoriteCollectionWriteFailure("delete", publicID, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "failed to delete favorite collection")
		return
	}
	// Remove children while the parent still identifies their owner. The
	// popularity trigger needs that identity for first/last membership facts.
	if _, err = tx.Exec(r.Context(), `delete from favorite_collection_items where collection_id=$1`, collectionID); err != nil {
		logFavoriteCollectionWriteFailure("delete", publicID, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "failed to delete favorite collection")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from favorite_collections where id=$1`, collectionID); err != nil {
		logFavoriteCollectionWriteFailure("delete", publicID, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "failed to delete favorite collection")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		logFavoriteCollectionWriteFailure("delete", publicID, claims.Subject, err)
		writeError(w, http.StatusInternalServerError, "failed to delete favorite collection")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) favoriteMembershipSummary(w http.ResponseWriter, r *http.Request) {
	var request favoriteMembershipSummaryRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid favorite membership summary")
		return
	}
	request, err := normalizeFavoriteMembershipSummaryRequest(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	claims := currentClaims(r)
	query, arguments := favoriteMembershipSummarySQL(request, claims.Subject, favoriteClaimsModerator(claims))
	rows, err := s.db.Query(r.Context(), query, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite membership summary")
		return
	}
	result, err := collectFavoriteMembershipSummaryRows(rows, len(request.CollectionIDs) > 0, len(request.EntityPublicIDs))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite membership summary")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entityPublicIds": result.PublicIDs, "collectionIdsByEntity": result.CollectionIDsByEntity,
	})
}

func (s *Server) patchFavoriteMembership(w http.ResponseWriter, r *http.Request) {
	var request favoriteMembershipDeltaRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid favorite membership update")
		return
	}
	request, err := normalizeFavoriteMembershipDeltaRequest(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	claims := currentClaims(r)
	membershipTx, err := s.beginFavoriteMembershipTx(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save favorites")
		return
	}
	defer membershipTx.close()
	tx := membershipTx.tx
	target, err := resolveFavoriteMutationTargetWithQueryer(r.Context(), tx, request.EntityType, request.EntityPublicID, claims, len(request.AddCollectionIDs) == 0)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "favorite target does not exist or is not visible")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite target")
		return
	}
	policy := s.favoriteQuotaPolicy()
	collectionIDs := append(append([]string{}, request.AddCollectionIDs...), request.RemoveCollectionIDs...)
	if err = validateFavoriteCollectionsTx(r.Context(), tx, claims.Subject, collectionIDs); err != nil {
		if errors.Is(err, errFavoriteCollectionNotOwned) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to validate favorite collections")
		return
	}
	if err = enforceFavoritePatchQuotaTx(r.Context(), tx, claims.Subject, target.Type, target.InternalID,
		request.AddCollectionIDs, request.RemoveCollectionIDs, policy); err != nil {
		if writeFavoriteQuotaError(w, err, policy) {
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to reserve favorite quota")
		return
	}
	for _, collectionID := range request.RemoveCollectionIDs {
		if _, err = tx.Exec(r.Context(), `delete from favorite_collection_items item using favorite_collections collection
			where item.collection_id=collection.id and collection.user_id=$1 and collection.public_id=$2
			  and item.entity_type=$3 and item.entity_id=$4`, claims.Subject, collectionID, target.Type, target.InternalID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save favorites")
			return
		}
	}
	for _, collectionID := range request.AddCollectionIDs {
		if _, err = tx.Exec(r.Context(), `insert into favorite_collection_items(collection_id,entity_type,entity_id)
			select id,$3,$4 from favorite_collections where user_id=$1 and public_id=$2
			on conflict do nothing`, claims.Subject, collectionID, target.Type, target.InternalID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save favorites")
			return
		}
	}
	var selected bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		where collection.user_id=$1 and item.entity_type=$2 and item.entity_id=$3)`,
		claims.Subject, target.Type, target.InternalID).Scan(&selected); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite membership")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save favorites")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "selected": selected})
}

func (s *Server) setFavoriteMembership(w http.ResponseWriter, r *http.Request) {
	var request struct {
		EntityType     string   `json:"entityType"`
		EntityPublicID string   `json:"entityPublicId"`
		CollectionIDs  []string `json:"collectionIds"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid favorite membership")
		return
	}
	request.EntityType = strings.ToLower(strings.TrimSpace(request.EntityType))
	request.EntityPublicID = strings.ToLower(strings.TrimSpace(request.EntityPublicID))
	if _, ok := favoriteTargetTypes[request.EntityType]; !ok || !validCatalogPublicID(request.EntityPublicID) {
		writeError(w, http.StatusBadRequest, "invalid favorite target")
		return
	}
	collectionIDs, err := normalizeFavoriteCollectionIDs(request.CollectionIDs, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.CollectionIDs = collectionIDs
	claims := currentClaims(r)
	userID := claims.Subject
	membershipTx, err := s.beginFavoriteMembershipTx(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save favorites")
		return
	}
	defer membershipTx.close()
	tx := membershipTx.tx
	target, err := resolveFavoriteMutationTargetWithQueryer(r.Context(), tx, request.EntityType, request.EntityPublicID, claims, len(request.CollectionIDs) == 0)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "favorite target does not exist or is not visible")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite target")
		return
	}
	policy := s.favoriteQuotaPolicy()
	if err = validateFavoriteCollectionsTx(r.Context(), tx, userID, request.CollectionIDs); err != nil {
		if errors.Is(err, errFavoriteCollectionNotOwned) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to validate favorite collections")
		return
	}
	if _, err = ensureDefaultFavoriteCollectionWithQueryer(r.Context(), tx, userID, policy); err != nil {
		if writeFavoriteQuotaError(w, err, policy) {
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create the default favorite collection")
		return
	}
	if err = enforceFavoriteReplacementQuotaTx(r.Context(), tx, userID, target.Type, target.InternalID,
		request.CollectionIDs, policy); err != nil {
		if writeFavoriteQuotaError(w, err, policy) {
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to reserve favorite quota")
		return
	}
	existingIDs, err := favoriteMembershipCollectionIDsTx(r.Context(), tx, userID, target.Type, target.InternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read favorites")
		return
	}
	desired := make(map[string]bool, len(request.CollectionIDs))
	for _, id := range request.CollectionIDs {
		desired[id] = true
	}
	existing := make(map[string]bool, len(existingIDs))
	for _, id := range existingIDs {
		existing[id] = true
		if desired[id] {
			continue
		}
		if _, err = tx.Exec(r.Context(), `delete from favorite_collection_items item using favorite_collections collection
			where item.collection_id=collection.id and collection.user_id=$1 and collection.public_id=$2
			  and item.entity_type=$3 and item.entity_id=$4`, userID, id, target.Type, target.InternalID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save favorites")
			return
		}
	}
	for _, id := range request.CollectionIDs {
		if existing[id] {
			continue
		}
		tag, insertErr := tx.Exec(r.Context(), `insert into favorite_collection_items(collection_id,entity_type,entity_id)
			select id,$3,$4 from favorite_collections where user_id=$1 and public_id=$2
			on conflict do nothing`, userID, id, target.Type, target.InternalID)
		if insertErr != nil || tag.RowsAffected() != 1 {
			writeError(w, http.StatusInternalServerError, "failed to save favorites")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save favorites")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "collectionIds": request.CollectionIDs})
}

// Membership writes are serialized for the user. Apply one collection change
// per statement so AFTER-row popularity triggers observe each transition.
func favoriteMembershipCollectionIDsTx(ctx context.Context, tx pgx.Tx, userID int64, entityType string, entityID int64) ([]string, error) {
	rows, err := tx.Query(ctx, `select collection.public_id from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		where collection.user_id=$1 and item.entity_type=$2 and item.entity_id=$3
		order by collection.public_id`, userID, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Server) favoriteCollectionItems(w http.ResponseWriter, r *http.Request) {
	collectionPublicID, ok := publicPathID(w, r, "id")
	if !ok {
		return
	}
	claims := currentClaims(r)
	s.writeFavoriteCollectionItems(w, r, collectionPublicID, claims.Subject, true, claims)
}

func (s *Server) publicFavoriteCollectionItems(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	collectionPublicID, ok := publicPathID(w, r, "collectionId")
	if !ok {
		return
	}
	s.writeFavoriteCollectionItems(w, r, collectionPublicID, identity.InternalID, false, currentClaims(r))
}

func (s *Server) writeFavoriteCollectionItems(w http.ResponseWriter, r *http.Request, collectionPublicID string, ownerID int64, includePrivate bool, claims security.Claims) {
	page, err := parseFavoriteItemPageRequest(r.URL.Query(), ownerID, includePrivate, collectionPublicID, claims)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query, arguments := favoriteItemPageSQL(page)
	rows, err := s.db.Query(r.Context(), query, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite collection items")
		return
	}
	pageRows, err := collectFavoriteItemPageRows(rows, page.Limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite collection items")
		return
	}
	hasMore := len(pageRows) > page.Limit
	if hasMore {
		pageRows = pageRows[:page.Limit]
	}
	items := make([]favoriteCollectionItem, 0, len(pageRows))
	ossCfg := s.ossConfigFromSettings(r.Context())
	iconURLs := make([]string, 0, len(pageRows)*3)
	for i := range pageRows {
		row := &pageRows[i].Item
		iconURLs = append(iconURLs, row.iconURL, row.blueprintIconURL, row.modpackIconURL)
	}
	iconURLs, err = s.resolveStoredOSSImageURLsWithConfig(r.Context(), ossCfg, iconURLs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate favorite icon URL")
		return
	}
	for index := range pageRows {
		row := &pageRows[index].Item
		row.iconURL, row.blueprintIconURL, row.modpackIconURL = iconURLs[index*3], iconURLs[index*3+1], iconURLs[index*3+2]
		metadata := map[string]any{}
		switch row.entityType {
		case "mod":
			metadata = map[string]any{"primaryName": row.primaryName, "secondaryName": row.secondaryName, "iconUrl": row.iconURL, "slug": row.slug}
		case "modpack":
			metadata = map[string]any{"primaryName": row.modpackPrimaryName, "secondaryName": row.modpackSecondaryName, "iconUrl": row.modpackIconURL, "slug": row.modpackSlug}
		case "blueprint":
			metadata = map[string]any{"title": row.title, "publicId": row.publicID, "iconUrl": row.blueprintIconURL}
		}
		items = append(items, favoriteCollectionItem{EntityType: row.entityType, EntityPublicID: row.entityPublicID, Metadata: metadata})
	}
	nextCursor := ""
	if hasMore && len(pageRows) > 0 {
		nextCursor = favoriteItemNextCursor(page, pageRows[len(pageRows)-1])
	}
	writeJSON(w, http.StatusOK, favoriteCollectionItemPageResponse{
		Items: items, Limit: page.Limit, HasMore: hasMore, NextCursor: nextCursor,
	})
}

func publicPathID(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	id := strings.ToLower(strings.TrimSpace(r.PathValue(key)))
	if len(id) != 9 {
		writeError(w, http.StatusBadRequest, "invalid public ID")
		return "", false
	}
	return id, true
}

func resolveFavoriteTargetWithQueryer(ctx context.Context, queryer followProjectQueryer, entityType, publicID string, claims security.Claims) (followProjectTarget, error) {
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if _, ok := favoriteTargetTypes[entityType]; !ok {
		return followProjectTarget{}, pgx.ErrNoRows
	}
	target, err := resolveFollowProjectTargetWithQueryer(ctx, queryer, publicID, claims)
	if err != nil || target.Type != entityType {
		if err == nil {
			err = pgx.ErrNoRows
		}
		return followProjectTarget{}, err
	}
	return target, nil
}

// Removing one's own membership must remain possible after its target becomes
// hidden. The fallback reveals no target metadata and cannot authorize adding
// a hidden target to another collection.
func resolveFavoriteMutationTargetWithQueryer(ctx context.Context, queryer followProjectQueryer, entityType, publicID string, claims security.Claims, removalOnly bool) (followProjectTarget, error) {
	target, err := resolveFavoriteTargetWithQueryer(ctx, queryer, entityType, publicID, claims)
	if !removalOnly || !errors.Is(err, pgx.ErrNoRows) || claims.Subject <= 0 {
		return target, err
	}
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if _, ok := favoriteTargetTypes[entityType]; !ok || !validCatalogPublicID(publicID) {
		return followProjectTarget{}, pgx.ErrNoRows
	}
	target = followProjectTarget{}
	err = queryer.QueryRow(ctx, `select route.internal_id,route.entity_type
		from public_routes route
		where route.public_id=$1 and route.entity_type=$2 and exists(
			select 1 from favorite_collection_items item
			join favorite_collections collection on collection.id=item.collection_id
			where collection.user_id=$3 and item.entity_type=route.entity_type and item.entity_id=route.internal_id
		)`, publicID, entityType, claims.Subject).Scan(&target.InternalID, &target.Type)
	return target, err
}
