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
	EntityID        string
	PublicID        string
	RevisionID      string
	ModSiteID       string
	VersionPublicID string
	KindCode        string
	Registry        string
	ObjectID        string
	IconPath        string
	PreviewPath     string
	Names           map[string]string
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
		// Manually-created resources have no import revision. Keep those keys
		// so the second-stage canonical-ID resolver can still turn them into
		// links after the referenced resource is collected by the site.
		if key.ResourceID == "" {
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
		coalesce(source.public_id,''),coalesce(source.public_id,''),coalesce(source.revision_id,''),
		coalesce(source.site_id,''),coalesce(source.version_public_id,''),coalesce(source.kind_code,''),coalesce(source.registry,''),
		coalesce(source.object_id,''),coalesce(source.icon_path,''),coalesce(source.preview_path,''),coalesce(source.names,'{}'::jsonb)
	from requested left join lateral (
		select candidate.entity_id,candidate.public_id,candidate.revision_id,candidate.site_id,candidate.version_public_id,candidate.kind_code,candidate.registry,candidate.object_id,
			candidate.icon_path,candidate.preview_path,candidate.names
		from (
			select resource.entity_id,entity.public_id,snapshot.revision_id,mod.slug site_id,version.public_id version_public_id,
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
			left join mod_content_versions version on version.id=revision.target_version_id
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
			&source.RevisionID, &source.ModSiteID, &source.VersionPublicID, &source.KindCode, &source.Registry, &source.ObjectID,
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
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	missing := make([]exportResourceKey, 0)
	for _, key := range unique {
		if _, exists := resolved[key]; !exists {
			missing = append(missing, key)
		}
	}
	manual, err := s.resolveManualModContentResources(ctx, missing)
	if err != nil {
		return nil, err
	}
	for key, source := range manual {
		resolved[key] = source
	}
	return resolved, nil
}

func (s *Server) resolveManualModContentResources(ctx context.Context, keys []exportResourceKey) (map[exportResourceKey]exportResourceSource, error) {
	result := make(map[exportResourceKey]exportResourceSource)
	if len(keys) == 0 {
		return result, nil
	}
	revisions := make([]string, len(keys))
	resourceIDs := make([]string, len(keys))
	kinds := make([]string, len(keys))
	for index, key := range keys {
		revisions[index], resourceIDs[index], kinds[index] = key.RevisionID, key.ResourceID, key.Kind
	}
	rows, err := s.db.Query(ctx, `with requested as (
		select request.preferred_revision_id,request.resource_id,request.resource_kind
		from unnest($1::text[],$2::text[],$3::text[]) request(preferred_revision_id,resource_id,resource_kind)
	)
	select requested.preferred_revision_id,requested.resource_id,requested.resource_kind,
		coalesce(source.public_id,''),coalesce(source.site_id,''),coalesce(source.version_public_id,''),
		coalesce(source.kind_code,''),coalesce(source.object_id,''),coalesce(source.names,'{}'::jsonb)
	from requested left join lateral (
		select entity.public_id,mod.slug site_id,version.public_id version_public_id,
			resource.kind_code,resource.canonical_id object_id,
			coalesce((select jsonb_object_agg(localization.locale,localization.name)
			 from mod_resource_version_detail_localizations localization
			 where localization.resource_id=resource.entity_id and localization.version_id=version.id),'{}'::jsonb) names
		from game_resources resource
		join catalog_entities entity on entity.id=resource.entity_id and entity.status='active'
		join mod_resource_bindings binding on binding.resource_id=resource.entity_id
		join mods mod on mod.id=binding.mod_id and mod.status='active'
		join mod_resource_version_details detail on detail.resource_id=resource.entity_id and detail.status='active'
		join mod_content_versions version on version.id=detail.version_id and version.status='active'
		where lower(resource.canonical_id)=lower(requested.resource_id)
		order by (lower(resource.kind_code)=lower('minecraft.'||requested.resource_kind)) desc,
			version.updated_at desc,resource.entity_id limit 1
	) source on true`, revisions, resourceIDs, kinds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key exportResourceKey
		var source exportResourceSource
		var names []byte
		if err = rows.Scan(&key.RevisionID, &key.ResourceID, &key.Kind, &source.PublicID, &source.ModSiteID,
			&source.VersionPublicID, &source.KindCode, &source.ObjectID, &names); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(names, &source.Names)
		if source.PublicID != "" {
			result[key] = source
		}
	}
	return result, rows.Err()
}

func normalizeExportResourceKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if separator := strings.LastIndex(value, ":"); separator >= 0 {
		value = value[separator+1:]
	}
	// Resource-kind codes use a dotted namespace (minecraft.loot_table),
	// while exporter registries and reference declarations use the family
	// name (loot_table). Normalize both representations before comparing.
	if namespace, family, found := strings.Cut(value, "."); found && family != "" {
		switch namespace {
		case "minecraft", "mod", "mekanism":
			value = family
		}
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
	for _, locale := range append(locales, "zh-CN", "en-US") {
		locale = normalizeContentLocale(locale)
		if locale == "" {
			continue
		}
		value := names[locale]
		if value == "" {
			value = names[strings.ToLower(strings.ReplaceAll(locale, "-", "_"))]
		}
		if value != "" {
			result[locale] = value
		}
	}
	return result
}

func (s *Server) decorateLootTableResources(ctx context.Context, revisionID string, items []map[string]any, locales ...string) error {
	revisionIDs := make([]string, len(items))
	for index := range revisionIDs {
		revisionIDs[index] = revisionID
	}
	return s.decorateLootTableResourcesByRevision(ctx, revisionIDs, items, locales...)
}

func (s *Server) decorateLootTableResourcesByRevision(ctx context.Context, revisionIDs []string, items []map[string]any, locales ...string) error {
	keys := make([]exportResourceKey, 0, len(items)*8)
	itemIDsByIndex := make([][]string, len(items))
	for index, item := range items {
		data, _ := item["data"].(map[string]any)
		itemIDs := lootTableItemIDs(data)
		itemIDsByIndex[index] = itemIDs
		revisionID := ""
		if index < len(revisionIDs) {
			revisionID = revisionIDs[index]
		}
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
		revisionID := ""
		if index < len(revisionIDs) {
			revisionID = revisionIDs[index]
		}
		sources := exportResourceSourceMap(data)
		previews := make([]any, 0, min(len(itemIDsByIndex[index]), 12))
		for _, itemID := range itemIDsByIndex[index] {
			source, exists := resolved[exportResourceKey{RevisionID: revisionID, ResourceID: itemID, Kind: "item"}]
			if !exists {
				continue
			}
			value := exportResourceSourceValue(itemID, source, locales...)
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

func (s *Server) decorateLootTableReferences(ctx context.Context, revisionID string, items []map[string]any, locales ...string) error {
	return s.decorateReferencedResources(ctx, revisionID, "loot_table", items, lootTableReferenceIDs, locales...)
}

func (s *Server) decorateCompatibleEnchantmentResources(ctx context.Context, revisionID string, items []map[string]any, locales ...string) error {
	return s.decorateReferencedResources(ctx, revisionID, "enchantment", items, compatibleEnchantmentIDs, locales...)
}

func (s *Server) decorateCanonicalDefinitionReferences(ctx context.Context, revisionID string, items []map[string]any, locales ...string) error {
	type reference struct {
		ID   string
		Kind string
	}
	referencesByIndex := make([][]reference, len(items))
	keys := make([]exportResourceKey, 0, len(items)*12)
	fields := map[string][]string{
		"enchantment":        {"compatibleEnchantments", "exclusiveWith"},
		"loot_table":         {"lootTable", "defaultLootTable", "referencedLootTables"},
		"item":               {"repairItems", "spawnEggs", "breedingMaterials", "possibleItemIds", "supportedItems", "bucketItemId", "iconItemId"},
		"biome":              {"biomeIds", "resolvedBiomeIds"},
		"dimension":          {"dimensionIds"},
		"entity":             {"spawnedEntityIds"},
		"natural_generation": {"featureIds", "carverIds"},
		"advancement":        {"parentId", "childrenIds"},
	}
	for index, item := range items {
		data, _ := item["data"].(map[string]any)
		seen := make(map[string]struct{})
		for kind, fieldNames := range fields {
			for _, fieldName := range fieldNames {
				for _, identifier := range canonicalReferenceValues(data[fieldName]) {
					key := kind + "\x00" + identifier
					if _, exists := seen[key]; exists {
						continue
					}
					seen[key] = struct{}{}
					referencesByIndex[index] = append(referencesByIndex[index], reference{ID: identifier, Kind: kind})
					keys = append(keys, exportResourceKey{RevisionID: revisionID, ResourceID: identifier, Kind: kind})
				}
			}
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
		sources := exportResourceSourceMap(data)
		for _, itemReference := range referencesByIndex[index] {
			key := exportResourceKey{RevisionID: revisionID, ResourceID: itemReference.ID, Kind: normalizeExportResourceKind(itemReference.Kind)}
			if source, exists := resolved[key]; exists && normalizeExportResourceKind(source.KindCode) == key.Kind {
				sources[itemReference.ID] = exportResourceSourceValue(itemReference.ID, source, locales...)
			}
		}
	}
	return nil
}

func canonicalReferenceValues(value any) []string {
	switch typed := value.(type) {
	case string:
		return uniqueTrimmed([]string{typed}, 1)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if identifier, ok := item.(string); ok {
				result = append(result, identifier)
			}
		}
		return uniqueTrimmed(result, 4096)
	default:
		return nil
	}
}

func (s *Server) decorateReferencedResources(
	ctx context.Context,
	revisionID string,
	kind string,
	items []map[string]any,
	resourceIDs func(map[string]any) []string,
	locales ...string,
) error {
	kind = normalizeExportResourceKind(kind)
	keys := make([]exportResourceKey, 0, len(items)*4)
	resourceIDsByIndex := make([][]string, len(items))
	for index, item := range items {
		data, _ := item["data"].(map[string]any)
		ids := resourceIDs(data)
		resourceIDsByIndex[index] = ids
		for _, resourceID := range ids {
			keys = append(keys, exportResourceKey{RevisionID: revisionID, ResourceID: resourceID, Kind: kind})
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
		sources := exportResourceSourceMap(data)
		for _, resourceID := range resourceIDsByIndex[index] {
			source, exists := resolved[exportResourceKey{RevisionID: revisionID, ResourceID: resourceID, Kind: kind}]
			if !exists {
				continue
			}
			sources[resourceID] = exportResourceSourceValue(resourceID, source, locales...)
		}
		data["resourceSources"] = sources
	}
	return nil
}

func exportResourceSourceMap(data map[string]any) map[string]any {
	if sources, ok := data["resourceSources"].(map[string]any); ok {
		return sources
	}
	sources := make(map[string]any)
	data["resourceSources"] = sources
	return sources
}

func exportResourceSourceValue(id string, source exportResourceSource, locales ...string) map[string]any {
	return map[string]any{
		"id": id, "entityId": source.EntityID, "publicId": source.PublicID,
		"sourceRevisionId": source.RevisionID, "sourceModSiteId": source.ModSiteID,
		"sourceVersionPublicId": source.VersionPublicID,
		"detailUrl":             canonicalResourceDetailURL(source),
		"kindCode":              source.KindCode,
		"sourceRegistry":        source.Registry, "sourceObjectId": source.ObjectID,
		"names":    localizedExportResourceNames(source.Names, locales...),
		"iconPath": source.IconPath, "previewPath": source.PreviewPath,
	}
}

func canonicalResourceDetailURL(source exportResourceSource) string {
	if source.ModSiteID == "" || source.PublicID == "" || source.VersionPublicID == "" {
		return ""
	}
	return "/mods/" + source.ModSiteID + "/resources/" + source.PublicID + "?version=" + source.VersionPublicID
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
	for _, key := range []string{"possible_item_ids", "possibleItemIds"} {
		if values, ok := data[key].([]any); ok {
			for _, value := range values {
				if itemID, ok := value.(string); ok {
					add(itemID)
				}
			}
		}
	}
	walkLootTableDefinition(data, func(entry map[string]any) {
		entryType, _ := entry["type"].(string)
		if entryType == "minecraft:item" || entryType == "item" {
			if itemID, ok := entry["name"].(string); ok {
				add(itemID)
			} else if itemID, ok := entry["value"].(string); ok {
				add(itemID)
			}
		}
	})
	return result
}

func walkLootTableDefinition(data map[string]any, visit func(map[string]any)) {
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			visit(typed)
			for _, nested := range typed {
				walk(nested)
			}
		case []any:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	// `definition` is the legacy exporter envelope; canonical website documents
	// store the same pools at the top level.
	walk(data["definition"])
	walk(data["pools"])
}

func lootTableIconPreviews(data map[string]any) []map[string]string {
	sources, _ := data["resourceSources"].(map[string]any)
	previews := make([]map[string]string, 0, len(sources))
	for _, itemID := range lootTableItemIDs(data) {
		source, _ := sources[itemID].(map[string]any)
		revisionID, _ := source["sourceRevisionId"].(string)
		iconPath, _ := source["iconPath"].(string)
		if revisionID == "" || iconPath == "" {
			continue
		}
		previews = append(previews, map[string]string{
			"sourceRevisionId": revisionID,
			"iconPath":         iconPath,
		})
	}
	return previews
}

func compatibleEnchantmentIDs(data map[string]any) []string {
	enchanting, _ := data["enchanting"].(map[string]any)
	values, _ := enchanting["compatible_enchantments"].([]any)
	if len(values) == 0 {
		values, _ = data["compatible_enchantments"].([]any)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		enchantmentID, _ := value.(string)
		enchantmentID = strings.TrimSpace(enchantmentID)
		if enchantmentID == "" {
			continue
		}
		if _, exists := seen[enchantmentID]; exists {
			continue
		}
		seen[enchantmentID] = struct{}{}
		result = append(result, enchantmentID)
	}
	return result
}

func lootTableReferenceIDs(data map[string]any) []string {
	result := make([]string, 0, 4)
	seen := make(map[string]struct{})
	add := func(value string) {
		value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	for _, key := range []string{"loot_table", "default_loot_table"} {
		if value, ok := data[key].(string); ok {
			add(value)
		}
	}
	for _, key := range []string{"referenced_loot_tables", "referencedLootTables"} {
		if values, ok := data[key].([]any); ok {
			for _, value := range values {
				if referenceID, ok := value.(string); ok {
					add(referenceID)
				}
			}
		}
	}
	walkLootTableDefinition(data, func(entry map[string]any) {
		entryType, _ := entry["type"].(string)
		entryKind, _ := entry["entry_kind"].(string)
		if strings.Contains(entryType, "loot_table") || strings.Contains(entryKind, "loot_table") {
			for _, key := range []string{"name", "value", "loot_table", "loot_table_id"} {
				if referenceID, ok := entry[key].(string); ok {
					add(referenceID)
				}
			}
		}
	})
	return result
}
