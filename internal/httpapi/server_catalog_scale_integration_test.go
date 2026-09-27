package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/searchindex"
)

func TestServerCatalogAuthoritativeIndexCursorAndMillionRowCardFetchIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute the server catalog scale proof against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
		create temporary table minecraft_servers (like public.minecraft_servers including all);
		create index idx_minecraft_servers_public_updated
			on minecraft_servers(updated_at desc,id desc) where review_status='approved';
		insert into minecraft_servers(
			id,public_id,slug,address,normalized_address,handshake_host,connect_host,connect_port,
			name,primary_tag,submitted_by,review_status,created_at,updated_at
		)
		select n,'s'||lpad(n::text,8,'0'),'scale-'||n,'scale-'||n||'.example:25565',
			'scale-'||n||'.example:25565','scale-'||n||'.example','127.0.0.1',25565,
			'Scale '||lpad(n::text,8,'0'),'technology',1,'approved',
			timestamptz '2025-01-01 00:00:00+00'+n*interval '1 second',
			timestamptz '2025-01-01 00:00:00+00'+n*interval '1 second'
		from generate_series(1,100000) n;
		analyze minecraft_servers`); err != nil {
		t.Fatal(err)
	}
	assertServerCardFetchPlan(t, ctx, pool, 100_000, []int64{99_941, 99_950, 99_960, 99_970, 99_980, 99_990, 100_000})
	assertServerSQLKeysetPlan(t, ctx, pool, 100_000, 50_000)
	if _, err = pool.Exec(ctx, `
		insert into minecraft_servers(
			id,public_id,slug,address,normalized_address,handshake_host,connect_host,connect_port,
			name,primary_tag,submitted_by,review_status,created_at,updated_at
		)
		select n,'s'||lpad(n::text,8,'0'),'scale-'||n,'scale-'||n||'.example:25565',
			'scale-'||n||'.example:25565','scale-'||n||'.example','127.0.0.1',25565,
			'Scale '||lpad(n::text,8,'0'),'technology',1,'approved',
			timestamptz '2025-01-01 00:00:00+00'+n*interval '1 second',
			timestamptz '2025-01-01 00:00:00+00'+n*interval '1 second'
		from generate_series(100001,1000000) n;
		analyze minecraft_servers`); err != nil {
		t.Fatal(err)
	}
	assertServerCardFetchPlan(t, ctx, pool, 1_000_000, []int64{999_941, 999_950, 999_960, 999_970, 999_980, 999_990, 1_000_000})
	assertServerSQLKeysetPlan(t, ctx, pool, 1_000_000, 500_000)

	var requestLock sync.Mutex
	searchRequests := make([]url.Values, 0, 2)
	searchHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestLock.Lock()
		searchRequests = append(searchRequests, r.URL.Query())
		requestNumber := len(searchRequests)
		requestLock.Unlock()
		start := int64(1_000_000)
		if requestNumber > 1 {
			start = 500_000
		}
		hits := make([]map[string]any, 0, 61)
		for offset := int64(0); offset < 61; offset++ {
			id := start - offset
			hits = append(hits, map[string]any{"document": map[string]any{
				"internal_id": id, "updated_at": 1_700_000_000 + id, "heat_sort_desc": id*2 + 1,
			}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"found": 1_000_000, "hits": hits})
	}))
	defer searchHTTP.Close()
	searchClient := searchindex.New(config.TypesenseConfig{
		Enabled: true, URL: searchHTTP.URL, APIKey: "perf-021", CollectionPrefix: "perf021", Timeout: time.Second,
	})
	searchClient.SetReady(true)
	server := &Server{db: pool, search: searchClient}
	path := "/api/v1/servers?q=forge&version=1.20.1,1.21.1&versionMode=all&mods=create,jei" +
		"&tag=technology&language=zh-CN&modded=true&online=true&whitelist=false&onlineMode=true" +
		"&excludeSiteId=s00000001&sort=heat&order=desc&limit=60"
	first := loadServerCatalogScalePage(t, server, path)
	if len(first.Items) != 60 || !first.HasMore || first.NextCursor == "" || first.Items[0].ID != "s01000000" {
		t.Fatalf("first indexed page=%+v first item=%+v", first, first.Items)
	}
	second := loadServerCatalogScalePage(t, server, path+"&cursor="+url.QueryEscape(first.NextCursor))
	if len(second.Items) != 60 || second.Items[0].ID != "s00500000" || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second indexed page=%+v first=%+v", second, second.Items)
	}
	requestLock.Lock()
	if len(searchRequests) != 2 {
		t.Fatalf("search requests=%d want 2", len(searchRequests))
	}
	firstFilter := searchRequests[0].Get("filter_by")
	for _, expected := range []string{
		"minecraft_versions:=`1.20.1`", "minecraft_versions:=`1.21.1`", "mods:=`create`", "mods:=`jei`",
		"public_id:!=`s00000001`", "modded:=true", "online:=true", "whitelist:=false", "online_mode:=true",
	} {
		if !strings.Contains(firstFilter, expected) {
			t.Fatalf("authoritative filter %q is missing %q", firstFilter, expected)
		}
	}
	if searchRequests[0].Get("page") != "1" || searchRequests[0].Get("per_page") != "61" ||
		searchRequests[0].Get("sort_by") != "heat_sort_desc:desc,updated_at:desc,internal_id:desc" {
		t.Fatalf("unexpected first search query %q", searchRequests[0].Encode())
	}
	if secondFilter := searchRequests[1].Get("filter_by"); !strings.Contains(secondFilter, "heat_sort_desc:<") ||
		searchRequests[1].Get("page") != "1" {
		t.Fatalf("second request did not continue with a keyset filter: %q", searchRequests[1].Encode())
	}
	requestLock.Unlock()

	searchClient.SetReady(false)
	failedContinuationRequest := httptest.NewRequest(http.MethodGet, path+"&cursor="+url.QueryEscape(first.NextCursor), nil)
	failedContinuationResponse := httptest.NewRecorder()
	server.publicMinecraftServers(failedContinuationResponse, failedContinuationRequest)
	if failedContinuationResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("indexed cursor switched authority after index failure: %d %s",
			failedContinuationResponse.Code, failedContinuationResponse.Body.String())
	}
	fallbackPath := "/api/v1/servers?sort=updated&order=desc&limit=60"
	fallbackFirst := loadServerCatalogScalePage(t, server, fallbackPath)
	if len(fallbackFirst.Items) != 60 || !fallbackFirst.HasMore || fallbackFirst.NextCursor == "" ||
		fallbackFirst.Items[0].ID != "s01000000" {
		t.Fatalf("first SQL fallback page=%+v first=%+v", fallbackFirst, fallbackFirst.Items)
	}
	fallbackSecond := loadServerCatalogScalePage(t, server, fallbackPath+"&cursor="+url.QueryEscape(fallbackFirst.NextCursor))
	if len(fallbackSecond.Items) != 60 || fallbackSecond.Items[0].ID != "s00999940" ||
		fallbackSecond.Items[0].ID == fallbackFirst.Items[0].ID {
		t.Fatalf("second SQL fallback page=%+v first=%+v", fallbackSecond, fallbackSecond.Items)
	}
}

type serverCatalogScalePage struct {
	Items      []minecraftServerListItem `json:"items"`
	Limit      int                       `json:"limit"`
	HasMore    bool                      `json:"hasMore"`
	NextCursor string                    `json:"nextCursor"`
}

func loadServerCatalogScalePage(t *testing.T, server *Server, path string) serverCatalogScalePage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	server.publicMinecraftServers(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
	}
	var envelope struct {
		Data serverCatalogScalePage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func assertServerCardFetchPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rows int, ids []int64) {
	t.Helper()
	planRows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select server.public_id,server.name,server.short_description,server.icon_data_uri,
			server.modded,server.loader,server.languages,server.primary_tag,server.minecraft_versions,
			server.last_online,coalesce(server.last_latency_ms,-1),server.last_players_online,
			server.last_players_max,server.last_checked_at
		from minecraft_servers server
		where server.review_status='approved' and server.id=any($1::bigint[])
		order by array_position($1::bigint[],server.id)`, ids)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err = planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.String(), "Seq Scan on minecraft_servers") || !strings.Contains(plan.String(), "Index") {
		t.Fatalf("%d-row card fetch did not use the server primary index:\n%s", rows, plan.String())
	}
	t.Logf("%d-row exact server card fetch plan:\n%s", rows, plan.String())
}

func assertServerSQLKeysetPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rows int, cursorID int64) {
	t.Helper()
	cursorTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(cursorID) * time.Second)
	planRows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select id from minecraft_servers
		where review_status='approved' and (updated_at,id)<($1,$2)
		order by updated_at desc,id desc limit 61`, cursorTime, cursorID)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err = planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_minecraft_servers_public_updated") ||
		strings.Contains(plan.String(), "Seq Scan on minecraft_servers") {
		t.Fatalf("%d-row SQL keyset page did not use the partial catalog index:\n%s", rows, plan.String())
	}
	t.Logf("%d-row SQL keyset plan:\n%s", rows, plan.String())
}
