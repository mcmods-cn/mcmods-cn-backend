package httpapi

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOCT02CommentListPreviewBoundsAndKeepsReplyExpansionIntegration(t *testing.T) {
	dsn := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MCMODS_TEST_DATABASE_URL required")
	}
	target, err := url.Parse(dsn)
	if err != nil || (target.Hostname() != "127.0.0.1" && target.Hostname() != "localhost") {
		t.Fatal("comment preview fixture requires isolated loopback database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	key := randomHex(8)
	var authors []int64
	for _, label := range []string{"visible", "blocked", "viewer"} {
		var id int64
		if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,status) values($1,$2,'fixture-only','active') returning id`, "oct02_preview_"+key+label, key+label+"@example.test").Scan(&id); err != nil {
			t.Fatal(err)
		}
		authors = append(authors, id)
	}
	var routeID int64
	if err = tx.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id) values(new_public_id(),'modpack',$1) returning id`, -authors[0]).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	root, err := insertCommentTree(ctx, tx, commentTargetInfo{Type: "modpack", InternalID: -authors[0]}, authors[0], createCommentRequest{Body: "root", IdempotencyKey: key, Status: "published"})
	if err != nil {
		t.Fatal(err)
	}
	for _, author := range []int64{authors[1], authors[0]} {
		if _, err = tx.Exec(ctx, `insert into comments(target_type,target_id,author_id,body,parent_id,root_id,depth,created_at)
			select 'modpack',$1,$2,'reply',$3,$3,1,clock_timestamp()+n*interval '1 microsecond' from generate_series(1,100) n`, -authors[0], author, root.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(ctx, `update comments set child_count=200,descendant_count=200 where id=$1`, root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into user_blocks(blocker_id,blocked_id) values($1,$2)`, authors[2], authors[1]); err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []int64{0, authors[2]} {
		items, err := queryCommentItemsWithQueryer(ctx, tx, []int64{root.ID}, true, 3, viewer)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 65 {
			t.Fatalf("viewer %d list preview returned %d nodes; want one root plus 64 replies", viewer, len(items))
		}
		if items[0].ID != root.PublicID || !items[0].HasMoreReplies {
			t.Fatal("bounded preview must retain root and expose reply expansion")
		}
		for _, item := range items {
			if viewer != 0 && item.Author.internalID == authors[1] {
				t.Fatal("blocked author returned in preview")
			}
		}
	}
}
