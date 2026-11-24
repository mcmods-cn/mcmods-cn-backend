package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// catalogResourcePresentation resolves one already-known resource reference
// for public cards and recipe icons. It deliberately has no fuzzy search,
// pagination or binding fields; directory browsing stays behind the admin
// permission boundary.
func (s *Server) catalogResourcePresentation(w http.ResponseWriter, r *http.Request) {
	reference := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("ref")))
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if reference == "" || len(reference) > 180 || len(kind) > 64 {
		writeError(w, http.StatusBadRequest, "invalid resource reference")
		return
	}
	primary, secondary := s.requestContentLocales(r)
	items, err := s.loadCatalogResourcePresentations(r.Context(), []catalogPresentationBatchReference{{
		PublicID: reference, ID: reference, Kind: kind,
	}}, primary, secondary)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read resource presentation")
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusNotFound, "resource presentation not found")
		return
	}
	item := items[0]
	if item.ambiguous {
		writeAPIError(w, http.StatusConflict, "resource_reference_ambiguous", "specify a resource kind or public ID", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"publicId": item.PublicID, "kind": item.Kind, "id": item.ID, "registry": item.Registry, "namespace": item.Namespace,
		"locale": item.ResolvedLocale, "name": item.ResolvedName, "iconUrl": item.IconURL,
	})
}

// adminGlobalResourceBindings exposes the technical binding graph only at an
// explicitly permission-protected administration boundary. Public presentation
// lookup never includes database IDs, binding IDs, counts, or technical state.
func (s *Server) adminGlobalResourceBindings(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	var resourceID int64
	if err := s.db.QueryRow(r.Context(), `select entity.id from catalog_entities entity
		join game_resources resource on resource.entity_id=entity.id
		where entity.public_id=$1 and entity.entity_type='resource'`, publicID).Scan(&resourceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "global resource not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read global resource")
		}
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !stringSet("active", "pending", "archived")[status] {
		writeError(w, http.StatusBadRequest, "invalid binding status")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 40, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	var total int
	if err := s.db.QueryRow(r.Context(), `select count(*)::int
		from mod_resource_version_details detail
		join mod_content_versions version on version.id=detail.version_id
		join mods mod on mod.id=version.mod_id
		where detail.resource_id=$1 and ($2='' or detail.status=$2)
		  and ($3='' or mod.primary_name ilike '%'||$3||'%' or mod.project_code ilike '%'||$3||'%'
		    or version.label ilike '%'||$3||'%')`, resourceID, status, query).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count global resource bindings")
		return
	}
	rows, err := s.db.Query(r.Context(), `select mod.project_code,mod.slug,mod.primary_name,
		version.public_id,version.label,version.minecraft_versions,detail.status,
		detail.created_at,detail.updated_at,coalesce(detail_localization.name,'')
		from mod_resource_version_details detail
		join mod_content_versions version on version.id=detail.version_id
		join mods mod on mod.id=version.mod_id
		left join lateral (select localization.name from mod_resource_version_detail_localizations localization
			where localization.resource_id=detail.resource_id and localization.version_id=detail.version_id
			order by case localization.locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) detail_localization on true
		where detail.resource_id=$1 and ($2='' or detail.status=$2)
		  and ($3='' or mod.primary_name ilike '%'||$3||'%' or mod.project_code ilike '%'||$3||'%'
		    or version.label ilike '%'||$3||'%')
		order by detail.updated_at desc,version.id desc limit $4 offset $5`, resourceID, status, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read global resource bindings")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var projectID, projectSlug, projectName, versionID, versionLabel, bindingStatus, resourceName string
		var versions []string
		var createdAt, updatedAt any
		if err = rows.Scan(&projectID, &projectSlug, &projectName, &versionID, &versionLabel, &versions,
			&bindingStatus, &createdAt, &updatedAt, &resourceName); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode global resource binding")
			return
		}
		items = append(items, map[string]any{
			"resourceName": resourceName, "projectId": projectID, "projectSiteId": projectSlug,
			"projectName": projectName, "projectType": "mod", "versionId": versionID,
			"versionLabel": versionLabel, "minecraftVersions": versions, "status": bindingStatus,
			"createdAt": createdAt, "updatedAt": updatedAt,
		})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read global resource bindings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}
