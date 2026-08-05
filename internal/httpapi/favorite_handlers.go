package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

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
		writeError(w, http.StatusInternalServerError, "创建默认收藏夹失败")
		return
	}
	rows, err := s.db.Query(r.Context(), `select collection.public_id,collection.name,collection.is_default,count(item.entity_id)
		from favorite_collections collection
		left join favorite_collection_items item on item.collection_id=collection.id
		where collection.user_id=$1 group by collection.id order by collection.is_default desc,collection.created_at`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取收藏夹失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name string
		var count int64
		var isDefault bool
		if err = rows.Scan(&id, &name, &isDefault, &count); err != nil {
			writeError(w, http.StatusInternalServerError, "读取收藏夹失败")
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "isDefault": isDefault, "itemCount": count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len([]rune(request.Name)) > 60 {
		writeError(w, http.StatusBadRequest, "收藏夹名称不能为空且不能超过 60 个字符")
		return
	}
	var publicID string
	err := s.db.QueryRow(r.Context(), `insert into favorite_collections(user_id,name)
		values($1,$2) returning public_id`, currentClaims(r).Subject, request.Name).Scan(&publicID)
	if err != nil {
		writeError(w, http.StatusConflict, "同名收藏夹已存在")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": publicID, "name": request.Name, "isDefault": false, "itemCount": 0})
}

func (s *Server) updateFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	publicID, ok := publicPathID(w, r, "id")
	if !ok {
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len([]rune(request.Name)) > 60 {
		writeError(w, http.StatusBadRequest, "收藏夹名称不能为空且不能超过 60 个字符")
		return
	}
	tag, err := s.db.Exec(r.Context(), `update favorite_collections set name=$3,updated_at=now()
		where public_id=$1 and user_id=$2 and not is_default`, publicID, currentClaims(r).Subject, request.Name)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "默认收藏夹不可改名，或同名收藏夹已存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": true})
}

func (s *Server) deleteFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	publicID, ok := publicPathID(w, r, "id")
	if !ok {
		return
	}
	tag, err := s.db.Exec(r.Context(), `delete from favorite_collections
		where public_id=$1 and user_id=$2 and not is_default`, publicID, currentClaims(r).Subject)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "默认收藏夹不可删除，或收藏夹不存在")
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
		writeError(w, http.StatusBadRequest, "收藏对象不存在")
		return
	}
	rows, err := s.db.Query(r.Context(), `select collection.public_id from favorite_collections collection
		join favorite_collection_items item on item.collection_id=collection.id
		where collection.user_id=$1 and item.entity_type=$2 and item.entity_id=$3`,
		currentClaims(r).Subject, entityType, entityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取收藏状态失败")
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
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.EntityType = strings.TrimSpace(request.EntityType)
	request.EntityPublicID = strings.TrimSpace(request.EntityPublicID)
	if request.EntityPublicID == "" {
		request.EntityPublicID = strings.TrimSpace(request.EntityKey)
	}
	entityID, err := s.resolveFavoriteEntity(r.Context(), request.EntityType, request.EntityPublicID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "收藏对象不存在")
		return
	}
	userID := currentClaims(r).Subject
	if _, err = s.ensureDefaultFavoriteCollection(r.Context(), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "创建默认收藏夹失败")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存收藏失败")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `delete from favorite_collection_items item using favorite_collections collection
		where item.collection_id=collection.id and collection.user_id=$1
		  and item.entity_type=$2 and item.entity_id=$3`, userID, request.EntityType, entityID); err != nil {
		writeError(w, http.StatusInternalServerError, "保存收藏失败")
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
			writeError(w, http.StatusInternalServerError, "保存收藏失败")
			return
		}
		if tag.RowsAffected() > 0 {
			saved = append(saved, collectionPublicID)
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存收藏失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "collectionIds": saved})
}

func (s *Server) favoriteCollectionItems(w http.ResponseWriter, r *http.Request) {
	collectionPublicID, ok := publicPathID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `select item.entity_type,route.public_id,
		coalesce(mods.primary_name,''),coalesce(mods.secondary_name,''),coalesce(mods.icon_url,''),coalesce(mods.slug,''),
		coalesce(modpack.primary_name,''),coalesce(modpack.secondary_name,''),coalesce(modpack.icon_url,''),coalesce(modpack.slug,''),
		coalesce(blueprint.title,''),coalesce(blueprint.public_id,'')
		from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		join public_routes route on route.entity_type=item.entity_type and route.internal_id=item.entity_id
		left join mods on item.entity_type='mod' and mods.id=item.entity_id
		left join modpacks modpack on item.entity_type='modpack' and modpack.id=item.entity_id
		left join blueprints blueprint on item.entity_type='blueprint' and blueprint.id=item.entity_id and blueprint.status<>'deleted'
		where collection.public_id=$1 and collection.user_id=$2 order by item.created_at desc`,
		collectionPublicID, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取收藏内容失败")
		return
	}
	defer rows.Close()
	items := make([]favoriteCollectionItem, 0)
	for rows.Next() {
		var entityType, entityKey, primaryName, secondaryName, iconURL, slug string
		var modpackPrimaryName, modpackSecondaryName, modpackIconURL, modpackSlug, title, publicID string
		if rows.Scan(&entityType, &entityKey, &primaryName, &secondaryName, &iconURL, &slug,
			&modpackPrimaryName, &modpackSecondaryName, &modpackIconURL, &modpackSlug, &title, &publicID) != nil {
			continue
		}
		metadata := map[string]any{}
		if entityType == "mod" {
			metadata = map[string]any{"primaryName": primaryName, "secondaryName": secondaryName, "iconUrl": iconURL, "slug": slug}
		} else if entityType == "modpack" {
			metadata = map[string]any{"primaryName": modpackPrimaryName, "secondaryName": modpackSecondaryName, "iconUrl": modpackIconURL, "slug": modpackSlug}
		} else if entityType == "blueprint" {
			metadata = map[string]any{"title": title, "publicId": publicID}
		}
		items = append(items, favoriteCollectionItem{EntityType: entityType, EntityKey: entityKey, Metadata: metadata})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func publicPathID(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	id := strings.ToLower(strings.TrimSpace(r.PathValue(key)))
	if len(id) != 9 {
		writeError(w, http.StatusBadRequest, "公开 ID 不正确")
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
