package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestProjectAuthorshipQueueTraversesPastLegacyWindowIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project authorship pagination against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	if _, err = pool.Exec(ctx, `
		create temporary table creators (like public.creators including all);
		create temporary table creator_role_definitions (like public.creator_role_definitions including all);
		create temporary table public_routes (like public.public_routes including all);
		create temporary table content_creator_bindings (like public.content_creator_bindings including all);
		create index idx_content_creator_bindings_review_page
			on content_creator_bindings(status,created_at,id);
		insert into creators(id,public_id,kind,name,normalized_name,review_status)
		select n,'a'||lpad(n::text,8,'0'),'author','Author '||n,'author '||n,'approved'
		from generate_series(1,205) n;
		insert into public_routes(id,public_id,entity_type,internal_id)
		select n,'m'||lpad(n::text,8,'0'),'mod',1000+n from generate_series(1,205) n;
		insert into content_creator_bindings(id,public_id,subject_type,subject_id,creator_id,
			name_snapshot,role_snapshot,status,permission_granting,created_at)
		select n,'b'||lpad(n::text,8,'0'),'mod',1000+n,n,'Author '||n,'Contributor','pending',false,
			timestamptz '2026-01-01 00:00:00+00'+n*interval '1 microsecond'
		from generate_series(1,205) n`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	cursor := ""
	seen := make(map[string]struct{}, 205)
	pageSizes := make([]int, 0, 3)
	for {
		path := "/api/v1/admin/project-authorship-relations?status=pending&limit=100"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		page := loadProjectAuthorshipIntegrationPage(t, ctx, server, path)
		pageSizes = append(pageSizes, len(page.Items))
		for _, item := range page.Items {
			if _, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("relationship %s repeated across pages", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Fatal("terminal project authorship page returned a cursor")
			}
			break
		}
		if page.NextCursor == "" {
			t.Fatal("non-terminal project authorship page omitted its cursor")
		}
		cursor = page.NextCursor
	}
	if len(seen) != 205 || len(pageSizes) != 3 || pageSizes[0] != 100 || pageSizes[1] != 100 || pageSizes[2] != 5 {
		t.Fatalf("traversal count=%d pageSizes=%v", len(seen), pageSizes)
	}

	first := loadProjectAuthorshipIntegrationPage(t, ctx, server, "/api/v1/admin/project-authorship-relations?status=pending&limit=100")
	request := projectAuthorshipIntegrationRequest(ctx,
		"/api/v1/admin/project-authorship-relations?status=approved&limit=100&cursor="+url.QueryEscape(first.NextCursor))
	response := httptest.NewRecorder()
	server.adminProjectAuthorshipRelations(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("cross-status cursor returned %d: %s", response.Code, response.Body.String())
	}

	if _, err = pool.Exec(ctx, `insert into content_creator_bindings(id,public_id,subject_type,subject_id,creator_id,
		name_snapshot,role_snapshot,status,permission_granting,created_at)
		select 1000+n,'q'||lpad(n::text,8,'0'),'mod',200000+n,200000+n,'Scale','Contributor','pending',false,
			timestamptz '2026-02-01 00:00:00+00'+n*interval '1 microsecond'
		from generate_series(1,100000) n;
		analyze content_creator_bindings`); err != nil {
		t.Fatal(err)
	}
	planRows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select id from content_creator_bindings
		where status='pending' and (created_at,id)>(timestamptz '2026-02-01 00:00:00.05+00',51000)
		order by created_at,id limit 100`)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	planRows.Close()
	if err = planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "Index") || strings.Contains(plan.String(), "Seq Scan on content_creator_bindings") {
		t.Fatalf("100k project authorship keyset plan missed review index:\n%s", plan.String())
	}
	t.Logf("100k project authorship keyset plan:\n%s", plan.String())
}

type projectAuthorshipIntegrationPage struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor"`
}

func loadProjectAuthorshipIntegrationPage(t *testing.T, ctx context.Context, server *Server, path string) projectAuthorshipIntegrationPage {
	t.Helper()
	request := projectAuthorshipIntegrationRequest(ctx, path)
	response := httptest.NewRecorder()
	server.adminProjectAuthorshipRelations(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
	}
	var envelope struct {
		Data projectAuthorshipIntegrationPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func projectAuthorshipIntegrationRequest(ctx context.Context, path string) *http.Request {
	claims := security.Claims{Subject: 42, PermissionRules: []security.PermissionRule{{
		Code: "project.authorship.manage", Allow: true, Priority: 100,
	}}}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	return request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
}
