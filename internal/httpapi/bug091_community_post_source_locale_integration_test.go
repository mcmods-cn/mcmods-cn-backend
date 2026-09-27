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

func TestCommunityPostExplicitSourceLocalePersistsThroughCreateAndEditIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify community post source locales")
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
			t.Errorf("drop BUG-091 ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var authorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug091_author_%d", suffix), fmt.Sprintf("bug091_author_%d@example.test", suffix)).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	claims := security.Claims{Subject: authorID, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 100}}}

	ambiguous := invokeBUG091CommunityPostMutation(t, ctx, server, claims, http.MethodPost, "", "", "", "printf setup")
	if ambiguous.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous missing source locale status = %d body=%s", ambiguous.Code, ambiguous.Body.String())
	}
	var postCount int
	if err = pool.QueryRow(ctx, `select count(*) from community_posts`).Scan(&postCount); err != nil || postCount != 0 {
		t.Fatalf("ambiguous request post count = %d err=%v, want 0", postCount, err)
	}

	createdResponse := invokeBUG091CommunityPostMutation(t, ctx, server, claims, http.MethodPost, "", "", "fr_fr", "printf setup")
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("explicit source locale create status = %d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		Data struct {
			ID         string `json:"id"`
			RevisionID string `json:"revisionId"`
		} `json:"data"`
	}
	if err = json.NewDecoder(createdResponse.Body).Decode(&created); err != nil || created.Data.ID == "" || created.Data.RevisionID == "" {
		t.Fatalf("decode create response: %#v err=%v", created, err)
	}
	assertBUG091CommunityPostLocale(t, ctx, pool, created.Data.ID, "fr-FR")

	updatedResponse := invokeBUG091CommunityPostMutation(t, ctx, server, claims, http.MethodPut, created.Data.ID, created.Data.RevisionID, "de", "foo.bar setup")
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("explicit source locale update status = %d body=%s", updatedResponse.Code, updatedResponse.Body.String())
	}
	assertBUG091CommunityPostLocale(t, ctx, pool, created.Data.ID, "de-DE")
	detail, err := server.loadCommunityPost(ctx, created.Data.ID, claims)
	if err != nil || detail.SourceLocale != "de-DE" {
		t.Fatalf("detail source locale = %q err=%v, want de-DE", detail.SourceLocale, err)
	}
}

func invokeBUG091CommunityPostMutation(t *testing.T, ctx context.Context, server *Server, claims security.Claims, method, publicID, baseRevisionID, sourceLocale, title string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"baseRevisionId": baseRevisionID, "kind": "tutorial", "category": "general", "title": title, "sourceLocale": sourceLocale,
		"bodyMarkdown": "Use foo.bar()", "minecraftVersions": []string{}, "projects": []any{}, "resources": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "/api/v1/community/posts/"+publicID, bytes.NewReader(body))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	if method == http.MethodPost {
		server.createCommunityPost(response, request)
	} else {
		server.updateCommunityPost(response, request, publicID)
	}
	return response
}

func assertBUG091CommunityPostLocale(t *testing.T, ctx context.Context, pool *pgxpool.Pool, publicID, expected string) {
	t.Helper()
	var sourceLocale string
	var snapshot []byte
	if err := pool.QueryRow(ctx, `select post.source_locale,revision.snapshot
		from community_posts post join content_revisions revision on revision.id=post.published_revision_id
		where post.public_id=$1`, publicID).Scan(&sourceLocale, &snapshot); err != nil {
		t.Fatal(err)
	}
	var revision communityPostSnapshot
	if err := json.Unmarshal(snapshot, &revision); err != nil {
		t.Fatal(err)
	}
	if sourceLocale != expected || revision.SourceLocale != expected {
		t.Fatalf("post/revision source locales = %q/%q, want %q/%q", sourceLocale, revision.SourceLocale, expected, expected)
	}
}
