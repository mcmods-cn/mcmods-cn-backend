package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestTEST041FavoriteAuthenticationVisibilityAtomicityPaginationAndProtocolIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("TEST-041 exercises an isolated PostgreSQL favorite state machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newTEST041IsolatedDatabase(t, ctx)
	cfg := appconfig.Load()
	server := &Server{db: pool, cfg: cfg}

	ownerID, ownerPublicID, ownerToken := createTEST041User(t, ctx, pool, cfg, "owner")
	createTEST041User(t, ctx, pool, cfg, "target-owner")
	_, _, viewerToken := createTEST041User(t, ctx, pool, cfg, "viewer")
	var viewerID int64
	if err := pool.QueryRow(ctx, `select id from users where username='test041-target-owner'`).Scan(&viewerID); err != nil {
		t.Fatal(err)
	}
	ownerClaims, err := security.ParseToken(cfg.JWTSecret, ownerToken)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.resolveClaimsSubject(ctx, &ownerClaims); err != nil {
		t.Fatal(err)
	}
	if ownerClaims.Subject != ownerID || favoriteClaimsModerator(ownerClaims) {
		t.Fatalf("owner claims subject/moderator=%d/%v want=%d/false rules=%+v", ownerClaims.Subject, favoriteClaimsModerator(ownerClaims), ownerID, ownerClaims.PermissionRules)
	}

	var approvedOneID, approvedTwoID, hiddenID int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('t041mod01','test041-approved-one','TEST-041 approved one','approved',$1) returning id`, ownerID).Scan(&approvedOneID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('t041mod02','test041-approved-two','TEST-041 approved two','approved',$1) returning id`, ownerID).Scan(&approvedTwoID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('t041hid01','test041-hidden','TEST-041 hidden','pending',$1) returning id`, viewerID).Scan(&hiddenID); err != nil {
		t.Fatal(err)
	}
	var hiddenStatus string
	var hiddenSubmitter int64
	if err := pool.QueryRow(ctx, `select review_status,submitted_by from mods where id=$1`, hiddenID).Scan(&hiddenStatus, &hiddenSubmitter); err != nil {
		t.Fatal(err)
	}
	if viewerID == ownerID || hiddenSubmitter != viewerID || hiddenStatus != "pending" {
		t.Fatalf("hidden seed owner/viewer/submitter/status=%d/%d/%d/%s", ownerID, viewerID, hiddenSubmitter, hiddenStatus)
	}
	for publicID, modID := range map[string]int64{"t041mod01": approvedOneID, "t041mod02": approvedTwoID, "t041hid01": hiddenID} {
		var routeInternalID int64
		if err := pool.QueryRow(ctx, `select internal_id from public_routes where entity_type='mod' and public_id=$1`, publicID).Scan(&routeInternalID); err != nil {
			t.Fatal(err)
		}
		if routeInternalID != modID {
			t.Fatalf("route %s internal ID=%d mod ID=%d", publicID, routeInternalID, modID)
		}
	}
	if hiddenTarget, resolveErr := resolveFavoriteTargetWithQueryer(ctx, pool, "mod", "t041hid01", ownerClaims); !errors.Is(resolveErr, pgx.ErrNoRows) {
		t.Fatalf("owner resolved another user's pending target before handler: target=%+v err=%v", hiddenTarget, resolveErr)
	}
	resolvedTwo, err := resolveFavoriteTargetWithQueryer(ctx, pool, "mod", "t041mod02", ownerClaims)
	if err != nil || resolvedTwo.InternalID != approvedTwoID {
		t.Fatalf("second approved target resolution=%+v err=%v", resolvedTwo, err)
	}

	collectionsHandler := server.requireAuth(server.favoriteCollections)
	if response := performTEST041Request(ctx, collectionsHandler, "", http.MethodGet,
		"/api/v1/users/me/favorite-collections?limit=1", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated collection list status=%d body=%s", response.Code, response.Body.String())
	}
	defaultPage := performTEST041Request(ctx, collectionsHandler, ownerToken, http.MethodGet,
		"/api/v1/users/me/favorite-collections?limit=1", "", nil)
	if defaultPage.Code != http.StatusOK {
		t.Fatalf("authenticated collection list status=%d body=%s", defaultPage.Code, defaultPage.Body.String())
	}
	defaultCollections := decodeTEST041Envelope[favoriteCollectionPageResponse](t, defaultPage)
	if len(defaultCollections.Items) != 1 || !defaultCollections.Items[0].IsDefault {
		t.Fatalf("default collection page=%+v", defaultCollections)
	}
	defaultCollectionID := defaultCollections.Items[0].ID
	publicCollection := createTEST041Collection(t, ctx, server, ownerToken, "TEST-041 public", true)
	privateCollection := createTEST041Collection(t, ctx, server, ownerToken, "TEST-041 private", false)
	faultCollection := createTEST041Collection(t, ctx, server, ownerToken, "TEST-041 fault", false)

	firstPage := performTEST041Request(ctx, collectionsHandler, ownerToken, http.MethodGet,
		"/api/v1/users/me/favorite-collections?limit=2", "", nil)
	pageOne := decodeTEST041Envelope[favoriteCollectionPageResponse](t, firstPage)
	if firstPage.Code != http.StatusOK || len(pageOne.Items) != 2 || !pageOne.HasMore || pageOne.NextCursor == "" {
		t.Fatalf("first collection page status=%d page=%+v", firstPage.Code, pageOne)
	}
	secondPage := performTEST041Request(ctx, collectionsHandler, ownerToken, http.MethodGet,
		"/api/v1/users/me/favorite-collections?limit=2&cursor="+url.QueryEscape(pageOne.NextCursor), "", nil)
	pageTwo := decodeTEST041Envelope[favoriteCollectionPageResponse](t, secondPage)
	if secondPage.Code != http.StatusOK || len(pageTwo.Items) != 2 {
		t.Fatalf("second collection page status=%d page=%+v", secondPage.Code, pageTwo)
	}
	seenCollections := map[string]struct{}{}
	for _, collection := range append(pageOne.Items, pageTwo.Items...) {
		if _, duplicate := seenCollections[collection.ID]; duplicate {
			t.Fatalf("duplicate collection across cursor pages: %s", collection.ID)
		}
		seenCollections[collection.ID] = struct{}{}
	}

	replacementHandler := server.requireAuth(server.setFavoriteMembership)
	replacementBody := fmt.Sprintf(`{"entityType":"mod","entityPublicId":"t041mod01","collectionIds":[%q,%q]}`,
		defaultCollectionID, publicCollection.ID)
	if response := performTEST041Request(ctx, replacementHandler, ownerToken, http.MethodPut,
		"/api/v1/users/me/favorites", replacementBody, nil); response.Code != http.StatusOK {
		t.Fatalf("canonical multi-collection replacement status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST041Memberships(t, ctx, pool, ownerID, approvedOneID, []string{defaultCollectionID, publicCollection.ID})

	legacyBody := fmt.Sprintf(`{"entityType":"mod","entityKey":"t041mod01","collectionIds":[%q]}`, privateCollection.ID)
	if response := performTEST041Request(ctx, replacementHandler, ownerToken, http.MethodPut,
		"/api/v1/users/me/favorites", legacyBody, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("legacy entityKey status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST041Memberships(t, ctx, pool, ownerID, approvedOneID, []string{defaultCollectionID, publicCollection.ID})

	invalidCollectionBody := fmt.Sprintf(`{"entityType":"mod","entityPublicId":"t041mod01","collectionIds":[%q,"missing01"]}`,
		privateCollection.ID)
	if response := performTEST041Request(ctx, replacementHandler, ownerToken, http.MethodPut,
		"/api/v1/users/me/favorites", invalidCollectionBody, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid collection replacement status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST041Memberships(t, ctx, pool, ownerID, approvedOneID, []string{defaultCollectionID, publicCollection.ID})

	for name, testCase := range map[string]struct {
		body   string
		status int
	}{
		"hidden target": {
			fmt.Sprintf(`{"entityType":"mod","entityPublicId":"t041hid01","collectionIds":[%q]}`, publicCollection.ID),
			http.StatusNotFound,
		},
		"unsupported target type": {
			fmt.Sprintf(`{"entityType":"plugin","entityPublicId":"t041mod01","collectionIds":[%q]}`, publicCollection.ID),
			http.StatusBadRequest,
		},
	} {
		t.Run(name, func(t *testing.T) {
			response := performTEST041Request(ctx, replacementHandler, ownerToken, http.MethodPut,
				"/api/v1/users/me/favorites", testCase.body, nil)
			if response.Code != testCase.status {
				t.Fatalf("status=%d body=%s want=%d pool acquired/idle/total=%d/%d/%d", response.Code, response.Body.String(), testCase.status,
					pool.Stat().AcquiredConns(), pool.Stat().IdleConns(), pool.Stat().TotalConns())
			}
		})
	}

	var faultCollectionInternalID int64
	if err := pool.QueryRow(ctx, `select id from favorite_collections where public_id=$1`, faultCollection.ID).Scan(&faultCollectionInternalID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `create function test041_reject_favorite_insert() returns trigger language plpgsql as $$
		begin
			if new.collection_id=`+fmt.Sprint(faultCollectionInternalID)+` and new.entity_id=`+fmt.Sprint(approvedOneID)+` then
				raise exception 'TEST041 injected favorite insert failure';
			end if;
			return new;
		end $$;
		create trigger test041_reject_favorite_insert before insert on favorite_collection_items
			for each row execute function test041_reject_favorite_insert()`); err != nil {
		t.Fatal(err)
	}
	faultBody := fmt.Sprintf(`{"entityType":"mod","entityPublicId":"t041mod01","collectionIds":[%q]}`, faultCollection.ID)
	if response := performTEST041Request(ctx, replacementHandler, ownerToken, http.MethodPut,
		"/api/v1/users/me/favorites", faultBody, nil); response.Code != http.StatusInternalServerError {
		t.Fatalf("injected replacement failure status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST041Memberships(t, ctx, pool, ownerID, approvedOneID, []string{defaultCollectionID, publicCollection.ID})
	if _, err := pool.Exec(ctx, `drop trigger test041_reject_favorite_insert on favorite_collection_items;
		drop function test041_reject_favorite_insert()`); err != nil {
		t.Fatal(err)
	}

	patchHandler := server.requireAuth(server.patchFavoriteMembership)
	patchBody := fmt.Sprintf(`{"entityType":"mod","entityPublicId":"t041mod02","addCollectionIds":[%q],"removeCollectionIds":[]}`,
		publicCollection.ID)
	patchResponse := performTEST041Request(ctx, patchHandler, ownerToken, http.MethodPatch,
		"/api/v1/users/me/favorites", patchBody, nil)
	if patchResponse.Code != http.StatusOK {
		response := patchResponse
		t.Fatalf("second target patch status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST041MembershipsWithContext(t, ctx, pool, ownerID, approvedTwoID, []string{publicCollection.ID}, patchResponse.Body.String())
	if _, err := pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id)
		select id,'mod',$2 from favorite_collections where public_id=$1`, publicCollection.ID, hiddenID); err != nil {
		t.Fatal(err)
	}

	publicItemsHandler := server.optionalAuth(server.publicFavoriteCollectionItems)
	itemIDs := map[string]struct{}{}
	cursor := ""
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		path := "/api/v1/users/" + ownerPublicID + "/favorite-collections/" + publicCollection.ID + "/items?limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		response := performTEST041Request(ctx, publicItemsHandler, viewerToken, http.MethodGet, path, "", map[string]string{
			"id": ownerPublicID, "collectionId": publicCollection.ID,
		})
		if response.Code != http.StatusOK {
			t.Fatalf("public item page %d status=%d body=%s", pageNumber+1, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "entityKey") || strings.Contains(response.Body.String(), "t041hid01") {
			t.Fatalf("public item page leaked legacy or hidden data: %s", response.Body.String())
		}
		page := decodeTEST041Envelope[favoriteCollectionItemPageResponse](t, response)
		for _, item := range page.Items {
			if item.EntityPublicID == "" {
				t.Fatalf("item missing canonical entityPublicId: %+v", item)
			}
			if _, duplicate := itemIDs[item.EntityPublicID]; duplicate {
				t.Fatalf("duplicate item across cursor pages: %s", item.EntityPublicID)
			}
			itemIDs[item.EntityPublicID] = struct{}{}
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" {
			t.Fatal("public favorite item page hasMore without nextCursor")
		}
		cursor = page.NextCursor
	}
	if len(itemIDs) != 2 {
		t.Fatalf("public visible item IDs=%v want two approved targets", itemIDs)
	}
	for _, publicID := range []string{"t041mod01", "t041mod02"} {
		if _, ok := itemIDs[publicID]; !ok {
			t.Fatalf("approved target %s missing from public favorite pages: %v", publicID, itemIDs)
		}
	}
}

func createTEST041User(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg appconfig.Config, suffix string) (int64, string, string) {
	t.Helper()
	username := "test041-" + suffix
	var userID, authVersion int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id,public_id,auth_version`, username, username+"@example.test").
		Scan(&userID, &publicID, &authVersion); err != nil {
		t.Fatal(err)
	}
	claims, err := security.NewClaims(publicID, username, username+"@example.test", authVersion, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		values($1,$2,$3,$4)`, security.SessionFingerprint(claims.SessionID), userID, authVersion, time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	token, err := security.SignToken(cfg.JWTSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	return userID, publicID, token
}

func createTEST041Collection(t *testing.T, ctx context.Context, server *Server, token, name string, public bool) favoriteCollectionSummary {
	t.Helper()
	body, err := json.Marshal(map[string]any{"name": name, "isPublic": public})
	if err != nil {
		t.Fatal(err)
	}
	response := performTEST041Request(ctx, server.requireAuth(server.createFavoriteCollection), token,
		http.MethodPost, "/api/v1/users/me/favorite-collections", string(body), nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create collection %q status=%d body=%s", name, response.Code, response.Body.String())
	}
	return decodeTEST041Envelope[favoriteCollectionSummary](t, response)
}

func performTEST041Request(
	ctx context.Context,
	handler http.HandlerFunc,
	token, method, path, body string,
	pathValues map[string]string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range pathValues {
		request.SetPathValue(key, value)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func decodeTEST041Envelope[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response status=%d body=%s: %v", response.Code, response.Body.String(), err)
	}
	return envelope.Data
}

func assertTEST041Memberships(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, entityID int64, want []string) {
	t.Helper()
	assertTEST041MembershipsWithContext(t, ctx, pool, userID, entityID, want, "")
}

func assertTEST041MembershipsWithContext(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, entityID int64, want []string, responseBody string) {
	t.Helper()
	rows, err := pool.Query(ctx, `select collection.public_id from favorite_collection_items item
		join favorite_collections collection on collection.id=item.collection_id
		where collection.user_id=$1 and item.entity_type='mod' and item.entity_id=$2 order by collection.public_id`, userID, entityID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make([]string, 0, len(want))
	for rows.Next() {
		var publicID string
		if err = rows.Scan(&publicID); err != nil {
			t.Fatal(err)
		}
		got = append(got, publicID)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, publicID := range want {
		wantSet[publicID] = struct{}{}
	}
	if len(got) != len(wantSet) {
		var all string
		_ = pool.QueryRow(ctx, `select coalesce(string_agg(collection.user_id::text||':'||route.public_id||':'||collection.public_id,',' order by item.id),'')
			from favorite_collection_items item join favorite_collections collection on collection.id=item.collection_id
			join public_routes route on route.entity_type=item.entity_type and route.internal_id=item.entity_id`).Scan(&all)
		t.Fatalf("favorite memberships=%v want=%v response=%s all=%s", got, want, responseBody, all)
	}
	for _, publicID := range got {
		if _, ok := wantSet[publicID]; !ok {
			t.Fatalf("unexpected favorite membership %s in %v, want=%v", publicID, got, want)
		}
	}
}

func newTEST041IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test041_favorite_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test041_favorite_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST041 database name %q", databaseName)
	}
	quotedName := pgx.Identifier{databaseName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create database "+quotedName); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if !namePattern.MatchString(databaseName) {
			t.Errorf("refuse unsafe TEST041 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST041 database: %v", dropErr)
		}
		adminPool.Close()
	})
	scopedConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.Database = databaseName
	scopedConfig.MaxConns, scopedConfig.MinConns = 8, 1
	pool, err = pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
