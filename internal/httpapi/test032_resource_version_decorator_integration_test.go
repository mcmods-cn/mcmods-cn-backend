package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestTEST032ResourceVersionDecoratorBatchesDuplicatesAndReportsDatabaseErrorsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute the TEST032 resource-version decorator proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	counter := &integrationQueryCounter{}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	poolConfig.ConnConfig.Tracer = counter
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
			t.Errorf("drop TEST032 ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	projectCode := fmt.Sprintf("t%08d", suffix%100_000_000)
	var modID, resourceID, versionID int64
	var resourcePublicID, versionPublicID string
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,'TEST032 resource versions','approved') returning id`,
		projectCode, fmt.Sprintf("test032-resource-%d", suffix)).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type,status)
		values($1,'resource','active') returning id,public_id`, fmt.Sprintf("resource:test032:%d", suffix)).
		Scan(&resourceID, &resourcePublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into game_resources(
		entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved
	) values($1,'minecraft.item',$2,'test032','item',$3,true)`, resourceID, "test032:item", modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(
		mod_id,label,minecraft_versions,loaders,mod_version,status
	) values($1,'TEST032 version',array['1.21.1'],array['fabric'],'1.0.0','active') returning id,public_id`, modID).
		Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_version_details(
		resource_id,version_id,default_locale,definition,status
	) values($1,$2,'zh-CN','{"test032":true}'::jsonb,'active')`, resourceID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_version_detail_localizations(
		resource_id,version_id,locale,name,summary,content_markdown
	) values($1,$2,'zh-CN','TEST032 名称','摘要','正文')`, resourceID, versionID); err != nil {
		t.Fatal(err)
	}

	items := make([]map[string]any, 0, 4_099)
	items = append(items,
		map[string]any{"publicId": resourcePublicID},
		map[string]any{"publicId": resourcePublicID},
		map[string]any{"publicId": ""},
	)
	for index := range 4_096 {
		items = append(items, map[string]any{"publicId": fmt.Sprintf("x%08d", index)})
	}
	counter.queries.Store(0)
	server := &Server{db: pool}
	if err = server.decorateResourceVersionRows(ctx, items, "zh-CN", "en-US"); err != nil {
		t.Fatal(err)
	}
	if got := counter.queries.Load(); got != 1 {
		t.Fatalf("decorating 4,099 inputs used %d SQL queries, want 1", got)
	}
	for index := range 2 {
		versions, ok := items[index]["versions"].([]map[string]any)
		if !ok || len(versions) != 1 || versions[0]["publicId"] != versionPublicID ||
			versions[0]["hasDetail"] != true || versions[0]["name"] != "TEST032 名称" {
			t.Fatalf("duplicate item %d versions=%#v", index, items[index]["versions"])
		}
	}
	for index := 2; index < len(items); index++ {
		versions, ok := items[index]["versions"].([]map[string]any)
		if !ok || len(versions) != 0 {
			t.Fatalf("unmatched item %d versions=%#v", index, items[index]["versions"])
		}
	}

	if _, err = pool.Exec(ctx, `alter table mod_resource_bindings rename to test032_broken_resource_bindings`); err != nil {
		t.Fatal(err)
	}
	brokenItems := []map[string]any{{"publicId": resourcePublicID}}
	decorateErr := server.decorateResourceVersionRows(ctx, brokenItems, "zh-CN", "en-US")
	if _, err = pool.Exec(ctx, `alter table test032_broken_resource_bindings rename to mod_resource_bindings`); err != nil {
		t.Fatal(err)
	}
	if decorateErr == nil {
		t.Fatal("resource-version database failure was reported as an empty successful decoration")
	}
	versions, ok := brokenItems[0]["versions"].([]map[string]any)
	if !ok || len(versions) != 0 {
		t.Fatalf("failed decoration retained partial versions: %#v", brokenItems[0]["versions"])
	}

	source, err := os.ReadFile("resource_versions.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(source)), "has_manual_detail") {
		t.Fatal("resource-version projection still reads the unused has_manual_detail value")
	}
}
