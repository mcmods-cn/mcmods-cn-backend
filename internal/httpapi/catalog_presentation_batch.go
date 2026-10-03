package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

const maxCatalogPresentationBatchItems = 1000

type catalogPresentationBatchReference struct {
	PublicID string `json:"publicId"`
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Registry string `json:"registry"`
}

type catalogPresentationBatchRequest struct {
	Locale string                              `json:"locale"`
	Items  []catalogPresentationBatchReference `json:"items"`
}

type catalogPresentationSource struct {
	PublicID string `json:"publicId,omitempty"`
	SiteID   string `json:"siteId,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
}

type catalogPresentationBatchItem struct {
	PublicID       string                    `json:"publicId"`
	ID             string                    `json:"id"`
	Registry       string                    `json:"registry"`
	Kind           string                    `json:"kind"`
	Names          map[string]string         `json:"names"`
	ResolvedName   string                    `json:"resolvedName,omitempty"`
	ResolvedLocale string                    `json:"resolvedLocale,omitempty"`
	IconURL        string                    `json:"iconUrl,omitempty"`
	Source         catalogPresentationSource `json:"source,omitempty"`
}

var catalogPresentationProjectKinds = stringSet(
	"mod", "modpack", "minecraft_server", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon",
)

func normalizeCatalogPresentationBatchRequest(request catalogPresentationBatchRequest) (string, string, []catalogPresentationBatchReference, error) {
	if len(request.Items) == 0 || len(request.Items) > maxCatalogPresentationBatchItems {
		return "", "", nil, errors.New("resource presentation batch size must be between 1 and 1000")
	}
	primary := strings.TrimSpace(request.Locale)
	if primary == "" {
		primary = "zh-CN"
	}
	var err error
	primary, err = normalizeCatalogLocale(primary)
	if err != nil {
		return "", "", nil, errors.New("invalid resource presentation locale")
	}
	secondary := "en-US"
	if primary == "zh-CN" {
		secondary = "zh-TW"
	} else if primary == "zh-TW" {
		secondary = "zh-CN"
	}
	items := make([]catalogPresentationBatchReference, 0, len(request.Items))
	seen := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		item.PublicID = strings.ToLower(strings.TrimSpace(item.PublicID))
		item.ID = strings.ToLower(strings.TrimSpace(item.ID))
		item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
		item.Registry = strings.ToLower(strings.TrimSpace(item.Registry))
		if (item.PublicID == "" && item.ID == "") || len(item.PublicID) > 180 || len(item.ID) > 255 ||
			len(item.Kind) > 64 || len(item.Registry) > 128 {
			return "", "", nil, errors.New("invalid resource presentation reference")
		}
		key := item.PublicID + "\x00" + item.ID + "\x00" + item.Kind + "\x00" + item.Registry
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}
	return primary, secondary, items, nil
}

func (s *Server) catalogResourcePresentations(w http.ResponseWriter, r *http.Request) {
	var request catalogPresentationBatchRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid resource presentation batch")
		return
	}
	primary, secondary, references, err := normalizeCatalogPresentationBatchRequest(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resources := make([]catalogPresentationBatchReference, 0, len(references))
	tags := make([]catalogPresentationBatchReference, 0, len(references))
	projects := make([]catalogPresentationBatchReference, 0, len(references))
	for _, reference := range references {
		switch {
		case reference.Kind == "tag":
			tags = append(tags, reference)
		case catalogPresentationProjectKinds[reference.Kind]:
			projects = append(projects, reference)
		default:
			resources = append(resources, reference)
		}
	}
	items := make([]catalogPresentationBatchItem, 0, len(references))
	groups := []struct {
		items []catalogPresentationBatchReference
		load  func(context.Context, []catalogPresentationBatchReference, string, string) ([]catalogPresentationBatchItem, error)
	}{
		{resources, s.loadCatalogResourcePresentations},
		{tags, s.loadCatalogTagPresentations},
		{projects, s.loadProjectPresentations},
	}
	for _, group := range groups {
		if len(group.items) == 0 {
			continue
		}
		loaded, loadErr := group.load(r.Context(), group.items, primary, secondary)
		if loadErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve resource presentations")
			return
		}
		items = append(items, loaded...)
	}
	writeBoundedCatalogJSON(w, map[string]any{"items": items})
}

func catalogPresentationInputs(items []catalogPresentationBatchReference) ([]string, []string, []string, []string) {
	publicIDs := make([]string, len(items))
	identifiers := make([]string, len(items))
	kinds := make([]string, len(items))
	registries := make([]string, len(items))
	for index, item := range items {
		publicIDs[index], identifiers[index], kinds[index], registries[index] = item.PublicID, strings.TrimPrefix(item.ID, "#"), item.Kind, item.Registry
	}
	return publicIDs, identifiers, kinds, registries
}

func (s *Server) loadCatalogResourcePresentations(ctx context.Context, items []catalogPresentationBatchReference, primary, secondary string) ([]catalogPresentationBatchItem, error) {
	publicIDs, identifiers, kinds, registries := catalogPresentationInputs(items)
	rows, err := s.db.Query(ctx, `with input as (
		select * from unnest($1::text[],$2::text[],$3::text[],$4::text[]) input(public_id,identifier,kind,registry)
	), selected as materialized (
		select distinct match.entity_id from input join lateral (
			select resource.entity_id from game_resources resource join catalog_entities entity on entity.id=resource.entity_id
			where entity.status='active' and `+publicCatalogEntitySQL("entity", "resource")+` and (
				(input.public_id<>'' and entity.public_id=input.public_id) or
				(input.identifier<>'' and (lower(resource.canonical_id)=input.identifier or exists(
					select 1 from game_resource_aliases alias where alias.resource_id=resource.entity_id
					and (input.kind='' or input.kind='resource' or alias.kind_code=input.kind) and lower(alias.alias_id)=input.identifier
				)) and (input.kind='' or input.kind='resource' or resource.kind_code=input.kind)))
			order by case when entity.public_id=input.public_id then 0 else 1 end limit 1
		) match on true
	), `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`
	select entity.public_id,resource.canonical_id,resource.namespace,resource.kind_code,entity.default_locale,
		coalesce(localization.locale,''),coalesce(localization.name,''),
		coalesce(imported.names,'{}'::jsonb) || coalesce((select jsonb_object_agg(candidate.locale,candidate.name)
			from content_localizations candidate where candidate.catalog_entity_id=entity.id and candidate.name<>''),'{}'::jsonb),
		coalesce((select public_id from oss_files where id=definition.icon_file_id),''),coalesce(imported.icon_path,''),
		coalesce(owner.project_code,''),coalesce(owner.slug,''),coalesce(owner.primary_name,'')
	from selected join game_resources resource on resource.entity_id=selected.entity_id
	join catalog_entities entity on entity.id=resource.entity_id
	left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
	left join latest_resource_snapshots imported on imported.resource_id=resource.entity_id
	left join mods owner on owner.id=resource.owner_mod_id and owner.review_status='approved'
	left join lateral (select candidate.locale,candidate.name from content_localizations candidate
		where candidate.catalog_entity_id=entity.id order by case candidate.locale when $5 then 0 when $6 then 1
		when entity.default_locale then 2 when 'en-US' then 3 else 4 end limit 1) localization on true
	order by resource.kind_code,resource.canonical_id`, publicIDs, identifiers, kinds, registries, primary, secondary)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]catalogPresentationBatchItem, 0, len(items))
	for rows.Next() {
		var item catalogPresentationBatchItem
		var defaultLocale, locale, name, iconFileID, importedIconPath string
		var names []byte
		if err = rows.Scan(&item.PublicID, &item.ID, &item.Registry, &item.Kind, &defaultLocale, &locale, &name, &names,
			&iconFileID, &importedIconPath, &item.Source.PublicID, &item.Source.SiteID, &item.Source.Name); err != nil {
			return nil, err
		}
		if resolvedLocale, resolvedName := catalogResolvedName(names, primary, secondary, defaultLocale); resolvedName != "" {
			locale, name = resolvedLocale, resolvedName
		}
		item.ResolvedLocale, item.ResolvedName = locale, truncateUTF8(name, 512)
		item.Names = presentationName(locale, item.ResolvedName)
		item.IconURL = catalogRecipeResourceIconURL(item.PublicID, iconFileID, "", importedIconPath)
		if item.Source.PublicID != "" || item.Source.SiteID != "" || item.Source.Name != "" {
			item.Source.Type = "mod"
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Server) loadCatalogTagPresentations(ctx context.Context, items []catalogPresentationBatchReference, primary, secondary string) ([]catalogPresentationBatchItem, error) {
	publicIDs, identifiers, _, registries := catalogPresentationInputs(items)
	rows, err := s.db.Query(ctx, `with input as (
		select * from unnest($1::text[],$2::text[],$3::text[]) input(public_id,identifier,registry)
	), selected as materialized (
		select distinct match.entity_id from input join lateral (
			select tag.entity_id from catalog_tags tag join catalog_entities entity on entity.id=tag.entity_id
			where entity.status='active' and `+publicCatalogEntitySQL("entity", "tag")+` and ((input.public_id<>'' and entity.public_id=input.public_id) or
				(input.identifier<>'' and (lower(tag.canonical_id)=input.identifier or lower(tag.registry||':'||tag.canonical_id)=input.identifier)
				and (input.registry='' or lower(tag.registry)=input.registry)))
			order by case when entity.public_id=input.public_id then 0 else 1 end limit 1
		) match on true
	)
	select entity.public_id,tag.canonical_id,tag.registry,entity.default_locale,
		coalesce(localization.locale,''),coalesce(localization.name,''),
		coalesce((select jsonb_object_agg(candidate.locale,candidate.name) from content_localizations candidate
			where candidate.catalog_entity_id=entity.id and candidate.name<>''),'{}'::jsonb)
	from selected join catalog_tags tag on tag.entity_id=selected.entity_id
	join catalog_entities entity on entity.id=tag.entity_id
	left join lateral (select candidate.locale,candidate.name from content_localizations candidate
		where candidate.catalog_entity_id=entity.id order by case candidate.locale when $4 then 0 when $5 then 1
		when entity.default_locale then 2 when 'en-US' then 3 else 4 end limit 1) localization on true
	order by tag.registry,tag.canonical_id`, publicIDs, identifiers, registries, primary, secondary)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]catalogPresentationBatchItem, 0, len(items))
	for rows.Next() {
		var item catalogPresentationBatchItem
		var defaultLocale, locale, name string
		var names []byte
		if err = rows.Scan(&item.PublicID, &item.ID, &item.Registry, &defaultLocale, &locale, &name, &names); err != nil {
			return nil, err
		}
		if resolvedLocale, resolvedName := catalogResolvedName(names, primary, secondary, defaultLocale); resolvedName != "" {
			locale, name = resolvedLocale, resolvedName
		}
		item.Kind = "tag"
		item.ResolvedLocale, item.ResolvedName = locale, truncateUTF8(name, 512)
		item.Names = presentationName(locale, item.ResolvedName)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Server) loadProjectPresentations(ctx context.Context, items []catalogPresentationBatchReference, primary, _ string) ([]catalogPresentationBatchItem, error) {
	publicIDs, identifiers, kinds, _ := catalogPresentationInputs(items)
	rows, err := s.db.Query(ctx, `with input as (
		select * from unnest($1::text[],$2::text[],$3::text[]) input(public_id,identifier,kind)
	)
	select match.public_id,match.identifier,match.registry,match.kind,match.name,match.icon_url,
		match.source_public_id,match.source_site_id,match.source_name
	from input join lateral (
		select candidate.* from (
			select mod.project_code public_id,coalesce((select identifier from mod_identifiers where mod_id=mod.id order by is_primary desc,display_order,id limit 1),mod.slug) identifier,
				'mods' registry,'mod' kind,mod.primary_name name,mod.icon_url,
				mod.project_code source_public_id,mod.slug source_site_id,mod.primary_name source_name,
				case when mod.project_code=input.public_id then 0 else 1 end priority
			from mods mod where input.kind='mod' and mod.review_status='approved' and
				(mod.project_code=input.public_id or lower(mod.slug)=input.identifier or exists(
					select 1 from mod_identifiers identifier where identifier.mod_id=mod.id and lower(identifier.identifier)=input.identifier))
			union all
			select pack.public_id,pack.slug,'modpacks','modpack',coalesce(nullif(pack.secondary_name,''),pack.primary_name),pack.icon_url,
				pack.public_id,pack.slug,pack.primary_name,case when pack.public_id=input.public_id then 0 else 1 end
			from modpacks pack where input.kind='modpack' and pack.review_status='approved' and
				(pack.public_id=input.public_id or lower(pack.slug)=input.identifier)
			union all
			select project.public_id,project.slug,project.project_type,project.project_type,project.primary_name,project.icon_url,
				project.public_id,project.slug,project.primary_name,case when project.public_id=input.public_id then 0 else 1 end
			from simple_projects project where input.kind=project.project_type and project.review_status='approved' and
				(project.public_id=input.public_id or lower(project.slug)=input.identifier)
			union all
			select server.public_id,server.public_id,'minecraft_server','minecraft_server',server.name,'',
				server.public_id,server.public_id,server.name,case when server.public_id=input.public_id then 0 else 1 end
			from minecraft_servers server where input.kind='minecraft_server' and server.review_status='approved' and
				(server.public_id=input.public_id or lower(server.slug)=input.identifier)
		) candidate order by candidate.priority limit 1
	) match on true`, publicIDs, identifiers, kinds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]catalogPresentationBatchItem, 0, len(items))
	for rows.Next() {
		var item catalogPresentationBatchItem
		if err = rows.Scan(&item.PublicID, &item.ID, &item.Registry, &item.Kind, &item.ResolvedName, &item.IconURL,
			&item.Source.PublicID, &item.Source.SiteID, &item.Source.Name); err != nil {
			return nil, err
		}
		item.ResolvedName = truncateUTF8(item.ResolvedName, 512)
		item.ResolvedLocale = primary
		item.Names = presentationName(primary, item.ResolvedName)
		item.Source.Type = item.Kind
		result = append(result, item)
	}
	return result, rows.Err()
}

func presentationName(locale, name string) map[string]string {
	if locale == "" || name == "" {
		return map[string]string{}
	}
	return map[string]string{locale: name}
}

func truncateUTF8(value string, maximumRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maximumRunes {
		return string(runes[:maximumRunes])
	}
	return value
}
