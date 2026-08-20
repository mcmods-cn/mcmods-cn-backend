package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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
		t.Fatal("comment author activity index is missing")
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	fixtureKey := fmt.Sprintf("comment-tree-integration-%d", time.Now().UnixNano())
	var authorID, targetID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id`, fixtureKey, fixtureKey+"@example.invalid").Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type,status)
		values($1,'recipe_type','active') returning id`, "recipe-type:"+fixtureKey).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into recipe_types(entity_id,canonical_id) values($1,$2)`, targetID, "test:"+fixtureKey); err != nil {
		t.Fatal(err)
	}

	key := fixtureKey
	target := commentTargetInfo{Type: "recipe_type", InternalID: targetID}
	root, err := insertCommentTree(ctx, tx, target, authorID, createCommentRequest{
		Body: "root", IdempotencyKey: key + "-root", Status: "published",
	})
	if err != nil {
		t.Fatalf("insert root: %v", err)
	}
	child, err := insertCommentTree(ctx, tx, target, authorID, createCommentRequest{
		Body: "child", ParentID: root.PublicID, IdempotencyKey: key + "-child", Status: "published",
	})
	if err != nil {
		t.Fatalf("insert child: %v", err)
	}
	secondRoot, err := insertCommentTree(ctx, tx, target, authorID, createCommentRequest{
		Body: "second root", IdempotencyKey: key + "-root-2", Status: "published",
	})
	if err != nil {
		t.Fatalf("insert second root: %v", err)
	}
	var rootFloor, childFloor, secondRootFloor *int64
	if err = tx.QueryRow(ctx, `select
		(select floor_number from comments where id=$1),
		(select floor_number from comments where id=$2),
		(select floor_number from comments where id=$3)`, root.ID, child.ID, secondRoot.ID).
		Scan(&rootFloor, &childFloor, &secondRootFloor); err != nil {
		t.Fatal(err)
	}
	if rootFloor == nil || *rootFloor != 1 || childFloor != nil || secondRootFloor == nil || *secondRootFloor != 2 {
		t.Fatalf("unexpected floors: root=%v child=%v second=%v", rootFloor, childFloor, secondRootFloor)
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

	var viewerID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id`, fixtureKey+"-viewer", fixtureKey+"-viewer@example.invalid").Scan(&viewerID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into user_blocks(blocker_id,blocked_id) values($1,$2)`, viewerID, authorID); err != nil {
		t.Fatal(err)
	}
	hiddenItems, err := queryCommentItemsWithQueryer(ctx, tx, []int64{root.ID}, true, 3, viewerID)
	if err != nil {
		t.Fatalf("query filtered comment tree: %v", err)
	}
	if len(hiddenItems) != 0 {
		t.Fatalf("blocked author's comments were returned: %#v", hiddenItems)
	}

	var attachmentPublicID string
	if err = tx.QueryRow(ctx, `insert into oss_files(object_key,category,source,original_name,source_original_name,
		content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values($1,'users/comments','comment','notes.txt','notes.txt','text/plain',12,12,$2,$3,'active','clean') returning public_id`,
		"tests/comments/"+fixtureKey+"/notes.txt", strings.Repeat("a", 64), authorID).Scan(&attachmentPublicID); err != nil {
		t.Fatal(err)
	}
	if err = bindCommentAttachmentsTx(ctx, tx, child.ID, authorID, []string{attachmentPublicID}); err != nil {
		t.Fatalf("bind reply attachment: %v", err)
	}
	var attachmentCount int
	if err = tx.QueryRow(ctx, `select count(*) from comment_attachments where comment_id=$1`, child.ID).Scan(&attachmentCount); err != nil {
		t.Fatal(err)
	}
	if attachmentCount != 1 {
		t.Fatalf("unexpected reply attachment count: %d", attachmentCount)
	}
	if err = bindCommentAttachmentsTx(ctx, tx, root.ID, viewerID, []string{attachmentPublicID}); !errors.Is(err, errCommentAttachmentUnavailable) {
		t.Fatalf("another user was able to bind the attachment: %v", err)
	}
}
