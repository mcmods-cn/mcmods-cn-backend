package httpapi

import (
	"context"
	"fmt"
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
	"mcmods-cn-backend/internal/security"
)

func TestCommentReplyCapabilityUsesTheSameTargetOwnerBlockAsCreationIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify comment reply capabilities")
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
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	fixture := fmt.Sprintf("bug085-%d", time.Now().UnixNano())
	var ownerID, viewerID, modID, creatorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id`, fixture+"-owner", fixture+"-owner@example.invalid").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id`, fixture+"-viewer", fixture+"-viewer@example.invalid").Scan(&viewerID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `delete from users where id in ($1,$2)`, ownerID, viewerID)
	}()
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by,review_status)
		values(new_public_id(),$1,$2,$3,'approved') returning id`, fixture, fixture, ownerID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `delete from mods where id=$1`, modID)
	if err = pool.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status)
		values('author',$1,$1,'approved') returning id`, fixture+"-creator").Scan(&creatorID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `delete from creators where id=$1`, creatorID)
	if _, err = pool.Exec(ctx, `insert into creator_claims(creator_id,user_id,status,reviewed_at)
		values($1,$2,'approved',now())`, creatorID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,name_snapshot,role_snapshot,status,permission_granting,approved_at)
		select 'mod',$2,$1,role.id,$3,role.name,'approved',true,now()
		from creator_role_definitions role where role.code='developer'`, creatorID, modID, fixture+"-creator"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_blocks(blocker_id,blocked_id) values($1,$2)`, ownerID, viewerID); err != nil {
		t.Fatal(err)
	}
	var commentID int64
	var commentPublicID string
	if err = pool.QueryRow(ctx, `insert into comments(target_type,target_id,author_id,body,status)
		values('mod',$1,$2,'owner comment','published') returning id,public_id`, modID, ownerID).
		Scan(&commentID, &commentPublicID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `delete from comments where id=$1`, commentID)
	server := &Server{db: pool, cache: querycache.New(config.RedisConfig{})}
	claims := security.Claims{Subject: viewerID, PermissionRules: []security.PermissionRule{{Code: "comment.create", Allow: true}}}
	items := []commentResponse{{ID: commentPublicID}}
	if err = server.annotateCommentPermissions(ctx, items, claims); err != nil {
		t.Fatal(err)
	}
	if items[0].CanReply {
		t.Fatal("target owner blocked the viewer, but the per-comment reply capability remained true")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/comment-targets/mod/example/comments", nil)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.createComment(response, request, commentTargetInfo{Type: "mod", InternalID: modID})
	if response.Code != http.StatusForbidden {
		t.Fatalf("blocked create status=%d body=%s", response.Code, response.Body.String())
	}

	if _, err = pool.Exec(ctx, `delete from user_blocks where blocker_id=$1 and blocked_id=$2`, ownerID, viewerID); err != nil {
		t.Fatal(err)
	}
	items = []commentResponse{{ID: commentPublicID}}
	if err = server.annotateCommentPermissions(ctx, items, claims); err != nil {
		t.Fatal(err)
	}
	if !items[0].CanReply {
		t.Fatal("reply capability did not recover after the target owner removed the block")
	}
}
