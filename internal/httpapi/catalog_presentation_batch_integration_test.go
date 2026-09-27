package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestCatalogResourcePresentationBatchHasConstantQueryBudgetIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify resource presentation batching against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	})

	if _, err = pool.Exec(ctx, `insert into catalog_entities(identity_key,entity_type,status,default_locale)
		select 'perf068:resource:'||value,'resource','active','zh-CN' from generate_series(1,1000) value;
		insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		select id,'minecraft.item','perf068:item_'||lpad(substring(identity_key from '[0-9]+$'),4,'0'),
			'perf068','item_'||lpad(substring(identity_key from '[0-9]+$'),4,'0'),true
		from catalog_entities where identity_key like 'perf068:resource:%';
		insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name)
		select 'resource',id,id,'zh-CN','PERF068 Item '||substring(identity_key from '[0-9]+$')
		from catalog_entities where identity_key like 'perf068:resource:%';
		insert into catalog_entities(identity_key,entity_type,status,default_locale)
		values('perf068:tag','tag','active','zh-CN');
		insert into catalog_tags(entity_id,registry,canonical_id)
		select id,'minecraft:item','perf068:all' from catalog_entities where identity_key='perf068:tag';
		insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name)
		select 'tag',id,id,'zh-CN','PERF068 All' from catalog_entities where identity_key='perf068:tag';
		insert into mods(project_code,slug,primary_name,review_status,published_at)
		values('p68mod001','perf068-mod','PERF068 Mod','approved',now())`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	resourceReferences := make([]catalogPresentationBatchReference, 0, maxCatalogPresentationBatchItems)
	for value := 1; value <= maxCatalogPresentationBatchItems; value++ {
		resourceReferences = append(resourceReferences, catalogPresentationBatchReference{
			ID: fmt.Sprintf("perf068:item_%04d", value), Kind: "minecraft.item", Registry: "perf068",
		})
	}
	started := time.Now()
	resourceItems, resourceBytes := invokeCatalogPresentationBatchIntegration(t, ctx, server, counter, resourceReferences, 1)
	if len(resourceItems) != maxCatalogPresentationBatchItems {
		t.Fatalf("resource presentations=%d want=%d", len(resourceItems), maxCatalogPresentationBatchItems)
	}
	if resourceBytes > maxCatalogResponseBytes {
		t.Fatalf("resource presentation response=%d bytes budget=%d", resourceBytes, maxCatalogResponseBytes)
	}
	if resourceItems[0].ResolvedName == "" || resourceItems[0].Names["zh-CN"] == "" {
		t.Fatalf("resource presentation omitted localized display fields: %+v", resourceItems[0])
	}
	t.Logf("1000 resource presentations: queries=1 bytes=%d wall=%s", resourceBytes, time.Since(started))

	mixedReferences := []catalogPresentationBatchReference{
		{ID: "perf068:item_0001", Kind: "minecraft.item", Registry: "perf068"},
		{ID: "#perf068:all", Kind: "tag", Registry: "minecraft:item"},
		{PublicID: "p68mod001", Kind: "mod"},
	}
	mixedItems, _ := invokeCatalogPresentationBatchIntegration(t, ctx, server, counter, mixedReferences, 3)
	if len(mixedItems) != len(mixedReferences) {
		t.Fatalf("mixed presentations=%d want=%d", len(mixedItems), len(mixedReferences))
	}
	kinds := map[string]bool{}
	for _, item := range mixedItems {
		kinds[item.Kind] = true
	}
	for _, kind := range []string{"minecraft.item", "tag", "mod"} {
		if !kinds[kind] {
			t.Errorf("mixed batch omitted %s presentation", kind)
		}
	}
}

func invokeCatalogPresentationBatchIntegration(
	t *testing.T,
	ctx context.Context,
	server *Server,
	counter *integrationQueryCounter,
	items []catalogPresentationBatchReference,
	wantQueries int64,
) ([]catalogPresentationBatchItem, int) {
	t.Helper()
	body, err := json.Marshal(catalogPresentationBatchRequest{Locale: "zh-CN", Items: items})
	if err != nil {
		t.Fatal(err)
	}
	counter.queries.Store(0)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/resource-presentations", bytes.NewReader(body)).WithContext(ctx)
	response := httptest.NewRecorder()
	server.catalogResourcePresentations(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("presentation batch status=%d body=%s", response.Code, response.Body.String())
	}
	if got := counter.queries.Load(); got != wantQueries {
		t.Fatalf("presentation batch queries=%d want=%d", got, wantQueries)
	}
	var envelope struct {
		Data struct {
			Items []catalogPresentationBatchItem `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data.Items, response.Body.Len()
}
