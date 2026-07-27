package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestInsertCommentTreeIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the comment tree integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var recentCommentIndex *string
	if err = db.QueryRow(ctx, `select to_regclass('public.idx_comments_author_created')::text`).Scan(&recentCommentIndex); err != nil {
		t.Fatal(err)
	}
	if recentCommentIndex == nil {
		t.Fatal("recent-comment rate-limit index is missing")
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var authorID, targetID int64
	if err = tx.QueryRow(ctx, `select users.id,recipe_type.entity_id
		from users cross join recipe_types recipe_type
		order by users.id,recipe_type.entity_id limit 1`).Scan(&authorID, &targetID); err != nil {
		t.Fatal(err)
	}

	key := fmt.Sprintf("comment-tree-integration-%d", time.Now().UnixNano())
	target := commentTargetInfo{Type: "recipe_type", InternalID: targetID}
	root, err := insertCommentTree(ctx, tx, target, authorID, createCommentRequest{
		Body: "root", IdempotencyKey: key + "-root",
	})
	if err != nil {
		t.Fatalf("insert root: %v", err)
	}
	child, err := insertCommentTree(ctx, tx, target, authorID, createCommentRequest{
		Body: "child", ParentID: root.PublicID, IdempotencyKey: key + "-child",
	})
	if err != nil {
		t.Fatalf("insert child: %v", err)
	}

	if child.ParentID == nil || *child.ParentID != root.ID {
		t.Fatalf("unexpected child parent: %#v", child.ParentID)
	}
	if child.RootID == nil || *child.RootID != root.ID || child.Depth != 1 {
		t.Fatalf("unexpected child tree position: root=%#v depth=%d", child.RootID, child.Depth)
	}
	var closureRows, childCount, descendantCount int
	if err = tx.QueryRow(ctx, `select
		(select count(*) from comment_closure where descendant_id=$2),
		(select child_count from comments where id=$1),
		(select descendant_count from comments where id=$1)`,
		root.ID, child.ID).Scan(&closureRows, &childCount, &descendantCount); err != nil {
		t.Fatal(err)
	}
	if closureRows != 2 || childCount != 1 || descendantCount != 1 {
		t.Fatalf("unexpected tree counters: closure=%d children=%d descendants=%d", closureRows, childCount, descendantCount)
	}

	var watchID int64
	if err = tx.QueryRow(ctx, `insert into comment_watches(user_id,comment_id)
		values($1,$2) returning id`, authorID, root.ID).Scan(&watchID); err != nil {
		t.Fatal(err)
	}
	pending, err := recordCommentWatchReplies(ctx, tx, root.ID, child.ID, 0)
	if err != nil {
		t.Fatalf("record watch replies: %v", err)
	}
	if len(pending) != 1 || pending[0].WatchID != watchID || pending[0].RecipientID != authorID {
		t.Fatalf("unexpected pending watch notifications: %#v", pending)
	}
	var unreadCount, watchedReplyCount, replyRows int
	if err = tx.QueryRow(ctx, `select unread_count,watched_reply_count,
		(select count(*) from comment_watch_replies where watch_id=$1 and comment_id=$2)
		from comment_watches where id=$1`, watchID, child.ID).
		Scan(&unreadCount, &watchedReplyCount, &replyRows); err != nil {
		t.Fatal(err)
	}
	if unreadCount != 1 || watchedReplyCount != 1 || replyRows != 1 {
		t.Fatalf("unexpected watch counters: unread=%d watched=%d replies=%d", unreadCount, watchedReplyCount, replyRows)
	}

	if _, err = tx.Exec(ctx, `insert into comment_reactions(comment_id,user_id,reaction)
		values($1,$2,'heart')`, child.ID, authorID); err != nil {
		t.Fatal(err)
	}
	items, err := queryCommentItemsWithQueryer(ctx, tx, []int64{root.ID}, true, 3, authorID)
	if err != nil {
		t.Fatalf("query comment tree: %v", err)
	}
	if len(items) != 2 || items[1].Reactions["heart"] != 1 ||
		len(items[1].UserReactions) != 1 || items[1].UserReactions[0] != "heart" {
		t.Fatalf("unexpected queried comment tree: %#v", items)
	}
}
