package httpapi

import (
	"bytes"
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
	"mcmods-cn-backend/internal/security"
)

func TestCommunityPostAutomaticApprovalRejectsAStaleRevisionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify community post revision conflicts")
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
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop BUG-090 ephemeral schema: %v", dropErr)
		}
	}()
	suffix := time.Now().UnixNano()
	setupTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var authorID, postID int64
	var publicID string
	if err = setupTx.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug090_author_%d", suffix), fmt.Sprintf("bug090_author_%d@example.test", suffix)).Scan(&authorID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	initial := communityPostSnapshot{Kind: "tutorial", Category: "general", Title: "BUG-090 initial", SourceLocale: "en-US",
		BodyMarkdown: "initial body", MinecraftVersions: []string{}, Projects: []communityPostReference{}, Resources: []communityPostReference{}}
	if err = setupTx.QueryRow(ctx, `insert into community_posts(kind,category,author_id,title,source_locale,body_markdown,review_status)
		values('tutorial','general',$1,$2,'en-US',$3,'approved') returning id,public_id`, authorID, initial.Title, initial.BodyMarkdown).
		Scan(&postID, &publicID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	created, err := createContentRevisionTx(ctx, setupTx, createContentRevisionParams{
		EntityType: "community_post", EntityID: postID, AggregateType: communityPostAggregate, AggregateKey: publicID,
		Snapshot: raw, Reason: "BUG-090 baseline", ActorID: authorID, Source: "user", Status: "approved",
		Metadata: map[string]any{"kind": "tutorial", "title": initial.Title},
	})
	if err == nil {
		err = applyCommunityPostSnapshotTx(ctx, setupTx, postID, created.RevisionID, initial, security.Claims{Subject: authorID})
	}
	if err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = setupTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	detail, err := server.loadCommunityPost(ctx, publicID, security.Claims{Subject: authorID})
	if err != nil {
		t.Fatal(err)
	}
	if detail.PublishedRevisionID != created.RevisionPublicID {
		t.Fatalf("detail publishedRevisionId = %q, want %q", detail.PublishedRevisionID, created.RevisionPublicID)
	}
	claims := security.Claims{Subject: authorID, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 100}}}
	first := invokeBUG090CommunityPostUpdate(t, ctx, server, claims, publicID, created.RevisionPublicID, "BUG-090 first editor")
	if first.Code != http.StatusOK {
		t.Fatalf("first automatic approval status = %d body=%s", first.Code, first.Body.String())
	}
	var firstEnvelope struct {
		Data struct {
			RevisionID string `json:"revisionId"`
		} `json:"data"`
	}
	if err = json.NewDecoder(first.Body).Decode(&firstEnvelope); err != nil || firstEnvelope.Data.RevisionID == "" {
		t.Fatalf("decode first automatic approval: revision=%q err=%v", firstEnvelope.Data.RevisionID, err)
	}
	stale := invokeBUG090CommunityPostUpdate(t, ctx, server, claims, publicID, created.RevisionPublicID, "BUG-090 stale editor")
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale automatic approval status = %d body=%s", stale.Code, stale.Body.String())
	}
	var conflictEnvelope struct {
		Code    string                    `json:"code"`
		Details communityPostEditConflict `json:"details"`
	}
	if err = json.NewDecoder(stale.Body).Decode(&conflictEnvelope); err != nil || conflictEnvelope.Code != "COMMUNITY_POST_EDIT_CONFLICT" ||
		conflictEnvelope.Details.CurrentRevisionID != firstEnvelope.Data.RevisionID {
		t.Fatalf("stale conflict = %#v err=%v, want current revision %q", conflictEnvelope, err, firstEnvelope.Data.RevisionID)
	}
	missingBase := invokeBUG090CommunityPostUpdate(t, ctx, server, claims, publicID, "", "BUG-090 missing baseline")
	if missingBase.Code != http.StatusConflict {
		t.Fatalf("missing baseline status = %d body=%s", missingBase.Code, missingBase.Body.String())
	}
	malformedBase := invokeBUG090CommunityPostUpdate(t, ctx, server, claims, publicID, "not-valid!", "BUG-090 malformed baseline")
	if malformedBase.Code != http.StatusBadRequest {
		t.Fatalf("malformed baseline status = %d body=%s", malformedBase.Code, malformedBase.Body.String())
	}
	var title string
	var revisionCount int
	if err = pool.QueryRow(ctx, `select title,(select count(*) from content_revisions
		where aggregate_type=$2 and aggregate_key=$3) from community_posts where id=$1`, postID, communityPostAggregate, publicID).
		Scan(&title, &revisionCount); err != nil {
		t.Fatal(err)
	}
	if title != "BUG-090 first editor" || revisionCount != 2 {
		t.Fatalf("published title/revision count = %q/%d, want first editor/2", title, revisionCount)
	}
}

func invokeBUG090CommunityPostUpdate(t *testing.T, ctx context.Context, server *Server, claims security.Claims, publicID, baseRevisionID, title string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"baseRevisionId": baseRevisionID, "category": "general", "title": title, "bodyMarkdown": title + " body",
		"sourceLocale":      "en-US",
		"minecraftVersions": []string{}, "projects": []any{}, "resources": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/community/posts/"+publicID, bytes.NewReader(body))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.updateCommunityPost(response, request, publicID)
	return response
}
