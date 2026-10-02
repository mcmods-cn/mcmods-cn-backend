package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestFavoriteCollectionDeletionKeepsFirstAndLastPopularityFactsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database")
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
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	}()
	if _, err = pool.Exec(ctx, `insert into users(id,username,email,password_hash,status,security_score,created_at)
		values(99390001,'favorite-facts-owner','favorite-facts@example.invalid','test-only','active',100,now()-interval '60 days');
		insert into mods(id,project_code,slug,primary_name,review_status) values
		(99390002,'facts0001','favorite-facts-target','synthetic target','approved');
		insert into favorite_collections(id,public_id,user_id,name,is_default) values
		(99390003,'facts0002',99390001,'first',false),(99390004,'facts0003',99390001,'second',false);
		insert into favorite_collection_items(collection_id,entity_type,entity_id) values(99390003,'mod',99390002);
		insert into favorite_collection_items(collection_id,entity_type,entity_id) values(99390004,'mod',99390002)`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	assertFacts := func(wantCount int, wantValue int) {
		t.Helper()
		var count int
		var value float64
		if err := pool.QueryRow(ctx, `select facts.favorite_count,coalesce((select sum(event.favorite_value) from content_popularity_events_daily event where event.object_route_id=route.id),0)
			from public_routes route join content_popularity_lifetime_facts facts on facts.object_route_id=route.id where route.public_id='facts0001'`).Scan(&count, &value); err != nil {
			t.Fatal(err)
		}
		if count != wantCount || value != float64(wantValue) {
			t.Fatalf("favorite projection count=%d value=%g; want %d/%d", count, value, wantCount, wantValue)
		}
	}
	assertFacts(1, 4)
	unchanged := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/favorites", strings.NewReader(
		`{"entityType":"mod","entityPublicId":"facts0001","collectionIds":["facts0002","facts0003"]}`))
	unchanged = unchanged.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 99390001}))
	unchangedResponse := httptest.NewRecorder()
	server.setFavoriteMembership(unchangedResponse, unchanged)
	if unchangedResponse.Code != http.StatusOK {
		t.Fatalf("unchanged multi-collection replacement=%d %s", unchangedResponse.Code, unchangedResponse.Body.String())
	}
	assertFacts(1, 4)
	for i, publicID := range []string{"facts0002", "facts0003"} {
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/me/favorite-collections/"+publicID, nil)
		request.SetPathValue("id", publicID)
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 99390001}))
		response := httptest.NewRecorder()
		server.deleteFavoriteCollection(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("delete=%d %s", response.Code, response.Body.String())
		}
		assertFacts(1-i, 4*(1-i))
	}
	if _, err = pool.Exec(ctx, `insert into favorite_collections(id,public_id,user_id,name,is_default) values
		(99390005,'facts0004',99390001,'third',false),(99390006,'facts0005',99390001,'fourth',false)`); err != nil {
		t.Fatal(err)
	}
	invoke := func(handler http.HandlerFunc, method, body string) {
		t.Helper()
		request := httptest.NewRequest(method, "/api/v1/users/me/favorites", strings.NewReader(body))
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 99390001}))
		response := httptest.NewRecorder()
		handler(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("membership=%d %s", response.Code, response.Body.String())
		}
	}
	invoke(server.patchFavoriteMembership, http.MethodPatch, `{"entityType":"mod","entityPublicId":"facts0001","addCollectionIds":["facts0004","facts0005"]}`)
	assertFacts(1, 4)
	var beforeIDs, afterIDs []int64
	if err := pool.QueryRow(ctx, `select array_agg(id order by id) from favorite_collection_items where entity_id=99390002`).Scan(&beforeIDs); err != nil {
		t.Fatal(err)
	}
	invoke(server.setFavoriteMembership, http.MethodPut, `{"entityType":"mod","entityPublicId":"facts0001","collectionIds":["facts0004","facts0005"]}`)
	assertFacts(1, 4)
	if err := pool.QueryRow(ctx, `select array_agg(id order by id) from favorite_collection_items where entity_id=99390002`).Scan(&afterIDs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeIDs, afterIDs) {
		t.Fatal("unchanged replacement recreated favorite membership IDs")
	}
	invoke(server.patchFavoriteMembership, http.MethodPatch, `{"entityType":"mod","entityPublicId":"facts0001","removeCollectionIds":["facts0004","facts0005"]}`)
	assertFacts(0, 0)
}
