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

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestUserBlockRelationshipAndOwnedCommentTargetsIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the user block integration test")
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

	fixtureKey := fmt.Sprintf("user-block-integration-%d", time.Now().UnixNano())
	var ownerID, blockedID int64
	var blockedPublicID string
	if err = db.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id`, fixtureKey+"-owner", fixtureKey+"-owner@example.invalid").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id,public_id`, fixtureKey+"-blocked", fixtureKey+"-blocked@example.invalid").Scan(&blockedID, &blockedPublicID); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `delete from users where id in ($1,$2)`, ownerID, blockedID)
	if _, err = db.Exec(ctx, `insert into user_blocks(blocker_id,blocked_id) values($1,$2)`, ownerID, blockedID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: db, cache: querycache.New(config.RedisConfig{})}
	blocked, err := server.usersBlockEachOther(ctx, blockedID, ownerID)
	if err != nil || !blocked {
		t.Fatalf("expected reverse relationship lookup to detect block: blocked=%v err=%v", blocked, err)
	}
	if _, err = db.Exec(ctx, `insert into user_follows(follower_id,followed_id) values($1,$2),($2,$1)`, ownerID, blockedID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+blockedPublicID+"/block", nil)
	request.SetPathValue("id", blockedPublicID)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: ownerID}))
	response := httptest.NewRecorder()
	server.userBlock(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent block request returned %d: %s", response.Code, response.Body.String())
	}
	var followCount int
	if err = db.QueryRow(ctx, `select count(*) from user_follows
		where (follower_id=$1 and followed_id=$2) or (follower_id=$2 and followed_id=$1)`, ownerID, blockedID).Scan(&followCount); err != nil {
		t.Fatal(err)
	}
	if followCount != 0 {
		t.Fatalf("blocking did not remove both follow directions: %d", followCount)
	}
	low, high := orderedUserIDs(ownerID, blockedID)
	if _, err = db.Exec(ctx, `insert into direct_conversations(user_low_id,user_high_id) values($1,$2)`, low, high); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/messages/conversations", nil)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: ownerID}))
	response = httptest.NewRecorder()
	server.conversations(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("conversation list returned %d: %s", response.Code, response.Body.String())
	}
	var conversationResponse struct {
		Data struct {
			Items []directConversationSummary `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &conversationResponse); err != nil {
		t.Fatal(err)
	}
	if len(conversationResponse.Data.Items) != 1 || conversationResponse.Data.Items[0].CanMessage {
		t.Fatalf("blocked conversation remained writable: %#v", conversationResponse.Data)
	}

	var modID int64
	if err = db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by,review_status)
		values(new_public_id(),$1,$2,$3,'approved') returning id`, fixtureKey, fixtureKey, ownerID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `delete from mods where id=$1`, modID)
	var authorID int64
	if err = db.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status)
		values('author',$1,$1,'approved') returning id`, fixtureKey+"-author").Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `delete from creators where id=$1`, authorID)
	if _, err = db.Exec(ctx, `insert into creator_claims(creator_id,user_id,status,reviewed_at)
		values($1,$2,'approved',now())`, authorID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,name_snapshot,role_snapshot,status,permission_granting,approved_at)
		select 'mod',$1,$2,role.id,$3,role.name,'approved',true,now()
		from creator_role_definitions role where role.code='developer'`, modID, authorID, fixtureKey+"-author"); err != nil {
		t.Fatal(err)
	}
	blocked, err = server.commentTargetOwnerBlocksUser(ctx, commentTargetInfo{Type: "mod", InternalID: modID}, blockedID)
	if err != nil || !blocked {
		t.Fatalf("expected mod owner block to reject comments: blocked=%v err=%v", blocked, err)
	}
	if _, err = db.Exec(ctx, `insert into comments(target_type,target_id,author_id,body,status)
		values('mod',$1,$2,'hidden by viewer block','published')`, modID, blockedID); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `delete from comments where target_type='mod' and target_id=$1`, modID)
	request = httptest.NewRequest(http.MethodGet, "/api/v1/comment-targets/mod/example/comments", nil)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: ownerID}))
	response = httptest.NewRecorder()
	server.listTargetComments(response, request, commentTargetInfo{Type: "mod", InternalID: modID})
	if response.Code != http.StatusOK {
		t.Fatalf("comment list returned %d: %s", response.Code, response.Body.String())
	}
	var commentList struct {
		Data struct {
			Items []commentResponse `json:"items"`
			Total int               `json:"total"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &commentList); err != nil {
		t.Fatal(err)
	}
	if len(commentList.Data.Items) != 0 || commentList.Data.Total != 0 {
		t.Fatalf("blocked user's comment leaked through list response: %#v", commentList)
	}

	for _, testCase := range []struct {
		kind        string
		wantBlocked bool
	}{
		{kind: "tutorial", wantBlocked: true},
		{kind: "discussion", wantBlocked: true},
		{kind: "issue", wantBlocked: false},
		{kind: "news", wantBlocked: false},
	} {
		var postID int64
		severity := ""
		if testCase.kind == "issue" {
			severity = "minor"
		}
		if err = db.QueryRow(ctx, `insert into community_posts(kind,category,author_id,title,source_locale,severity,review_status)
			values($1,'general',$2,$3,'zh-CN',$4,'approved') returning id`, testCase.kind, ownerID, fixtureKey+"-"+testCase.kind, severity).Scan(&postID); err != nil {
			t.Fatal(err)
		}
		blocked, err = server.commentTargetOwnerBlocksUser(ctx, commentTargetInfo{Type: "community_post", InternalID: postID}, blockedID)
		if err != nil || blocked != testCase.wantBlocked {
			t.Fatalf("kind %s: blocked=%v want=%v err=%v", testCase.kind, blocked, testCase.wantBlocked, err)
		}
		if _, err = db.Exec(ctx, `delete from community_posts where id=$1`, postID); err != nil {
			t.Fatal(err)
		}
	}
}
