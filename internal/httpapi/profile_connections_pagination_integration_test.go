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

func TestUserConnectionCursorTraversesAndUsesDeepPageIndexesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute connection pagination against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
		create temporary table connection_user_fixtures(
			id bigint primary key,public_id text,username text,avatar_url text,signature text,status text);
		create temporary table connection_scale_ids(id bigint primary key);
		create temporary view users as
			select id,public_id,username,avatar_url,signature,status from connection_user_fixtures
			union all
			select id,'scale-'||id::text,'Scale '||id::text,'','','active' from connection_scale_ids;
		create temporary table user_follows(
			follower_id bigint not null,followed_id bigint not null,created_at timestamptz not null,
			primary key(follower_id,followed_id));
		create index idx_user_follows_followed_page
			on user_follows(followed_id,created_at desc,follower_id desc);
		create index idx_user_follows_follower_page
			on user_follows(follower_id,created_at desc,followed_id desc);
		insert into connection_user_fixtures
		select id,'fixture-'||id::text,'Fixture '||id::text,'','','active'
		from generate_series(1001,1205) id
		union all
		select id,'fixture-'||id::text,'Fixture '||id::text,'','','active'
		from generate_series(2001,2205) id;
		insert into user_follows(follower_id,followed_id,created_at)
		select id,1,to_timestamp(10000+((id-1001)/2)) from generate_series(1001,1205) id
		union all
		select 2,id,to_timestamp(20000+((id-2001)/2)) from generate_series(2001,2205) id;
		analyze connection_user_fixtures; analyze user_follows`); err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		connectionType string
		userID         int64
	}{
		{connectionType: "followers", userID: 1},
		{connectionType: "following", userID: 2},
	} {
		ids := traverseUserConnectionFixture(t, ctx, pool, testCase.userID, testCase.connectionType, 24)
		if len(ids) != 205 {
			t.Fatalf("%s traversed %d rows, want 205", testCase.connectionType, len(ids))
		}
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if _, exists := seen[id]; exists {
				t.Fatalf("%s repeated %s", testCase.connectionType, id)
			}
			seen[id] = struct{}{}
		}
	}

	if _, err = pool.Exec(ctx, `
		insert into connection_scale_ids(id)
		select 1000000+value from generate_series(1,600000) value
		union all
		select 2000000+value from generate_series(1,600000) value;
		insert into user_follows(follower_id,followed_id,created_at)
		select 1000000+value,50,to_timestamp(value) from generate_series(1,600000) value
		union all
		select 60,2000000+value,to_timestamp(value) from generate_series(1,600000) value;
		analyze connection_scale_ids; analyze user_follows`); err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		connectionType string
		userID         int64
		connectionID   int64
		indexName      string
	}{
		{connectionType: "followers", userID: 50, connectionID: 1_001_001, indexName: "idx_user_follows_followed_page"},
		{connectionType: "following", userID: 60, connectionID: 2_001_001, indexName: "idx_user_follows_follower_page"},
	} {
		request := userConnectionPageRequest{
			Limit: 24,
			Scope: userConnectionPageScope(testCase.userID, testCase.connectionType, 24),
			Cursor: &userConnectionPageCursor{
				Version: userConnectionCursorVersion, CreatedAt: time.Unix(1001, 0).UTC(), ConnectionUserID: testCase.connectionID,
			},
		}
		request.Cursor.Scope = request.Scope
		page, hasMore, nextCursor, loadErr := loadUserConnectionPage(ctx, pool, testCase.userID, testCase.connectionType, request)
		if loadErr != nil || len(page) != 24 || !hasMore || nextCursor == "" {
			t.Fatalf("deep %s page=(rows=%d hasMore=%t cursor=%t err=%v)", testCase.connectionType, len(page), hasMore, nextCursor != "", loadErr)
		}

		query, args := userConnectionPageQuery(testCase.userID, testCase.connectionType, request)
		planRows, planErr := pool.Query(ctx, `explain (analyze,buffers,format text) `+query, args...)
		if planErr != nil {
			t.Fatal(planErr)
		}
		var plan strings.Builder
		for planRows.Next() {
			var line string
			if err = planRows.Scan(&line); err != nil {
				planRows.Close()
				t.Fatal(err)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		planRows.Close()
		if err = planRows.Err(); err != nil {
			t.Fatal(err)
		}
		planText := strings.ToLower(plan.String())
		if !strings.Contains(planText, testCase.indexName) || strings.Contains(planText, "seq scan on user_follows") {
			t.Fatalf("deep %s plan did not use %s:\n%s", testCase.connectionType, testCase.indexName, plan.String())
		}
		for _, line := range strings.Split(plan.String(), "\n") {
			if strings.Contains(line, "Execution Time:") {
				t.Logf("600k-deep %s plan: %s", testCase.connectionType, strings.TrimSpace(line))
			}
		}
	}
}

func traverseUserConnectionFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID int64,
	connectionType string,
	limit int,
) []string {
	t.Helper()
	request := userConnectionPageRequest{Limit: limit, Scope: userConnectionPageScope(userID, connectionType, limit)}
	ids := make([]string, 0)
	for {
		page, hasMore, nextCursor, err := loadUserConnectionPage(ctx, pool, userID, connectionType, request)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range page {
			ids = append(ids, row.Item.ID)
		}
		if !hasMore {
			if nextCursor != "" {
				t.Fatal("terminal connection page returned a cursor")
			}
			break
		}
		cursor, decodeErr := decodeUserConnectionPageCursor(nextCursor, request.Scope)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		request.Cursor = cursor
	}
	return ids
}
