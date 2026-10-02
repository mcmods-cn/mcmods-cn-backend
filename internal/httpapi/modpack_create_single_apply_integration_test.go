package httpapi

import (
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
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestApprovedModpackCreationAppliesAssociationsExactlyOnceIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify initial modpack association writes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
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

	var actorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('perf020_actor','perf020@example.invalid','test-only',true) returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		select 'p'||lpad(value::text,8,'0'),'perf020-mod-'||value,'PERF020 Mod '||value,'approved'
		from generate_series(1,16) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create temporary table perf020_association_writes(
		table_name text not null,operation text not null,writes integer not null,primary key(table_name,operation));
		create or replace function pg_temp.perf020_count_association_write() returns trigger as $$
		begin
			insert into perf020_association_writes(table_name,operation,writes) values(TG_TABLE_NAME,TG_OP,1)
			on conflict(table_name,operation) do update set writes=perf020_association_writes.writes+1;
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql;
		create trigger trg_perf020_loader_write after insert or delete on modpack_loader_compatibilities
			for each row execute function pg_temp.perf020_count_association_write();
		create trigger trg_perf020_tag_write after insert or delete on modpack_tags
			for each row execute function pg_temp.perf020_count_association_write();
		create trigger trg_perf020_link_write after insert or delete on modpack_links
			for each row execute function pg_temp.perf020_count_association_write();
		create trigger trg_perf020_mod_write after insert or delete on modpack_mods
			for each row execute function pg_temp.perf020_count_association_write()`); err != nil {
		t.Fatal(err)
	}

	claims := security.Claims{Subject: actorID, PermissionRules: []security.PermissionRule{
		{Code: "modpack.create", Allow: true},
		{Code: "content.no-review", Allow: true},
	}}
	server := &Server{cfg: loaded, db: pool, cache: querycache.New(config.RedisConfig{})}
	approved := invokeProjectHandler[modpackResponse](t, ctx, claims, http.MethodPost, "/api/v1/modpacks",
		perf020ModpackSnapshot("perf020-approved", 16), http.StatusCreated, server.createModpack)
	if approved.ReviewStatus != "approved" || approved.PublishedRevisionID == nil ||
		len(approved.Compatibilities) != 2 || len(approved.Tags) != 4 || len(approved.Links) != 3 || len(approved.Mods) != 16 {
		t.Fatalf("approved creation did not publish the complete snapshot: %#v", approved)
	}
	assertPERF020AssociationWrites(t, ctx, pool, map[string]int{
		"modpack_loader_compatibilities": 8,
		"modpack_tags":                   4,
		"modpack_links":                  3,
		"modpack_mods":                   16,
	})

	if _, err = pool.Exec(ctx, `truncate perf020_association_writes`); err != nil {
		t.Fatal(err)
	}
	pendingRequest := newProjectHandlerRequest(t, ctx, claims, http.MethodPost, "/api/v1/modpacks",
		perf020ModpackSnapshot("perf020-pending", 1))
	pendingRequest = pendingRequest.WithContext(context.WithValue(pendingRequest.Context(), antiAbuseModerationContextKey, true))
	pendingResponse := httptest.NewRecorder()
	server.createModpack(pendingResponse, pendingRequest)
	if pendingResponse.Code != http.StatusCreated {
		t.Fatalf("pending modpack status=%d body=%s", pendingResponse.Code, pendingResponse.Body.String())
	}
	var pendingEnvelope struct {
		Data modpackResponse `json:"data"`
	}
	if err = json.Unmarshal(pendingResponse.Body.Bytes(), &pendingEnvelope); err != nil {
		t.Fatal(err)
	}
	if pendingEnvelope.Data.ReviewStatus != "pending" || pendingEnvelope.Data.PublishedRevisionID != nil ||
		len(pendingEnvelope.Data.Mods) != 1 {
		t.Fatalf("pending creation did not retain one preview association set: %#v", pendingEnvelope.Data)
	}
	assertPERF020AssociationWrites(t, ctx, pool, map[string]int{
		"modpack_loader_compatibilities": 8,
		"modpack_tags":                   4,
		"modpack_links":                  3,
		"modpack_mods":                   1,
	})
}

func perf020ModpackSnapshot(siteID string, modCount int) createModpackRequest {
	mods := make([]modpackModPayload, modCount)
	for index := range mods {
		mods[index] = modpackModPayload{
			ModPublicID: fmt.Sprintf("p%08d", index+1), Provider: "manual",
			ClientRequired: true, ServerRequired: true,
		}
	}
	return createModpackRequest{
		SiteID: siteID, PrimaryName: "PERF020 Pack", DefaultLocale: "en-US",
		Environment: "bothRequired", PrimaryCategory: "adventure", PackType: "native", PackagingMethod: "other",
		OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual",
		Compatibilities: []modLoaderCompatibilityPayload{
			{Loader: "Fabric", Versions: []string{"1.21.1", "1.21", "1.20.6", "1.20.4"}},
			{Loader: "NeoForge", Versions: []string{"1.21.1", "1.21", "1.20.6", "1.20.4"}},
		},
		Tags: []string{"adventure", "magic", "technology", "quests"},
		Links: []modLinkPayload{
			{Type: "official", URL: "https://example.test/perf020"},
			{Type: "github", URL: "https://example.test/perf020/source"},
			{Type: "wiki", URL: "https://example.test/perf020/wiki"},
		},
		Mods: mods,
	}
}

func assertPERF020AssociationWrites(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wantInserts map[string]int) {
	t.Helper()
	for tableName, want := range wantInserts {
		var inserts, deletes int
		if err := pool.QueryRow(ctx, `select
			coalesce(sum(writes) filter(where operation='INSERT'),0)::int,
			coalesce(sum(writes) filter(where operation='DELETE'),0)::int
			from perf020_association_writes where table_name=$1`, tableName).Scan(&inserts, &deletes); err != nil {
			t.Fatal(err)
		}
		if inserts != want || deletes != 0 {
			t.Errorf("%s physical writes: inserts=%d want=%d deletes=%d want=0", tableName, inserts, want, deletes)
		}
	}
}
