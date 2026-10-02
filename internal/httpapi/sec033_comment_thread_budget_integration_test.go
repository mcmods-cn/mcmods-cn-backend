package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestSEC033CommentThreadNeighborhoodUsesBoundedKeysetPagesIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run SEC033 integration")
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `
		create temporary table comments(
			id bigint primary key,
			parent_id bigint,
			author_id bigint not null,
			status text not null,
			created_at timestamptz not null
		);
		create index comments_parent_created_visible on comments(parent_id,created_at,id)
			where status in ('published','deleted');
		create temporary table comment_closure(
			ancestor_id bigint not null,
			descendant_id bigint not null,
			depth integer not null,
			primary key(ancestor_id,descendant_id)
		);
		create index comment_closure_descendant on comment_closure(descendant_id,depth,ancestor_id);
		create temporary table user_blocks(blocker_id bigint not null,blocked_id bigint not null);
		insert into comments(id,parent_id,author_id,status,created_at)
		select depth,null,1,'published','2026-08-24T00:00:00Z'::timestamptz-depth*interval '1 second'
		from generate_series(1,25) depth;
		insert into comments values(1000,1,1,'published','2026-08-24T00:00:00Z');
		insert into comment_closure
		select depth,1000,depth from generate_series(1,25) depth;
		insert into comments(id,parent_id,author_id,status,created_at)
		select 2000+value,1000,case when value=73 then 99 else 1 end,'published',
			'2026-08-24T00:00:00Z'::timestamptz+value*interval '1 second'
		from generate_series(1,200) value;
		insert into user_blocks values(7,99);
	`); err != nil {
		t.Fatal(err)
	}

	ancestors, replies, hasMore, pathTruncated, err := queryCommentThreadNeighborhoodWithQueryer(ctx, tx, 1000, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ancestors) != maxCommentThreadPathNodes || !pathTruncated {
		t.Fatalf("ancestor budget not enforced: len=%d truncated=%v", len(ancestors), pathTruncated)
	}
	if ancestors[0] != 16 || ancestors[len(ancestors)-1] != 1 {
		t.Fatalf("nearest ancestor path has unexpected order: %v", ancestors)
	}
	if len(replies) != maxCommentThreadNodes-maxCommentThreadPathNodes-1 || !hasMore {
		t.Fatalf("initial neighborhood budget not enforced: replies=%d more=%v", len(replies), hasMore)
	}

	seen := make(map[int64]struct{}, 199)
	for _, reply := range replies {
		seen[reply.id] = struct{}{}
	}
	for hasMore {
		last := replies[len(replies)-1]
		cursor := &commentReplyPageCursor{
			Version:   commentPageCursorVersion,
			Scope:     commentReplyCursorScope(1000, 7),
			CreatedAt: last.createdAt,
			ID:        last.id,
		}
		ancestors, replies, hasMore, pathTruncated, err = queryCommentThreadNeighborhoodWithQueryer(ctx, tx, 1000, 7, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(ancestors) != 0 || pathTruncated {
			t.Fatalf("cursor page repeated path: ancestors=%v truncated=%v", ancestors, pathTruncated)
		}
		if len(replies) == 0 {
			t.Fatal("cursor page did not advance")
		}
		for _, reply := range replies {
			if _, duplicate := seen[reply.id]; duplicate {
				t.Fatalf("duplicate reply across keyset pages: %d", reply.id)
			}
			seen[reply.id] = struct{}{}
		}
	}
	if len(seen) != 199 {
		t.Fatalf("visible keyset traversal returned %d replies, want 199", len(seen))
	}
	if _, blocked := seen[2073]; blocked {
		t.Fatal("blocked author's reply escaped the neighborhood visibility boundary")
	}
}

func TestSEC033CommentThreadHandlerCapsNodesAndEncodedBytesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to run SEC033 handler integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = config.Load().DB.ConnString()
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop ephemeral schema: %v", err)
		}
	})

	var authorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('sec033author','sec033-author@example.test','test',true) returning id`).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	var modID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('s33m00001','sec033-public-mod','SEC033 Public Mod','approved',$1) returning id`, authorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	largeBody := strings.Repeat("x", maxCommentMarkdownRunes-8)
	var rootID int64
	var rootPublicID string
	if err = pool.QueryRow(ctx, `insert into comments(target_type,target_id,author_id,body,status,child_count,descendant_count)
		values('mod',$1,$2,$3,'published',200,200) returning id,public_id`, modID, authorID, largeBody).
		Scan(&rootID, &rootPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into comments(target_type,target_id,author_id,parent_id,root_id,depth,body,status,created_at)
		select 'mod',$1,$2,$3,$3,1,left($4||lpad(value::text,8,'0'),10000),'published',
			'2026-08-24T00:00:00Z'::timestamptz+value*interval '1 second'
		from generate_series(1,200) value`, modID, authorID, rootID, largeBody); err != nil {
		t.Fatal(err)
	}

	cfg := config.Load()
	server := &Server{db: pool, cache: querycache.New(config.RedisConfig{}), cfg: cfg}
	seen := make(map[string]struct{}, 201)
	cursor := ""
	for pageNumber := 1; pageNumber <= 10; pageNumber++ {
		target := "/api/v1/comments/" + rootPublicID + "/thread"
		if cursor != "" {
			target += "?cursor=" + cursor
		}
		request := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
		request.SetPathValue("commentId", rootPublicID)
		response := httptest.NewRecorder()
		server.commentThread(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("page=%d status=%d body=%s", pageNumber, response.Code, response.Body.String())
		}
		if response.Body.Len() > maxCommentThreadResponseBytes {
			t.Fatalf("page=%d bytes=%d budget=%d", pageNumber, response.Body.Len(), maxCommentThreadResponseBytes)
		}
		var envelope struct {
			Data commentThreadPageResponse `json:"data"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Data.Items) == 0 || len(envelope.Data.Items) > maxCommentThreadNodes {
			t.Fatalf("page=%d nodes=%d", pageNumber, len(envelope.Data.Items))
		}
		for _, item := range envelope.Data.Items {
			if _, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("page=%d duplicate comment=%s", pageNumber, item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		cursor = envelope.Data.NextCursor
		if cursor == "" {
			break
		}
		if pageNumber == 10 {
			t.Fatal("thread cursor did not terminate")
		}
	}
	if len(seen) != 201 {
		t.Fatalf("bounded handler traversed %d comments, want root plus 200 replies", len(seen))
	}
	if _, ok := seen[rootPublicID]; !ok {
		t.Fatal("bounded handler omitted the focus comment")
	}
}
