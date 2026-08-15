package httpapi

import (
	"context"
	"encoding/json"
)

// decorateResourceVersionRows keeps tags and resource pages attached to the
// immutable global identity while exposing every mod-content version that can
// supply a concrete name/icon/detail for that identity.
func (s *Server) decorateResourceVersionRows(ctx context.Context, items []map[string]any, primary, secondary string) error {
	resourcePublicIDs := make([]string, 0, len(items))
	byResource := make(map[string]map[string]any, len(items))
	for _, item := range items {
		resourceID, _ := item["publicId"].(string)
		if resourceID == "" {
			continue
		}
		if _, exists := byResource[resourceID]; !exists {
			resourcePublicIDs = append(resourcePublicIDs, resourceID)
			byResource[resourceID] = item
		}
		item["versions"] = []map[string]any{}
	}
	if len(resourcePublicIDs) == 0 {
		return nil
	}
	rows, err := s.db.Query(ctx, `select entity.public_id,resource.canonical_id,version.public_id,version.label,version.minecraft_versions,version.loaders,
		version.mod_version,
		true has_manual_page,
		(version_detail.resource_id is not null) has_manual_detail,
		(version_detail.resource_id is not null or snapshot.revision_id is not null) has_detail,
		coalesce(snapshot.revision_id,''),coalesce(snapshot.registry,''),coalesce(snapshot.icon_path,''),coalesce(snapshot.preview_path,''),mod.slug,
		coalesce(nullif(version_names.names,'{}'::jsonb),nullif(snapshot.names,'{}'::jsonb),'{}'::jsonb),
		coalesce(icon_file.public_id,''),coalesce(render_file.public_id,'')
		from game_resources resource
		join catalog_entities entity on entity.id=resource.entity_id
		join mod_resource_bindings binding on binding.resource_id=resource.entity_id
		join mods mod on mod.id=binding.mod_id
		join mod_content_versions version on version.mod_id=binding.mod_id and version.status='active'
		left join lateral (select imported.revision_id,imported.registry,imported.icon_path,imported.preview_path,imported.names
		 from resource_import_snapshots imported join catalog_import_revisions revision on revision.id=imported.revision_id
		 where imported.resource_id=resource.entity_id and revision.target_version_id=version.id and revision.is_active
		 order by (imported.icon_path<>'') desc,revision.created_at desc limit 1) snapshot on true
		left join mod_resource_version_details version_detail on version_detail.resource_id=resource.entity_id and version_detail.version_id=version.id and version_detail.status='active'
		left join oss_files icon_file on icon_file.id=version_detail.icon_file_id and icon_file.status='active'
		 and icon_file.scan_status in ('clean','trusted_generated')
		left join oss_files render_file on render_file.id=version_detail.render_file_id and render_file.status='active'
		 and render_file.scan_status in ('clean','trusted_generated')
		left join lateral (select jsonb_object_agg(localization.locale,localization.name) names
		 from mod_resource_version_detail_localizations localization where version_detail.resource_id is not null
		  and localization.resource_id=resource.entity_id and localization.version_id=version.id and coalesce(localization.name,'')<>'') version_names on true
		where entity.public_id=any($1::text[]) and entity.status='active' and entity.archived_at is null
		order by resource.entity_id,version.updated_at desc,version.id desc`, resourcePublicIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var resourcePublicID, canonicalID, publicID, label, modVersion, revisionID, registry, iconPath, previewPath, modSiteID string
		var minecraftVersions, loaders []string
		var hasManualPage, hasManualDetail, hasDetail bool
		var names []byte
		var iconFilePublicID, renderFilePublicID string
		if err = rows.Scan(&resourcePublicID, &canonicalID, &publicID, &label, &minecraftVersions, &loaders, &modVersion, &hasManualPage, &hasManualDetail, &hasDetail,
			&revisionID, &registry, &iconPath, &previewPath, &modSiteID, &names, &iconFilePublicID, &renderFilePublicID); err != nil {
			return err
		}
		locale, name := catalogResolvedName(names, primary, secondary, "en-US")
		detailURL := ""
		if hasDetail || hasManualPage {
			detailURL = "/mods/" + modSiteID + "/resources/" + resourcePublicID + "?version=" + publicID
		}
		version := map[string]any{"publicId": publicID, "label": label, "minecraftVersions": minecraftVersions, "loaders": loaders,
			"modVersion": modVersion, "hasDetail": hasDetail, "revisionId": revisionID,
			"registry": registry, "iconPath": iconPath, "previewPath": previewPath, "modSiteId": modSiteID, "names": json.RawMessage(names), "locale": locale, "name": name,
			"iconFileId": iconFilePublicID, "renderFileId": renderFilePublicID, "detailUrl": detailURL}
		item := byResource[resourcePublicID]
		item["versions"] = append(item["versions"].([]map[string]any), version)
	}
	return rows.Err()
}
