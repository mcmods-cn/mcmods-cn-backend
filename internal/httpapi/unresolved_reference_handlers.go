package httpapi

import (
	"net/http"
	"strings"
	"time"
)

type unresolvedReferenceListItem struct {
	ID             string     `json:"id"`
	SourceType     string     `json:"sourceType"`
	SourceID       string     `json:"sourceId"`
	FieldPath      string     `json:"fieldPath"`
	ReferenceType  string     `json:"referenceType"`
	RawIdentifier  string     `json:"rawIdentifier"`
	Status         string     `json:"status"`
	ResolvedType   string     `json:"resolvedType,omitempty"`
	ResolvedID     string     `json:"resolvedId,omitempty"`
	SourceLabel    string     `json:"sourceLabel,omitempty"`
	SourcePublicID string     `json:"sourcePublicId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
}

func (s *Server) adminUnresolvedReferences(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	referenceType := strings.TrimSpace(r.URL.Query().Get("type"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "pending"
	}
	if status != "all" && status != "pending" && status != "resolved" && status != "ignored" {
		writeError(w, http.StatusBadRequest, "invalid unresolved reference status")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 50, 200)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	rows, err := s.db.Query(r.Context(), `with reference_rows as (
		select 'general:'||unresolved.id::text id,unresolved.source_type,
			unresolved.source_id::text source_id,unresolved.field_path,unresolved.reference_type,
			unresolved.raw_identifier,unresolved.status,unresolved.resolved_type,
			coalesce(unresolved.resolved_id::text,'') resolved_id,unresolved.created_at,unresolved.resolved_at,
			coalesce(source_mod.primary_name,community_post.title,source_modpack.primary_name,'') source_label,
			coalesce(source_mod.project_code,community_post.public_id,source_modpack.public_id,'') source_public_id
		from unresolved_references unresolved
		left join mod_relationships relationship
		  on unresolved.source_type='mod_relationship' and relationship.id=unresolved.source_id
		left join mods source_mod on source_mod.id=relationship.mod_id
		left join community_post_project_refs community_project
		  on unresolved.source_type='community_post_project' and community_project.id=unresolved.source_id
		left join community_post_resource_refs community_resource
		  on unresolved.source_type='community_post_resource' and community_resource.id=unresolved.source_id
		left join community_posts community_post
		  on community_post.id=coalesce(community_project.post_id,community_resource.post_id)
		left join modpack_mods modpack_entry
		  on unresolved.source_type='modpack_mod' and modpack_entry.id=unresolved.source_id
		left join modpacks source_modpack on source_modpack.id=modpack_entry.modpack_id
		union all
		select 'resource:'||unresolved.id::text,'catalog_resource',
			unresolved.source_entity_id::text,unresolved.field_path,unresolved.kind_code,
			unresolved.raw_resource_id,unresolved.status,'resource',
			coalesce(unresolved.resolved_resource_id::text,''),unresolved.created_at,unresolved.resolved_at,
			coalesce(recipe.canonical_source_id,source_entity.identity_key),
			coalesce(source_entity.public_id,'')
		from unresolved_resource_references unresolved
		join catalog_entities source_entity on source_entity.id=unresolved.source_entity_id
		left join recipes recipe on recipe.entity_id=unresolved.source_entity_id
	), filtered as (
		select *,count(*) over() total from reference_rows
		where ($1='' or raw_identifier ilike '%'||$1||'%' or source_label ilike '%'||$1||'%')
		  and ($2='' or reference_type=$2)
		  and ($3='all' or status=$3)
	)
	select id,source_type,source_id,field_path,reference_type,raw_identifier,status,
		resolved_type,resolved_id,source_label,source_public_id,created_at,resolved_at,total
	from filtered order by created_at desc,id desc limit $4 offset $5`,
		query, referenceType, status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load unresolved references")
		return
	}
	defer rows.Close()
	items := make([]unresolvedReferenceListItem, 0)
	total := 0
	for rows.Next() {
		var item unresolvedReferenceListItem
		if err = rows.Scan(&item.ID, &item.SourceType, &item.SourceID, &item.FieldPath,
			&item.ReferenceType, &item.RawIdentifier, &item.Status, &item.ResolvedType,
			&item.ResolvedID, &item.SourceLabel, &item.SourcePublicID, &item.CreatedAt,
			&item.ResolvedAt, &total); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read unresolved references")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read unresolved references")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "limit": limit, "offset": offset,
	})
}
