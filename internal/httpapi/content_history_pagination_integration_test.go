package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestContentHistoryMergesOneHundredThousandRowsWithBoundedKeysetsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify content history scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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

	if _, err = pool.Exec(ctx, `create temporary table users(id bigint primary key,public_id text,username text);
		create temporary table content_revisions(
			id bigint primary key,public_id text not null,aggregate_type text not null,aggregate_key text not null,
			revision_no bigint not null,source text not null,created_by bigint,created_by_snapshot text not null,created_at timestamptz not null);
		create index idx_content_revisions_history on content_revisions(aggregate_type,aggregate_key,created_at desc,public_id desc);
		create temporary table change_requests(
			proposed_revision_id bigint primary key,status text not null,reason text not null,submitted_by bigint,submitted_by_snapshot text not null);
		create temporary table catalog_import_revisions(
			id text primary key,target_version_id bigint not null,revision_no bigint not null,status text not null,source_kind text not null,
			source_namespace text not null,submitted_by bigint,submitted_by_snapshot text not null,is_active boolean not null,created_at timestamptz not null);
		create temporary table resource_import_snapshots(resource_id bigint not null,revision_id text not null,created_at timestamptz not null);
		create index idx_resource_import_snapshots_history on resource_import_snapshots(resource_id,created_at desc,revision_id desc)`); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fixtureStarted := time.Now()
	if _, err = pool.Exec(ctx, `insert into users values(1,'user00001','history_user')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_revisions
		select series,'m'||lpad(series::text,8,'0'),'community_post','resource01:version01',series,'user',1,'history_user',$1::timestamptz+series*interval '2 seconds'
		from generate_series(1,50000) series`, base); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests
		select series,'approved','manual '||series::text,1,'history_user' from generate_series(1,50000) series`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into catalog_import_revisions
		select 'import-'||lpad(series::text,8,'0'),7,series,'ready','mcmods_exporter','minecraft',1,'history_user',series=50000,$1::timestamptz+(series*2+1)*interval '1 second'
		from generate_series(1,50000) series`, base); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into resource_import_snapshots
		select 9,'import-'||lpad(series::text,8,'0'),$1::timestamptz+(series*2+1)*interval '1 second' from generate_series(1,50000) series`, base); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `analyze content_revisions; analyze change_requests; analyze catalog_import_revisions; analyze resource_import_snapshots`); err != nil {
		t.Fatal(err)
	}
	t.Logf("100k mixed-source fixture loaded in %s", time.Since(fixtureStarted))

	loadPage := func(request contentHistoryPageRequest) contentHistoryPage {
		t.Helper()
		manual, queryErr := contentRevisionHistoryPage(ctx, pool, "community_post", "resource01:version01", nil,
			pendingReviewVisibility{}, request)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		imports, queryErr := importedResourceHistoryPage(ctx, pool, 9, 7, false, request)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		return mergeContentHistoryPage(manual, imports, request)
	}
	firstRequest, err := parseContentHistoryPageRequest(map[string][]string{"limit": {"50"}}, "resource:scale")
	if err != nil {
		t.Fatal(err)
	}
	first := loadPage(firstRequest)
	if len(first.Items) != 50 || !first.HasMore || first.NextCursor == "" || first.Items[0].ID != "import-00050000" || first.Items[1].ID != "m00050000" {
		t.Fatalf("first page=%+v", first)
	}
	if _, err = pool.Exec(ctx, `insert into content_revisions values(50001,'m00050001','community_post','resource01:version01',50001,'user',1,'history_user',$1)`, base.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests values(50001,'approved','concurrent',1,'history_user')`); err != nil {
		t.Fatal(err)
	}
	secondRequest, err := parseContentHistoryPageRequest(map[string][]string{"limit": {"50"}, "cursor": {first.NextCursor}}, "resource:scale")
	if err != nil {
		t.Fatal(err)
	}
	second := loadPage(secondRequest)
	if len(second.Items) != 50 || second.Items[0].ID != "import-00049975" {
		t.Fatalf("second page=%+v", second)
	}
	seen := make(map[string]struct{}, len(first.Items))
	for _, item := range first.Items {
		seen[item.Origin+":"+item.ID] = struct{}{}
	}
	for _, item := range second.Items {
		if item.ID == "m00050001" {
			t.Fatal("concurrent head insertion leaked into an older cursor page")
		}
		if _, duplicate := seen[item.Origin+":"+item.ID]; duplicate {
			t.Fatalf("duplicate item across pages: %s:%s", item.Origin, item.ID)
		}
	}

	deepCursor := encodeContentHistoryPageCursor(contentHistoryPageCursor{
		Version: contentHistoryCursorVersion, Scope: firstRequest.Scope, CreatedAt: base.Add(101 * time.Second), Origin: "import", ID: "import-00000050",
	})
	deepRequest, err := parseContentHistoryPageRequest(map[string][]string{"limit": {"50"}, "cursor": {deepCursor}}, "resource:scale")
	if err != nil {
		t.Fatal(err)
	}
	deepFirst := loadPage(deepRequest)
	deepSecondRequest, err := parseContentHistoryPageRequest(map[string][]string{"limit": {"50"}, "cursor": {deepFirst.NextCursor}}, "resource:scale")
	if err != nil {
		t.Fatal(err)
	}
	deepSecond := loadPage(deepSecondRequest)
	if len(deepFirst.Items) != 50 || !deepFirst.HasMore || len(deepSecond.Items) != 49 || deepSecond.HasMore || deepSecond.NextCursor != "" || deepSecond.Items[48].ID != "m00000001" {
		t.Fatalf("deep pages first=%+v second=%+v", deepFirst, deepSecond)
	}

	manualSQL, manualArgs := contentRevisionHistoryPageSQL("community_post", "resource01:version01", nil, pendingReviewVisibility{}, deepRequest)
	importSQL, importArgs := importedResourceHistoryPageSQL(9, 7, false, deepRequest)
	for name, statement := range map[string]struct {
		query string
		args  []any
	}{"manual": {manualSQL, manualArgs}, "import": {importSQL, importArgs}} {
		rows, explainErr := pool.Query(ctx, "explain (analyze,buffers,format text) "+statement.query, statement.args...)
		if explainErr != nil {
			t.Fatal(explainErr)
		}
		lines := make([]string, 0)
		for rows.Next() {
			var line string
			if scanErr := rows.Scan(&line); scanErr != nil {
				rows.Close()
				t.Fatal(scanErr)
			}
			lines = append(lines, line)
		}
		if explainErr = rows.Err(); explainErr != nil {
			rows.Close()
			t.Fatal(explainErr)
		}
		rows.Close()
		plan := strings.Join(lines, "\n")
		t.Logf("%s deep plan:\n%s", name, plan)
		requiredIndex := "idx_content_revisions_history"
		forbiddenScan := "Seq Scan on content_revisions"
		if name == "import" {
			requiredIndex = "idx_resource_import_snapshots_history"
			forbiddenScan = "Seq Scan on resource_import_snapshots"
		}
		if !strings.Contains(plan, requiredIndex) || strings.Contains(plan, forbiddenScan) {
			t.Fatalf("%s page did not use bounded history index:\n%s", name, plan)
		}
	}
}
