package httpapi

import (
	"context"
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

func TestFavoriteRemovalSurvivesTargetVisibilityChangeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database to verify favorite cleanup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
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
			t.Errorf("drop favorite cleanup schema: %v", dropErr)
		}
	}()
	if _, err = pool.Exec(ctx, `
		insert into users(id,public_id,username,email,password_hash) values
			(99170001,'cleanup01','favorite-cleanup-owner','favorite-cleanup-owner@example.invalid','test-only'),
			(99170002,'cleanup02','favorite-cleanup-other','favorite-cleanup-other@example.invalid','test-only');
		insert into mods(id,project_code,slug,primary_name,review_status,submitted_by) values
			(99170003,'cleanup03','favorite-cleanup-target','Hidden private title','pending',99170002);
		insert into favorite_collections(id,public_id,user_id,name,is_default) values
			(99170004,'cleanup04',99170001,'default',true),
			(99170005,'cleanup05',99170001,'another',false);
		insert into favorite_collection_items(collection_id,entity_type,entity_id) values
			(99170004,'mod',99170003);
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	invoke := func(handler http.HandlerFunc, method string, userID int64, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/api/v1/users/me/favorites", strings.NewReader(body))
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
		response := httptest.NewRecorder()
		handler(response, request)
		return response
	}
	for _, test := range []struct {
		name, method, body string
		userID             int64
		handler            http.HandlerFunc
		wantStatus         int
	}{
		{"add hidden", http.MethodPatch, `{"entityType":"mod","entityPublicId":"cleanup03","addCollectionIds":["cleanup05"]}`, 99170001, server.patchFavoriteMembership, http.StatusNotFound},
		{"move hidden", http.MethodPatch, `{"entityType":"mod","entityPublicId":"cleanup03","addCollectionIds":["cleanup05"],"removeCollectionIds":["cleanup04"]}`, 99170001, server.patchFavoriteMembership, http.StatusNotFound},
		{"replace hidden", http.MethodPut, `{"entityType":"mod","entityPublicId":"cleanup03","collectionIds":["cleanup05"]}`, 99170001, server.setFavoriteMembership, http.StatusNotFound},
		{"target owner cannot mutate another user's collection", http.MethodPatch, `{"entityType":"mod","entityPublicId":"cleanup03","removeCollectionIds":["cleanup04"]}`, 99170002, server.patchFavoriteMembership, http.StatusBadRequest},
		{"unrelated viewer cannot resolve hidden membership", http.MethodPatch, `{"entityType":"mod","entityPublicId":"cleanup03","removeCollectionIds":["cleanup04"]}`, 99170006, server.patchFavoriteMembership, http.StatusNotFound},
		{"mismatched target type", http.MethodPut, `{"entityType":"modpack","entityPublicId":"cleanup03","collectionIds":[]}`, 99170001, server.setFavoriteMembership, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := invoke(test.handler, test.method, test.userID, test.body)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s; want %d", response.Code, response.Body.String(), test.wantStatus)
			}
		})
	}
	assertCount := func(want int) {
		t.Helper()
		var count int
		if queryErr := pool.QueryRow(ctx, `select count(*) from favorite_collection_items where entity_id=99170003`).Scan(&count); queryErr != nil {
			t.Fatal(queryErr)
		}
		if count != want {
			t.Fatalf("membership count=%d; want %d", count, want)
		}
	}
	assertCount(1)
	removed := invoke(server.patchFavoriteMembership, http.MethodPatch, 99170001,
		`{"entityType":"mod","entityPublicId":"cleanup03","removeCollectionIds":["cleanup04"]}`)
	if removed.Code != http.StatusOK || strings.Contains(removed.Body.String(), "Hidden private title") {
		t.Fatalf("hidden target cleanup status=%d body=%s", removed.Code, removed.Body.String())
	}
	assertCount(0)
	if _, err = pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id) values(99170004,'mod',99170003)`); err != nil {
		t.Fatal(err)
	}
	replaced := invoke(server.setFavoriteMembership, http.MethodPut, 99170001,
		`{"entityType":"mod","entityPublicId":"cleanup03","collectionIds":[]}`)
	if replaced.Code != http.StatusOK || strings.Contains(replaced.Body.String(), "Hidden private title") {
		t.Fatalf("empty replacement status=%d body=%s", replaced.Code, replaced.Body.String())
	}
	assertCount(0)
}

func TestFavoriteAtCollectionLimitCanListAndRemoveMembershipIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, owner := newPostgresUserFixture(t, ctx, "favorite-default-stock")
	server := &Server{db: pool}
	server.cfg.Favorite.MaxCollectionsPerUser = 2
	ctx = context.WithValue(ctx, claimsContextKey, security.Claims{Subject: owner})
	create := func(name string) int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/favorite-collections", strings.NewReader(`{"name":"`+name+`"}`)).WithContext(ctx)
		server.createFavoriteCollection(recorder, request)
		return recorder.Code
	}
	if status := create("custom"); status != http.StatusCreated {
		t.Fatalf("create first custom folder = %d", status)
	}
	if status := create("second"); status != http.StatusConflict {
		t.Errorf("custom folder consumed the default folder's reserved space, HTTP %d", status)
	}
	// This inventory has no default, as can happen for a previously imported
	// account, and the operator has lowered its stock limit.
	if _, err := pool.Exec(ctx, `delete from favorite_collections where user_id=$1 and is_default`, owner); err != nil {
		t.Fatal(err)
	}
	server.cfg.Favorite.MaxCollectionsPerUser = 1
	var projectCode string
	var projectID int64
	if err := pool.QueryRow(ctx, `insert into mods(slug,primary_name,review_status) values('favorite-stock-test','Stock test','approved') returning id,project_code`).Scan(&projectID, &projectCode); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id)
		select id,'mod',$2 from favorite_collections where user_id=$1 and name='custom'`, owner, projectID); err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRecorder()
	server.favoriteCollections(list, httptest.NewRequest(http.MethodGet, "/api/v1/users/me/favorite-collections", nil).WithContext(ctx))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"name":"custom"`) {
		t.Errorf("listing existing folders at quota, HTTP %d", list.Code)
	}
	removed := invokeBUG089FavoriteReplacement(t, ctx, server, owner, projectCode, []string{})
	if removed.Code != http.StatusOK {
		t.Errorf("clear existing membership at quota, HTTP %d", removed.Code)
	}
	var survivors int
	if err := pool.QueryRow(ctx, "select count(*) from favorite_collection_items where entity_type='mod' and entity_id=$1", projectID).Scan(&survivors); err != nil || survivors != 0 {
		t.Fatalf("remaining memberships = %d, %v", survivors, err)
	}
}
