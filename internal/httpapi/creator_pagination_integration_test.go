package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestCreatorCatalogKeysetCursorTraversesEverySQLSortIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute creator pagination against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		create temporary table creator_claims (like public.creator_claims including all);
		create temporary table content_creator_bindings (like public.content_creator_bindings including all);
		create temporary table mods (like public.mods including all);
		create temporary table public_routes (like public.public_routes including all);
		create temporary table content_popularity_stats (like public.content_popularity_stats including all);
		insert into creators(id,public_id,kind,name,normalized_name,review_status,created_at,updated_at) values
			(11,'c00000001','author','Alice','alice','approved',now()-interval '3 days',now()-interval '3 hours'),
			(12,'c00000002','author','Bob','bob','approved',now()-interval '2 days',now()-interval '2 hours'),
			(13,'c00000003','team','Craft Team','craft team','approved',now()-interval '1 day',now()-interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}

	for _, sort := range []string{
		"name", "published", "updated", "collected", "heat", "downloads", "favorites", "rating", "views", "comments", "relevance",
	} {
		sort := sort
		t.Run(sort, func(t *testing.T) {
			first := loadCreatorIntegrationPage(t, server, fmt.Sprintf("/api/v1/creators?limit=1&sort=%s&order=asc", sort))
			if !first.HasMore || first.NextCursor == "" || len(first.Items) != 1 {
				t.Fatalf("first page=%+v", first)
			}
			second := loadCreatorIntegrationPage(t, server, fmt.Sprintf(
				"/api/v1/creators?limit=1&sort=%s&order=asc&cursor=%s", sort, url.QueryEscape(first.NextCursor),
			))
			if len(second.Items) != 1 || second.Items[0].PublicID == first.Items[0].PublicID {
				t.Fatalf("pages repeated or lost the next row: first=%+v second=%+v", first.Items, second.Items)
			}
		})
	}

	first := loadCreatorIntegrationPage(t, server, "/api/v1/creators?kind=author&limit=1&sort=name&order=asc")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/creators?kind=team&limit=1&sort=name&order=asc&cursor="+url.QueryEscape(first.NextCursor), nil).WithContext(ctx)
	response := httptest.NewRecorder()
	server.creators(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("cross-filter cursor returned %d: %s", response.Code, response.Body.String())
	}

	if _, err = pool.Exec(ctx, `insert into creators(id,public_id,kind,name,normalized_name,review_status,created_at,updated_at)
		select 1000+n,'p'||lpad(n::text,8,'0'),'author','Scale '||lpad(n::text,8,'0'),
		       'scale '||lpad(n::text,8,'0'),'approved',now(),now()
		from generate_series(1,100000) n;
		analyze creators`); err != nil {
		t.Fatal(err)
	}
	planRows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select id from creators
		where kind='author' and review_status='approved'
		  and (lower(name),id)>('scale 00050000',51000)
		order by lower(name),id limit 40`)
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
	if !strings.Contains(plan.String(), "Index") || strings.Contains(plan.String(), "Seq Scan on creators") {
		t.Fatalf("100k creator keyset plan did not use the catalog index:\n%s", plan.String())
	}
	t.Logf("100k creator keyset plan:\n%s", plan.String())
}

type creatorIntegrationPage struct {
	Items      []creatorSummary `json:"items"`
	HasMore    bool             `json:"hasMore"`
	NextCursor string           `json:"nextCursor"`
}

func loadCreatorIntegrationPage(t *testing.T, server *Server, path string) creatorIntegrationPage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	server.creators(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
	}
	var envelope struct {
		Data creatorIntegrationPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
