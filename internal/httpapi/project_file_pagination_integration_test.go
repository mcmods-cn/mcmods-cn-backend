package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestInternalProjectFilePageStaysBoundedAtHundredThousandRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate project file keyset scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err = pool.Exec(ctx, `
		create temporary table oss_files (
			id bigint primary key,status text not null,scan_status text not null
		);
		create temporary table project_files (
			id bigint primary key,public_id text not null,project_type text not null,project_internal_id bigint not null,
			oss_file_id bigint not null,display_name text not null,file_name text not null,version_name text not null,
			release_channel text not null,game_versions text[] not null,loaders text[] not null,created_at timestamptz not null,
			size_bytes bigint not null,download_count bigint not null,sha256 text not null,status text not null
		);
		create index idx_project_files_project_published
			on project_files(project_type,project_internal_id,status,created_at desc,id desc);
		insert into oss_files
		select value,'active','clean' from generate_series(1,100000) value;
		insert into project_files
		select value,'f'||lpad(value::text,8,'0'),'mod',42,value,'Release '||value,'file-'||value||'.jar','v'||value,
			'release',array['1.21.1'],array['fabric'],timestamptz '2026-01-01 00:00:00+00'+value*interval '1 millisecond',
			1024,value,'sha-'||value,'active'
		from generate_series(1,100000) value;
		analyze project_files;
		analyze oss_files
	`); err != nil {
		t.Fatal(err)
	}

	api := &Server{db: pool}
	project := projectFileContext{ProjectType: "mod", ProjectID: "mod000001", ProjectInternalID: 42}
	cursor := ""
	seen := map[string]bool{}
	counter.queries.Store(0)
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		request, parseErr := parseProjectFilePageRequest(map[string][]string{
			"source": {"internal"}, "limit": {"50"}, "cursor": {cursor},
		}, project)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		page, pageErr := api.internalProjectFilePage(ctx, project, request)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if len(page.Items) != 50 || !page.HasMore || page.NextCursor == "" {
			t.Fatalf("page %d items=%d hasMore=%t cursor=%q", pageNumber, len(page.Items), page.HasMore, page.NextCursor)
		}
		encoded, marshalErr := json.Marshal(page.Items)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if len(encoded) >= 128*1024 {
			t.Fatalf("50-item page is %d bytes", len(encoded))
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate project file %s", item.ID)
			}
			seen[item.ID] = true
		}
		cursor = page.NextCursor
	}
	if queries := counter.queries.Load(); queries != 3 {
		t.Fatalf("three pages executed %d SQL statements, want 3", queries)
	}

	rows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select project_file.id,project_file.public_id
		from project_files project_file join oss_files oss on oss.id=project_file.oss_file_id and oss.status='active'
			and oss.scan_status in ('clean','trusted_generated')
		where project_file.project_type='mod' and project_file.project_internal_id=42 and project_file.status='active'
			and (project_file.created_at,project_file.id)<(timestamptz '2026-01-01 00:01:39.950+00',99950)
		order by project_file.created_at desc,project_file.id desc limit 51`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	planLines := make([]string, 0)
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		planLines = append(planLines, line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(planLines, "\n")
	if !strings.Contains(plan, "idx_project_files_project_published") || strings.Contains(plan, "Seq Scan on project_files") {
		t.Fatalf("project file page did not use its bounded index:\n%s", plan)
	}
	t.Logf("100k project file keyset plan:\n%s", plan)
}
