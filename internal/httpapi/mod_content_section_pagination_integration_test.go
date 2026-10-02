package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestModContentSectionCursorSearchAndGraphIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to run the mod-content section cursor integration test")
	}
	ctx := context.Background()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var modID, versionID, itemTemplateID, advancementTemplateID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values('crsrscale','cursor-scale','Cursor scale','approved') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,mod_version,status)
		values($1,'1.21.1 / NeoForge',array['1.21.1'],array['neoforge'],'1.0.0','active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from mod_content_templates where code='item_block' and builtin`).Scan(&itemTemplateID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from mod_content_templates where code='advancement' and builtin`).Scan(&advancementTemplateID); err != nil {
		t.Fatal(err)
	}
	var rootID, categoryID int64
	var rootPublicID string
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,default_locale,display_mode,status,ordinal)
		values($1,$2,$3,'en-US','compact','active',0) returning id,public_id`, modID, versionID, itemTemplateID).Scan(&rootID, &rootPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,parent_id,default_locale,display_mode,status,ordinal)
		values($1,$2,$3,$4,'en-US','compact','active',0) returning id`, modID, versionID, itemTemplateID, rootID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name)
		values($1,'en-US','Items'),($2,'en-US','Components')`, rootID, categoryID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into catalog_entities(identity_key,entity_type,status)
		select 'cursor-resource-'||value::text,'resource','active' from generate_series(1,350) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		select entity.id,'minecraft.item','cursor:item_'||lpad(split_part(entity.identity_key,'-',3),6,'0'),
		'cursor','item_'||lpad(split_part(entity.identity_key,'-',3),6,'0'),$1,true
		from catalog_entities entity where entity.identity_key like 'cursor-resource-%'`, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id)
		select entity.id,$1 from catalog_entities entity where entity.identity_key like 'cursor-resource-%'`, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,default_locale,definition,status)
		select entity.id,$1,'en-US','{}'::jsonb,'active' from catalog_entities entity where entity.identity_key like 'cursor-resource-%'`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_version_detail_localizations(resource_id,version_id,locale,name)
		select entity.id,$1,'en-US',case when entity.identity_key='cursor-resource-275' then 'Unique Needle Component'
		else 'Component '||split_part(entity.identity_key,'-',3) end
		from catalog_entities entity where entity.identity_key like 'cursor-resource-%'`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal)
		select $1,$2,entity.id,row_number() over(order by entity.id)::int-1
		from catalog_entities entity where entity.identity_key like 'cursor-resource-%' order by entity.id`, categoryID, versionID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	loadCards := func(rawQuery string) (modContentSectionIntegrationPage, int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/mods/cursor-scale/content-sections/root/resources?"+rawQuery, nil)
		request.SetPathValue("siteId", "cursor-scale")
		request.SetPathValue("sectionId", rootPublicID)
		response := httptest.NewRecorder()
		server.modContentSectionResources(response, request)
		var envelope struct {
			Data modContentSectionIntegrationPage `json:"data"`
		}
		if response.Code == http.StatusOK {
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
		}
		return envelope.Data, response.Code
	}

	page, status := loadCards("limit=47&locale=en-US")
	if status != http.StatusOK || page.Total != 350 || len(page.Items) != 47 || !page.HasMore || page.NextCursor == "" || len(page.Categories) != 1 {
		t.Fatalf("first page status=%d total=%d items=%d hasMore=%t cursor=%t categories=%d",
			status, page.Total, len(page.Items), page.HasMore, page.NextCursor != "", len(page.Categories))
	}
	seen := make(map[string]struct{}, page.Total)
	firstCursor := page.NextCursor
	for _, item := range page.Items {
		seen[item.ResourcePublicID] = struct{}{}
	}
	cursor := page.NextCursor
	for page.HasMore {
		page, status = loadCards("limit=47&locale=en-US&cursor=" + url.QueryEscape(cursor))
		if status != http.StatusOK || len(page.Categories) != 0 {
			t.Fatalf("next page status=%d categories=%d", status, len(page.Categories))
		}
		for _, item := range page.Items {
			if _, duplicate := seen[item.ResourcePublicID]; duplicate {
				t.Fatalf("resource %s was duplicated across cursor pages", item.ResourcePublicID)
			}
			seen[item.ResourcePublicID] = struct{}{}
		}
		cursor = page.NextCursor
	}
	if len(seen) != 350 || cursor != "" {
		t.Fatalf("cursor walk loaded=%d terminalCursor=%q", len(seen), cursor)
	}
	search, status := loadCards("limit=20&locale=en-US&q=needle")
	if status != http.StatusOK || search.Total != 1 || len(search.Items) != 1 || search.Items[0].CanonicalID != "cursor:item_000275" {
		t.Fatalf("indexed search status=%d total=%d items=%+v", status, search.Total, search.Items)
	}
	if _, err = pool.Exec(ctx, `update mod_resource_version_detail_localizations localization
		set name='Renamed Haystack Component' from catalog_entities entity
		where entity.id=localization.resource_id and entity.identity_key='cursor-resource-275' and localization.version_id=$1`, versionID); err != nil {
		t.Fatal(err)
	}
	if stale, staleStatus := loadCards("limit=20&locale=en-US&q=needle"); staleStatus != http.StatusOK || stale.Total != 0 {
		t.Fatalf("stale localized search status=%d total=%d", staleStatus, stale.Total)
	}
	if refreshed, refreshedStatus := loadCards("limit=20&locale=en-US&q=haystack"); refreshedStatus != http.StatusOK || refreshed.Total != 1 {
		t.Fatalf("refreshed localized search status=%d total=%d", refreshedStatus, refreshed.Total)
	}
	for name, query := range map[string]string{
		"deep offset": "offset=1000000", "legacy all": "all=1", "short query": "q=ab",
		"foreign cursor": "limit=47&q=needle&cursor=" + url.QueryEscape(firstCursor),
	} {
		if _, status = loadCards(query); status != http.StatusBadRequest {
			t.Errorf("%s returned %d; want 400", name, status)
		}
	}

	var advancementRootID int64
	var advancementRootPublicID string
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,default_locale,display_mode,status,ordinal)
		values($1,$2,$3,'en-US','large','active',1) returning id,public_id`, modID, versionID, advancementTemplateID).
		Scan(&advancementRootID, &advancementRootPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name) values($1,'en-US','Advancements')`, advancementRootID); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		identityKey := fmt.Sprintf("cursor-advancement-%d", index)
		canonicalID := fmt.Sprintf("cursor:adv_%06d", index)
		var resourceID int64
		if err = pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type,status) values($1,'resource','active') returning id`, identityKey).Scan(&resourceID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
			values($1,'minecraft.advancement',$2,'cursor',$3,$4,true)`, resourceID, canonicalID, fmt.Sprintf("adv_%06d", index), modID); err != nil {
			t.Fatal(err)
		}
		definition := map[string]any{"layoutGroupId": "graph-main", "display": map[string]any{"x": index, "y": index * 2, "frame": "task"}}
		if index > 0 {
			definition["parentId"] = fmt.Sprintf("cursor:adv_%06d", index-1)
		}
		rawDefinition, _ := json.Marshal(definition)
		if _, err = pool.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resourceID, modID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,default_locale,definition,status)
			values($1,$2,'en-US',$3,'active')`, resourceID, versionID, rawDefinition); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into mod_resource_version_detail_localizations(resource_id,version_id,locale,name)
			values($1,$2,'en-US',$3)`, resourceID, versionID, fmt.Sprintf("Advancement %d", index)); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal)
			values($1,$2,$3,$4)`, advancementRootID, versionID, resourceID, index); err != nil {
			t.Fatal(err)
		}
	}
	graphRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mods/cursor-scale/content-sections/root/resource-graph?limit=2&locale=en-US", nil)
	graphRequest.SetPathValue("siteId", "cursor-scale")
	graphRequest.SetPathValue("sectionId", advancementRootPublicID)
	graphResponse := httptest.NewRecorder()
	server.modContentAdvancementGraph(graphResponse, graphRequest)
	if graphResponse.Code != http.StatusOK || graphResponse.Body.Len() > maxModContentGraphResponseBytes ||
		!strings.Contains(graphResponse.Header().Get("Cache-Control"), "public") {
		t.Fatalf("graph status=%d bytes=%d cache=%q body=%s", graphResponse.Code, graphResponse.Body.Len(), graphResponse.Header().Get("Cache-Control"), graphResponse.Body.String())
	}
	var graphEnvelope struct {
		Data modContentSectionIntegrationPage `json:"data"`
	}
	if err = json.Unmarshal(graphResponse.Body.Bytes(), &graphEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(graphEnvelope.Data.Items) != 2 || !graphEnvelope.Data.HasMore || graphEnvelope.Data.NextCursor == "" ||
		graphEnvelope.Data.Items[1].Advancement == nil || graphEnvelope.Data.Items[1].Advancement.GroupID != "graph-main" ||
		graphEnvelope.Data.Items[1].Advancement.ParentResourcePublicID != graphEnvelope.Data.Items[0].ResourcePublicID {
		t.Fatalf("unexpected graph page: %s", graphResponse.Body.String())
	}

	t.Run("100k production handler and 1m keyset plan", func(t *testing.T) {
		var scaleRootID int64
		var scaleRootPublicID string
		if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,default_locale,display_mode,status,ordinal)
			values($1,$2,$3,'en-US','compact','active',2) returning id,public_id`, modID, versionID, itemTemplateID).
			Scan(&scaleRootID, &scaleRootPublicID); err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`alter table catalog_entities disable trigger user`,
			`alter table game_resources disable trigger user`,
			`alter table mod_content_section_resources disable trigger user`,
		} {
			if _, err = pool.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = pool.Exec(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
			select 'section-scale-'||value::text,'s'||lpad(value::text,8,'0'),'resource','active'
			from generate_series(1,100000) value`); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
			select entity.id,'minecraft.item','scale:item_'||lpad(split_part(entity.identity_key,'-',3),8,'0'),
			'scale','item_'||lpad(split_part(entity.identity_key,'-',3),8,'0'),$1,true
			from catalog_entities entity where entity.identity_key like 'section-scale-%'`, modID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into mod_content_section_resources(
			section_id,version_id,resource_id,placement_identity_key,search_document,ordinal)
			select $1,$2,resource.entity_id,'resource:'||resource.entity_id::text,
			 to_tsvector('simple',resource.canonical_id||case when resource.resource_path='item_00077778' then ' scale needle' else '' end),
			 row_number() over(order by resource.entity_id)::int-1
			from game_resources resource where resource.namespace='scale' order by resource.entity_id`, scaleRootID, versionID); err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`alter table mod_content_section_resources enable trigger user`,
			`alter table game_resources enable trigger user`,
			`alter table catalog_entities enable trigger user`,
			`analyze mod_content_section_resources`,
			`analyze catalog_entities`,
			`analyze game_resources`,
		} {
			if _, err = pool.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}

		loadScaleCards := func(rawQuery string) (modContentSectionIntegrationPage, int, time.Duration) {
			t.Helper()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/mods/cursor-scale/content-sections/root/resources?"+rawQuery, nil)
			request.SetPathValue("siteId", "cursor-scale")
			request.SetPathValue("sectionId", scaleRootPublicID)
			response := httptest.NewRecorder()
			started := time.Now()
			server.modContentSectionResources(response, request)
			elapsed := time.Since(started)
			var envelope struct {
				Data modContentSectionIntegrationPage `json:"data"`
			}
			if response.Code == http.StatusOK {
				if decodeErr := json.Unmarshal(response.Body.Bytes(), &envelope); decodeErr != nil {
					t.Fatal(decodeErr)
				}
			}
			return envelope.Data, response.Code, elapsed
		}
		firstScale, firstStatus, firstElapsed := loadScaleCards("limit=50&locale=en-US")
		if firstStatus != http.StatusOK || firstScale.Total != 100000 || len(firstScale.Items) != 50 || !firstScale.HasMore {
			t.Fatalf("100k first page status=%d total=%d items=%d hasMore=%t", firstStatus, firstScale.Total, len(firstScale.Items), firstScale.HasMore)
		}
		loadScaleLayout := func(rawQuery string) (modContentSectionIntegrationPage, int, int, time.Duration) {
			t.Helper()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/mods/cursor-scale/content-sections/root/layout?"+rawQuery, nil)
			request.SetPathValue("sectionId", scaleRootPublicID)
			response := httptest.NewRecorder()
			started := time.Now()
			server.writeModContentLayoutQuery(response, request, modIdentityRecord{ID: modID, SiteID: "cursor-scale"},
				"layout", false, maxModContentLayoutResponseBytes)
			elapsed := time.Since(started)
			var envelope struct {
				Data modContentSectionIntegrationPage `json:"data"`
			}
			if response.Code == http.StatusOK {
				if decodeErr := json.Unmarshal(response.Body.Bytes(), &envelope); decodeErr != nil {
					t.Fatal(decodeErr)
				}
			}
			if bodyBytes := response.Body.Bytes(); bytes.Contains(bodyBytes, []byte(`"definition"`)) || bytes.Contains(bodyBytes, []byte(`"names"`)) || bytes.Contains(bodyBytes, []byte(`"iconPath"`)) {
				t.Fatalf("layout summary leaked full resource fields: %.200s", bodyBytes)
			}
			return envelope.Data, response.Code, response.Body.Len(), elapsed
		}
		firstLayout, firstLayoutStatus, firstLayoutBytes, firstLayoutElapsed := loadScaleLayout("limit=500&locale=en-US")
		if firstLayoutStatus != http.StatusOK || firstLayout.Total != 100000 || len(firstLayout.Items) != 500 ||
			!firstLayout.HasMore || firstLayoutBytes > maxModContentLayoutResponseBytes {
			t.Fatalf("100k layout first status=%d total=%d items=%d hasMore=%t bytes=%d",
				firstLayoutStatus, firstLayout.Total, len(firstLayout.Items), firstLayout.HasMore, firstLayoutBytes)
		}
		var deepResourceID int64
		if err = pool.QueryRow(ctx, `select resource_id from mod_content_section_resources
			where section_id=$1 and version_id=$2 and ordinal=99949`, scaleRootID, versionID).Scan(&deepResourceID); err != nil {
			t.Fatal(err)
		}
		deepCursor := encodeModContentSectionCursor(modContentSectionPageCursor{
			Version:  modContentSectionCursorVersion,
			Scope:    modContentSectionCursorScope(modID, scaleRootID, versionID, "", "", 50, "cards"),
			SortPath: []int64{2, scaleRootID}, Ordinal: 99949, ResourceID: deepResourceID, Total: 100000,
		})
		deepPage, deepStatus, deepElapsed := loadScaleCards("limit=50&locale=en-US&cursor=" + url.QueryEscape(deepCursor))
		if deepStatus != http.StatusOK || len(deepPage.Items) != 50 || deepPage.Items[0].CanonicalID != "scale:item_00099951" || deepPage.HasMore {
			t.Fatalf("100k deep page status=%d items=%d first=%q hasMore=%t", deepStatus, len(deepPage.Items), firstCanonicalID(deepPage), deepPage.HasMore)
		}
		searchPage, searchStatus, searchElapsed := loadScaleCards("limit=50&locale=en-US&q=needle")
		if searchStatus != http.StatusOK || searchPage.Total != 1 || len(searchPage.Items) != 1 {
			t.Fatalf("100k search status=%d total=%d items=%d", searchStatus, searchPage.Total, len(searchPage.Items))
		}
		if deepElapsed > 2*time.Second || searchElapsed > 2*time.Second {
			t.Fatalf("100k handler exceeded budget: first=%s deep=%s search=%s", firstElapsed, deepElapsed, searchElapsed)
		}
		var layoutDeepResourceID int64
		if err = pool.QueryRow(ctx, `select resource_id from mod_content_section_resources
			where section_id=$1 and version_id=$2 and ordinal=19499`, scaleRootID, versionID).Scan(&layoutDeepResourceID); err != nil {
			t.Fatal(err)
		}
		layoutDeepCursor := encodeModContentSectionCursor(modContentSectionPageCursor{
			Version:  modContentSectionCursorVersion,
			Scope:    modContentSectionCursorScope(modID, scaleRootID, versionID, "", "", 500, "layout"),
			SortPath: []int64{2, scaleRootID}, Ordinal: 19499, ResourceID: layoutDeepResourceID, Total: 100000,
		})
		deepLayout, deepLayoutStatus, deepLayoutBytes, deepLayoutElapsed := loadScaleLayout("limit=500&locale=en-US&cursor=" + url.QueryEscape(layoutDeepCursor))
		if deepLayoutStatus != http.StatusOK || len(deepLayout.Items) != 500 || !deepLayout.HasMore ||
			deepLayoutBytes > maxModContentLayoutResponseBytes || firstLayoutElapsed > 3*time.Second || deepLayoutElapsed > 3*time.Second {
			t.Fatalf("100k layout deep status=%d items=%d hasMore=%t bytes=%d first=%s deep=%s",
				deepLayoutStatus, len(deepLayout.Items), deepLayout.HasMore, deepLayoutBytes, firstLayoutElapsed, deepLayoutElapsed)
		}

		if _, err = pool.Exec(ctx, `create temporary table mod_content_cursor_million(
			section_id bigint not null,version_id bigint not null,resource_id bigint not null,ordinal integer not null,search_document tsvector not null
		)`); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into mod_content_cursor_million
			select 1,1,value,value::int-1,case when value=777778 then to_tsvector('simple','million needle') else ''::tsvector end
			from generate_series(1,1000000) value`); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `create unique index mod_content_cursor_million_keyset
			on mod_content_cursor_million(section_id,version_id,ordinal,resource_id);
			create index mod_content_cursor_million_search on mod_content_cursor_million using gin(search_document);
			analyze mod_content_cursor_million`); err != nil {
			t.Fatal(err)
		}
		millionKeysetPlan := explainModContentScalePlan(t, ctx, pool, `select resource_id from mod_content_cursor_million
			where section_id=1 and version_id=1 and (ordinal,resource_id)>(999949,999950)
			order by ordinal,resource_id limit 50`)
		millionSearchPlan := explainModContentScalePlan(t, ctx, pool, `select count(*) from mod_content_cursor_million
			where search_document@@websearch_to_tsquery('simple','needle')`)
		if !strings.Contains(millionKeysetPlan, "mod_content_cursor_million_keyset") || strings.Contains(millionKeysetPlan, "Seq Scan") {
			t.Fatalf("1m keyset query missed its covering index:\n%s", millionKeysetPlan)
		}
		if !strings.Contains(millionSearchPlan, "mod_content_cursor_million_search") || strings.Contains(millionSearchPlan, "Seq Scan") {
			t.Fatalf("1m search query missed its GIN projection:\n%s", millionSearchPlan)
		}
		t.Logf("100k card handlers: first=%s deep=%s search=%s; layout500 first=%s/%dB depth20k=%s/%dB\n1m keyset plan:\n%s\n1m search plan:\n%s",
			firstElapsed, deepElapsed, searchElapsed, firstLayoutElapsed, firstLayoutBytes, deepLayoutElapsed, deepLayoutBytes,
			millionKeysetPlan, millionSearchPlan)
	})
}

type modContentSectionIntegrationPage struct {
	Total      int              `json:"total"`
	HasMore    bool             `json:"hasMore"`
	NextCursor string           `json:"nextCursor"`
	Categories []map[string]any `json:"categories"`
	Items      []struct {
		ResourcePublicID string `json:"resourcePublicId"`
		CanonicalID      string `json:"canonicalId"`
		Label            string `json:"label"`
		Advancement      *struct {
			ParentResourcePublicID string `json:"parentResourcePublicId"`
			GroupID                string `json:"groupId"`
		} `json:"advancement"`
	} `json:"items"`
}

func firstCanonicalID(page modContentSectionIntegrationPage) string {
	if len(page.Items) == 0 {
		return ""
	}
	return page.Items[0].CanonicalID
}

func explainModContentScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) string {
	t.Helper()
	rows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}
