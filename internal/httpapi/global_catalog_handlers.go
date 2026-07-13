package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

const latestGlobalExportScopeCTE = `
latest_packages as (
	select distinct on (mod_id) mod_id,package_id
	from mod_export_revisions
	where is_active and status in ('ready','partial')
	order by mod_id,coalesce(activated_at,created_at) desc,created_at desc,package_id desc
), latest_revisions as (
	select revision.* from mod_export_revisions revision
	join latest_packages package on package.mod_id=revision.mod_id and package.package_id=revision.package_id
	where revision.is_active and revision.status in ('ready','partial')
)`

// Registry and tag identifiers cannot contain newlines. Using a printable,
// database-safe separator keeps the compound key reversible in audit history.
const globalCatalogKeySeparator = "\n"

type globalTagSnapshot struct {
	Registry        string   `json:"registry"`
	TagID           string   `json:"tagId"`
	Locale          string   `json:"locale"`
	ContentMarkdown string   `json:"contentMarkdown"`
	MemberIDs       []string `json:"memberIds"`
}

type globalRecipeTypeSnapshot struct {
	RecipeTypeID    string           `json:"recipeTypeId"`
	Locale          string           `json:"locale"`
	ContentMarkdown string           `json:"contentMarkdown"`
	Catalysts       []map[string]any `json:"catalysts"`
}

type globalRecipeSnapshot struct {
	RecipeKey      string         `json:"recipeKey"`
	Note           string         `json:"note"`
	LayoutOverride map[string]any `json:"layoutOverride,omitempty"`
}

func (s *Server) globalModTags(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 60)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	key := fmt.Sprintf("global-tags:list:%s:%s:%d:%d", registry, query, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		var total int
		countSQL := `with ` + latestGlobalExportScopeCTE + ` select count(*)::int from (
			select tag.registry,tag.tag_id from latest_revisions revision
			join mod_export_tags tag on tag.revision_id=revision.id
			where ($1='' or tag.registry=$1) and ($2='' or tag.tag_id ilike '%'||$2||'%')
			group by tag.registry,tag.tag_id) grouped`
		if err := s.db.QueryRow(ctx, countSQL, registry, query).Scan(&total); err != nil {
			return nil, err
		}
		listSQL := `with ` + latestGlobalExportScopeCTE + `
			select tag.registry,tag.tag_id,count(distinct member.member_id)::int
			from latest_revisions revision join mod_export_tags tag on tag.revision_id=revision.id
			left join mod_export_tag_members member on member.revision_id=tag.revision_id and member.registry=tag.registry and member.tag_id=tag.tag_id
			where ($1='' or tag.registry=$1) and ($2='' or tag.tag_id ilike '%'||$2||'%')
			group by tag.registry,tag.tag_id order by tag.registry,tag.tag_id limit $3 offset $4`
		rows, err := s.db.Query(ctx, listSQL, registry, query, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		for rows.Next() {
			var registryName, tagID string
			var memberCount int
			if err = rows.Scan(&registryName, &tagID, &memberCount); err != nil {
				return nil, err
			}
			previews, previewErr := s.globalTagMemberRows(ctx, registryName, tagID, "", "", 10, 0)
			if previewErr != nil {
				return nil, previewErr
			}
			items = append(items, map[string]any{"registry": registryName, "tagId": tagID, "memberCount": memberCount, "previews": previews})
		}
		return map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}, rows.Err()
	})
}

func (s *Server) globalModTagDetail(w http.ResponseWriter, r *http.Request) {
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	tagID := strings.TrimSpace(r.URL.Query().Get("tagId"))
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 80, 200)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	if registry == "" || tagID == "" || len(registry) > 160 || len(tagID) > 512 {
		writeError(w, http.StatusBadRequest, "registry and tagId are required")
		return
	}
	key := fmt.Sprintf("global-tags:detail:%s:%s:%s:%s:%d:%d", registry, tagID, primary, secondary, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		content, contentLocale, revisionID, err := s.localizedGlobalContent(ctx, "tag", registry+globalCatalogKeySeparator+tagID, primary, secondary)
		if err != nil {
			return nil, err
		}
		members, err := s.globalTagMemberRows(ctx, registry, tagID, primary, secondary, limit, offset)
		if err != nil {
			return nil, err
		}
		var total int
		if err = s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`
			select count(distinct member.member_id)::int from latest_revisions revision
			join mod_export_tag_members member on member.revision_id=revision.id
			where member.registry=$1 and member.tag_id=$2`, registry, tagID).Scan(&total); err != nil {
			return nil, err
		}
		var override []string
		if overrideErr := s.db.QueryRow(ctx, `select member_ids from global_tag_member_overrides where registry=$1 and tag_id=$2`, registry, tagID).Scan(&override); overrideErr == nil {
			total = len(override)
			members, err = s.globalResourceRows(ctx, override, primary, secondary, limit, offset)
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{"registry": registry, "tagId": tagID, "contentMarkdown": content,
			"contentLocale": contentLocale, "publishedRevisionId": revisionID, "memberCount": total,
			"members": members, "limit": limit, "offset": offset}, nil
	})
}

func (s *Server) updateGlobalModTag(w http.ResponseWriter, r *http.Request) {
	var snapshot globalTagSnapshot
	if err := decodeJSON(r, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	snapshot.Registry = strings.TrimSpace(snapshot.Registry)
	snapshot.TagID = strings.TrimSpace(snapshot.TagID)
	snapshot.Locale = normalizeExportContentLocale(snapshot.Locale)
	snapshot.MemberIDs = uniqueExportResourceIDs(snapshot.MemberIDs)
	if snapshot.Registry == "" || snapshot.TagID == "" || len(snapshot.ContentMarkdown) > maxModExportEntryMarkdownBytes || len(snapshot.MemberIDs) > maxExportTagMemberCount {
		writeError(w, http.StatusBadRequest, "invalid tag content")
		return
	}
	s.submitGlobalCatalogRevision(w, r, "global_tag", snapshot.Registry+globalCatalogKeySeparator+snapshot.TagID, snapshot)
}

func (s *Server) globalRecipeTypes(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 60)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	key := fmt.Sprintf("recipe-types:list:%s:%s:%s:%d:%d", query, primary, secondary, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		var total int
		if err := s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+` select count(distinct recipe_type.recipe_type_id)::int
			from latest_revisions revision join mod_export_recipe_types recipe_type on recipe_type.revision_id=revision.id
			where $1='' or recipe_type.recipe_type_id ilike '%'||$1||'%' or recipe_type.title_names->>$2 ilike '%'||$1||'%' or recipe_type.title_names->>$3 ilike '%'||$1||'%'`, query, primary, secondary).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := s.db.Query(ctx, `with `+latestGlobalExportScopeCTE+`
			select recipe_type.recipe_type_id,(jsonb_agg(recipe_type.title_names order by revision.activated_at desc nulls last)->0),
			(select count(distinct layout.recipe_key)::int from latest_revisions source
			 join mod_export_recipe_layouts layout on layout.revision_id=source.id where layout.recipe_type_id=recipe_type.recipe_type_id),
			coalesce(jsonb_agg(distinct (catalyst.value || jsonb_build_object('revisionId',recipe_type.revision_id)))
			 filter(where catalyst.value is not null),'[]'::jsonb),(array_agg(recipe_type.revision_id order by revision.activated_at desc nulls last))[1]
			from latest_revisions revision join mod_export_recipe_types recipe_type on recipe_type.revision_id=revision.id
			left join lateral jsonb_array_elements(recipe_type.catalysts) catalyst(value) on true
			where $1='' or recipe_type.recipe_type_id ilike '%'||$1||'%' or recipe_type.title_names->>$2 ilike '%'||$1||'%' or recipe_type.title_names->>$3 ilike '%'||$1||'%'
			group by recipe_type.recipe_type_id order by recipe_type.recipe_type_id limit $4 offset $5`, query, primary, secondary, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := make([]map[string]any, 0, limit)
		for rows.Next() {
			var id, revisionID string
			var names, catalysts []byte
			var recipeCount int
			if err = rows.Scan(&id, &names, &recipeCount, &catalysts, &revisionID); err != nil {
				return nil, err
			}
			items = append(items, map[string]any{"recipeTypeId": id, "names": json.RawMessage(names), "recipeCount": recipeCount,
				"catalysts": decorateCatalystJSON(catalysts, revisionID), "revisionId": revisionID})
		}
		return map[string]any{"items": items, "total": total, "limit": limit, "offset": offset}, rows.Err()
	})
}

func (s *Server) globalRecipeTypeDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	primary, secondary := requestedContentLocales(r)
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 80)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	if id == "" || len(id) > 512 {
		writeError(w, http.StatusBadRequest, "recipe type id is required")
		return
	}
	key := fmt.Sprintf("recipe-types:detail:v2:%s:%s:%s:%d:%d", id, primary, secondary, limit, offset)
	s.writeCachedCatalog(w, r, key, func(ctx context.Context) (any, error) {
		var names, catalysts []byte
		var revisionID, background string
		var width, height, scale int
		var contains bool
		err := s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+`
			select (jsonb_agg(recipe_type.title_names order by revision.activated_at desc nulls last)->0),
			coalesce(jsonb_agg(distinct (catalyst.value || jsonb_build_object('revisionId',recipe_type.revision_id)))
			 filter(where catalyst.value is not null),'[]'::jsonb),(array_agg(recipe_type.revision_id order by revision.activated_at desc nulls last))[1],
			(array_agg(recipe_type.background_path order by revision.activated_at desc nulls last))[1],
			(array_agg(recipe_type.width order by revision.activated_at desc nulls last))[1],
			(array_agg(recipe_type.height order by revision.activated_at desc nulls last))[1],
			(array_agg(recipe_type.image_scale order by revision.activated_at desc nulls last))[1],
			bool_or(recipe_type.background_contains_ingredients)
			from latest_revisions revision join mod_export_recipe_types recipe_type on recipe_type.revision_id=revision.id
			left join lateral jsonb_array_elements(recipe_type.catalysts) catalyst(value) on true
			where recipe_type.recipe_type_id=$1 group by recipe_type.recipe_type_id`, id).Scan(&names, &catalysts, &revisionID, &background, &width, &height, &scale, &contains)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errCatalogNotFound
		}
		if err != nil {
			return nil, err
		}
		var override []byte
		if overrideErr := s.db.QueryRow(ctx, `select catalysts from global_recipe_type_catalyst_overrides where recipe_type_id=$1`, id).Scan(&override); overrideErr == nil {
			catalysts = override
		}
		content, contentLocale, publishedRevisionID, err := s.localizedGlobalContent(ctx, "recipe_type", id, primary, secondary)
		if err != nil {
			return nil, err
		}
		var total int
		if err = s.db.QueryRow(ctx, `with `+latestGlobalExportScopeCTE+` select count(distinct layout.recipe_key)::int
			from latest_revisions revision join mod_export_recipe_layouts layout on layout.revision_id=revision.id where layout.recipe_type_id=$1`, id).Scan(&total); err != nil {
			return nil, err
		}
		rows, err := s.db.Query(ctx, `with `+latestGlobalExportScopeCTE+`
			select distinct on (layout.recipe_key) layout.recipe_key,layout.recipe_id,layout.recipe_id_source,
			layout.recipe_id_canonical,layout.semantic_fingerprint,layout.compact_layout,layout.revision_id,
			mod.slug,coalesce(content.note,''),coalesce(content.layout_override,layout.compact_layout)
			from latest_revisions revision join mod_export_recipe_layouts layout on layout.revision_id=revision.id
			join mods mod on mod.id=revision.mod_id left join global_recipe_contents content on content.recipe_key=layout.recipe_key
			where layout.recipe_type_id=$1 order by layout.recipe_key,revision.activated_at desc nulls last limit $2 offset $3`, id, limit, offset)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		recipes := make([]map[string]any, 0, limit)
		for rows.Next() {
			var recipeKey, recipeID, recipeIDSource, semanticFingerprint, sourceRevisionID, siteID, note string
			var recipeIDCanonical bool
			var original, effective []byte
			if err = rows.Scan(&recipeKey, &recipeID, &recipeIDSource, &recipeIDCanonical, &semanticFingerprint,
				&original, &sourceRevisionID, &siteID, &note, &effective); err != nil {
				return nil, err
			}
			var layout map[string]any
			if err = json.Unmarshal(effective, &layout); err != nil {
				return nil, err
			}
			recipes = append(recipes, map[string]any{"recipeKey": recipeKey, "recipeId": recipeID, "recipeIdSource": recipeIDSource,
				"recipeIdCanonical": recipeIDCanonical, "semanticFingerprint": semanticFingerprint, "revisionId": sourceRevisionID,
				"modSiteId": siteID, "note": note, "layout": layout})
		}
		if err = s.decorateGlobalRecipeTags(ctx, recipes); err != nil {
			return nil, err
		}
		if err = s.decorateRecipeResources(ctx, recipes); err != nil {
			return nil, err
		}
		return map[string]any{"recipeTypeId": id, "names": json.RawMessage(names), "contentMarkdown": content,
			"contentLocale": contentLocale, "publishedRevisionId": publishedRevisionID, "catalysts": decorateCatalystJSON(catalysts, revisionID),
			"backgroundPath": background, "revisionId": revisionID, "width": width, "height": height, "imageScale": scale,
			"backgroundContainsIngredients": contains, "recipes": recipes, "total": total, "limit": limit, "offset": offset}, rows.Err()
	})
}

func (s *Server) decorateGlobalRecipeTags(ctx context.Context, recipes []map[string]any) error {
	type recipeTagKey struct {
		revisionID string
		recipeKey  string
		slotIndex  int
	}

	revisionIDs := make([]string, 0, len(recipes))
	recipeKeys := make([]string, 0, len(recipes))
	seenRecipes := make(map[string]struct{}, len(recipes))
	for _, recipe := range recipes {
		revisionID, _ := recipe["revisionId"].(string)
		recipeKey, _ := recipe["recipeKey"].(string)
		pairKey := revisionID + "\x00" + recipeKey
		if revisionID == "" || recipeKey == "" {
			continue
		}
		if _, exists := seenRecipes[pairKey]; exists {
			continue
		}
		seenRecipes[pairKey] = struct{}{}
		revisionIDs = append(revisionIDs, revisionID)
		recipeKeys = append(recipeKeys, recipeKey)
	}
	precomputed := make(map[recipeTagKey]string)
	if len(revisionIDs) > 0 {
		rows, err := s.db.Query(ctx, `with requested as (
			select distinct revision_id,recipe_key from unnest($1::text[],$2::text[]) request(revision_id,recipe_key)
		)
		select item.revision_id,item.recipe_key,item.slot_index,max(item.tag_id)
		from requested join mod_export_recipe_items item using(revision_id,recipe_key)
		where item.tag_id<>'' group by item.revision_id,item.recipe_key,item.slot_index`, revisionIDs, recipeKeys)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key recipeTagKey
			var tagID string
			if err = rows.Scan(&key.revisionID, &key.recipeKey, &key.slotIndex, &tagID); err != nil {
				rows.Close()
				return err
			}
			precomputed[key] = tagID
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}

	cache := make(map[string]string)
	for _, recipe := range recipes {
		revisionID, _ := recipe["revisionId"].(string)
		recipeKey, _ := recipe["recipeKey"].(string)
		layout, _ := recipe["layout"].(map[string]any)
		slots, _ := layout["slots"].([]any)
		for slotIndex, rawSlot := range slots {
			slot, _ := rawSlot.(map[string]any)
			if slot["role"] != "input" {
				continue
			}
			tagID := exportRecipeSlotTagID(slot)
			if tagID == "" {
				tagID = precomputed[recipeTagKey{revisionID: revisionID, recipeKey: recipeKey, slotIndex: slotIndex}]
			}
			if tagID != "" {
				slot["tag"] = tagID
				continue
			}
			alternatives, _ := slot["alternatives"].([]any)
			ids := make([]string, 0, len(alternatives))
			for _, rawAlternative := range alternatives {
				alternative, _ := rawAlternative.(map[string]any)
				item, _ := alternative["item"].(string)
				if item == "" {
					item, _ = alternative["resource_location"].(string)
				}
				if item != "" {
					ids = append(ids, item)
				}
			}
			ids = uniqueExportResourceIDs(ids)
			if len(ids) < 2 {
				continue
			}
			sort.Strings(ids)
			cacheKey := revisionID + "\x00" + strings.Join(ids, "\x00")
			cachedTagID, exists := cache[cacheKey]
			if !exists {
				err := s.db.QueryRow(ctx, `select tag.tag_id from mod_export_tags tag
					where tag.revision_id=$1 and tag.registry='minecraft:item' and tag.member_count=$2
					and not exists(select 1 from mod_export_tag_members member where member.revision_id=tag.revision_id
						and member.registry=tag.registry and member.tag_id=tag.tag_id and not(member.member_id=any($3::text[])))
					order by tag.tag_id limit 1`, revisionID, len(ids), ids).Scan(&cachedTagID)
				if errors.Is(err, pgx.ErrNoRows) {
					cachedTagID = ""
				} else if err != nil {
					return err
				}
				cache[cacheKey] = cachedTagID
			}
			if cachedTagID != "" {
				slot["tag"] = cachedTagID
			}
		}
	}
	return nil
}

type recipeResourceSource struct {
	RevisionID string
	ModSiteID  string
	Registry   string
	IconPath   string
}

// decorateRecipeResources resolves every recipe ingredient against the recipe's
// own export first, then against the latest active exports from every mod. This
// keeps cross-mod recipes working without issuing one database query per slot.
func (s *Server) decorateRecipeResources(ctx context.Context, recipes []map[string]any) error {
	type requestKey struct {
		revisionID string
		itemID     string
	}

	keys := make([]requestKey, 0, len(recipes)*8)
	seen := make(map[requestKey]struct{})
	for _, recipe := range recipes {
		revisionID, _ := recipe["revisionId"].(string)
		layout, _ := recipe["layout"].(map[string]any)
		slots, _ := layout["slots"].([]any)
		for _, rawSlot := range slots {
			slot, _ := rawSlot.(map[string]any)
			alternatives, _ := slot["alternatives"].([]any)
			for _, rawAlternative := range alternatives {
				alternative, _ := rawAlternative.(map[string]any)
				itemID := recipeAlternativeItemID(alternative)
				key := requestKey{revisionID: revisionID, itemID: itemID}
				if itemID == "" {
					continue
				}
				if _, exists := seen[key]; !exists {
					seen[key] = struct{}{}
					keys = append(keys, key)
				}
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}

	itemIDs := make([]string, len(keys))
	revisionIDs := make([]string, len(keys))
	for index, key := range keys {
		itemIDs[index] = key.itemID
		revisionIDs[index] = key.revisionID
	}
	rows, err := s.db.Query(ctx, `with requested as (
		select distinct request.item_id,request.recipe_revision_id,recipe_revision.minecraft_version,recipe_revision.loader
		from unnest($1::text[],$2::text[]) request(item_id,recipe_revision_id)
		join mod_export_revisions recipe_revision on recipe_revision.id::text=request.recipe_revision_id
	)
	select requested.item_id,requested.recipe_revision_id,coalesce(source.revision_id,''),
	coalesce(source.site_id,''),coalesce(source.registry,''),coalesce(source.icon_path,'')
	from requested left join lateral (
		select entry.revision_id::text revision_id,mod.slug site_id,entry.registry,
		case entry.registry when 'items' then 'icons/items/32/'||entry.namespace||'/'||entry.object_path||'.png'
		when 'blocks' then 'icons/blocks/32/'||entry.namespace||'/'||entry.object_path||'.png' else '' end icon_path
		from mod_export_registry_entries entry
		join mod_export_revisions revision on revision.id=entry.revision_id
		join mods mod on mod.id=revision.mod_id
		where entry.object_id=requested.item_id and entry.registry in ('items','blocks')
		and (entry.revision_id::text=requested.recipe_revision_id or
			(revision.is_active and revision.status in ('ready','partial')))
		order by (entry.revision_id::text=requested.recipe_revision_id) desc,
		(revision.minecraft_version=requested.minecraft_version) desc,
		(revision.loader=requested.loader) desc,
		(entry.namespace=split_part(requested.item_id,':',1)) desc,(entry.registry='items') desc,
		coalesce(revision.activated_at,revision.created_at) desc limit 1
	) source on true`, itemIDs, revisionIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	resolved := make(map[requestKey]recipeResourceSource, len(keys))
	for rows.Next() {
		var key requestKey
		var source recipeResourceSource
		if err = rows.Scan(&key.itemID, &key.revisionID, &source.RevisionID, &source.ModSiteID, &source.Registry, &source.IconPath); err != nil {
			return err
		}
		if source.RevisionID != "" {
			resolved[key] = source
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}

	for _, recipe := range recipes {
		revisionID, _ := recipe["revisionId"].(string)
		layout, _ := recipe["layout"].(map[string]any)
		slots, _ := layout["slots"].([]any)
		for _, rawSlot := range slots {
			slot, _ := rawSlot.(map[string]any)
			alternatives, _ := slot["alternatives"].([]any)
			for _, rawAlternative := range alternatives {
				alternative, _ := rawAlternative.(map[string]any)
				source, exists := resolved[requestKey{revisionID: revisionID, itemID: recipeAlternativeItemID(alternative)}]
				if !exists {
					continue
				}
				alternative["sourceRevisionId"] = source.RevisionID
				alternative["sourceModSiteId"] = source.ModSiteID
				alternative["sourceRegistry"] = source.Registry
				alternative["iconPath"] = source.IconPath
			}
		}
	}
	return nil
}

func recipeAlternativeItemID(alternative map[string]any) string {
	itemID, _ := alternative["item"].(string)
	if itemID == "" {
		itemID, _ = alternative["resource_location"].(string)
	}
	return itemID
}

func (s *Server) updateGlobalRecipeType(w http.ResponseWriter, r *http.Request) {
	var snapshot globalRecipeTypeSnapshot
	if err := decodeJSON(r, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	snapshot.RecipeTypeID = strings.TrimSpace(snapshot.RecipeTypeID)
	snapshot.Locale = normalizeExportContentLocale(snapshot.Locale)
	if snapshot.RecipeTypeID == "" || len(snapshot.ContentMarkdown) > maxModExportEntryMarkdownBytes || len(snapshot.Catalysts) > 500 {
		writeError(w, http.StatusBadRequest, "invalid recipe type content")
		return
	}
	s.submitGlobalCatalogRevision(w, r, "global_recipe_type", snapshot.RecipeTypeID, snapshot)
}

func (s *Server) updateGlobalRecipe(w http.ResponseWriter, r *http.Request) {
	var snapshot globalRecipeSnapshot
	if err := decodeJSON(r, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pathKey := strings.TrimSpace(r.PathValue("recipeKey"))
	if snapshot.RecipeKey != "" && strings.TrimSpace(snapshot.RecipeKey) != pathKey {
		writeError(w, http.StatusBadRequest, "recipe key does not match request path")
		return
	}
	snapshot.RecipeKey = pathKey
	if snapshot.RecipeKey == "" || len(snapshot.Note) > 4096 {
		writeError(w, http.StatusBadRequest, "invalid recipe content")
		return
	}
	var exists bool
	if err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`
		select exists(select 1 from latest_revisions revision join mod_export_recipe_layouts layout on layout.revision_id=revision.id
		where layout.recipe_key=$1)`, snapshot.RecipeKey).Scan(&exists); err != nil || !exists {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	s.submitGlobalCatalogRevision(w, r, "global_recipe", snapshot.RecipeKey, snapshot)
}

var errCatalogNotFound = errors.New("catalog entry not found")

func (s *Server) writeCachedCatalog(w http.ResponseWriter, r *http.Request, key string, loader func(context.Context) (any, error)) {
	var datasetVersion string
	if err := s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`
		select coalesce(md5(string_agg(id||':'||package_id,',' order by id)),'empty') from latest_revisions`).Scan(&datasetVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read catalog version")
		return
	}
	cacheKey := key + ":dataset:" + datasetVersion
	data, err := s.cache.GetOrLoad(r.Context(), cacheKey, func(ctx context.Context) ([]byte, error) {
		value, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		return json.Marshal(apiResponse{Data: value})
	})
	if errors.Is(err, errCatalogNotFound) {
		writeError(w, http.StatusNotFound, "catalog entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query catalog")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=30, stale-while-revalidate=90")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func requestedContentLocales(r *http.Request) (string, string) {
	primary := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	secondary := normalizeExportContentLocale(r.URL.Query().Get("secondaryLocale"))
	if secondary == primary {
		secondary = "en_us"
	}
	return primary, secondary
}

func (s *Server) localizedGlobalContent(ctx context.Context, kind, key, primary, secondary string) (string, string, *int64, error) {
	var query string
	var args []any
	if kind == "tag" {
		registry, tagID, _ := strings.Cut(key, globalCatalogKeySeparator)
		query = `select content_markdown,locale,published_revision_id from global_tag_contents where registry=$1 and tag_id=$2 and locale=any($3::text[])
			order by array_position($3::text[],locale) limit 1`
		args = []any{registry, tagID, []string{primary, secondary, "en_us", "zh_cn"}}
	} else {
		query = `select content_markdown,locale,published_revision_id from global_recipe_type_contents where recipe_type_id=$1 and locale=any($2::text[])
			order by array_position($2::text[],locale) limit 1`
		args = []any{key, []string{primary, secondary, "en_us", "zh_cn"}}
	}
	var content, locale string
	var revisionID *int64
	err := s.db.QueryRow(ctx, query, args...).Scan(&content, &locale, &revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, nil
	}
	return content, locale, revisionID, err
}

func (s *Server) globalTagMemberRows(ctx context.Context, registry, tagID, primary, secondary string, limit, offset int) ([]map[string]any, error) {
	query := `with ` + latestGlobalExportScopeCTE + `, members as (
		select distinct member.member_id from latest_revisions revision join mod_export_tag_members member on member.revision_id=revision.id
		where member.registry=$1 and member.tag_id=$2 order by member.member_id limit $3 offset $4)
		select member.member_id,coalesce(entry.registry,''),coalesce(entry.names,'{}'::jsonb),coalesce(entry.revision_id,''),
		coalesce(mod.slug,''),coalesce(case entry.registry when 'items' then 'icons/items/32/'||entry.namespace||'/'||entry.object_path||'.png'
		when 'blocks' then 'icons/blocks/32/'||entry.namespace||'/'||entry.object_path||'.png' else '' end,'')
		from members member left join lateral (
			select candidate.* from latest_revisions source join mod_export_registry_entries candidate on candidate.revision_id=source.id
			where candidate.object_id=member.member_id and candidate.registry in ('items','blocks')
			order by (candidate.registry='items') desc,source.activated_at desc nulls last limit 1
		) entry on true left join mod_export_revisions source_revision on source_revision.id=entry.revision_id
		left join mods mod on mod.id=source_revision.mod_id order by member.member_id`
	rows, err := s.db.Query(ctx, query, registry, tagID, limit, offset)
	if err != nil {
		return nil, err
	}
	return scanGlobalResources(rows)
}

func (s *Server) globalResourceRows(ctx context.Context, memberIDs []string, primary, secondary string, limit, offset int) ([]map[string]any, error) {
	if offset >= len(memberIDs) {
		return []map[string]any{}, nil
	}
	end := min(len(memberIDs), offset+limit)
	selected := memberIDs[offset:end]
	query := `with ` + latestGlobalExportScopeCTE + ` select member.member_id,coalesce(entry.registry,''),coalesce(entry.names,'{}'::jsonb),
		coalesce(entry.revision_id,''),coalesce(mod.slug,''),coalesce(case entry.registry when 'items' then 'icons/items/32/'||entry.namespace||'/'||entry.object_path||'.png'
		when 'blocks' then 'icons/blocks/32/'||entry.namespace||'/'||entry.object_path||'.png' else '' end,'')
		from unnest($1::text[]) with ordinality member(member_id,ordinal) left join lateral (
			select candidate.* from latest_revisions source join mod_export_registry_entries candidate on candidate.revision_id=source.id
			where candidate.object_id=member.member_id and candidate.registry in ('items','blocks') order by (candidate.registry='items') desc limit 1
		) entry on true left join mod_export_revisions source_revision on source_revision.id=entry.revision_id
		left join mods mod on mod.id=source_revision.mod_id order by member.ordinal`
	rows, err := s.db.Query(ctx, query, selected)
	if err != nil {
		return nil, err
	}
	return scanGlobalResources(rows)
}

func scanGlobalResources(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, registry, revisionID, siteID, iconPath string
		var names []byte
		if err := rows.Scan(&id, &registry, &names, &revisionID, &siteID, &iconPath); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "registry": registry, "names": json.RawMessage(names), "revisionId": revisionID, "modSiteId": siteID, "iconPath": iconPath})
	}
	return items, rows.Err()
}

func decorateCatalystJSON(raw []byte, revisionID string) []map[string]any {
	var catalysts []map[string]any
	_ = json.Unmarshal(raw, &catalysts)
	for _, catalyst := range catalysts {
		item, _ := catalyst["item"].(string)
		if item == "" {
			item, _ = catalyst["resource_location"].(string)
		}
		if _, exists := catalyst["revisionId"]; !exists {
			catalyst["revisionId"] = revisionID
		}
		catalyst["iconPath"] = itemIconAssetPath(item)
	}
	return catalysts
}

func itemIconAssetPath(itemID string) string {
	namespace, objectPath, found := strings.Cut(itemID, ":")
	if !found || namespace == "" || objectPath == "" {
		return ""
	}
	return "icons/items/32/" + namespace + "/" + objectPath + ".png"
}

func (s *Server) submitGlobalCatalogRevision(w http.ResponseWriter, r *http.Request, aggregateType, aggregateKey string, snapshot any) {
	claims := currentClaims(r)
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid catalog content")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start catalog revision")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock catalog content")
		return
	}
	var baseRevisionID *int64
	_ = tx.QueryRow(r.Context(), `select max(id) from content_revisions where aggregate_type=$1 and aggregate_key=$2 and id in (
		select published_revision_id from global_tag_contents where published_revision_id is not null union all
		select published_revision_id from global_tag_member_overrides where published_revision_id is not null union all
		select published_revision_id from global_recipe_type_contents where published_revision_id is not null union all
		select published_revision_id from global_recipe_type_catalyst_overrides where published_revision_id is not null union all
		select published_revision_id from global_recipe_contents where published_revision_id is not null)`, aggregateType, aggregateKey).Scan(&baseRevisionID)
	status := "pending"
	if hasPermission(claims.Permissions, "content.review") {
		status = "approved"
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{AggregateType: aggregateType, AggregateKey: aggregateKey,
		BaseRevision: baseRevisionID, Snapshot: encoded, Reason: "Update global catalog content", ActorID: claims.Subject,
		Source: "user", Status: status, Metadata: map[string]any{"catalog": aggregateType}, Request: r})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create catalog revision")
		return
	}
	if status == "approved" {
		if err = publishGlobalCatalogSnapshotTx(r.Context(), tx, created.RevisionID, aggregateType, encoded, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish catalog revision")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record catalog approval")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit catalog revision")
		return
	}
	s.cache.InvalidatePrefix(context.Background(), "global-tags:")
	s.cache.InvalidatePrefix(context.Background(), "recipe-types:")
	writeJSON(w, http.StatusAccepted, map[string]any{"status": status, "revisionId": created.RevisionID, "changeRequestId": created.ChangeRequestID})
}

func publishGlobalCatalogSnapshotTx(ctx context.Context, tx pgx.Tx, revisionID int64, aggregateType string, raw []byte, actorID int64) error {
	switch aggregateType {
	case "global_tag":
		var snapshot globalTagSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into global_tag_contents(registry,tag_id,locale,content_markdown,updated_by,published_revision_id)
			values($1,$2,$3,$4,$5,$6) on conflict(registry,tag_id,locale) do update set content_markdown=excluded.content_markdown,
			updated_by=excluded.updated_by,published_revision_id=excluded.published_revision_id,updated_at=now()`, snapshot.Registry, snapshot.TagID,
			snapshot.Locale, snapshot.ContentMarkdown, nullableActorID(actorID), revisionID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into global_tag_member_overrides(registry,tag_id,member_ids,updated_by,published_revision_id)
			values($1,$2,$3,$4,$5) on conflict(registry,tag_id) do update set member_ids=excluded.member_ids,updated_by=excluded.updated_by,
			published_revision_id=excluded.published_revision_id,updated_at=now()`, snapshot.Registry, snapshot.TagID, snapshot.MemberIDs, nullableActorID(actorID), revisionID)
		return err
	case "global_recipe_type":
		var snapshot globalRecipeTypeSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into global_recipe_type_contents(recipe_type_id,locale,content_markdown,updated_by,published_revision_id)
			values($1,$2,$3,$4,$5) on conflict(recipe_type_id,locale) do update set content_markdown=excluded.content_markdown,
			updated_by=excluded.updated_by,published_revision_id=excluded.published_revision_id,updated_at=now()`, snapshot.RecipeTypeID,
			snapshot.Locale, snapshot.ContentMarkdown, nullableActorID(actorID), revisionID); err != nil {
			return err
		}
		catalysts, _ := json.Marshal(snapshot.Catalysts)
		_, err := tx.Exec(ctx, `insert into global_recipe_type_catalyst_overrides(recipe_type_id,catalysts,updated_by,published_revision_id)
			values($1,$2::jsonb,$3,$4) on conflict(recipe_type_id) do update set catalysts=excluded.catalysts,updated_by=excluded.updated_by,
			published_revision_id=excluded.published_revision_id,updated_at=now()`, snapshot.RecipeTypeID, string(catalysts), nullableActorID(actorID), revisionID)
		return err
	case "global_recipe":
		var snapshot globalRecipeSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return err
		}
		var override any
		if len(snapshot.LayoutOverride) > 0 {
			encoded, _ := json.Marshal(snapshot.LayoutOverride)
			override = string(encoded)
		}
		_, err := tx.Exec(ctx, `insert into global_recipe_contents(recipe_key,note,layout_override,updated_by,published_revision_id)
			values($1,$2,$3::jsonb,$4,$5) on conflict(recipe_key) do update set note=excluded.note,layout_override=excluded.layout_override,
			updated_by=excluded.updated_by,published_revision_id=excluded.published_revision_id,updated_at=now()`, snapshot.RecipeKey, snapshot.Note, override, nullableActorID(actorID), revisionID)
		return err
	default:
		return errors.New("unsupported global catalog revision")
	}
}
