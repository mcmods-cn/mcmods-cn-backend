package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestSEC036FavoriteCollectionAndItemQuotasAreAtomicIntegration(t *testing.T) {
	pool := newSEC036FavoriteQuotaPool(t)
	policy := favoriteQuotaPolicy{MaximumCollections: 3, MaximumItems: 3}

	t.Run("collections", func(t *testing.T) {
		const attempts = 12
		results := runSEC036ConcurrentQuotaAttempts(attempts, func(attempt int) error {
			tx, err := pool.Begin(context.Background())
			if err != nil {
				return err
			}
			defer tx.Rollback(context.Background())
			if err = lockFavoriteStockQuotaTx(context.Background(), tx, 36001); err == nil {
				err = enforceFavoriteCollectionGrowthTx(context.Background(), tx, 36001, 1, policy)
			}
			if err == nil {
				_, err = tx.Exec(context.Background(), `select pg_sleep(0.04)`)
			}
			if err == nil {
				_, err = tx.Exec(context.Background(), `insert into favorite_collections(public_id,user_id,name)
					values($1,36001,$2)`, fmt.Sprintf("s036c%04d", attempt), fmt.Sprintf("collection-%d", attempt))
			}
			if err == nil {
				err = tx.Commit(context.Background())
			}
			return err
		})
		assertSEC036QuotaResults(t, results, 3, errFavoriteCollectionQuota)
		assertSEC036Count(t, pool, `select count(*) from favorite_collections where user_id=36001`, 3)
	})

	t.Run("items", func(t *testing.T) {
		if _, err := pool.Exec(context.Background(), `insert into favorite_collections(public_id,user_id,name)
			values('s036items',36002,'items')`); err != nil {
			t.Fatal(err)
		}
		const attempts = 12
		results := runSEC036ConcurrentQuotaAttempts(attempts, func(attempt int) error {
			tx, err := pool.Begin(context.Background())
			if err != nil {
				return err
			}
			defer tx.Rollback(context.Background())
			if err = lockFavoriteStockQuotaTx(context.Background(), tx, 36002); err == nil {
				err = enforceFavoritePatchQuotaTx(context.Background(), tx, 36002, "mod", int64(attempt+1),
					[]string{"s036items"}, nil, policy)
			}
			if err == nil {
				_, err = tx.Exec(context.Background(), `select pg_sleep(0.04)`)
			}
			if err == nil {
				_, err = tx.Exec(context.Background(), `insert into favorite_collection_items(collection_id,entity_type,entity_id)
					select id,'mod',$1 from favorite_collections where public_id='s036items'`, attempt+1)
			}
			if err == nil {
				err = tx.Commit(context.Background())
			}
			return err
		})
		assertSEC036QuotaResults(t, results, 3, errFavoriteItemQuota)
		assertSEC036Count(t, pool, `select count(*) from favorite_collection_items item
			join favorite_collections collection on collection.id=item.collection_id where collection.user_id=36002`, 3)
	})

	t.Run("repeatable visibility snapshot starts after quota lock", func(t *testing.T) {
		if _, err := pool.Exec(context.Background(), `insert into favorite_collections(public_id,user_id,name)
			values('s036snap1',36004,'snapshot')`); err != nil {
			t.Fatal(err)
		}
		first, err := pool.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer first.Rollback(context.Background())
		if err = lockFavoriteStockQuotaTx(context.Background(), first, 36004); err != nil {
			t.Fatal(err)
		}
		if _, err = first.Exec(context.Background(), `insert into favorite_collection_items(collection_id,entity_type,entity_id)
			select id,'mod',1 from favorite_collections where public_id='s036snap1'`); err != nil {
			t.Fatal(err)
		}
		server := &Server{db: pool}
		result := make(chan error, 1)
		go func() {
			membershipTx, beginErr := server.beginFavoriteMembershipTx(context.Background(), 36004)
			if beginErr == nil {
				defer membershipTx.close()
				beginErr = enforceFavoritePatchQuotaTx(context.Background(), membershipTx.tx, 36004,
					"mod", 2, []string{"s036snap1"}, nil,
					favoriteQuotaPolicy{MaximumCollections: 10, MaximumItems: 1})
			}
			result <- beginErr
		}()
		deadline := time.Now().Add(3 * time.Second)
		for {
			var waiting bool
			if err = pool.QueryRow(context.Background(), `select exists(select 1 from pg_stat_activity
				where datname=current_database() and wait_event_type='Lock' and wait_event='advisory'
				and query like '%favorite-stock-quota:%')`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("repeatable-read contender never waited on the favorite quota lock")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err = first.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err = <-result; !errors.Is(err, errFavoriteItemQuota) {
			t.Fatalf("post-lock repeatable snapshot quota result = %v, want %v", err, errFavoriteItemQuota)
		}
	})
}

func TestSEC036FavoriteItemQuotaUsesNetGrowthAndAllowsCleanupIntegration(t *testing.T) {
	pool := newSEC036FavoriteQuotaPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `insert into favorite_collections(public_id,user_id,name) values
		('s036net01',36003,'one'),('s036net02',36003,'two');
		insert into favorite_collection_items(collection_id,entity_type,entity_id)
		select id,'mod',11 from favorite_collections where public_id='s036net01';
		insert into favorite_collection_items(collection_id,entity_type,entity_id)
		select id,'mod',12 from favorite_collections where public_id='s036net01'`); err != nil {
		t.Fatal(err)
	}
	policy := favoriteQuotaPolicy{MaximumCollections: 10, MaximumItems: 2}

	check := func(name string, evaluate func(pgx.Tx) error, want error) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if err = lockFavoriteStockQuotaTx(ctx, tx, 36003); err == nil {
				err = evaluate(tx)
			}
			if !errors.Is(err, want) {
				t.Fatalf("quota result = %v, want %v", err, want)
			}
		})
	}
	check("idempotent patch at limit", func(tx pgx.Tx) error {
		return enforceFavoritePatchQuotaTx(ctx, tx, 36003, "mod", 11, []string{"s036net01"}, nil, policy)
	}, nil)
	check("balanced move at limit", func(tx pgx.Tx) error {
		return enforceFavoritePatchQuotaTx(ctx, tx, 36003, "mod", 11, []string{"s036net02"}, []string{"s036net01"}, policy)
	}, nil)
	check("new relation at limit", func(tx pgx.Tx) error {
		return enforceFavoritePatchQuotaTx(ctx, tx, 36003, "mod", 13, []string{"s036net02"}, nil, policy)
	}, errFavoriteItemQuota)
	check("idempotent replacement at limit", func(tx pgx.Tx) error {
		return enforceFavoriteReplacementQuotaTx(ctx, tx, 36003, "mod", 11, []string{"s036net01"}, policy)
	}, nil)
	check("growing replacement at limit", func(tx pgx.Tx) error {
		return enforceFavoriteReplacementQuotaTx(ctx, tx, 36003, "mod", 11, []string{"s036net01", "s036net02"}, policy)
	}, errFavoriteItemQuota)
	check("cleanup remains available above a lowered limit", func(tx pgx.Tx) error {
		return enforceFavoriteReplacementQuotaTx(ctx, tx, 36003, "mod", 11, nil,
			favoriteQuotaPolicy{MaximumCollections: 10, MaximumItems: 1})
	}, nil)
}

func TestSEC036FavoriteHandlersReturnStableQuotaCodesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify favorite quota handlers")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
			t.Errorf("drop SEC-036 handler schema: %v", dropErr)
		}
	})

	ownerIDs := make([]int64, 3)
	for index, username := range []string{"Sec036Create", "Sec036Default", "Sec036Items"} {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
			values($1,$2,'not-used','active') returning id`, username, strings.ToLower(username)+"@example.test").Scan(&ownerIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	for _, ownerID := range ownerIDs[:2] {
		if _, err = pool.Exec(ctx, `insert into favorite_collections(user_id,name) values($1,'one'),($1,'two')`, ownerID); err != nil {
			t.Fatal(err)
		}
	}
	var defaultCollectionID int64
	var defaultCollectionPublicID, secondCollectionPublicID string
	if err = pool.QueryRow(ctx, `insert into favorite_collections(user_id,name,is_default)
		values($1,'default',true) returning id,public_id`, ownerIDs[2]).Scan(&defaultCollectionID, &defaultCollectionPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(user_id,name)
		values($1,'second') returning public_id`, ownerIDs[2]).Scan(&secondCollectionPublicID); err != nil {
		t.Fatal(err)
	}
	modIDs := make([]int64, 3)
	modPublicIDs := make([]string, 3)
	for index := range modIDs {
		if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
			values($1,$2,$3,'approved',$4) returning id,project_code`, fmt.Sprintf("s036m%04d", index),
			fmt.Sprintf("sec036-mod-%d", index), fmt.Sprintf("SEC-036 mod %d", index), ownerIDs[2]).
			Scan(&modIDs[index], &modPublicIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id)
		values($1,'mod',$2),($1,'mod',$3)`, defaultCollectionID, modIDs[0], modIDs[1]); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool, cfg: config.Config{Favorite: config.FavoriteConfig{
		MaxCollectionsPerUser: 2, MaxItemsPerUser: 2,
	}}}
	invoke := func(method, path, body string, ownerID int64, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
		response := httptest.NewRecorder()
		handler(response, request)
		return response
	}
	assertQuota := func(response *httptest.ResponseRecorder, code string) {
		t.Helper()
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
			t.Fatalf("quota response status=%d body=%s, want 409/%s", response.Code, response.Body.String(), code)
		}
	}

	assertQuota(invoke(http.MethodPost, "/api/v1/users/me/favorite-collections", `{"name":"three"}`,
		ownerIDs[0], server.createFavoriteCollection), "FAVORITE_COLLECTION_LIMIT")
	assertQuota(invoke(http.MethodGet, "/api/v1/users/me/favorite-collections", "",
		ownerIDs[1], server.favoriteCollections), "FAVORITE_COLLECTION_LIMIT")

	patchBody := fmt.Sprintf(`{"entityType":"mod","entityPublicId":%q,"addCollectionIds":[%q],"removeCollectionIds":[]}`,
		modPublicIDs[2], secondCollectionPublicID)
	assertQuota(invoke(http.MethodPatch, "/api/v1/users/me/favorites", patchBody,
		ownerIDs[2], server.patchFavoriteMembership), "FAVORITE_ITEM_LIMIT")
	replacementBody := fmt.Sprintf(`{"entityType":"mod","entityPublicId":%q,"collectionIds":[%q,%q]}`,
		modPublicIDs[0], defaultCollectionPublicID, secondCollectionPublicID)
	assertQuota(invoke(http.MethodPut, "/api/v1/users/me/favorites", replacementBody,
		ownerIDs[2], server.setFavoriteMembership), "FAVORITE_ITEM_LIMIT")
	idempotentBody := fmt.Sprintf(`{"entityType":"mod","entityPublicId":%q,"collectionIds":[%q]}`,
		modPublicIDs[0], defaultCollectionPublicID)
	if response := invoke(http.MethodPut, "/api/v1/users/me/favorites", idempotentBody,
		ownerIDs[2], server.setFavoriteMembership); response.Code != http.StatusOK {
		t.Fatalf("idempotent replacement at quota status=%d body=%s", response.Code, response.Body.String())
	}
	assertSEC036Count(t, pool, `select count(*) from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id where collection.user_id=`+fmt.Sprint(ownerIDs[2]), 2)
}

func runSEC036ConcurrentQuotaAttempts(attempts int, operation func(int) error) []error {
	start := make(chan struct{})
	results := make(chan error, attempts)
	var ready sync.WaitGroup
	ready.Add(attempts)
	for attempt := range attempts {
		go func() {
			ready.Done()
			<-start
			results <- operation(attempt)
		}()
	}
	ready.Wait()
	close(start)
	collected := make([]error, 0, attempts)
	for range attempts {
		collected = append(collected, <-results)
	}
	return collected
}

func assertSEC036QuotaResults(t *testing.T, results []error, wantAccepted int, quotaError error) {
	t.Helper()
	accepted, limited := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, quotaError):
			limited++
		default:
			t.Fatalf("unexpected quota result: %v", err)
		}
	}
	if accepted != wantAccepted || limited != len(results)-wantAccepted {
		t.Fatalf("quota results accepted=%d limited=%d, want %d/%d", accepted, limited,
			wantAccepted, len(results)-wantAccepted)
	}
}

func assertSEC036Count(t *testing.T, pool *pgxpool.Pool, query string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(), query).Scan(&got); err != nil || got != want {
		t.Fatalf("persisted quota rows = %d err=%v, want %d", got, err, want)
	}
}

func newSEC036FavoriteQuotaPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with isolated PostgreSQL to verify favorite stock quotas")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)
	schemaName := fmt.Sprintf("sec036_favorite_quota_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, dropErr := adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated SEC-036 schema: %v", dropErr)
		}
	})

	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.MaxConns = 24
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `create table favorite_collections(
		id bigserial primary key, public_id text not null unique, user_id bigint not null, name text not null);
		create table favorite_collection_items(
		id bigserial primary key, collection_id bigint not null references favorite_collections(id) on delete cascade,
		entity_type text not null, entity_id bigint not null, unique(collection_id,entity_type,entity_id));
		create index idx_sec036_items_target on favorite_collection_items(entity_type,entity_id,collection_id)`); err != nil {
		t.Fatal(err)
	}
	return pool
}
