package httpapi

import (
	"context"
	"encoding/json"
	"strings"
)

type exportResourceKey struct {
	RevisionID string
	ResourceID string
	Kind       string
}

type exportResourceSource struct {
	EntityID    string
	PublicID    string
	RevisionID  string
	ModSiteID   string
	KindCode    string
	Registry    string
	ObjectID    string
	IconPath    string
	PreviewPath string
	Names       map[string]string
}

// resolveExportResources resolves exported resources in one query. It prefers
// the requesting revision, then compatible active revisions across all mods.
// Registry entries and JEI ingredient media share this lookup so every feature
// can reference resources owned by another mod.
func (s *Server) resolveExportResources(ctx context.Context, keys []exportResourceKey) (map[exportResourceKey]exportResourceSource, error) {
	unique := make([]exportResourceKey, 0, len(keys))
	seen := make(map[exportResourceKey]struct{}, len(keys))
	for _, key := range keys {
		key.RevisionID = strings.TrimSpace(key.RevisionID)
		key.ResourceID = strings.TrimSpace(key.ResourceID)
		key.Kind = normalizeExportResourceKind(key.Kind)
		if key.RevisionID == "" || key.ResourceID == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}
	resolved := make(map[exportResourceKey]exportResourceSource, len(unique))
	if len(unique) == 0 {
		return resolved, nil
	}

	revisionIDs := make([]string, len(unique))
	resourceIDs := make([]string, len(unique))
	kinds := make([]string, len(unique))
	for index, key := range unique {
		revisionIDs[index] = key.RevisionID
		resourceIDs[index] = key.ResourceID
		kinds[index] = key.Kind
	}

	rows, err := s.db.Query(ctx, `with requested as (
		select distinct request.preferred_revision_id,request.resource_id,request.resource_kind,
			preferred.minecraft_version,preferred.loader
		from unnest($1::text[],$2::text[],$3::text[]) request(preferred_revision_id,resource_id,resource_kind)
		join catalog_import_revisions preferred on preferred.id::text=request.preferred_revision_id
	)
	select requested.preferred_revision_id,requested.resource_id,requested.resource_kind,
		coalesce(source.entity_id,''),coalesce(source.public_id,''),coalesce(source.revision_id,''),
		coalesce(source.site_id,''),coalesce(source.kind_code,''),coalesce(source.registry,''),
		coalesce(source.object_id,''),coalesce(source.icon_path,''),coalesce(source.preview_path,''),coalesce(source.names,'{}'::jsonb)
	from requested left join lateral (
		select candidate.entity_id,candidate.public_id,candidate.revision_id,candidate.site_id,candidate.kind_code,candidate.registry,candidate.object_id,
			candidate.icon_path,candidate.preview_path,candidate.names
		from (
			select resource.entity_id,entity.public_id,snapshot.revision_id,mod.slug site_id,
				resource.kind_code,snapshot.registry,resource.canonical_id object_id,
				snapshot.icon_path,snapshot.preview_path,snapshot.names,kind.family resource_kind,
				revision.minecraft_version,revision.loader,revision.is_active,revision.status,
				coalesce(revision.activated_at,revision.created_at) source_time,resource.namespace
			from game_resources resource
			join resource_kinds kind on kind.code=resource.kind_code
			join catalog_entities entity on entity.id=resource.entity_id
			join resource_import_snapshots snapshot on snapshot.resource_id=resource.entity_id
			join catalog_import_revisions revision on revision.id=snapshot.revision_id
			join mods mod on mod.id=revision.mod_id
			where resource.canonical_id=requested.resource_id
		) candidate
		where candidate.revision_id=requested.preferred_revision_id
			or (candidate.is_active and candidate.status in ('ready','partial'))
		order by (candidate.revision_id=requested.preferred_revision_id) desc,
			(candidate.resource_kind=requested.resource_kind) desc,
			(candidate.minecraft_version=requested.minecraft_version) desc,
			(candidate.loader=requested.loader) desc,
			(candidate.namespace=split_part(requested.resource_id,':',1)) desc,
			case when requested.resource_kind='' or candidate.resource_kind=requested.resource_kind then 0 else 1 end,
			candidate.source_time desc limit 1
	) source on true`, revisionIDs, resourceIDs, kinds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key exportResourceKey
		var source exportResourceSource
		var names []byte
		if err = rows.Scan(&key.RevisionID, &key.ResourceID, &key.Kind, &source.EntityID, &source.PublicID,
			&source.RevisionID, &source.ModSiteID, &source.KindCode, &source.Registry, &source.ObjectID,
			&source.IconPath, &source.PreviewPath, &names); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(names, &source.Names)
		if source.Names == nil {
			source.Names = map[string]string{}
		}
		if source.RevisionID != "" {
			resolved[key] = source
		}
	}
	return resolved, rows.Err()
}

func normalizeExportResourceKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if separator := strings.LastIndex(value, ":"); separator >= 0 {
		value = value[separator+1:]
	}
	switch {
	case strings.Contains(value, "fluid"):
		return "fluid"
	case strings.Contains(value, "gas"):
		return "gas"
	case strings.Contains(value, "infusion"):
		return "infusion"
	case strings.Contains(value, "pigment"):
		return "pigment"
	case strings.Contains(value, "slurry"):
		return "slurry"
	case strings.Contains(value, "block"):
		return "block"
	case strings.Contains(value, "entity"):
		return "entity"
	case strings.Contains(value, "effect"):
		return "effect"
	case strings.Contains(value, "item"):
		return "item"
	default:
		return value
	}
}

func localizedExportResourceNames(names map[string]string, locales ...string) map[string]string {
	result := make(map[string]string, len(locales)+2)
	for _, locale := range append(locales, "zh_cn", "en_us") {
		locale = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "-", "_"))
		if locale == "" {
			continue
		}
		if value := names[locale]; value != "" {
			result[locale] = value
		}
	}
	return result
}

func (s *Server) decorateLootTableResources(ctx context.Context, revisionID string, items []map[string]any, locales ...string) error {
	keys := make([]exportResourceKey, 0, len(items)*8)
	itemIDsByIndex := make([][]string, len(items))
	for index, item := range items {
		data, _ := item["data"].(map[string]any)
		itemIDs := lootTableItemIDs(data)
		itemIDsByIndex[index] = itemIDs
		for _, itemID := range itemIDs {
			keys = append(keys, exportResourceKey{RevisionID: revisionID, ResourceID: itemID, Kind: "item"})
		}
	}
	resolved, err := s.resolveExportResources(ctx, keys)
	if err != nil {
		return err
	}
	for index, item := range items {
		data, _ := item["data"].(map[string]any)
		if data == nil {
			data = make(map[string]any)
			item["data"] = data
		}
		sources := make(map[string]any)
		previews := make([]any, 0, min(len(itemIDsByIndex[index]), 12))
		for _, itemID := range itemIDsByIndex[index] {
			source, exists := resolved[exportResourceKey{RevisionID: revisionID, ResourceID: itemID, Kind: "item"}]
			if !exists {
				continue
			}
			value := map[string]any{
				"id": itemID, "entityId": source.EntityID, "publicId": source.PublicID,
				"sourceRevisionId": source.RevisionID, "sourceModSiteId": source.ModSiteID,
				"kindCode":       source.KindCode,
				"sourceRegistry": source.Registry, "sourceObjectId": source.ObjectID,
				"names":    localizedExportResourceNames(source.Names, locales...),
				"iconPath": source.IconPath, "previewPath": source.PreviewPath,
			}
			sources[itemID] = value
			if len(previews) < 12 && source.IconPath != "" {
				previews = append(previews, value)
			}
		}
		data["resourceSources"] = sources
		data["previewResources"] = previews
	}
	return nil
}

func lootTableItemIDs(data map[string]any) []string {
	result := make([]string, 0, 16)
	seen := make(map[string]struct{})
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if values, ok := data["possible_item_ids"].([]any); ok {
		for _, value := range values {
			if itemID, ok := value.(string); ok {
				add(itemID)
			}
		}
	}
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			entryType, _ := typed["type"].(string)
			if entryType == "minecraft:item" || entryType == "item" {
				if itemID, ok := typed["name"].(string); ok {
					add(itemID)
				} else if itemID, ok := typed["value"].(string); ok {
					add(itemID)
				}
			}
			for _, nested := range typed {
				visit(nested)
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		}
	}
	visit(data["definition"])
	return result
}
