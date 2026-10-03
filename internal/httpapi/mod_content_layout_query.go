package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	maxModContentGraphResponseBytes  = 1 << 20
	maxModContentLayoutResponseBytes = 1 << 20
)

type modContentLayoutQueryItem struct {
	VersionPublicID  string                              `json:"versionPublicId"`
	ResourcePublicID string                              `json:"resourcePublicId"`
	SectionPublicID  string                              `json:"sectionPublicId"`
	Label            string                              `json:"label"`
	Ordinal          int                                 `json:"ordinal"`
	SimilarGroupID   string                              `json:"similarGroupId,omitempty"`
	Advancement      *modContentLayoutAdvancementSummary `json:"advancement,omitempty"`
	cursor           modContentSectionPageCursor
}

type modContentLayoutAdvancementSummary struct {
	ParentResourcePublicID string  `json:"parentResourcePublicId"`
	GroupID                string  `json:"groupId"`
	X                      float64 `json:"x"`
	Y                      float64 `json:"y"`
	Frame                  string  `json:"frame,omitempty"`
}

func (s *Server) modContentAdvancementGraph(w http.ResponseWriter, r *http.Request) {
	if !s.allowModContentRead(w, r, "mod-content-advancement-graph", 30) {
		return
	}
	identity, err := s.readableModIdentity(r.Context(), r.PathValue("siteId"), currentClaims(r), "content.review")
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod")
		return
	}
	s.writeModContentLayoutQuery(w, r, identity, "graph", true, maxModContentGraphResponseBytes)
}

func (s *Server) modContentSectionLayoutSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.allowModContentRead(w, r, "mod-content-layout-snapshot", 60) {
		return
	}
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	s.writeModContentLayoutQuery(w, r, identity, "layout", false, maxModContentLayoutResponseBytes)
}

func (s *Server) writeModContentLayoutQuery(
	w http.ResponseWriter,
	r *http.Request,
	identity modIdentityRecord,
	mode string,
	advancementOnly bool,
	responseBudget int,
) {
	values := r.URL.Query()
	query := strings.TrimSpace(values.Get("q"))
	if values.Has("offset") || values.Has("all") || (mode != "graph" && query != "") {
		writeAPIError(w, http.StatusBadRequest, "MOD_CONTENT_LAYOUT_CURSOR_REQUIRED", "layout resources accept only cursor pagination", 0, nil)
		return
	}
	if query != "" && (utf8.RuneCountInString(query) < 3 || utf8.RuneCountInString(query) > 100) {
		writeAPIError(w, http.StatusBadRequest, "MOD_CONTENT_SECTION_QUERY_INVALID", "resource search must contain between 3 and 100 characters", 0, nil)
		return
	}
	limit := boundedLimit(values.Get("limit"), 500, maxModContentLayoutPageSize)
	sectionPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("sectionId")))
	primary, secondary := s.requestContentLocales(r)
	if requestedLocale := normalizeContentLocale(values.Get("locale")); requestedLocale != "" {
		primary = requestedLocale
	}
	localeCandidates := []string{strings.ToLower(normalizeContentLocale(primary)), strings.ToLower(normalizeContentLocale(secondary)), "en", "en-us", "zh-cn", "zh-tw"}

	var sectionID, versionID int64
	var publicID, versionPublicID, versionLabel, templatePublicID, templateCode, templateI18nKey, parentPublicID, systemKey, defaultLocale, displayMode, status string
	var templateBuiltin bool
	var ordinal int
	var revisionID *string
	var localizations []byte
	err := s.db.QueryRow(r.Context(), `select section.id,section.version_id,section.public_id,version.public_id,version.label,
		template.public_id,template.code,template.builtin,template.i18n_key,coalesce(parent.public_id,''),section.system_key,section.default_locale,
		section.display_mode,section.ordinal,section.status,
		(select revision.public_id from content_revisions revision where revision.id=section.published_revision_id),
		coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
		 'summary',localization.description,'contentMarkdown','') order by localization.locale)
		 from mod_content_section_localizations localization where localization.section_id=section.id),'[]'::jsonb)
		from mod_content_sections section join mod_content_versions version on version.id=section.version_id
		join mod_content_templates template on template.id=section.template_id
		left join mod_content_sections parent on parent.id=section.parent_id
		where section.mod_id=$1 and section.public_id=$2 and section.parent_id is null
		 and section.status='active' and version.status='active'`, identity.ID, sectionPublicID).
		Scan(&sectionID, &versionID, &publicID, &versionPublicID, &versionLabel, &templatePublicID, &templateCode,
			&templateBuiltin, &templateI18nKey, &parentPublicID, &systemKey, &defaultLocale, &displayMode, &ordinal, &status, &revisionID, &localizations)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "root content section not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content section")
		return
	}
	if advancementOnly && templateCode != "advancement" {
		writeAPIError(w, http.StatusUnprocessableEntity, "MOD_CONTENT_GRAPH_NOT_ADVANCEMENT", "resource graph is available only for advancement sections", 0, nil)
		return
	}
	revisionScope := ""
	if revisionID != nil {
		revisionScope = *revisionID
	}
	cursorScope := modContentSectionCursorScope(identity.ID, sectionID, versionID, revisionScope, query, limit, mode)
	cursor, err := decodeModContentSectionCursor(values.Get("cursor"), cursorScope)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "MOD_CONTENT_SECTION_CURSOR_INVALID", "invalid layout resource cursor", 0, nil)
		return
	}
	total := 0
	if cursor != nil {
		total = cursor.Total
	} else if err = s.db.QueryRow(r.Context(), `with recursive subtree as (
		select id from mod_content_sections where id=$1 and status='active'
		union all select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id where child.status='active'
	) select count(*)::int from subtree
	join mod_content_section_resources placement on placement.section_id=subtree.id and placement.version_id=$2
	join catalog_entities entity on entity.id=placement.resource_id and entity.status='active' and entity.archived_at is null
	where ($3='' or placement.search_document@@websearch_to_tsquery('simple',$3))`, sectionID, versionID, query).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count layout resources")
		return
	}
	cursorActive := cursor != nil
	cursorPath := []int64{}
	cursorOrdinal := 0
	cursorResourceID := int64(0)
	if cursor != nil {
		cursorPath, cursorOrdinal, cursorResourceID = cursor.SortPath, cursor.Ordinal, cursor.ResourceID
	}

	rows, err := s.db.Query(r.Context(), `with recursive subtree as (
		select id,public_id,array[ordinal::bigint,id] sort_path from mod_content_sections where id=$1 and status='active'
		union all select child.id,child.public_id,parent.sort_path||array[child.ordinal::bigint,child.id]
		from mod_content_sections child join subtree parent on child.parent_id=parent.id where child.status='active'
	)
	select entity.public_id,subtree.public_id,placement.ordinal,placement.similar_group_id,
		coalesce(version_name.name,imported_name.name,left(resource.canonical_id,256)),
		resource.kind_code,coalesce(parent_entity.public_id,''),
		coalesce(nullif(effective.data->>'layoutGroupId',''),'advancement:'||entity.public_id),
		case when jsonb_typeof(effective.data#>'{display,x}')='number' then (effective.data#>>'{display,x}')::double precision
		 else (placement.ordinal%5)::double precision end,
		case when jsonb_typeof(effective.data#>'{display,y}')='number' then (effective.data#>>'{display,y}')::double precision
		 else floor(placement.ordinal/5.0)::double precision end,
		left(coalesce(effective.data#>>'{display,frame}',''),32),subtree.sort_path,placement.resource_id
	from subtree join mod_content_section_resources placement on placement.section_id=subtree.id and placement.version_id=$2
	join catalog_entities entity on entity.id=placement.resource_id and entity.status='active' and entity.archived_at is null
	join game_resources resource on resource.entity_id=placement.resource_id
	left join mod_resource_version_details detail on detail.resource_id=placement.resource_id and detail.version_id=$2 and detail.status='active'
	left join lateral (select snapshot.names,snapshot.data
	 from resource_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
	 where resource.kind_code='minecraft.advancement' and snapshot.resource_id=placement.resource_id
	  and revision.target_version_id=$2 and revision.is_active and revision.status in ('ready','partial')
	 order by coalesce(revision.activated_at,revision.created_at) desc limit 1) imported on true
	left join lateral (select case when detail.definition is not null and detail.definition<>'{}'::jsonb
	 then detail.definition else coalesce(imported.data,'{}'::jsonb) end data) effective on true
	left join lateral (select left(localization.name,256) name
	 from mod_resource_version_detail_localizations localization
	 where detail.resource_id is not null and localization.resource_id=placement.resource_id and localization.version_id=$2
	  and coalesce(localization.name,'')<>'' and replace(lower(localization.locale),'_','-')=any($3::text[])
	 order by array_position($3::text[],replace(lower(localization.locale),'_','-')) limit 1) version_name on true
	left join lateral (select left(name.value,256) name from jsonb_each_text(coalesce(imported.names,'{}'::jsonb)) name
	 where replace(lower(name.key),'_','-')=any($3::text[])
	 order by array_position($3::text[],replace(lower(name.key),'_','-')) limit 1) imported_name on true
	left join lateral (select parent_entity.public_id
	 from game_resources parent_resource join catalog_entities parent_entity on parent_entity.id=parent_resource.entity_id
	 join mod_resource_bindings parent_binding on parent_binding.resource_id=parent_resource.entity_id and parent_binding.mod_id=$4
	 join mod_resource_version_details parent_detail on parent_detail.resource_id=parent_resource.entity_id
	  and parent_detail.version_id=$2 and parent_detail.status='active'
	 where resource.kind_code='minecraft.advancement' and parent_resource.kind_code='minecraft.advancement'
	  and lower(parent_resource.canonical_id)=lower(coalesce(effective.data->>'parentId',effective.data->>'parent',''))
	  and parent_entity.status='active' and parent_entity.archived_at is null limit 1) parent_entity on true
	where ($5='' or placement.search_document@@websearch_to_tsquery('simple',$5))
	 and (not $6 or subtree.sort_path>$7::bigint[] or
	 (subtree.sort_path=$7::bigint[] and (placement.ordinal,placement.resource_id)>($8::integer,$9::bigint)))
	order by subtree.sort_path,placement.ordinal,placement.resource_id limit $10`,
		sectionID, versionID, localeCandidates, identity.ID, query, cursorActive, cursorPath, cursorOrdinal, cursorResourceID, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read layout resources")
		return
	}
	defer rows.Close()
	items := make([]modContentLayoutQueryItem, 0, min(limit+1, total))
	for rows.Next() {
		var item modContentLayoutQueryItem
		var kindCode, parentResourcePublicID, groupID, frame string
		var x, y float64
		var sortPath []int64
		var resourceID int64
		if err = rows.Scan(&item.ResourcePublicID, &item.SectionPublicID, &item.Ordinal, &item.SimilarGroupID, &item.Label,
			&kindCode, &parentResourcePublicID, &groupID, &x, &y, &frame, &sortPath, &resourceID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode layout resources")
			return
		}
		item.VersionPublicID = versionPublicID
		if kindCode == "minecraft.advancement" {
			item.Advancement = &modContentLayoutAdvancementSummary{
				ParentResourcePublicID: parentResourcePublicID, GroupID: groupID, X: x, Y: y, Frame: frame,
			}
		}
		item.cursor = modContentSectionPageCursor{Version: modContentSectionCursorVersion, Scope: cursorScope,
			SortPath: sortPath, Ordinal: item.Ordinal, ResourceID: resourceID, Total: total}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read layout resources")
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	categories := make([]map[string]any, 0)
	if cursor == nil {
		categories, err = readModContentDescendantSections(r.Context(), s.db, identity.ID, sectionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read content categories")
			return
		}
	}
	if advancementOnly {
		localizations = compactModContentLocalizations(localizations, localeCandidates)
		compactModContentCategoryLocalizations(categories, localeCandidates)
	}
	section := map[string]any{"publicId": publicID, "versionPublicId": versionPublicID, "templatePublicId": templatePublicID,
		"templateCode": templateCode, "templateBuiltin": templateBuiltin, "templateI18nKey": templateI18nKey,
		"parentPublicId": parentPublicID, "systemKey": systemKey, "defaultLocale": defaultLocale, "displayMode": displayMode,
		"ordinal": ordinal, "status": status, "publishedRevisionId": revisionID, "localizations": json.RawMessage(localizations), "resourceCount": total}
	data := map[string]any{"section": section, "versionLabel": versionLabel, "categories": categories, "items": items,
		"total": total, "limit": limit, "capabilities": modContentCapabilitiesFor(currentClaims(r), identity)}
	payload, _, encodeErr := encodeBudgetedModContentLayoutPage(data, items, hasMore, responseBudget)
	if encodeErr != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "MOD_CONTENT_LAYOUT_RESPONSE_BUDGET", "layout response exceeds its byte budget", 0, nil)
		return
	}
	w.Header().Add("Vary", "Authorization")
	w.Header().Add("Vary", "Cookie")
	if mode == "graph" && currentClaims(r).Subject == 0 {
		w.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=120")
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	writeJSONBytes(w, http.StatusOK, payload)
}

func encodeBudgetedModContentLayoutPage(data map[string]any, items []modContentLayoutQueryItem, hasMore bool, budget int) ([]byte, int, error) {
	for {
		nextCursor := ""
		pageHasMore := hasMore
		if pageHasMore && len(items) > 0 {
			nextCursor = encodeModContentSectionCursor(items[len(items)-1].cursor)
		}
		data["items"] = items
		data["hasMore"] = pageHasMore
		data["nextCursor"] = nextCursor
		payload, err := json.Marshal(apiResponse{Data: data})
		if err != nil {
			return nil, 0, err
		}
		if len(payload) <= budget {
			return append(payload, '\n'), len(items), nil
		}
		if len(items) <= 1 {
			return nil, 0, errors.New("mod-content layout response budget exceeded")
		}
		items = items[:len(items)-1]
		hasMore = true
	}
}

func compactModContentLocalizations(raw []byte, localeCandidates []string) []byte {
	var values []map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return []byte("[]")
	}
	allowed := make(map[string]struct{}, len(localeCandidates))
	for _, locale := range localeCandidates {
		allowed[strings.ReplaceAll(strings.ToLower(locale), "_", "-")] = struct{}{}
	}
	result := make([]map[string]any, 0, min(len(values), len(allowed)))
	for _, value := range values {
		locale, _ := value["locale"].(string)
		if _, ok := allowed[strings.ReplaceAll(strings.ToLower(locale), "_", "-")]; !ok {
			continue
		}
		name, _ := value["name"].(string)
		if len(name) > 256 {
			name = name[:256]
		}
		result = append(result, map[string]any{"locale": locale, "name": name, "summary": "", "contentMarkdown": ""})
	}
	encoded, _ := json.Marshal(result)
	return encoded
}

func compactModContentCategoryLocalizations(categories []map[string]any, localeCandidates []string) {
	for _, category := range categories {
		raw, ok := category["localizations"].(json.RawMessage)
		if !ok {
			continue
		}
		category["localizations"] = json.RawMessage(compactModContentLocalizations(raw, localeCandidates))
	}
}
