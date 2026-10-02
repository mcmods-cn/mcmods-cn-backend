package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

type favoriteExportIntegrationPage struct {
	Items      []favoriteModpackExportSummary `json:"items"`
	Limit      int                            `json:"limit"`
	HasMore    bool                           `json:"hasMore"`
	NextCursor string                         `json:"nextCursor"`
}

func TestFavoriteExportHistoryTraversesEveryTaskWithStableKeysetsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify favorite export history pagination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
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

	var ownerID, otherOwnerID, collectionID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('perf041-owner','perf041-owner@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('perf041-other','perf041-other@example.test','not-used','active') returning id`).Scan(&otherOwnerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(user_id,name) values($1,'PERF-041') returning id`, ownerID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if _, err = pool.Exec(ctx, `insert into favorite_modpack_export_tasks(
		owner_user_id,collection_id,collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,loader_type,status,created_at)
		select $1,$2,(select public_id from favorite_collections where id=$2),'Pack '||value,'version-'||value,'1.21.1','fabric',
			case when value%3=0 then 'ready' when value%3=1 then 'failed' else 'pending' end,$3
		from generate_series(1,150) value`, ownerID, collectionID, createdAt); err != nil {
		t.Fatal(err)
	}
	var otherCollectionID int64
	if err = pool.QueryRow(ctx, `insert into favorite_collections(user_id,name) values($1,'Other') returning id`, otherOwnerID).Scan(&otherCollectionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_modpack_export_tasks(
		owner_user_id,collection_id,collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,loader_type,status,created_at)
		values($1,$2,(select public_id from favorite_collections where id=$2),'Foreign pack','foreign-version','1.21.1','fabric','ready',$3)`, otherOwnerID, otherCollectionID, createdAt); err != nil {
		t.Fatal(err)
	}
	var ownerTaskCount int
	if err = pool.QueryRow(ctx, `select count(*) from favorite_modpack_export_tasks where owner_user_id=$1`, ownerID).Scan(&ownerTaskCount); err != nil || ownerTaskCount != 150 {
		t.Fatalf("owner task setup count = %d, error = %v", ownerTaskCount, err)
	}

	server := &Server{db: pool}
	invoke := func(status string, limit int, cursor string) favoriteExportIntegrationPage {
		t.Helper()
		query := url.Values{"status": {status}, "limit": {fmt.Sprint(limit)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		request := httptest.NewRequest("GET", "/api/v1/users/me/modpack-exports?"+query.Encode(), nil)
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: ownerID}))
		response := httptest.NewRecorder()
		server.favoriteModpackExports(response, request)
		if response.Code != 200 {
			t.Fatalf("history status %s returned %d: %s", status, response.Code, response.Body.String())
		}
		var envelope struct {
			Data favoriteExportIntegrationPage `json:"data"`
		}
		if decodeErr := json.Unmarshal(response.Body.Bytes(), &envelope); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return envelope.Data
	}

	first := invoke("all", 100, "")
	second := invoke("all", 100, first.NextCursor)
	if len(first.Items) != 100 || !first.HasMore || first.NextCursor == "" || len(second.Items) != 50 || second.HasMore || second.NextCursor != "" {
		t.Fatalf("all pages = %d/%t/%q then %d/%t/%q", len(first.Items), first.HasMore, first.NextCursor,
			len(second.Items), second.HasMore, second.NextCursor)
	}
	seen := make(map[string]struct{}, 150)
	for _, item := range append(first.Items, second.Items...) {
		if _, duplicate := seen[item.ID]; duplicate {
			t.Fatalf("duplicate export task %s across identical-timestamp pages", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	if len(seen) != 150 {
		t.Fatalf("history exposed %d owner tasks, want 150", len(seen))
	}

	readySeen := make(map[string]struct{}, 50)
	cursor := ""
	for {
		page := invoke("ready", 17, cursor)
		for _, item := range page.Items {
			if item.Status != "ready" {
				t.Fatalf("ready page contained status %q", item.Status)
			}
			if _, duplicate := readySeen[item.ID]; duplicate {
				t.Fatalf("duplicate ready export %s", item.ID)
			}
			readySeen[item.ID] = struct{}{}
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" {
			t.Fatal("ready page has more rows without a cursor")
		}
		cursor = page.NextCursor
	}
	if len(readySeen) != 50 {
		t.Fatalf("ready filter exposed %d tasks, want 50", len(readySeen))
	}
}

func TestFavoriteExportHistoryUsesBoundedIndexesAtMillionTasksIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify favorite export history at scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table favorite_collections(id bigint primary key,public_id text not null);
	create temporary table favorite_modpack_export_tasks(
		id bigint primary key,public_id text not null,owner_user_id bigint not null,collection_id bigint,
		collection_public_id_snapshot text not null,
		pack_name text not null,pack_version_id text not null,minecraft_version text not null,loader_type text not null,
		loader_version text not null,allow_compatible_only boolean not null,report_version integer not null,status text not null,
		collection_item_count integer not null,exported_mod_count integer not null,
		auto_dependency_count integer not null,skipped_item_count integer not null,failed_item_count integer not null,
		final_file_count integer not null,result_file_size bigint not null,result_sha256 text not null,error_code text not null,
		created_at timestamptz not null,finished_at timestamptz,expires_at timestamptz);
	create index idx_perf041_owner_created on favorite_modpack_export_tasks(owner_user_id,created_at desc,id desc);
	create index idx_perf041_owner_status_created on favorite_modpack_export_tasks(owner_user_id,status,created_at desc,id desc);
	insert into favorite_collections values(1,'perf041c1');
	insert into favorite_modpack_export_tasks
	select value,'task-'||value,case when value%10=0 then 43 else 42 end,1,'perf041c1','Pack','v1','1.21.1','fabric','0.16.0',false,1,
		case value%6 when 0 then 'pending' when 1 then 'processing' when 2 then 'ready' when 3 then 'failed' when 4 then 'expired' else 'cancelled' end,
		0,0,0,0,0,0,0,'','',timestamp with time zone '2026-01-01 00:00:00+00'+value*interval '1 microsecond',null,null
	from generate_series(1,1000000) value;
	analyze favorite_collections; analyze favorite_modpack_export_tasks`); err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(500_000 * time.Microsecond)
	for name, request := range map[string]favoriteExportPageRequest{
		"all":   {OwnerUserID: 42, Status: "all", Limit: 30, AfterCreatedAt: anchor, AfterID: 500_000},
		"ready": {OwnerUserID: 42, Status: "ready", Limit: 30, AfterCreatedAt: anchor, AfterID: 500_000},
	} {
		query, arguments := favoriteExportPageSQL(request)
		started := time.Now()
		rows, queryErr := pool.Query(ctx, "explain (analyze,buffers,format text) "+query, arguments...)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if queryErr = rows.Scan(&line); queryErr != nil {
				rows.Close()
				t.Fatal(queryErr)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			t.Fatal(queryErr)
		}
		rows.Close()
		duration := time.Since(started)
		planText := plan.String()
		expectedIndex := "idx_perf041_owner_created"
		if name == "ready" {
			expectedIndex = "idx_perf041_owner_status_created"
		}
		if duration > 2*time.Second || strings.Contains(planText, "Seq Scan on favorite_modpack_export_tasks") || !strings.Contains(planText, expectedIndex) {
			t.Fatalf("%s million-task page took %s or missed %s:\n%s", name, duration, expectedIndex, planText)
		}
		t.Logf("%s million-task page: %s\n%s", name, duration, planText)
	}
}
