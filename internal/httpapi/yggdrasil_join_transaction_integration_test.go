package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestYggdrasilJoinCommitsSessionAndTokenUsageAtomicallyIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify Yggdrasil join transactionality")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
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

	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	loaded.Yggdrasil.JoinTTL = time.Minute
	server := &Server{cfg: loaded, db: pool, cache: cache}

	if _, err = pool.Exec(ctx, `insert into permissions(code,module,name,description)
		values('skin.launcher.login','skin','Use launcher login','integration fixture')`); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		username, accessToken, profileUUID string
		userID, profileID, tokenID         int64
	}{
		{username: "JoinOne", accessToken: "join-transaction-token-one", profileUUID: "10000000-0000-0000-0000-000000000001"},
		{username: "JoinTwo", accessToken: "join-transaction-token-two", profileUUID: "20000000-0000-0000-0000-000000000002"},
	}
	for index := range fixtures {
		fixture := &fixtures[index]
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
			values($1,$2,'test-only',true,'active') returning id`, fixture.username,
			fixture.username+"@join.test").Scan(&fixture.userID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into user_permissions(user_id,permission_id,allow)
			select $1,id,true from permissions where code='skin.launcher.login'`, fixture.userID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into yggdrasil_accounts(user_id,account_uuid,enabled)
			values($1,$2,true)`, fixture.userID, fixture.profileUUID); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `insert into player_profiles(user_id,uuid,name,status)
			values($1,$2,$3,'active') returning id`, fixture.userID, fixture.profileUUID,
			fixture.username).Scan(&fixture.profileID); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `insert into yggdrasil_tokens
			(access_token_hash,user_id,player_profile_id,client_token,status,expires_at)
			values($1,$2,$3,$4,'active',now()+interval '1 hour') returning id`,
			hashYggdrasilToken(fixture.accessToken), fixture.userID, fixture.profileID,
			"client-token-"+fixture.username).Scan(&fixture.tokenID); err != nil {
			t.Fatal(err)
		}
	}

	if _, err = pool.Exec(ctx, `create function pg_temp.reject_yggdrasil_last_used_update() returns trigger as $$
		begin
			raise exception 'injected last_used_at failure';
		end;
		$$ language plpgsql`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create trigger trg_reject_yggdrasil_last_used_update
		before update of last_used_at on yggdrasil_tokens
		for each row execute function pg_temp.reject_yggdrasil_last_used_update()`); err != nil {
		t.Fatal(err)
	}

	failureServerID := "join-rollback-server"
	failure := invokeYggdrasilJoin(server, fixtures[0], failureServerID)
	if failure.Code != http.StatusServiceUnavailable {
		t.Fatalf("fault-injected join status=%d want=%d body=%s", failure.Code, http.StatusServiceUnavailable, failure.Body.String())
	}
	var sessionCount int
	var lastUsedSet bool
	if err = pool.QueryRow(ctx, `select count(*)::int from yggdrasil_join_sessions where server_id=$1`, failureServerID).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select last_used_at is not null from yggdrasil_tokens where id=$1`, fixtures[0].tokenID).Scan(&lastUsedSet); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 || lastUsedSet {
		t.Fatalf("failed join committed partial state: sessions=%d lastUsedSet=%t", sessionCount, lastUsedSet)
	}
	hasJoinedURL := "/api/yggdrasil/sessionserver/session/minecraft/hasJoined?" + url.Values{
		"username": {fixtures[0].username}, "serverId": {failureServerID},
	}.Encode()
	hasJoined := httptest.NewRecorder()
	server.yggdrasilHasJoined(hasJoined, httptest.NewRequest(http.MethodGet, hasJoinedURL, nil).WithContext(ctx))
	if hasJoined.Code != http.StatusNoContent {
		t.Fatalf("hasJoined observed failed join: status=%d body=%s", hasJoined.Code, hasJoined.Body.String())
	}

	if _, err = pool.Exec(ctx, `drop trigger trg_reject_yggdrasil_last_used_update on yggdrasil_tokens`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `drop function pg_temp.reject_yggdrasil_last_used_update()`); err != nil {
		t.Fatal(err)
	}

	successServerID := "join-commit-server"
	success := invokeYggdrasilJoin(server, fixtures[0], successServerID)
	if success.Code != http.StatusNoContent {
		t.Fatalf("successful join status=%d want=%d body=%s", success.Code, http.StatusNoContent, success.Body.String())
	}
	var committedTokenID int64
	if err = pool.QueryRow(ctx, `select token_id from yggdrasil_join_sessions where server_id=$1`, successServerID).Scan(&committedTokenID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select last_used_at is not null from yggdrasil_tokens where id=$1`, fixtures[0].tokenID).Scan(&lastUsedSet); err != nil {
		t.Fatal(err)
	}
	if committedTokenID != fixtures[0].tokenID || !lastUsedSet {
		t.Fatalf("successful join facts token=%d want=%d lastUsedSet=%t", committedTokenID, fixtures[0].tokenID, lastUsedSet)
	}

	if _, err = pool.Exec(ctx, `update yggdrasil_tokens set last_used_at=null where id=any($1::bigint[])`,
		[]int64{fixtures[0].tokenID, fixtures[1].tokenID}); err != nil {
		t.Fatal(err)
	}
	concurrentServerID := "join-concurrent-server"
	start := make(chan struct{})
	codes := make([]int, len(fixtures))
	var wait sync.WaitGroup
	for index := range fixtures {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			codes[index] = invokeYggdrasilJoin(server, fixtures[index], concurrentServerID).Code
		}()
	}
	close(start)
	wait.Wait()
	sort.Ints(codes)
	if codes[0] != http.StatusNoContent || codes[1] != http.StatusForbidden {
		t.Fatalf("concurrent join statuses=%v want=[%d %d]", codes, http.StatusNoContent, http.StatusForbidden)
	}
	if err = pool.QueryRow(ctx, `select token_id from yggdrasil_join_sessions where server_id=$1`, concurrentServerID).Scan(&committedTokenID); err != nil {
		t.Fatal(err)
	}
	lastUsedByToken := make(map[int64]bool, len(fixtures))
	for _, fixture := range fixtures {
		if err = pool.QueryRow(ctx, `select last_used_at is not null from yggdrasil_tokens where id=$1`, fixture.tokenID).
			Scan(&lastUsedSet); err != nil {
			t.Fatal(err)
		}
		lastUsedByToken[fixture.tokenID] = lastUsedSet
	}
	if !lastUsedByToken[committedTokenID] || lastUsedByToken[fixtures[0].tokenID] == lastUsedByToken[fixtures[1].tokenID] {
		t.Fatalf("concurrent winner=%d lastUsed=%v", committedTokenID, lastUsedByToken)
	}
}

func invokeYggdrasilJoin(server *Server, fixture struct {
	username, accessToken, profileUUID string
	userID, profileID, tokenID         int64
}, serverID string) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(yggdrasilJoinRequest{
		AccessToken: fixture.accessToken, SelectedProfile: fixture.profileUUID, ServerID: serverID,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/yggdrasil/sessionserver/session/minecraft/join", bytes.NewReader(payload))
	request.RemoteAddr = "198.51.100.24:25565"
	response := httptest.NewRecorder()
	server.yggdrasilJoin(response, request)
	return response
}
