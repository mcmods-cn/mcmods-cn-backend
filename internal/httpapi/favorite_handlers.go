package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type favoriteCollectionSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
	IsPublic  bool   `json:"isPublic"`
	ItemCount int64  `json:"itemCount"`
}

type favoriteCollectionItem struct {
	EntityType string         `json:"entityType"`
	EntityKey  string         `json:"entityKey"`
	Metadata   map[string]any `json:"metadata"`
}

func (s *Server) ensureDefaultFavoriteCollection(ctx context.Context, userID int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `insert into favorite_collections(user_id,name,is_default)
		values($1,'default',true)
		on conflict(user_id) where is_default do update set updated_at=favorite_collections.updated_at
		returning id`, userID).Scan(&id)
	return id, err
}

func (s *Server) favoriteCollections(w http.ResponseWriter, r *http.Request) {
	userID := currentClaims(r).Subject
	if _, err := s.ensureDefaultFavoriteCollection(r.Context(), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the default favorite collection")
		return
	}
	items, err := s.loadFavoriteCollections(r.Context(), userID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite collections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) publicFavoriteCollections(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	items, err := s.loadFavoriteCollections(r.Context(), identity.InternalID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load public favorite collections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) loadFavoriteCollections(ctx context.Context, userID int64, includePrivate bool) ([]favoriteCollectionSummary, error) {
	rows, err := s.db.Query(ctx, `select collection.public_id,collection.name,collection.is_default,
		collection.is_public,count(item.entity_id)
		from favorite_collections collection
		left join favorite_collection_items item on item.collection_id=collection.id
		where collection.user_id=$1 and ($2 or collection.is_public)
		group by collection.id
		order by collection.is_default desc,collection.created_at,collection.id`, userID, includePrivate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]favoriteCollectionSummary, 0)
	for rows.Next() {
		var item favoriteCollectionSummary
		if err = rows.Scan(&item.ID, &item.Name, &item.IsDefault, &item.IsPublic, &item.ItemCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
	var item favoriteCollectionSummary
	err := s.db.QueryRow(r.Context(), `insert into favorite_collections(user_id,name,is_public)
		values($1,$2,$3) returning public_id,name,is_default,is_public`,
		currentClaims(r).Subject, request.Name, request.IsPublic).
		Scan(&item.ID, &item.Name, &item.IsDefault, &item.IsPublic)
	if err != nil {
		writeError(w, http.StatusConflict, "a favorite collection with this name already exists")
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
	var item favoriteCollectionSummary
	err := s.db.QueryRow(r.Context(), `update favorite_collections set
		name=case when is_default or $3='' then name else $3 end,
		is_public=coalesce($4::boolean,is_public),updated_at=now()
		where public_id=$1 and user_id=$2
		returning public_id,name,is_default,is_public,
		(select count(*) from favorite_collection_items where collection_id=favorite_collections.id)`,
		publicID, currentClaims(r).Subject, name, visibility).
		Scan(&item.ID, &item.Name, &item.IsDefault, &item.IsPublic, &item.ItemCount)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a favorite collection with this name already exists")
			return
		}
		writeError(w, http.StatusNotFound, "favorite collection was not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	publicID, ok := publicPathID(w, r, "id")
	if !ok {
		return
	}
	tag, err := s.db.Exec(r.Context(), `delete from favorite_collections
		where public_id=$1 and user_id=$2 and not is_default`, publicID, currentClaims(r).Subject)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "the default collection cannot be deleted, or the collection does not exist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) favoriteMembership(w http.ResponseWriter, r *http.Request) {
	entityType := strings.TrimSpace(r.URL.Query().Get("entityType"))
	entityPublicID := strings.TrimSpace(r.URL.Query().Get("entityPublicId"))
	if entityPublicID == "" {
		entityPublicID = strings.TrimSpace(r.URL.Query().Get("entityKey"))
	}
	entityID, err := s.resolveFavoriteEntity(r.Context(), entityType, entityPublicID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "favorite target does not exist")
		return
	}
	rows, err := s.db.Query(r.Context(), `select collection.public_id from favorite_collections collection
		join favorite_collection_items item on item.collection_id=collection.id
		where collection.user_id=$1 and item.entity_type=$2 and item.entity_id=$3`,
		currentClaims(r).Subject, entityType, entityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite membership")
		return
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"collectionIds": ids})
}

func (s *Server) setFavoriteMembership(w http.ResponseWriter, r *http.Request) {
	var request struct {
		EntityType     string   `json:"entityType"`
		EntityPublicID string   `json:"entityPublicId"`
		EntityKey      string   `json:"entityKey"`
		CollectionIDs  []string `json:"collectionIds"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid favorite membership")
		return
	}
	request.EntityType = strings.TrimSpace(request.EntityType)
	request.EntityPublicID = strings.TrimSpace(request.EntityPublicID)
	if request.EntityPublicID == "" {
		request.EntityPublicID = strings.TrimSpace(request.EntityKey)
	}
	entityID, err := s.resolveFavoriteEntity(r.Context(), request.EntityType, request.EntityPublicID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "favorite target does not exist")
		return
	}
	userID := currentClaims(r).Subject
	if _, err = s.ensureDefaultFavoriteCollection(r.Context(), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the default favorite collection")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save favorites")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `delete from favorite_collection_items item using favorite_collections collection
		where item.collection_id=collection.id and collection.user_id=$1
		  and item.entity_type=$2 and item.entity_id=$3`, userID, request.EntityType, entityID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save favorites")
		return
	}
	saved := make([]string, 0, len(request.CollectionIDs))
	for _, collectionPublicID := range request.CollectionIDs {
		collectionPublicID = strings.ToLower(strings.TrimSpace(collectionPublicID))
		if collectionPublicID == "" {
			continue
		}
		tag, insertErr := tx.Exec(r.Context(), `insert into favorite_collection_items(collection_id,entity_type,entity_id)
			select id,$3,$4 from favorite_collections where public_id=$1 and user_id=$2
			on conflict do nothing`, collectionPublicID, userID, request.EntityType, entityID)
		if insertErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to save favorites")
			return
		}
		if tag.RowsAffected() > 0 {
			saved = append(saved, collectionPublicID)
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save favorites")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "collectionIds": saved})
}

func (s *Server) favoriteCollectionItems(w http.ResponseWriter, r *http.Request) {
	collectionPublicID, ok := publicPathID(w, r, "id")
	if !ok {
		return
	}
	s.writeFavoriteCollectionItems(w, r, collectionPublicID, currentClaims(r).Subject, true)
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
	s.writeFavoriteCollectionItems(w, r, collectionPublicID, identity.InternalID, false)
}

func (s *Server) writeFavoriteCollectionItems(w http.ResponseWriter, r *http.Request, collectionPublicID string, ownerID int64, includePrivate bool) {
	rows, err := s.db.Query(r.Context(), `select item.entity_type,route.public_id,
		coalesce(mods.primary_name,''),coalesce(mods.secondary_name,''),coalesce(mods.icon_url,''),coalesce(mods.slug,''),
		coalesce(modpack.primary_name,''),coalesce(modpack.secondary_name,''),coalesce(modpack.icon_url,''),coalesce(modpack.slug,''),
		coalesce(blueprint.title,''),coalesce(blueprint.public_id,''),coalesce(blueprint.cover_object_key,'')
		from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		join public_routes route on route.entity_type=item.entity_type and route.internal_id=item.entity_id
		left join mods on item.entity_type='mod' and mods.id=item.entity_id
		left join modpacks modpack on item.entity_type='modpack' and modpack.id=item.entity_id
		left join blueprints blueprint on item.entity_type='blueprint' and blueprint.id=item.entity_id and blueprint.status<>'deleted'
		where collection.public_id=$1 and collection.user_id=$2 and ($3 or collection.is_public)
		  and ($3 or
			(item.entity_type='mod' and mods.review_status='approved') or
			(item.entity_type='modpack' and modpack.review_status='approved') or
			(item.entity_type='blueprint' and blueprint.status in ('ready','partial') and blueprint.review_status in ('not_required','approved')))
		order by item.created_at desc`, collectionPublicID, ownerID, includePrivate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite collection items")
		return
	}
	defer rows.Close()
	items := make([]favoriteCollectionItem, 0)
	ossCfg := s.ossConfigFromSettings(r.Context())
	for rows.Next() {
		var entityType, entityKey, primaryName, secondaryName, iconURL, slug string
		var modpackPrimaryName, modpackSecondaryName, modpackIconURL, modpackSlug, title, publicID, blueprintIconURL string
		if err = rows.Scan(&entityType, &entityKey, &primaryName, &secondaryName, &iconURL, &slug,
			&modpackPrimaryName, &modpackSecondaryName, &modpackIconURL, &modpackSlug, &title, &publicID, &blueprintIconURL); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode favorite collection items")
			return
		}
		iconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, iconURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate favorite icon URL")
			return
		}
		blueprintIconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, blueprintIconURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate favorite icon URL")
			return
		}
		modpackIconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, modpackIconURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate favorite icon URL")
			return
		}
		metadata := map[string]any{}
		switch entityType {
		case "mod":
			metadata = map[string]any{"primaryName": primaryName, "secondaryName": secondaryName, "iconUrl": iconURL, "slug": slug}
		case "modpack":
			metadata = map[string]any{"primaryName": modpackPrimaryName, "secondaryName": modpackSecondaryName, "iconUrl": modpackIconURL, "slug": modpackSlug}
		case "blueprint":
			metadata = map[string]any{"title": title, "publicId": publicID, "iconUrl": blueprintIconURL}
		}
		items = append(items, favoriteCollectionItem{EntityType: entityType, EntityKey: entityKey, Metadata: metadata})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorite collection items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func publicPathID(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	id := strings.ToLower(strings.TrimSpace(r.PathValue(key)))
	if len(id) != 9 {
		writeError(w, http.StatusBadRequest, "invalid public ID")
		return "", false
	}
	return id, true
}

func (s *Server) resolveFavoriteEntity(ctx context.Context, entityType, publicID string) (int64, error) {
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if entityType == "" || publicID == "" {
		return 0, pgx.ErrNoRows
	}
	var id int64
	err := s.db.QueryRow(ctx, `select internal_id from public_routes
		where entity_type=$1 and public_id=$2`, entityType, publicID).Scan(&id)
	return id, err
}
