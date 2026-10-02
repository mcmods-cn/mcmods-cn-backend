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

func TestBlueprintMaterialRevisionLookupStaysNamespaceScopedAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify blueprint material revision lookup at scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
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
	if _, err = pool.Exec(ctx, `create temporary table catalog_import_revisions(
		id text primary key,source_namespace text not null,status text not null,is_active boolean not null,
		activated_at timestamptz,created_at timestamptz not null);
		create index idx_catalog_import_revisions_material_namespace
		 on catalog_import_revisions(source_namespace,coalesce(activated_at,created_at) desc,id desc)
		 where is_active and status in ('ready','partial')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into catalog_import_revisions
		select 'noise-'||value,'noise_'||lpad(value::text,8,'0'),'ready',true,null,
			timestamptz '2025-01-01 00:00:00+00'+value*interval '1 second'
		from generate_series(1,100000) value;
		insert into catalog_import_revisions values
			('alpha-old','used_alpha','ready',true,null,'2026-01-01 00:00:00+00'),
			('alpha-new','used_alpha','partial',true,'2026-02-01 00:00:00+00','2026-01-01 00:00:00+00'),
			('alpha-inactive','used_alpha','ready',false,'2026-03-01 00:00:00+00','2026-01-01 00:00:00+00'),
			('beta-new','used_beta','ready',true,null,'2026-02-02 00:00:00+00'),
			('beta-rejected','used_beta','rejected',true,'2026-03-02 00:00:00+00','2026-01-01 00:00:00+00')`); err != nil {
		t.Fatal(err)
	}
	namespaces := []string{"used_alpha", "used_alpha", "used_beta"}
	check := func(scale int) {
		t.Helper()
		if _, analyzeErr := pool.Exec(ctx, `analyze catalog_import_revisions`); analyzeErr != nil {
			t.Fatal(analyzeErr)
		}
		rows, queryErr := pool.Query(ctx, blueprintMaterialRevisionSQL, namespaces)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		got := make(map[string]string)
		for rows.Next() {
			var namespace, revisionID string
			if queryErr = rows.Scan(&namespace, &revisionID); queryErr != nil {
				rows.Close()
				t.Fatal(queryErr)
			}
			got[namespace] = revisionID
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			t.Fatal(queryErr)
		}
		rows.Close()
		if len(got) != 2 || got["used_alpha"] != "alpha-new" || got["used_beta"] != "beta-new" {
			t.Fatalf("%d-row scoped revisions = %#v", scale, got)
		}

		started := time.Now()
		planRows, queryErr := pool.Query(ctx, "explain (analyze,buffers,format text) "+blueprintMaterialRevisionSQL, namespaces)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		var plan strings.Builder
		for planRows.Next() {
			var line string
			if queryErr = planRows.Scan(&line); queryErr != nil {
				planRows.Close()
				t.Fatal(queryErr)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		if queryErr = planRows.Err(); queryErr != nil {
			planRows.Close()
			t.Fatal(queryErr)
		}
		planRows.Close()
		duration := time.Since(started)
		planText := plan.String()
		if duration > 2*time.Second || strings.Contains(planText, "Seq Scan on catalog_import_revisions") ||
			!strings.Contains(planText, "idx_catalog_import_revisions_material_namespace") {
			t.Fatalf("%d-row lookup took %s or missed its namespace index:\n%s", scale, duration, planText)
		}
		t.Logf("%d-row namespace lookup: %s\n%s", scale, duration, planText)
	}
	check(100_000)
	if _, err = pool.Exec(ctx, `insert into catalog_import_revisions
		select 'noise-'||value,'noise_'||lpad(value::text,8,'0'),'ready',true,null,
			timestamptz '2025-01-01 00:00:00+00'+value*interval '1 second'
		from generate_series(100001,1000000) value`); err != nil {
		t.Fatal(err)
	}
	check(1_000_000)
}
