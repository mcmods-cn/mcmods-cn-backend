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
	if reference == "" || len(reference) > 180 {
		writeError(w, http.StatusBadRequest, "invalid resource reference")
		return
	}
	primary, secondary := s.requestContentLocales(r)
	var publicID, kind, canonicalID, namespace, defaultLocale, locale, name, iconFileID, importedIconPath string
	var names []byte
	err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`
		select entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,entity.default_locale,
		coalesce(localization.locale,''),coalesce(localization.name,''),coalesce((select public_id from oss_files where id=definition.icon_file_id),''),
		coalesce(imported.icon_path,''),coalesce(imported.names,'{}'::jsonb) || coalesce((select jsonb_object_agg(candidate.locale,candidate.name)
		 from content_localizations candidate where candidate.catalog_entity_id=entity.id and candidate.name<>''),'{}'::jsonb)
		from game_resources resource join catalog_entities entity on entity.id=resource.entity_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join latest_resource_snapshots imported on imported.resource_id=resource.entity_id
		left join lateral (select candidate.locale,candidate.name from content_localizations candidate
		 where candidate.catalog_entity_id=entity.id order by case candidate.locale when $2 then 0 when $3 then 1
		 when entity.default_locale then 2 when 'en-US' then 3 else 4 end limit 1) localization on true
		where entity.status='active' and (entity.public_id=$1 or lower(resource.canonical_id)=$1) limit 1`, reference, primary, secondary).
		Scan(&publicID, &kind, &canonicalID, &namespace, &defaultLocale, &locale, &name, &iconFileID, &importedIconPath, &names)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "resource presentation not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read resource presentation")
		}
		return
	}
	if resolvedLocale, resolvedName := catalogResolvedName(names, primary, secondary, defaultLocale); resolvedName != "" {
		locale, name = resolvedLocale, resolvedName
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"publicId": publicID, "kind": kind, "id": canonicalID, "registry": namespace,
		"locale": locale, "name": name, "iconUrl": catalogRecipeResourceIconURL(publicID, iconFileID, "", importedIconPath),
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
