package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
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
	rows, err := s.db.Query(r.Context(), `select collection.id,collection.name,collection.is_default,count(item.entity_key)
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
		var id, count int64
		var name string
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
	var id int64
	err := s.db.QueryRow(r.Context(), `insert into favorite_collections(user_id,name) values($1,$2) returning id`, currentClaims(r).Subject, request.Name).Scan(&id)
	if err != nil {
		writeError(w, http.StatusConflict, "同名收藏夹已存在")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": request.Name, "isDefault": false, "itemCount": 0})
}

func (s *Server) updateFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := positivePathID(w, r, "id")
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
		where id=$1 and user_id=$2 and not is_default`, id, currentClaims(r).Subject, request.Name)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "默认收藏夹不可改名，或同名收藏夹已存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": true})
}

func (s *Server) deleteFavoriteCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := positivePathID(w, r, "id")
	if !ok {
		return
	}
	tag, err := s.db.Exec(r.Context(), `delete from favorite_collections where id=$1 and user_id=$2 and not is_default`, id, currentClaims(r).Subject)
	if err != nil || tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "默认收藏夹不可删除，或收藏夹不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) favoriteMembership(w http.ResponseWriter, r *http.Request) {
	entityType := strings.TrimSpace(r.URL.Query().Get("entityType"))
	entityKey := strings.TrimSpace(r.URL.Query().Get("entityKey"))
	if entityType == "" || entityKey == "" {
		writeError(w, http.StatusBadRequest, "缺少收藏对象")
		return
	}
	rows, err := s.db.Query(r.Context(), `select collection.id from favorite_collections collection
		join favorite_collection_items item on item.collection_id=collection.id
		where collection.user_id=$1 and item.entity_type=$2 and item.entity_key=$3`, currentClaims(r).Subject, entityType, entityKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取收藏状态失败")
		return
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"collectionIds": ids})
}

func (s *Server) setFavoriteMembership(w http.ResponseWriter, r *http.Request) {
	var request struct {
		EntityType    string  `json:"entityType"`
		EntityKey     string  `json:"entityKey"`
		CollectionIDs []int64 `json:"collectionIds"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.EntityType = strings.TrimSpace(request.EntityType)
	request.EntityKey = strings.TrimSpace(request.EntityKey)
	if request.EntityType == "" || request.EntityKey == "" {
		writeError(w, http.StatusBadRequest, "缺少收藏对象")
		return
	}
	userID := currentClaims(r).Subject
	if _, err := s.ensureDefaultFavoriteCollection(r.Context(), userID); err != nil {
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
		where item.collection_id=collection.id and collection.user_id=$1 and item.entity_type=$2 and item.entity_key=$3`, userID, request.EntityType, request.EntityKey); err != nil {
		writeError(w, http.StatusInternalServerError, "保存收藏失败")
		return
	}
	for _, id := range request.CollectionIDs {
		if id <= 0 {
			continue
		}
		if _, err = tx.Exec(r.Context(), `insert into favorite_collection_items(collection_id,entity_type,entity_key)
			select id,$3,$4 from favorite_collections where id=$1 and user_id=$2 on conflict do nothing`, id, userID, request.EntityType, request.EntityKey); err != nil {
			writeError(w, http.StatusInternalServerError, "保存收藏失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存收藏失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "collectionIds": request.CollectionIDs})
}

func (s *Server) favoriteCollectionItems(w http.ResponseWriter, r *http.Request) {
	id, ok := positivePathID(w, r, "id")
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `select item.entity_type,item.entity_key,
		coalesce(mods.primary_name,''),coalesce(mods.secondary_name,''),coalesce(mods.icon_url,''),coalesce(mods.slug,''),
		coalesce(blueprint.title,''),coalesce(blueprint.public_id,'')
		from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		left join mods on item.entity_type='mod' and mods.slug=item.entity_key
		left join blueprints blueprint on item.entity_type='blueprint' and blueprint.public_id=item.entity_key and blueprint.status<>'deleted'
		where collection.id=$1 and collection.user_id=$2 order by item.created_at desc`, id, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取收藏内容失败")
		return
	}
	defer rows.Close()
	items := make([]favoriteCollectionItem, 0)
	for rows.Next() {
		var entityType, entityKey, primaryName, secondaryName, iconURL, slug, title, publicID string
		if rows.Scan(&entityType, &entityKey, &primaryName, &secondaryName, &iconURL, &slug, &title, &publicID) != nil {
			continue
		}
		metadata := map[string]any{}
		if entityType == "mod" {
			metadata = map[string]any{"primaryName": primaryName, "secondaryName": secondaryName, "iconUrl": iconURL, "slug": slug}
		} else if entityType == "blueprint" {
			metadata = map[string]any{"title": title, "publicId": publicID}
		}
		items = append(items, favoriteCollectionItem{EntityType: entityType, EntityKey: entityKey, Metadata: metadata})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func positivePathID(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "ID 不正确")
		return 0, false
	}
	return id, true
}
