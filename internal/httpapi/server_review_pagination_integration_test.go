package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestServerReviewKeysetCursorTraversesEveryStatusIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the server review pagination integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `create temporary table users (
		id bigint primary key, public_id text not null, username text not null
	); create temporary table minecraft_servers (
		id bigint primary key, public_id text not null, name text not null, address text not null,
		short_description text not null default '', body_markdown text not null default '',
		minecraft_versions text[] not null default '{}', dedicated_client boolean not null default false,
		languages text[] not null default '{}', primary_tag text not null default 'survival',
		has_whitelist boolean not null default false, online_mode boolean not null default true,
		modded boolean not null default false, loader text not null default '', proof_text text not null default '',
		review_status text not null, review_note text not null default '', submitted_by bigint not null,
		created_at timestamptz not null, reviewed_at timestamptz
	); create temporary table oss_files (
		id bigint primary key, status text not null, scan_status text not null
	); create temporary table minecraft_server_proof_files (
		server_id bigint not null, oss_file_id bigint not null, primary key(server_id,oss_file_id)
	); create temporary table minecraft_server_links (
		id bigint primary key, server_id bigint not null
	); create index idx_minecraft_server_links_order on minecraft_server_links(server_id,id);
	create temporary table minecraft_server_mods (
		id bigint primary key, server_id bigint not null, raw_mod_id text not null,
		unique(server_id,raw_mod_id)
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err = connection.Exec(ctx, `insert into users values(1,'u00000001','reviewer')`); err != nil {
		t.Fatal(err)
	}
	statuses := []string{"pending", "approved", "rejected"}
	for statusIndex, status := range statuses {
		prefix := string(status[0])
		if _, err = connection.Exec(ctx, `insert into minecraft_servers(id,public_id,name,address,review_status,submitted_by,created_at)
			select $1+value,$2||lpad(value::text,8,'0'),'server-'||value,'example.test:'||(25000+value),$3,1,
			timestamptz '2026-08-21 00:00:00+00'+((value-1)/3)*interval '1 second'
			from generate_series(1,205) value`, int64(statusIndex*1000), prefix, status); err != nil {
			t.Fatal(err)
		}
		seen := map[string]struct{}{}
		cursor := ""
		for page := 0; ; page++ {
			request, parseErr := parseServerReviewPageRequest(url.Values{
				"status": {status}, "limit": {"100"}, "cursor": {cursor},
			})
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			pageIDs, next, queryErr := queryServerReviewIntegrationPage(ctx, connection, request)
			if queryErr != nil {
				t.Fatal(queryErr)
			}
			for _, id := range pageIDs {
				if _, duplicate := seen[id]; duplicate {
					t.Fatalf("status %s repeated %s", status, id)
				}
				seen[id] = struct{}{}
			}
			if next == "" {
				if page != 2 {
					t.Fatalf("status %s ended on page %d", status, page)
				}
				break
			}
			cursor = next
		}
		if len(seen) != 205 {
			t.Fatalf("status %s traversed %d rows", status, len(seen))
		}
	}

	if _, err = connection.Exec(ctx, `truncate minecraft_servers;
		insert into minecraft_servers(id,public_id,name,address,review_status,submitted_by,created_at)
		select value,'p'||lpad(value::text,8,'0'),'server-'||value,'example.test:'||(25000+value),'pending',1,
		timestamptz '2026-08-21 00:00:00+00'+value*interval '1 millisecond'
		from generate_series(1,100000) value;
		create index idx_minecraft_servers_review_page on minecraft_servers(review_status,created_at,id);
		analyze minecraft_servers`); err != nil {
		t.Fatal(err)
	}
	request, err := parseServerReviewPageRequest(url.Values{"status": {"pending"}, "limit": {"100"}})
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &serverReviewPageCursor{
		Version: serverReviewCursorVersion, Scope: request.Scope,
		CreatedAt: time.Date(2026, 8, 21, 0, 0, 50, 0, time.UTC), ID: 50000,
	}
	query, arguments := serverReviewPageSQL(request)
	planRows, err := connection.Query(ctx, "explain (analyze,buffers,format text) "+query, arguments...)
	if err != nil {
		t.Fatal(err)
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
	if !strings.Contains(plan.String(), "idx_minecraft_servers_review_page") || strings.Contains(plan.String(), "Seq Scan on minecraft_servers") {
		t.Fatalf("100k server review cursor missed its index:\n%s", plan.String())
	}
	t.Logf("100k server review plan:\n%s", plan.String())
}

func queryServerReviewIntegrationPage(
	ctx context.Context,
	connection *pgxpool.Conn,
	request serverReviewPageRequest,
) ([]string, string, error) {
	query, arguments := serverReviewPageSQL(request)
	rows, err := connection.Query(ctx, query, arguments...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	type rowIdentity struct {
		PublicID string
		Created  time.Time
		ID       int64
	}
	identities := make([]rowIdentity, 0, request.Limit+1)
	for rows.Next() {
		var identity rowIdentity
		var ignored [19]any
		for index := range ignored {
			ignored[index] = new(any)
		}
		destinations := []any{
			&identity.PublicID, ignored[0], ignored[1], ignored[2], ignored[3], ignored[4],
			ignored[5], ignored[6], ignored[7], ignored[8], ignored[9], ignored[10], ignored[11],
			ignored[12], ignored[13], ignored[14], &identity.Created, ignored[15], &identity.ID,
			ignored[16], ignored[17], ignored[18],
		}
		if err = rows.Scan(destinations...); err != nil {
			return nil, "", fmt.Errorf("scan server review page: %w", err)
		}
		identities = append(identities, identity)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	hasMore := len(identities) > request.Limit
	if hasMore {
		identities = identities[:request.Limit]
	}
	ids := make([]string, 0, len(identities))
	for _, identity := range identities {
		ids = append(ids, identity.PublicID)
	}
	if !hasMore {
		return ids, "", nil
	}
	last := identities[len(identities)-1]
	return ids, encodeServerReviewPageCursor(serverReviewPageCursor{
		Version: serverReviewCursorVersion, Scope: request.Scope, CreatedAt: last.Created, ID: last.ID,
	}), nil
}
