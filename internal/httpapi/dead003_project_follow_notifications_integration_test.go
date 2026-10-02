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
	"mcmods-cn-backend/internal/security"
)

func TestDEAD003AndDEAD012ActivePreferenceAndCacheContractsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project follow notification preferences")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	if _, err = pool.Exec(ctx, `
		insert into users(id,public_id,username,email,password_hash) values
			(99166001,'udead003a','dead003-owner','dead003-owner@example.test','x'),
			(99166002,'udead003b','dead003-other','dead003-other@example.test','x');
		insert into mods(id,project_code,slug,primary_name,review_status,submitted_by)
		values(99166003,'d003m0001','dead003-project','DEAD003 project','approved',99166001)
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	var stickerVersionStateExists bool
	if err = pool.QueryRow(ctx, `select to_regclass('sticker_catalog_state') is not null`).Scan(&stickerVersionStateExists); err != nil {
		t.Fatal(err)
	}
	if stickerVersionStateExists {
		t.Fatal("generation 165 still installs sticker_catalog_state")
	}
	catalogResponse := httptest.NewRecorder()
	server.publicStickerCatalog(catalogResponse, httptest.NewRequest(http.MethodGet, "/api/v1/stickers?locale=en-US", nil).WithContext(ctx))
	if catalogResponse.Code != http.StatusOK {
		t.Fatalf("public sticker catalog status=%d body=%s", catalogResponse.Code, catalogResponse.Body.String())
	}
	var catalogEnvelope struct {
		Data map[string]any `json:"data"`
	}
	if err = json.Unmarshal(catalogResponse.Body.Bytes(), &catalogEnvelope); err != nil {
		t.Fatal(err)
	}
	if _, hasVersion := catalogEnvelope.Data["version"]; hasVersion || catalogEnvelope.Data["locale"] != "en-US" {
		t.Fatalf("public sticker catalog retained dead version contract: %s", catalogResponse.Body.String())
	}
	owner := security.Claims{Subject: 99166001}
	other := security.Claims{Subject: 99166002}

	followed := invokeDEAD003ProjectFollow(t, ctx, server.followProject, http.MethodPut, "d003m0001", owner, nil)
	assertDEAD003PreferenceResponse(t, followed, http.StatusOK, true)
	disabled := invokeDEAD003ProjectFollow(t, ctx, server.updateProjectFollowNotifications, http.MethodPatch, "d003m0001", owner,
		[]byte(`{"notificationsEnabled":false}`))
	assertDEAD003PreferenceResponse(t, disabled, http.StatusOK, false)

	duplicateFollow := invokeDEAD003ProjectFollow(t, ctx, server.followProject, http.MethodPut, "d003m0001", owner, nil)
	assertDEAD003PreferenceResponse(t, duplicateFollow, http.StatusOK, false)
	assertDEAD003StoredPreference(t, ctx, pool, false)

	for _, invalidBody := range [][]byte{
		[]byte(`{}`),
		[]byte(`{"notificationsEnabled":"false"}`),
		[]byte(`{"notificationsEnabled":true,"extra":1}`),
	} {
		response := invokeDEAD003ProjectFollow(t, ctx, server.updateProjectFollowNotifications, http.MethodPatch, "d003m0001", owner, invalidBody)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid preference body=%s status=%d response=%s", invalidBody, response.Code, response.Body.String())
		}
	}
	assertDEAD003StoredPreference(t, ctx, pool, false)

	notOwner := invokeDEAD003ProjectFollow(t, ctx, server.updateProjectFollowNotifications, http.MethodPatch, "d003m0001", other,
		[]byte(`{"notificationsEnabled":true}`))
	if notOwner.Code != http.StatusNotFound {
		t.Fatalf("non-owner preference update status=%d body=%s", notOwner.Code, notOwner.Body.String())
	}
	assertDEAD003StoredPreference(t, ctx, pool, false)

	if _, err = pool.Exec(ctx, `update mods set review_status='pending' where id=99166003`); err != nil {
		t.Fatal(err)
	}
	enabledWhileHidden := invokeDEAD003ProjectFollow(t, ctx, server.updateProjectFollowNotifications, http.MethodPatch, "d003m0001", owner,
		[]byte(`{"notificationsEnabled":true}`))
	assertDEAD003PreferenceResponse(t, enabledWhileHidden, http.StatusOK, true)
	assertDEAD003StoredPreference(t, ctx, pool, true)

	status := invokeDEAD003ProjectFollow(t, ctx, server.projectFollowStatus, http.MethodGet, "d003m0001", owner, nil)
	assertDEAD003PreferenceResponse(t, status, http.StatusOK, true)
}

func invokeDEAD003ProjectFollow(
	t *testing.T,
	ctx context.Context,
	handler http.HandlerFunc,
	method, publicID string,
	claims security.Claims,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/projects/"+publicID+"/follow", bytes.NewReader(body))
	request.SetPathValue("publicId", publicID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func assertDEAD003PreferenceResponse(t *testing.T, response *httptest.ResponseRecorder, status int, enabled bool) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("preference response status=%d body=%s, want %d", response.Code, response.Body.String(), status)
	}
	var envelope struct {
		Data struct {
			Followed             bool `json:"followed"`
			NotificationsEnabled bool `json:"notificationsEnabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.Followed || envelope.Data.NotificationsEnabled != enabled {
		t.Fatalf("preference response=%s, want followed=true notificationsEnabled=%v", response.Body.String(), enabled)
	}
}

func assertDEAD003StoredPreference(t *testing.T, ctx context.Context, pool *pgxpool.Pool, enabled bool) {
	t.Helper()
	var stored bool
	if err := pool.QueryRow(ctx, `select notifications_enabled from project_follows where user_id=99166001`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != enabled {
		t.Fatalf("stored notifications_enabled=%v, want %v", stored, enabled)
	}
}
