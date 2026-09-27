package httpapi

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestProjectEditorApplicationListUsesConstantQueriesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify editor application query count")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	counter := &integrationQueryCounter{}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
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
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
	var ownerID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test-only',true) returning id`, "editor_list_owner_"+nonce, nonce+"@editor-list.invalid").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into users(username,email,password_hash,email_verified)
		select $1 || value::text,$2 || value::text || '@editor-list.invalid','test-only',true
		from generate_series(1,205) value`, "editor_list_"+nonce+"_", nonce+"-"); err != nil {
		t.Fatal(err)
	}
	var modID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,$3,'approved',$4) returning id`, randomCatalogPublicID(), "editor-list-"+nonce,
		"Editor list "+nonce, ownerID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	var routeID int64
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into project_editor_applications(target_route_id,user_id,proof_markdown,status)
		select $1,id,'query-count proof','approved' from users where username like $2 order by id`,
		routeID, "editor_list_"+nonce+"_%"); err != nil {
		t.Fatal(err)
	}

	counter.queries.Store(0)
	queryContext, cancelQuery := context.WithTimeout(ctx, 2*time.Second)
	defer cancelQuery()
	items, err := (&Server{db: pool}).queryProjectEditorApplications(queryContext, 0, 0, 0, "approved")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 205 {
		t.Fatalf("items=%d want=205", len(items))
	}
	for index, item := range items {
		if item.TargetName != "Editor list "+nonce {
			t.Fatalf("item %d target name=%q", index, item.TargetName)
		}
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("205 editor applications executed %d SQL statements, want exactly 2 (list + attachments)", queries)
	}

	request, err := parseProjectEditorApplicationPageRequest(map[string][]string{
		"status": {"approved"},
		"limit":  {"100"},
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]struct{}, 205)
	pageSizes := make([]int, 0, 3)
	for {
		counter.queries.Store(0)
		page, hasMore, pageErr := (&Server{db: pool}).queryProjectEditorApplicationPage(queryContext, request)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if queries := counter.queries.Load(); queries != 2 {
			t.Fatalf("%d-item page executed %d SQL statements, want exactly 2", len(page), queries)
		}
		pageSizes = append(pageSizes, len(page))
		for _, item := range page {
			if _, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("application %s appeared on multiple pages", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		if !hasMore {
			break
		}
		last := page[len(page)-1]
		request.Cursor = &projectEditorApplicationPageCursor{
			Version: projectEditorApplicationCursorVersion,
			Scope:   request.Scope, CreatedAt: last.CreatedAt, ID: last.InternalID,
		}
	}
	if len(seen) != 205 || len(pageSizes) != 3 || pageSizes[0] != 100 || pageSizes[1] != 100 || pageSizes[2] != 5 {
		t.Fatalf("paged applications=%d page sizes=%v", len(seen), pageSizes)
	}

	if _, err = pool.Exec(ctx, `create temporary table project_editor_application_page_scale(
		id bigint primary key,status text not null,created_at timestamptz not null
	); create index idx_project_editor_application_page_scale
		on project_editor_application_page_scale(status,created_at,id);
	insert into project_editor_application_page_scale
	select value,'approved',timestamptz '2026-01-01 00:00:00+00'+value*interval '1 millisecond'
	from generate_series(1,100000) value;
	analyze project_editor_application_page_scale`); err != nil {
		t.Fatal(err)
	}
	planRows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select id from project_editor_application_page_scale
		where status='approved' and (created_at,id)<(timestamptz '2026-01-01 00:00:50+00',50000)
		order by created_at desc,id desc limit 51`)
	if err != nil {
		t.Fatal(err)
	}
	planLines := make([]string, 0)
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			planRows.Close()
			t.Fatal(err)
		}
		planLines = append(planLines, line)
	}
	if err = planRows.Err(); err != nil {
		planRows.Close()
		t.Fatal(err)
	}
	planRows.Close()
	plan := strings.ToLower(strings.Join(planLines, "\n"))
	if !strings.Contains(plan, "idx_project_editor_application_page_scale") || strings.Contains(plan, "seq scan") {
		t.Fatalf("100k deep page missed the status/time/id index:\n%s", plan)
	}
	t.Logf("100k editor application deep-page plan:\n%s", plan)
}
