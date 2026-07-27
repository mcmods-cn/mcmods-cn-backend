package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) modExportTags(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 80, 200)
	if r.URL.Query().Get("all") == "1" {
		limit = 10000
	}
	offset := boundedOffset(r.URL.Query().Get("offset"))
	var total int
	if err := s.db.QueryRow(r.Context(), `
		select count(*)::int from tag_import_snapshots snapshot
		join catalog_tags tag on tag.entity_id=snapshot.tag_id
		where snapshot.revision_id=$1 and ($2='' or tag.registry=$2)
		  and ($3='' or tag.canonical_id ilike '%' || $3 || '%')`, revisionID, registry, query).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count tags")
		return
	}
	rows, err := s.db.Query(r.Context(), `
		select entity.public_id,entity.public_id,tag.registry,tag.canonical_id,snapshot.member_count
		from tag_import_snapshots snapshot
		join catalog_tags tag on tag.entity_id=snapshot.tag_id
		join catalog_entities entity on entity.id=tag.entity_id
		where snapshot.revision_id=$1 and ($2='' or tag.registry=$2)
		  and ($3='' or tag.canonical_id ilike '%' || $3 || '%')
		order by tag.registry,tag.canonical_id limit $4 offset $5`, revisionID, registry, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tags")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var entityID, publicID, registryName, tagID string
		var memberCount int
		if err = rows.Scan(&entityID, &publicID, &registryName, &tagID, &memberCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode tag")
			return
		}
		parts := strings.SplitN(tagID, ":", 2)
		namespace, objectPath := "", tagID
		if len(parts) == 2 {
			namespace, objectPath = parts[0], parts[1]
		}
		items = append(items, map[string]any{
			"entityId": entityID, "publicId": publicID, "id": tagID, "registry": registryName, "namespace": namespace, "path": objectPath,
			"translationKey": "", "iconPath": "", "previewPath": "", "names": map[string]string{},
			"data": map[string]any{"memberCount": memberCount},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) modExportTagDetail(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	tagID := strings.TrimSpace(r.URL.Query().Get("tagId"))
	entityID := strings.TrimSpace(r.URL.Query().Get("entityId"))
	if registry == "" || (tagID == "" && entityID == "") || len(registry) > 160 || len(tagID) > 512 {
		writeError(w, http.StatusBadRequest, "registry and tagId are required")
		return
	}
	var memberCount int
	var publicID string
	var tagInternalID int64
	if err := s.db.QueryRow(r.Context(), `select tag.entity_id,entity.public_id,tag.canonical_id,snapshot.member_count
		from tag_import_snapshots snapshot join catalog_tags tag on tag.entity_id=snapshot.tag_id
		join catalog_entities entity on entity.id=tag.entity_id
		where snapshot.revision_id=$1 and tag.registry=$2
		and (($3<>'' and entity.public_id=$3) or ($3='' and tag.canonical_id=$4))`,
		revisionID, registry, entityID, tagID).Scan(&tagInternalID, &publicID, &tagID, &memberCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "tag not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read tag")
		}
		return
	}
	locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	rows, err := s.db.Query(r.Context(), `
		select member.raw_member_id,coalesce(entity.public_id,''),coalesce(entity.public_id,''),
		coalesce(snapshot.registry,''),coalesce(snapshot.translation_key,''),
		coalesce(snapshot.names,'{}'::jsonb),
		coalesce(snapshot.icon_path,'')
		from tag_import_members member
		join tag_import_snapshots tag_snapshot on tag_snapshot.id=member.tag_snapshot_id
		left join game_resources resource on resource.entity_id=member.resource_id
		left join catalog_entities entity on entity.id=resource.entity_id
		left join lateral (select candidate.* from resource_import_snapshots candidate
			where candidate.resource_id=resource.entity_id
			order by (candidate.revision_id=$1) desc,candidate.created_at desc limit 1) snapshot on true
		where tag_snapshot.revision_id=$1 and tag_snapshot.tag_id=$2
		order by member.ordinal`, revisionID, tagInternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tag members")
		return
	}
	defer rows.Close()
	members := make([]map[string]any, 0, memberCount)
	for rows.Next() {
		var memberID, memberEntityID, memberPublicID, memberRegistry, translationKey, iconPath string
		var names []byte
		if err = rows.Scan(&memberID, &memberEntityID, &memberPublicID, &memberRegistry, &translationKey, &names, &iconPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode tag member")
			return
		}
		var decodedNames map[string]string
		_ = json.Unmarshal(names, &decodedNames)
		members = append(members, map[string]any{"entityId": memberEntityID, "publicId": memberPublicID, "id": memberID,
			"registry": memberRegistry, "translationKey": translationKey, "names": decodedNames, "iconPath": iconPath})
	}
	if err = s.decorateExportTranslationNames(r.Context(), revisionID, locale, members); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve tag member translations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entityId": publicID, "publicId": publicID, "id": tagID,
		"registry": registry, "memberCount": memberCount, "members": members})
}
