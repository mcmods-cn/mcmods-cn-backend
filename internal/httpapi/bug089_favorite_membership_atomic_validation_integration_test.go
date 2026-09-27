package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
	"mcmods-cn-backend/internal/security"
)

func TestFavoriteMembershipReplacementValidatesEveryCollectionBeforeMutationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify atomic favorite replacement")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop BUG-089 ephemeral schema: %v", dropErr)
		}
	})
	suffix := time.Now().UnixNano()
	setupTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ownerID, otherID, collectionID, modID int64
	var collectionPublicID, otherCollectionPublicID, modPublicID string
	if err = setupTx.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug089_owner_%d", suffix), fmt.Sprintf("bug089_owner_%d@example.test", suffix)).Scan(&ownerID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = setupTx.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug089_other_%d", suffix), fmt.Sprintf("bug089_other_%d@example.test", suffix)).Scan(&otherID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = setupTx.QueryRow(ctx, `insert into favorite_collections(user_id,name,is_default)
		values($1,'BUG-089 owner',true) returning id,public_id`, ownerID).Scan(&collectionID, &collectionPublicID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = setupTx.QueryRow(ctx, `insert into favorite_collections(user_id,name,is_default)
		values($1,'BUG-089 other',true) returning public_id`, otherID).Scan(&otherCollectionPublicID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	projectCode := fmt.Sprintf("b%08x", uint64(suffix)&0xffffffff)
	if err = setupTx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,'BUG-089 target','approved',$3) returning id,project_code`, projectCode, fmt.Sprintf("bug089-%d", suffix), ownerID).
		Scan(&modID, &modPublicID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err = setupTx.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id)
		values($1,'mod',$2)`, collectionID, modID); err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = setupTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `delete from favorite_collection_items where entity_type='mod' and entity_id=$1;
			delete from favorite_collections where user_id=any($2::bigint[]);
			delete from mods where id=$1;
			delete from users where id=any($2::bigint[])`, modID, []int64{ownerID, otherID})
	}()

	server := &Server{db: pool}
	for name, collectionIDs := range map[string][]string{
		"missing collection": {"missing01"},
		"somebody else's ID": {collectionPublicID, otherCollectionPublicID},
	} {
		t.Run(name, func(t *testing.T) {
			ensureBUG089Membership(t, ctx, pool, collectionID, modID)
			response := invokeBUG089FavoriteReplacement(t, ctx, server, ownerID, modPublicID, collectionIDs)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid replacement status = %d body=%s", response.Code, response.Body.String())
			}
			assertBUG089MembershipCount(t, ctx, pool, collectionID, modID, 1)
		})
	}

	response := invokeBUG089FavoriteReplacement(t, ctx, server, ownerID, modPublicID,
		[]string{collectionPublicID, collectionPublicID})
	if response.Code != http.StatusOK {
		t.Fatalf("deduplicated valid replacement status = %d body=%s", response.Code, response.Body.String())
	}
	assertBUG089MembershipCount(t, ctx, pool, collectionID, modID, 1)
	response = invokeBUG089FavoriteReplacement(t, ctx, server, ownerID, modPublicID, []string{})
	if response.Code != http.StatusOK {
		t.Fatalf("explicit empty replacement status = %d body=%s", response.Code, response.Body.String())
	}
	assertBUG089MembershipCount(t, ctx, pool, collectionID, modID, 0)
}

func invokeBUG089FavoriteReplacement(t *testing.T, ctx context.Context, server *Server, ownerID int64, modPublicID string, collectionIDs []string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"entityType": "mod", "entityPublicId": modPublicID, "collectionIds": collectionIDs,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/favorites", bytes.NewReader(body))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
	response := httptest.NewRecorder()
	server.setFavoriteMembership(response, request)
	return response
}

func ensureBUG089Membership(t *testing.T, ctx context.Context, pool *pgxpool.Pool, collectionID, modID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id)
		values($1,'mod',$2) on conflict do nothing`, collectionID, modID); err != nil {
		t.Fatal(err)
	}
}

func assertBUG089MembershipCount(t *testing.T, ctx context.Context, queryer followProjectQueryer, collectionID, modID int64, want int) {
	t.Helper()
	var count int
	if err := queryer.QueryRow(ctx, `select count(*) from favorite_collection_items
		where collection_id=$1 and entity_type='mod' and entity_id=$2`, collectionID, modID).Scan(&count); err != nil || count != want {
		t.Fatalf("favorite membership count = %d err=%v, want %d", count, err, want)
	}
}
