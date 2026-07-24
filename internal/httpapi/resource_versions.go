package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
)

// decorateResourceVersionRows keeps tags and resource pages attached to the
// immutable global identity while exposing every mod-content version that can
// supply a concrete name/icon/detail for that identity.
func (s *Server) decorateResourceVersionRows(ctx context.Context, items []map[string]any, primary, secondary string) error {
	resourceIDs := make([]string, 0, len(items))
	byResource := make(map[string]map[string]any, len(items))
	for _, item := range items {
		resourceID, _ := item["entityId"].(string)
		if resourceID == "" {
			continue
		}
		if _, exists := byResource[resourceID]; !exists {
			resourceIDs = append(resourceIDs, resourceID)
			byResource[resourceID] = item
		}
		item["versions"] = []map[string]any{}
	}
	if len(resourceIDs) == 0 {
		return nil
	}
	rows, err := s.db.Query(ctx, `select resource.entity_id,entity.public_id,resource.canonical_id,version.public_id,version.label,version.minecraft_versions,version.loaders,
		version.mod_version,
		(binding.resource_id is not null) has_manual_page,
		(version_detail.resource_id is not null) has_manual_detail,
		(version_detail.resource_id is not null or snapshot.revision_id is not null) has_detail,
		coalesce(snapshot.revision_id,''),coalesce(snapshot.registry,''),coalesce(snapshot.icon_path,''),mod.slug,
		coalesce(nullif(version_names.names,'{}'::jsonb),nullif(snapshot.names,'{}'::jsonb),'{}'::jsonb),
		coalesce(version_detail.icon_file_id,0)
		from game_resources resource
		join catalog_entities entity on entity.id=resource.entity_id
		join mods mod on mod.id=resource.owner_mod_id
		join mod_content_versions version on version.mod_id=resource.owner_mod_id and version.status='active'
		left join mod_resource_bindings binding on binding.resource_id=resource.entity_id and binding.mod_id=resource.owner_mod_id
		left join lateral (select imported.revision_id,imported.registry,imported.icon_path,imported.names
		 from resource_import_snapshots imported join catalog_import_revisions revision on revision.id=imported.revision_id
		 where imported.resource_id=resource.entity_id and revision.target_version_public_id=version.public_id and revision.is_active
		 order by (imported.icon_path<>'') desc,revision.created_at desc limit 1) snapshot on true
		left join mod_resource_version_details version_detail on version_detail.resource_id=resource.entity_id and version_detail.version_id=version.id and version_detail.status='active'
		left join lateral (select jsonb_object_agg(localization.locale,localization.name) names
		 from mod_resource_version_detail_localizations localization where localization.resource_id=resource.entity_id and localization.version_id=version.id and coalesce(localization.name,'')<>'') version_names on true
		where resource.entity_id=any($1::text[])
		order by resource.entity_id,version.updated_at desc,version.id desc`, resourceIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var resourceID, resourcePublicID, canonicalID, publicID, label, modVersion, revisionID, registry, iconPath, modSiteID string
		var minecraftVersions, loaders []string
		var hasManualPage, hasManualDetail, hasDetail bool
		var names []byte
		var iconFileID sql.NullInt64
		if err = rows.Scan(&resourceID, &resourcePublicID, &canonicalID, &publicID, &label, &minecraftVersions, &loaders, &modVersion, &hasManualPage, &hasManualDetail, &hasDetail,
			&revisionID, &registry, &iconPath, &modSiteID, &names, &iconFileID); err != nil {
			return err
		}
		locale, name := catalogResolvedName(names, primary, secondary, "en-US")
		detailURL := ""
		if hasManualDetail {
			detailURL = "/mods/" + modSiteID + "/resources/" + resourcePublicID + "?version=" + publicID
		} else if revisionID != "" {
			detailURL = "/mods/" + modSiteID + "/data/" + revisionID + "/itemsBlocks/entry?entityId=" + url.QueryEscape(resourceID) + "&registry=" + url.QueryEscape(registry) + "&objectId=" + url.QueryEscape(canonicalID)
		} else if hasManualPage {
			detailURL = "/mods/" + modSiteID + "/resources/" + resourcePublicID + "?version=" + publicID
		}
		version := map[string]any{"publicId": publicID, "label": label, "minecraftVersions": minecraftVersions, "loaders": loaders,
			"modVersion": modVersion, "hasDetail": hasDetail, "revisionId": revisionID,
			"registry": registry, "iconPath": iconPath, "modSiteId": modSiteID, "names": json.RawMessage(names), "locale": locale, "name": name,
			"iconFileId": nullableCatalogInt64(iconFileID), "detailUrl": detailURL}
		item := byResource[resourceID]
		item["versions"] = append(item["versions"].([]map[string]any), version)
	}
	return rows.Err()
}
