package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestTEST044EconomyPermissionsConcurrencyInventoryAndLevelRevocationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("TEST-044 exercises an isolated PostgreSQL economy and permission state machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newTEST044IsolatedDatabase(t, ctx)
	cfg := appconfig.Load()
	server := &Server{db: pool, cfg: cfg}

	senderID, _, senderToken := createTEST044User(t, ctx, pool, cfg, "sender", "Asia/Shanghai")
	recipientID, recipientPublicID, _ := createTEST044User(t, ctx, pool, cfg, "recipient", "UTC")
	adminID, _, adminToken := createTEST044User(t, ctx, pool, cfg, "admin", "UTC")

	transferHandler := server.requirePermission("economy.transfer", server.transferCurrency)
	transferBody := fmt.Sprintf(`{"recipient":%q,"currency":"diamond","amount":500}`, recipientPublicID)
	if response := performTEST044Request(ctx, transferHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/economy/transfer", transferBody, nil); response.Code != http.StatusForbidden {
		t.Fatalf("transfer without permission status=%d body=%s", response.Code, response.Body.String())
	}
	grantTEST044Permissions(t, ctx, pool, senderID,
		"economy.checkin", "economy.transfer", "shop.purchase", "shop.use", "project.review")

	var diamondID int64
	if err := pool.QueryRow(ctx, `update currencies set transfer_tax_bps=1000 where code='diamond' returning id`).Scan(&diamondID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into user_currency_balances(user_id,currency_id,balance) values
		($1,$3,2000),($2,$3,100)`, senderID, recipientID, diamondID); err != nil {
		t.Fatal(err)
	}
	if response := performTEST044Request(ctx, transferHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/economy/transfer", transferBody, nil); response.Code != http.StatusOK {
		t.Fatalf("valid transfer status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST044Balances(t, ctx, pool, diamondID, map[int64]int64{senderID: 1500, recipientID: 550})
	assertTEST044TransferLedger(t, ctx, pool, diamondID, 2)

	var transferWait sync.WaitGroup
	startTransfers := make(chan struct{})
	transferStatuses := make(chan int, 2)
	concurrentBody := fmt.Sprintf(`{"recipient":%q,"currency":"diamond","amount":800}`, recipientPublicID)
	for index := 0; index < 2; index++ {
		transferWait.Add(1)
		go func() {
			defer transferWait.Done()
			<-startTransfers
			response := performTEST044Request(ctx, transferHandler, senderToken, http.MethodPost,
				"/api/v1/users/me/economy/transfer", concurrentBody, nil)
			transferStatuses <- response.Code
		}()
	}
	close(startTransfers)
	transferWait.Wait()
	close(transferStatuses)
	statuses := make([]int, 0, 2)
	for status := range transferStatuses {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)
	if len(statuses) != 2 || statuses[0] != http.StatusOK || statuses[1] != http.StatusConflict {
		t.Fatalf("concurrent transfer statuses=%v want=[200 409]", statuses)
	}
	assertTEST044Balances(t, ctx, pool, diamondID, map[int64]int64{senderID: 700, recipientID: 1270})
	assertTEST044TransferLedger(t, ctx, pool, diamondID, 4)

	if response := performTEST044Request(ctx, transferHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/economy/transfer",
		fmt.Sprintf(`{"recipient":%q,"currency":"diamond","amount":%d}`, recipientPublicID, maxCurrencyAmount+1), nil); response.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range transfer status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST044Balances(t, ctx, pool, diamondID, map[int64]int64{senderID: 700, recipientID: 1270})

	if _, err := pool.Exec(ctx, `insert into system_settings(key,value,updated_by) values(
		'economy.config',
		'{"checkin":{"enabled":true,"currency":"gold_nugget","amount":25,"minimumHours":20},"downloadRewards":[]}'::jsonb,
		$1) on conflict(key) do update set value=excluded.value,updated_by=excluded.updated_by`, adminID); err != nil {
		t.Fatal(err)
	}
	checkinHandler := server.requirePermission("economy.checkin", server.checkIn)
	if response := performTEST044Request(ctx, checkinHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/economy/checkin", `{}`, nil); response.Code != http.StatusOK {
		t.Fatalf("first check-in status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performTEST044Request(ctx, checkinHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/economy/checkin", `{}`, nil); response.Code != http.StatusConflict {
		t.Fatalf("duplicate check-in status=%d body=%s", response.Code, response.Body.String())
	}
	var checkins, goldTransactions int
	var goldBalance int64
	if err := pool.QueryRow(ctx, `select count(*) from user_checkins where user_id=$1`, senderID).Scan(&checkins); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select coalesce(balance.balance,0),count(transaction.id)
		from currencies currency left join user_currency_balances balance
			on balance.currency_id=currency.id and balance.user_id=$1
		left join currency_transactions transaction
			on transaction.currency_id=currency.id and transaction.user_id=$1
		where currency.code='gold_nugget' group by balance.balance`, senderID).Scan(&goldBalance, &goldTransactions); err != nil {
		t.Fatal(err)
	}
	if checkins != 1 || goldBalance != 25 || goldTransactions != 1 {
		t.Fatalf("check-in facts count=%d balance=%d ledger=%d", checkins, goldBalance, goldTransactions)
	}

	purchaseHandler := server.requirePermission("shop.purchase", server.purchaseShopItem)
	if response := performTEST044Request(ctx, purchaseHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/shop/purchase", `{"itemCode":"project_heat_boost","quantity":2}`, nil); response.Code != http.StatusForbidden {
		t.Fatalf("item-specific purchase permission status=%d body=%s", response.Code, response.Body.String())
	}
	grantTEST044Permissions(t, ctx, pool, senderID,
		"shop.project_heat_boost.purchase", "shop.project_heat_boost.use")
	if response := performTEST044Request(ctx, purchaseHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/shop/purchase", `{"itemCode":"project_heat_boost","quantity":2}`, nil); response.Code != http.StatusOK {
		t.Fatalf("purchase status=%d body=%s", response.Code, response.Body.String())
	}
	var heatItemID int64
	if err := pool.QueryRow(ctx, `select id from shop_items where code='project_heat_boost'`).Scan(&heatItemID); err != nil {
		t.Fatal(err)
	}
	assertTEST044Inventory(t, ctx, pool, senderID, heatItemID, 2)
	assertTEST044Balances(t, ctx, pool, diamondID, map[int64]int64{senderID: 690, recipientID: 1270})

	if _, err := pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('t044mod01','test044-mod','TEST-044 mod','approved',$1)`, senderID); err != nil {
		t.Fatal(err)
	}
	var routeID int64
	if err := pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and public_id='t044mod01'`).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	useHandler := server.requirePermission("shop.use", server.useShopItem)
	useBody := `{"itemCode":"project_heat_boost","targetType":"mod","targetId":"t044mod01"}`
	var useWait sync.WaitGroup
	startUses := make(chan struct{})
	useStatuses := make(chan int, 2)
	for index := 0; index < 2; index++ {
		useWait.Add(1)
		go func() {
			defer useWait.Done()
			<-startUses
			response := performTEST044Request(ctx, useHandler, senderToken, http.MethodPost,
				"/api/v1/users/me/shop/use", useBody, nil)
			useStatuses <- response.Code
		}()
	}
	close(startUses)
	useWait.Wait()
	close(useStatuses)
	for status := range useStatuses {
		if status != http.StatusOK {
			t.Fatalf("concurrent heat use status=%d want=200", status)
		}
	}
	assertTEST044Inventory(t, ctx, pool, senderID, heatItemID, 0)
	var counter int64
	var sequences []int64
	var powers []float64
	if err := pool.QueryRow(ctx, `select last_sequence_no from content_heat_promotion_counters where object_route_id=$1`, routeID).Scan(&counter); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `select sequence_no,effective_power from content_heat_promotions
		where object_route_id=$1 order by sequence_no`, routeID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var sequence int64
		var power float64
		if err = rows.Scan(&sequence, &power); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		sequences = append(sequences, sequence)
		powers = append(powers, power)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if counter != 2 || len(sequences) != 2 || sequences[0] != 1 || sequences[1] != 2 || !(powers[0] > powers[1]) {
		t.Fatalf("heat facts counter=%d sequences=%v powers=%v", counter, sequences, powers)
	}
	if response := performTEST044Request(ctx, useHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/shop/use", useBody, nil); response.Code != http.StatusConflict {
		t.Fatalf("empty inventory use status=%d body=%s", response.Code, response.Body.String())
	}

	adjustHandler := server.requirePermission("economy.balance.write", server.adjustAdminUserBalance)
	pathValues := map[string]string{"id": recipientPublicID}
	adjustBody := `{"currencyCode":"diamond","mode":"adjust","amount":30,"reason":"TEST-044 adjustment"}`
	if response := performTEST044Request(ctx, adjustHandler, adminToken, http.MethodPost,
		"/api/v1/admin/users/recipient/balances", adjustBody, pathValues); response.Code != http.StatusForbidden {
		t.Fatalf("admin adjustment without permission status=%d body=%s", response.Code, response.Body.String())
	}
	grantTEST044Permissions(t, ctx, pool, adminID, "economy.balance.write", "permission.write")
	if response := performTEST044Request(ctx, adjustHandler, adminToken, http.MethodPost,
		"/api/v1/admin/users/recipient/balances", adjustBody, pathValues); response.Code != http.StatusOK {
		t.Fatalf("admin adjustment status=%d body=%s", response.Code, response.Body.String())
	}
	assertTEST044Balances(t, ctx, pool, diamondID, map[int64]int64{senderID: 690, recipientID: 1300})
	var adminLedger int
	if err := pool.QueryRow(ctx, `select count(*) from currency_transactions
		where user_id=$1 and currency_id=$2 and transaction_type='admin_adjustment'
			and metadata->>'reason'='TEST-044 adjustment'`, recipientID, diamondID).Scan(&adminLedger); err != nil {
		t.Fatal(err)
	}
	if adminLedger != 1 {
		t.Fatalf("admin adjustment ledger=%d want=1", adminLedger)
	}

	var lowRoleID, highRoleID int64
	if err := pool.QueryRow(ctx, `insert into roles(code,name,weight)
		values('test044_level_low','TEST-044 low',10) returning id`).Scan(&lowRoleID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into roles(code,name,weight)
		values('test044_level_high','TEST-044 high',20) returning id`).Scan(&highRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into permission_role_tracks(code,name)
		values('test044_track','TEST-044 track')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into permission_role_track_roles(track_code,role_id,position) values
		('test044_track',$1,0),('test044_track',$2,1)`, lowRoleID, highRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_experience(user_id,experience,level) values($1,150,0),($2,10,0)`,
		senderID, recipientID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_role_bindings(user_id,role_id,source,source_key)
		values($1,$2,'manual','')`, senderID, lowRoleID); err != nil {
		t.Fatal(err)
	}
	var authBefore, permissionBefore int64
	if err = pool.QueryRow(ctx, `select auth_version,permission_version from users where id=$1`, senderID).
		Scan(&authBefore, &permissionBefore); err != nil {
		t.Fatal(err)
	}
	levelHandler := server.requirePermission("permission.write", server.updateLevelConfig)
	if response := performTEST044Request(ctx, levelHandler, adminToken, http.MethodPut,
		"/api/v1/admin/levels/config", `{"roleTrackCode":"test044_track","levelThresholds":[100,1000]}`, nil); response.Code != http.StatusOK {
		t.Fatalf("enable level track status=%d body=%s", response.Code, response.Body.String())
	}
	runTEST044LevelRecalculation(t, ctx, pool)
	assertTEST044RoleSources(t, ctx, pool, senderID, lowRoleID, 1, 1)
	var senderLevel int
	if err = pool.QueryRow(ctx, `select level from user_experience where user_id=$1`, senderID).Scan(&senderLevel); err != nil {
		t.Fatal(err)
	}
	if senderLevel != 1 {
		t.Fatalf("sender level=%d want=1", senderLevel)
	}
	if response := performTEST044Request(ctx, levelHandler, adminToken, http.MethodPut,
		"/api/v1/admin/levels/config", `{"roleTrackCode":"","levelThresholds":[]}`, nil); response.Code != http.StatusOK {
		t.Fatalf("clear level track status=%d body=%s", response.Code, response.Body.String())
	}
	runTEST044LevelRecalculation(t, ctx, pool)
	assertTEST044RoleSources(t, ctx, pool, senderID, lowRoleID, 1, 0)
	var authAfter, permissionAfter int64
	if err = pool.QueryRow(ctx, `select auth_version,permission_version from users where id=$1`, senderID).
		Scan(&authAfter, &permissionAfter); err != nil {
		t.Fatal(err)
	}
	if authAfter != authBefore || permissionAfter <= permissionBefore {
		t.Fatalf("version changes auth=%d->%d permission=%d->%d", authBefore, authAfter, permissionBefore, permissionAfter)
	}
	if response := performTEST044Request(ctx, transferHandler, senderToken, http.MethodPost,
		"/api/v1/users/me/economy/transfer",
		fmt.Sprintf(`{"recipient":%q,"currency":"diamond","amount":10}`, recipientPublicID), nil); response.Code != http.StatusOK {
		t.Fatalf("session after role recalculation status=%d body=%s", response.Code, response.Body.String())
	}
}

func newTEST044IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test044_economy_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test044_economy_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST044 database name %q", databaseName)
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
			t.Errorf("refuse unsafe TEST044 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST044 database: %v", dropErr)
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

func createTEST044User(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	cfg appconfig.Config,
	suffix, timezone string,
) (int64, string, string) {
	t.Helper()
	username := "test044-" + suffix
	var userID, authVersion int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,status,timezone)
		values($1,$2,'not-used','active',$3) returning id,public_id,auth_version`,
		username, username+"@example.test", timezone).Scan(&userID, &publicID, &authVersion); err != nil {
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

func grantTEST044Permissions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, codes ...string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `insert into permissions(code,module,name)
		select code,'test044',code from unnest($1::text[]) code on conflict(code) do nothing`, codes); err != nil {
		t.Fatal(err)
	}
	command, err := pool.Exec(ctx, `insert into user_permissions(user_id,permission_id,allow,source,source_key)
		select $1,id,true,'manual','' from permissions where code=any($2::text[])
		on conflict(user_id,permission_id,source,source_key) do update set allow=true`, userID, codes)
	if err != nil {
		t.Fatal(err)
	}
	if command.RowsAffected() != int64(len(codes)) {
		t.Fatalf("granted %d permissions, want %d for %v", command.RowsAffected(), len(codes), codes)
	}
}

func performTEST044Request(
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

func assertTEST044Balances(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	currencyID int64,
	want map[int64]int64,
) {
	t.Helper()
	for userID, expected := range want {
		var balance int64
		if err := pool.QueryRow(ctx, `select balance from user_currency_balances
			where user_id=$1 and currency_id=$2`, userID, currencyID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if balance != expected {
			t.Fatalf("user %d balance=%d want=%d", userID, balance, expected)
		}
	}
}

func assertTEST044TransferLedger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, currencyID int64, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from currency_transactions
		where currency_id=$1 and transaction_type in ('transfer_in','transfer_out')`, currencyID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer ledger rows=%d want=%d", count, want)
	}
}

func assertTEST044Inventory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, itemID int64, want int) {
	t.Helper()
	var quantity int
	if err := pool.QueryRow(ctx, `select quantity from user_inventory where user_id=$1 and shop_item_id=$2`, userID, itemID).
		Scan(&quantity); err != nil {
		t.Fatal(err)
	}
	if quantity != want {
		t.Fatalf("inventory quantity=%d want=%d", quantity, want)
	}
}

func runTEST044LevelRecalculation(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	worker := NewLevelRecalculationWorker(pool)
	job, claimed, err := worker.claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim level recalculation: claimed=%t err=%v", claimed, err)
	}
	for {
		result, processErr := worker.processBatch(ctx, job)
		if processErr != nil {
			t.Fatal(processErr)
		}
		if result.done {
			return
		}
	}
}

func assertTEST044RoleSources(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, roleID int64,
	wantManual, wantTrack int,
) {
	t.Helper()
	var manual, track int
	if err := pool.QueryRow(ctx, `select
		count(*) filter(where source='manual' and source_key=''),
		count(*) filter(where source='level_track' and source_key='test044_track')
		from user_role_bindings where user_id=$1 and role_id=$2`, userID, roleID).Scan(&manual, &track); err != nil {
		t.Fatal(err)
	}
	if manual != wantManual || track != wantTrack {
		t.Fatalf("role sources manual=%d track=%d want=%d/%d", manual, track, wantManual, wantTrack)
	}
}
