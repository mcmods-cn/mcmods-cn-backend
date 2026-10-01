package httpapi

import (
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
	"mcmods-cn-backend/internal/security"
)

func TestPrivateFavoriteCannotRevealOtherPendingModCoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("requires dedicated migrated PostgreSQL via MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	key := "fav-core-" + randomHex(8)
	var submitter, viewer, modID, collectionID int64
	var modPublicID, collectionPublicID string
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `delete from mods where id=$1`, modID); err != nil {
			t.Errorf("synthetic mod cleanup: %v", err)
		}
		if _, err := pool.Exec(context.Background(), `delete from users where id in ($1,$2)`, submitter, viewer); err != nil {
			t.Errorf("synthetic user cleanup: %v", err)
		}
	})
	for index, target := range []*int64{&submitter, &viewer} {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'synthetic') returning id`, fmt.Sprintf("%s-%d", key, index), fmt.Sprintf("%s-%d@example.test", key, index)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by,review_status) values(new_public_id(),$1,$2,$3,'pending') returning id`, key, "Synthetic pending secret", submitter).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select public_id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&modPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(user_id,name,is_public) values($1,$2,false) returning id,public_id`, viewer, key).Scan(&collectionID, &collectionPublicID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/favorites", strings.NewReader(fmt.Sprintf(`{"entityType":"mod","entityPublicId":%q,"collectionIds":[%q]}`, modPublicID, collectionPublicID)))
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, security.Claims{Subject: viewer}))
	res := httptest.NewRecorder()
	server.setFavoriteMembership(res, req)
	if res.Code != http.StatusBadRequest && res.Code != http.StatusNotFound && res.Code != http.StatusForbidden {
		t.Fatalf("unexpected visibility response: %d %s", res.Code, res.Body.String())
	}
	// Existing entries may outlive a project's public status. The read boundary
	// must filter those entries even when the write boundary now rejects them.
	if _, err = pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id)
		values($1,'mod',$2) on conflict do nothing`, collectionID, modID); err != nil {
		t.Fatal(err)
	}
	loadItems := func() []favoriteCollectionItem {
		t.Helper()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/me/favorite-collections/"+collectionPublicID+"/items", nil)
		req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, security.Claims{Subject: viewer}))
		res = httptest.NewRecorder()
		server.writeFavoriteCollectionItems(res, req, collectionPublicID, viewer, true)
		if res.Code != http.StatusOK {
			t.Fatalf("loading private collection failed: %d %s", res.Code, res.Body.String())
		}
		var reply struct {
			Data struct {
				Items []favoriteCollectionItem `json:"items"`
			} `json:"data"`
		}
		if err = json.Unmarshal(res.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		return reply.Data.Items
	}
	for _, item := range loadItems() {
		if item.EntityKey == modPublicID {
			t.Fatalf("private collection exposed another submitter's pending project: status=%d metadata=%v", res.Code, item.Metadata)
		}
	}
	if _, err = pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	items := loadItems()
	if len(items) != 1 || items[0].EntityKey != modPublicID {
		t.Fatalf("approved project did not remain available in the private collection: %#v", items)
	}
}
