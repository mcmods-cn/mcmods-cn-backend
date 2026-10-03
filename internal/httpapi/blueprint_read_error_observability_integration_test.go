package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBlueprintDerivedReadFailuresAreObservableIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify blueprint read failures")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err = pool.Exec(ctx, `
		create temp table users(id bigint primary key,username text not null,avatar_url text not null);
		create temp table blueprints(
			id bigint primary key,public_id text not null,owner_id bigint not null,original_file_id bigint,
			title text not null,description_markdown text not null,source_format text not null,status text not null,
			review_status text not null,original_object_key text not null,normalized_object_key text not null,
			cover_object_key text not null,cover_file_id bigint,size_x integer not null,size_y integer not null,
			size_z integer not null,block_count bigint not null,palette_count integer not null,entity_count integer not null,
			data_version integer not null,last_error text not null,created_at timestamptz not null,updated_at timestamptz not null
		);
		create temp table blueprint_variants(
			blueprint_id bigint not null,public_id text not null,format text not null,original boolean not null,
			recommended boolean not null,status text not null,original_name text not null,content_type text not null,
			size_bytes bigint not null,sha256 text not null,created_at timestamptz not null
		);
		create temp table blueprint_materials(
			blueprint_id bigint not null,block_state text not null,block_id text not null,
			properties jsonb not null,block_count bigint not null
		);
		create temp table catalog_import_revisions(
			id text primary key,mod_id bigint not null,source_namespace text not null,status text not null,is_active boolean not null,
			activated_at timestamptz,created_at timestamptz not null
		);
		create temp table mods(id bigint primary key,slug text not null,review_status text not null);
		insert into mods values(1,'synthetic-blueprint-source','approved');
		-- Deliberately broken asset projection, rather than relying on unrelated
		-- absent tables or a stale revision schema to produce this read failure.
		create temp table catalog_import_text_assets(revision_id text not null,broken_asset_path text not null);
		create temp table catalog_import_binary_assets(revision_id text not null,asset_path text not null);
		create temp table catalog_import_media(revision_id text not null,asset_path text not null);
		create temp table oss_files(
			id bigint primary key,object_key text not null,content_type text not null,status text not null,scan_status text not null
		);
		insert into users values(7,'arch019-user','');
		insert into blueprints values(
			1,'arch019-blueprint',7,null,'ARCH-019 blueprint','','nbt','ready','approved','original/key','normalized/key','',null,
			1,1,1,1,1,0,3700,'',now(),now()
		);
		insert into catalog_import_revisions values('arch019-revision',1,'minecraft','ready',true,now(),now());
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	if _, err = server.blueprintAssetRevisions(ctx, 1); err == nil {
		t.Fatal("asset revision query failure was converted to an empty list")
	}

	if status, body := invokeBlueprintCoverRead(server, ctx, "arch019-blueprint"); status != http.StatusNoContent {
		t.Fatalf("explicitly absent cover status=%d body=%s; want 204", status, body)
	}
	if _, err = pool.Exec(ctx, `alter table oss_files rename column status to arch019_broken_status`); err != nil {
		t.Fatal(err)
	}
	if status, body := invokeBlueprintCoverRead(server, ctx, "arch019-blueprint"); status != http.StatusInternalServerError {
		t.Fatalf("cover database failure status=%d body=%s; want 500", status, body)
	}
	if _, err = pool.Exec(ctx, `alter table oss_files rename column arch019_broken_status to status`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update blueprints set normalized_object_key=''`); err != nil {
		t.Fatal(err)
	}
	if status, body := invokeBlueprintRenderDataRead(server, ctx, "arch019-blueprint"); status != http.StatusNotFound {
		t.Fatalf("explicitly absent render data status=%d body=%s; want 404", status, body)
	}
	if _, err = pool.Exec(ctx, `alter table blueprints rename column normalized_object_key to arch019_broken_normalized_key`); err != nil {
		t.Fatal(err)
	}
	if status, body := invokeBlueprintRenderDataRead(server, ctx, "arch019-blueprint"); status != http.StatusInternalServerError {
		t.Fatalf("render data database failure status=%d body=%s; want 500", status, body)
	}
	if _, err = pool.Exec(ctx, `alter table blueprints rename column arch019_broken_normalized_key to normalized_object_key`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `insert into blueprint_materials values(1,'minecraft:stone','minecraft:stone','{}'::jsonb,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = server.blueprintMaterialRows(ctx, 1, "en_us"); err == nil || !strings.Contains(err.Error(), "resolve blueprint material resources") {
		t.Fatalf("resource resolver failure = %v", err)
	}
	if _, err = pool.Exec(ctx, `update blueprint_materials set properties='"scalar"'::jsonb`); err != nil {
		t.Fatal(err)
	}
	if _, err = server.blueprintMaterialRows(ctx, 1, "en_us"); err == nil || !strings.Contains(err.Error(), "decode blueprint material properties") {
		t.Fatalf("material property shape failure = %v", err)
	}
	if status, body := invokeBlueprintDetailRead(server, ctx, "arch019-blueprint"); status != http.StatusInternalServerError {
		t.Fatalf("material detail failure status=%d body=%s; want 500", status, body)
	}

	if _, err = pool.Exec(ctx, `delete from blueprint_materials`); err != nil {
		t.Fatal(err)
	}
	if status, body := invokeBlueprintDetailRead(server, ctx, "arch019-blueprint"); status != http.StatusInternalServerError || !strings.Contains(body, "读取蓝图资源版本失败") {
		t.Fatalf("asset detail failure status=%d body=%s; want explicit 500", status, body)
	}
}

func invokeBlueprintDetailRead(server *Server, ctx context.Context, publicID string) (int, string) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/"+publicID, nil).WithContext(ctx)
	request.SetPathValue("publicId", publicID)
	response := httptest.NewRecorder()
	server.blueprintDetail(response, request)
	return response.Code, response.Body.String()
}

func invokeBlueprintCoverRead(server *Server, ctx context.Context, publicID string) (int, string) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/"+publicID+"/cover", nil).WithContext(ctx)
	request.SetPathValue("publicId", publicID)
	response := httptest.NewRecorder()
	server.blueprintCover(response, request)
	return response.Code, response.Body.String()
}

func invokeBlueprintRenderDataRead(server *Server, ctx context.Context, publicID string) (int, string) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/"+publicID+"/render-data", nil).WithContext(ctx)
	request.SetPathValue("publicId", publicID)
	response := httptest.NewRecorder()
	server.blueprintRenderData(response, request)
	return response.Code, response.Body.String()
}
