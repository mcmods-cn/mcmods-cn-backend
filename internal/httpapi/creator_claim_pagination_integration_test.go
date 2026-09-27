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

func TestCreatorClaimPageUsesConstantQueriesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify creator claim query count")
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
	usernamePrefix := "claim_page_" + nonce + "_"
	if _, err = pool.Exec(ctx, `insert into users(username,email,password_hash,email_verified)
		select $1 || value::text,$2 || value::text || '@claim-page.invalid','test-only',true
		from generate_series(1,205) value`, usernamePrefix, nonce+"-"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into creators(kind,name,normalized_name,review_status,created_by,created_at)
		select 'author','Claim page author ' || row_number() over(order by account.id),
			'claim page author ' || row_number() over(order by account.id),'approved',account.id,
			timestamptz '2026-01-01 00:00:00+00'+row_number() over(order by account.id)*interval '1 millisecond'
		from users account where account.username like $1 || '%' order by account.id`, usernamePrefix); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into creator_claims(creator_id,user_id,proof_markdown,created_at)
		select creator.id,creator.created_by,'query-count proof ' || creator.id::text,creator.created_at
		from creators creator where creator.name like 'Claim page author %' order by creator.id`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into oss_files(object_key,original_name,size_bytes,source_size_bytes,uploader_id,status,scan_status)
		select 'creator-claim-page/' || claim.id::text,'proof-' || claim.id::text || '.txt',claim.id,claim.id+10,
			claim.user_id,'active','clean'
		from creator_claims claim order by claim.id limit 5`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into creator_claim_attachments(claim_id,oss_file_id,display_order)
		select claim.id,file.id,0 from creator_claims claim
		join oss_files file on file.object_key='creator-claim-page/' || claim.id::text`); err != nil {
		t.Fatal(err)
	}

	request, err := parseCreatorClaimPageRequest(map[string][]string{"limit": {"100"}})
	if err != nil {
		t.Fatal(err)
	}
	queryContext, cancelQuery := context.WithTimeout(ctx, 5*time.Second)
	defer cancelQuery()
	seen := make(map[string]struct{}, 205)
	pageSizes := make([]int, 0, 3)
	attachmentCount := 0
	for {
		counter.queries.Store(0)
		page, hasMore, pageErr := (&Server{db: pool}).queryCreatorClaimPage(queryContext, request)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if queries := counter.queries.Load(); queries != 2 {
			t.Fatalf("%d-item creator claim page executed %d SQL statements, want exactly 2", len(page), queries)
		}
		pageSizes = append(pageSizes, len(page))
		for _, item := range page {
			if _, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("creator claim %s appeared on multiple pages", item.ID)
			}
			seen[item.ID] = struct{}{}
			attachmentCount += len(item.Attachments)
			for _, attachment := range item.Attachments {
				if attachment.Name == "" || attachment.SizeBytes <= 10 {
					t.Fatalf("invalid attachment projection: %+v", attachment)
				}
			}
		}
		if !hasMore {
			break
		}
		last := page[len(page)-1]
		request.Cursor = &creatorClaimPageCursor{
			Version: creatorClaimCursorVersion,
			Scope:   request.Scope, CreatedAt: last.CreatedAt, ID: last.InternalID,
		}
	}
	if len(seen) != 205 || len(pageSizes) != 3 || pageSizes[0] != 100 || pageSizes[1] != 100 || pageSizes[2] != 5 {
		t.Fatalf("paged creator claims=%d page sizes=%v", len(seen), pageSizes)
	}
	if attachmentCount != 5 {
		t.Fatalf("projected attachments=%d want=5", attachmentCount)
	}

	if _, err = pool.Exec(ctx, `create temporary table creator_claim_page_scale(
		id bigint primary key,status text not null,created_at timestamptz not null
	); create index idx_creator_claim_page_scale on creator_claim_page_scale(status,created_at,id);
	insert into creator_claim_page_scale
	select value,'pending',timestamptz '2026-01-01 00:00:00+00'+value*interval '1 millisecond'
	from generate_series(1,100000) value;
	analyze creator_claim_page_scale`); err != nil {
		t.Fatal(err)
	}
	planRows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select id from creator_claim_page_scale
		where status='pending' and (created_at,id)>(timestamptz '2026-01-01 00:00:50+00',50000)
		order by created_at,id limit 51`)
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
	if !strings.Contains(plan, "idx_creator_claim_page_scale") || strings.Contains(plan, "seq scan") {
		t.Fatalf("100k deep page missed the status/time/id index:\n%s", plan)
	}
	t.Logf("100k creator claim deep-page plan:\n%s", plan)
}
