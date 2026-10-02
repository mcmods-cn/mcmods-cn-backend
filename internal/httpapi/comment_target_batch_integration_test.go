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

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

type countingCommentTargetQueryer struct {
	pool    *pgxpool.Pool
	queries int
}

func TestCommentTargetBatchAllBranchesCompileAgainstFullSchemaIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify full-schema comment watch batching")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop ephemeral schema: %v", err)
		}
	})
	targetTypes := []string{
		"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon",
		"blueprint", "skin", "creator", "community_post", "ban_record", "player_profile", "tag", "recipe_type", "mod_resource",
	}
	identities := make([]commentTargetIdentity, 0, len(targetTypes))
	for index, targetType := range targetTypes {
		identity := commentTargetIdentity{Type: targetType, ID: int64(900000000 + index)}
		if targetType == "mod_resource" {
			identity.VersionKey = 900000999
		}
		identities = append(identities, identity)
	}
	queryer := &countingCommentTargetQueryer{pool: pool}
	targets, err := queryCommentTargetsByInternalWithQueryer(ctx, queryer, identities, security.Claims{})
	if err != nil {
		t.Fatal(err)
	}
	if queryer.queries != 1 || len(targets) != 0 {
		t.Fatalf("full-schema batch queries=%d targets=%d", queryer.queries, len(targets))
	}
	for _, identity := range identities {
		queryer.queries = 0
		targets, err = queryCommentTargetsByInternalWithQueryer(ctx, queryer, []commentTargetIdentity{identity}, security.Claims{})
		if err != nil || queryer.queries != 1 || len(targets) != 0 {
			t.Fatalf("single-type full-schema target=%s queries=%d targets=%d err=%v", identity.Type, queryer.queries, len(targets), err)
		}
	}

	const ownerID int64 = 846000001
	const authorID int64 = 846000002
	if _, err = pool.Exec(ctx, `insert into users(id,public_id,username,email,password_hash,email_verified)
		values($1,'uwatch046','Watch Owner','watch-owner@example.test','test',true),
		      ($2,'awatch046','Watch Author','watch-author@example.test','test',true)`, ownerID, authorID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		select 'w'||lpad(value::text,8,'0'),'watch-mod-'||value,'Watch Mod '||value,'approved',$1
		from generate_series(1,100) value`, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into comments(public_id,target_type,target_id,author_id,body,status,created_at)
		select 'c'||lpad(row_number() over(order by mod.id)::text,8,'0'),'mod',mod.id,$1,
			'watch batch '||mod.id,'published',now()-row_number() over(order by mod.id)*interval '1 second'
		from mods mod where mod.slug like 'watch-mod-%'`, authorID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into comment_watches(user_id,comment_id,created_at,last_activity_at)
		select $1,comment.id,comment.created_at,comment.created_at
		from comments comment where comment.body like 'watch batch %'`, ownerID); err != nil {
		t.Fatal(err)
	}
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "comment-watch-batch",
		PoolSize: 4, MinIdleConns: 1, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, TTL: time.Minute,
	})
	t.Cleanup(func() { _ = cache.Close() })
	server := &Server{db: pool, cache: cache, cfg: loaded}
	claims := security.Claims{Subject: ownerID, PermissionRules: []security.PermissionRule{{
		Code: "comment.watch", Allow: true, Priority: 100,
	}}}
	invoke := func(limit int) (int64, []commentWatchListItem) {
		t.Helper()
		counter.queries.Store(0)
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/users/me/comment-watches?limit=%d", limit), nil)
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
		response := httptest.NewRecorder()
		server.myCommentWatches(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("watch limit=%d status=%d body=%s", limit, response.Code, response.Body.String())
		}
		var envelope struct {
			Data struct {
				Items []commentWatchListItem `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return counter.queries.Load(), envelope.Data.Items
	}
	oneQueries, oneItem := invoke(1)
	hundredQueries, hundredItems := invoke(100)
	if len(oneItem) != 1 || len(hundredItems) != 100 {
		t.Fatalf("watch items one=%d hundred=%d", len(oneItem), len(hundredItems))
	}
	// Target-owner blocks are a separate authorization fact from target
	// visibility. They add one deduplicated batch query, independent of page
	// size, so the security boundary remains constant rather than N+1.
	if oneQueries != hundredQueries || hundredQueries > 13 {
		t.Fatalf("watch query budget one=%d hundred=%d want equal and <=13", oneQueries, hundredQueries)
	}
	for _, item := range hundredItems {
		if item.Target.Key == "" || item.Comment.ID == "" {
			t.Fatalf("incomplete batched watch item: %+v", item)
		}
	}
	t.Logf("watch page database queries: one=%d hundred=%d", oneQueries, hundredQueries)
}

func (queryer *countingCommentTargetQueryer) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	queryer.queries++
	return queryer.pool.Query(ctx, query, args...)
}

func TestCommentTargetsBatchResolveOneHundredRowsInOneQueryIntegration(t *testing.T) {
	pool, ctx := openDeadLetterPageTestDB(t, 30*time.Second)
	if _, err := pool.Exec(ctx, `create temporary table mods(
		id bigint primary key,project_code text not null,primary_name text not null,
		review_status text not null,submitted_by bigint not null);
		create temporary table public_routes(
		public_id text not null,entity_type text not null,internal_id bigint not null,canonical_path text not null,
		primary key(public_id,entity_type));
		insert into mods
		select value,'mod-'||value,'Mod '||value,'approved',1 from generate_series(1,100) value;
		insert into public_routes
		select 'mod-'||value,'mod',value,'/mods/mod-'||value from generate_series(1,100) value;
		insert into mods values(101,'mod-101','Private Mod','pending',9);
		insert into public_routes values('mod-101','mod',101,'/mods/mod-101')`); err != nil {
		t.Fatal(err)
	}
	identities := make([]commentTargetIdentity, 0, 102)
	for id := int64(1); id <= 100; id++ {
		identities = append(identities, commentTargetIdentity{Type: "mod", ID: id})
	}
	identities = append(identities, commentTargetIdentity{Type: "mod", ID: 1})
	identities = append(identities, commentTargetIdentity{Type: "unknown", ID: 999})
	queryer := &countingCommentTargetQueryer{pool: pool}
	started := time.Now()
	targets, err := queryCommentTargetsByInternalWithQueryer(ctx, queryer, identities, security.Claims{})
	if err != nil {
		t.Fatal(err)
	}
	if queryer.queries != 1 {
		t.Fatalf("target queries=%d want 1", queryer.queries)
	}
	if len(targets) != 100 {
		t.Fatalf("resolved targets=%d want 100", len(targets))
	}
	for id := int64(1); id <= 100; id++ {
		identity := commentTargetIdentity{Type: "mod", ID: id}
		target := targets[identity]
		if target.Key != fmt.Sprintf("mod-%d", id) || target.URL != fmt.Sprintf("/mods/mod-%d", id) {
			t.Fatalf("target %d mismatch: %+v", id, target)
		}
	}
	if _, visible := targets[commentTargetIdentity{Type: "unknown", ID: 999}]; visible {
		t.Fatal("unknown target type was resolved")
	}
	t.Logf("100 target batch wall=%s", time.Since(started))

	queryer.queries = 0
	privateIdentity := commentTargetIdentity{Type: "mod", ID: 101}
	hiddenTargets, err := queryCommentTargetsByInternalWithQueryer(ctx, queryer, []commentTargetIdentity{privateIdentity}, security.Claims{})
	if err != nil || queryer.queries != 1 || len(hiddenTargets) != 0 {
		t.Fatalf("private target leaked queries=%d targets=%d err=%v", queryer.queries, len(hiddenTargets), err)
	}
	queryer.queries = 0
	privateTargets, err := queryCommentTargetsByInternalWithQueryer(ctx, queryer, []commentTargetIdentity{privateIdentity}, security.Claims{Subject: 9})
	if err != nil || queryer.queries != 1 {
		t.Fatalf("owner target resolve queries=%d err=%v", queryer.queries, err)
	}
	if privateTargets[privateIdentity].Title != "Private Mod" {
		t.Fatalf("owner target missing: %+v", privateTargets)
	}
}
