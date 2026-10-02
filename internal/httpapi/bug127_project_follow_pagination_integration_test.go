package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestProjectFollowKeysetTraversesPastLegacyWindowIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project follow keyset traversal")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
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

	var userID int64
	stamp := time.Now().UnixNano()
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'bug127',true) returning id`, fmt.Sprintf("bug127-%d", stamp), fmt.Sprintf("bug127-%d@example.invalid", stamp)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		select 'f127'||lpad(value::text,5,'0'),'bug127-'||value,'BUG127 project '||value,'approved',$1
		from generate_series(1,125) value`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `with followed as (
		select route.id,row_number() over(order by route.id) ordinal
		from public_routes route where route.public_id like 'f127%'
	)
	insert into project_follows(user_id,project_route_id,notifications_enabled,created_at,updated_at)
	select $1,id,true,timestamptz '2026-08-24 12:00:00+00'-((ordinal-1)/5)*interval '1 minute',
		timestamptz '2026-08-24 12:00:00+00' from followed`, userID); err != nil {
		t.Fatal(err)
	}
	var expected []string
	rows, err := pool.Query(ctx, `select route.public_id from project_follows follow
		join public_routes route on route.id=follow.project_route_id where follow.user_id=$1
		order by follow.created_at desc,route.id asc`, userID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var publicID string
		if err = rows.Scan(&publicID); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		expected = append(expected, publicID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()

	server := &Server{db: pool}
	claims := security.Claims{Subject: userID}
	cursor := ""
	actual := make([]string, 0, 125)
	pageCount := 0
	for {
		page := invokeBUG127ProjectFollows(t, ctx, server, claims, cursor, "")
		if page.Code != http.StatusOK {
			t.Fatalf("page %d status=%d body=%s", pageCount+1, page.Code, page.Body.String())
		}
		var envelope struct {
			Data struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
				HasMore    bool   `json:"hasMore"`
				NextCursor string `json:"nextCursor"`
				Limit      int    `json:"limit"`
			} `json:"data"`
		}
		if err = json.Unmarshal(page.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		pageCount++
		if envelope.Data.Limit != 40 || len(envelope.Data.Items) == 0 || len(envelope.Data.Items) > 40 {
			t.Fatalf("page %d payload=%+v", pageCount, envelope.Data)
		}
		for _, item := range envelope.Data.Items {
			actual = append(actual, item.ID)
		}
		if !envelope.Data.HasMore {
			if envelope.Data.NextCursor != "" {
				t.Fatalf("terminal page exposed cursor %q", envelope.Data.NextCursor)
			}
			break
		}
		if envelope.Data.NextCursor == "" || envelope.Data.NextCursor == cursor {
			t.Fatalf("page %d did not advance cursor", pageCount)
		}
		cursor = envelope.Data.NextCursor
	}
	if pageCount != 4 || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("traversal pages=%d items=%d expected=%d equal=%v", pageCount, len(actual), len(expected), reflect.DeepEqual(actual, expected))
	}
	foreignScope := invokeBUG127ProjectFollows(t, ctx, server, claims, cursor, "missing")
	if foreignScope.Code != http.StatusBadRequest {
		t.Fatalf("cross-query cursor status=%d body=%s", foreignScope.Code, foreignScope.Body.String())
	}
}

func TestProjectFollowKeysetUsesUserCreatedIndexAt100KIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project follow keyset plan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table bug127_follows(
		user_id bigint not null,project_route_id bigint not null,created_at timestamptz not null,
		primary key(user_id,project_route_id));
		create index bug127_follows_page on bug127_follows(user_id,created_at desc,project_route_id);
		insert into bug127_follows select 1,value,timestamptz '2026-08-24 12:00:00+00'-(value/5)*interval '1 second'
		from generate_series(1,100000) value;
		analyze bug127_follows`); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err = pool.QueryRow(ctx, `explain(analyze,buffers,format json)
		select project_route_id,created_at from bug127_follows
		where user_id=1 and (created_at<timestamptz '2026-08-24 09:13:20+00'
			or (created_at=timestamptz '2026-08-24 09:13:20+00' and project_route_id>50000))
		order by created_at desc,project_route_id asc limit 40`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "Index Only Scan") || strings.Contains(plan, `"Node Type": "Seq Scan"`) {
		t.Fatalf("100k keyset plan did not use the page index: %s", plan)
	}
}

func invokeBUG127ProjectFollows(t *testing.T, ctx context.Context, server *Server, claims security.Claims, cursor, query string) *httptest.ResponseRecorder {
	t.Helper()
	parameters := url.Values{"limit": {"40"}, "type": {"mod"}}
	if cursor != "" {
		parameters.Set("cursor", cursor)
	}
	if query != "" {
		parameters.Set("q", query)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/project-follows?"+parameters.Encode(), nil)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.myProjectFollows(response, request)
	return response
}
