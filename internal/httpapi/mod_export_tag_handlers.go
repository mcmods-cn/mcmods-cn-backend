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
		select count(*)::int from mod_export_tags
		where revision_id=$1 and ($2='' or registry=$2)
		  and ($3='' or tag_id ilike '%' || $3 || '%')`, revisionID, registry, query).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count tags")
		return
	}
	rows, err := s.db.Query(r.Context(), `
		select registry,tag_id,member_count from mod_export_tags
		where revision_id=$1 and ($2='' or registry=$2)
		  and ($3='' or tag_id ilike '%' || $3 || '%')
		order by registry,tag_id limit $4 offset $5`, revisionID, registry, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tags")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var registryName, tagID string
		var memberCount int
		if err = rows.Scan(&registryName, &tagID, &memberCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode tag")
			return
		}
		parts := strings.SplitN(tagID, ":", 2)
		namespace, objectPath := "", tagID
		if len(parts) == 2 {
			namespace, objectPath = parts[0], parts[1]
		}
		items = append(items, map[string]any{
			"id": tagID, "registry": registryName, "namespace": namespace, "path": objectPath,
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
	if registry == "" || tagID == "" || len(registry) > 160 || len(tagID) > 512 {
		writeError(w, http.StatusBadRequest, "registry and tagId are required")
		return
	}
	var memberCount int
	if err := s.db.QueryRow(r.Context(), `select member_count from mod_export_tags where revision_id=$1 and registry=$2 and tag_id=$3`, revisionID, registry, tagID).Scan(&memberCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "tag not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read tag")
		}
		return
	}
	entryRegistry := tagRegistryEntryRegistry(registry)
	locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	rows, err := s.db.Query(r.Context(), `
		select m.member_id,coalesce(e.registry,''),
		 jsonb_strip_nulls(jsonb_build_object('zh_cn',e.names->>'zh_cn','en_us',e.names->>'en_us',$5::text,e.names->>($5::text))),
		 coalesce(icon.asset_path,'')
		from mod_export_tag_members m
		left join mod_export_registry_entries e on e.revision_id=m.revision_id and e.registry=$4 and e.object_id=m.member_id
		left join mod_export_media icon on icon.revision_id=e.revision_id and icon.asset_path=case e.registry
		 when 'items' then concat('icons/items/32/',e.namespace,'/',e.object_path,'.png')
		 when 'blocks' then concat('icons/blocks/32/',e.namespace,'/',e.object_path,'.png')
		 when 'entity_types' then concat('entities/renders/32/',e.namespace,'/',e.object_path,'.png')
		 when 'mob_effects' then concat('assets/',e.namespace,'/textures/mob_effect/',e.object_path,'.png')
		 when 'potions' then concat('potions/potion/32/',e.namespace,'/',e.object_path,'.png')
		 else '' end
		where m.revision_id=$1 and m.registry=$2 and m.tag_id=$3
		order by m.ordinal`, revisionID, registry, tagID, entryRegistry, locale)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tag members")
		return
	}
	defer rows.Close()
	members := make([]map[string]any, 0, memberCount)
	for rows.Next() {
		var memberID, memberRegistry, iconPath string
		var names []byte
		if err = rows.Scan(&memberID, &memberRegistry, &names, &iconPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode tag member")
			return
		}
		var decodedNames map[string]string
		_ = json.Unmarshal(names, &decodedNames)
		members = append(members, map[string]any{"id": memberID, "registry": memberRegistry, "names": decodedNames, "iconPath": iconPath})
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": tagID, "registry": registry, "memberCount": memberCount, "members": members})
}

func tagRegistryEntryRegistry(registry string) string {
	switch strings.ToLower(strings.TrimSpace(registry)) {
	case "minecraft:item":
		return "items"
	case "minecraft:block":
		return "blocks"
	case "minecraft:entity_type":
		return "entity_types"
	case "minecraft:biome", "minecraft:worldgen/biome":
		return "biomes"
	case "minecraft:enchantment":
		return "enchantments"
	case "minecraft:mob_effect":
		return "mob_effects"
	case "minecraft:potion":
		return "potions"
	case "minecraft:fluid":
		return "fluids"
	default:
		return ""
	}
}
