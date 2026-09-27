package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestSEC031CommentSubresourcesHideCommentsWhoseTargetIsInvisibleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify comment target visibility")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	cfg := config.Load()
	poolConfig, err := pgxpool.ParseConfig(cfg.DB.ConnString())
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

	var ownerID, viewerID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('sec031owner','sec031-owner@example.test','test',true) returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('sec031viewer','sec031-viewer@example.test','test',true) returning id`).Scan(&viewerID); err != nil {
		t.Fatal(err)
	}
	var modID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('s31m00001','sec031-private-mod','SEC031 Private Mod','pending',$1) returning id`, ownerID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	var rootID int64
	var rootPublicID string
	var rootUpdatedAt time.Time
	if err = pool.QueryRow(ctx, `insert into comments(target_type,target_id,author_id,body,status)
		values('mod',$1,$2,'SEC031 root secret','published') returning id,public_id,updated_at`, modID, viewerID).
		Scan(&rootID, &rootPublicID, &rootUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into comments(target_type,target_id,author_id,body,status,parent_id,root_id,depth)
		values('mod',$1,$2,'SEC031 reply secret','published',$3,$3,1)`, modID, ownerID, rootID); err != nil {
		t.Fatal(err)
	}
	var watchPublicID string
	if err = pool.QueryRow(ctx, `insert into comment_watches(user_id,comment_id)
		values($1,$2) returning public_id`, viewerID, rootID).Scan(&watchPublicID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool, cache: querycache.New(config.RedisConfig{}), cfg: cfg}
	claims := security.Claims{Subject: viewerID, PermissionRules: []security.PermissionRule{
		{Code: "comment.react", Allow: true, Priority: 100},
		{Code: "comment.watch", Allow: true, Priority: 100},
		{Code: "comment.edit.own", Allow: true, Priority: 100},
		{Code: "comment.pin", Allow: true, Priority: 100},
	}}
	request := func(method, target, pathKey, pathValue string, body []byte) *http.Request {
		r := httptest.NewRequest(method, target, bytes.NewReader(body))
		r.SetPathValue(pathKey, pathValue)
		return r.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	}
	assertNotFound := func(t *testing.T, invoke func(http.ResponseWriter, *http.Request), r *http.Request) {
		t.Helper()
		response := httptest.NewRecorder()
		invoke(response, r)
		if response.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}

	t.Run("replies", func(t *testing.T) {
		assertNotFound(t, server.commentReplies,
			request(http.MethodGet, "/api/v1/comments/"+rootPublicID+"/replies", "commentId", rootPublicID, nil))
	})
	t.Run("reaction", func(t *testing.T) {
		assertNotFound(t, server.commentReaction,
			request(http.MethodPut, "/api/v1/comments/"+rootPublicID+"/reaction", "commentId", rootPublicID, []byte(`{"reaction":"eyes"}`)))
	})
	t.Run("watch", func(t *testing.T) {
		assertNotFound(t, server.commentWatch,
			request(http.MethodGet, "/api/v1/comments/"+rootPublicID+"/watch", "commentId", rootPublicID, nil))
	})
	t.Run("watch item", func(t *testing.T) {
		assertNotFound(t, server.commentWatchItem,
			request(http.MethodPost, "/api/v1/comment-watches/"+watchPublicID+"/read", "watchId", watchPublicID, nil))
	})
	t.Run("edit", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"body": "SEC031 leaked edit", "baseUpdatedAt": rootUpdatedAt})
		if err != nil {
			t.Fatal(err)
		}
		assertNotFound(t, server.commentItem,
			request(http.MethodPatch, "/api/v1/comments/"+rootPublicID, "commentId", rootPublicID, body))
	})
	t.Run("pin", func(t *testing.T) {
		assertNotFound(t, server.commentPin,
			request(http.MethodPut, "/api/v1/comments/"+rootPublicID+"/pin", "commentId", rootPublicID, nil))
	})

	t.Run("watch list", func(t *testing.T) {
		response := httptest.NewRecorder()
		server.myCommentWatches(response, request(http.MethodGet, "/api/v1/users/me/comment-watches", "unused", "unused", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var envelope struct {
			Data struct {
				Items []commentWatchListItem `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Data.Items) != 0 {
			t.Fatalf("invisible target leaked watch items: %+v", envelope.Data.Items)
		}
	})

	var reactions int
	if err = pool.QueryRow(ctx, `select count(*) from comment_reactions where comment_id=$1 and user_id=$2`, rootID, viewerID).Scan(&reactions); err != nil {
		t.Fatal(err)
	}
	if reactions != 0 {
		t.Fatalf("invisible comment accepted %d reaction rows", reactions)
	}
	var body string
	var pinnedAt *time.Time
	if err = pool.QueryRow(ctx, `select body,pinned_at from comments where id=$1`, rootID).Scan(&body, &pinnedAt); err != nil {
		t.Fatal(err)
	}
	if body != "SEC031 root secret" || pinnedAt != nil {
		t.Fatalf("invisible comment mutation persisted body=%q pinnedAt=%v", body, pinnedAt)
	}

	assertRepliesVisible := func(t *testing.T, accessClaims security.Claims) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/comments/"+rootPublicID+"/replies", nil)
		r.SetPathValue("commentId", rootPublicID)
		r = r.WithContext(context.WithValue(ctx, claimsContextKey, accessClaims))
		response := httptest.NewRecorder()
		server.commentReplies(response, r)
		if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("SEC031 reply secret")) {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	t.Run("target owner retains access", func(t *testing.T) {
		assertRepliesVisible(t, security.Claims{Subject: ownerID})
	})
	if _, err = pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	t.Run("public visibility restores access", func(t *testing.T) {
		assertRepliesVisible(t, claims)
	})
}
