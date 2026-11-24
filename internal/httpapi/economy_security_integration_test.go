package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func openEconomySecurityTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run economy security tests")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestCurrencyBalanceRangeFailureDoesNotWriteLedger(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	suffix := randomCatalogPublicID()
	var userID, currencyID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash)
		values($1,$2,'test-only') returning id`, "sec039_"+suffix, "sec039_"+suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into currencies(code,name) values($1,'SEC-039') returning id`, "sec039_"+suffix).Scan(&currencyID); err != nil {
		t.Fatal(err)
	}
	if balance, changeErr := changeCurrencyBalanceByIDTx(ctx, tx, userID, currencyID, 50,
		"security_test", nil, "security_test", suffix, nil); changeErr != nil || balance != 50 {
		t.Fatalf("valid balance change = %d, %v", balance, changeErr)
	}
	if _, changeErr := changeCurrencyBalanceByIDTx(ctx, tx, userID, currencyID, maxCurrencyBalance,
		"security_test_overflow", nil, "security_test", suffix, nil); !errors.Is(changeErr, errCurrencyOutOfRange) {
		t.Fatalf("out-of-range balance change returned %v", changeErr)
	}
	var balance int64
	var transactionCount int
	if err = tx.QueryRow(ctx, `select balance from user_currency_balances where user_id=$1 and currency_id=$2`, userID, currencyID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*) from currency_transactions where user_id=$1 and currency_id=$2`, userID, currencyID).Scan(&transactionCount); err != nil {
		t.Fatal(err)
	}
	if balance != 50 || transactionCount != 1 {
		t.Fatalf("failed range check changed durable facts: balance=%d transactions=%d", balance, transactionCount)
	}
}

func TestEconomyCurrencyReferencesRequireActiveCurrencies(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, statement := range []string{
		`create temp table currencies(id bigint primary key,code text not null unique,status text not null) on commit drop`,
		`create temp table system_settings(key text primary key,value jsonb not null) on commit drop`,
		`create temp table shop_items(id bigint primary key,price_currency_id bigint not null,status text not null) on commit drop`,
		`insert into currencies values(1,'gold','active'),(2,'diamond','disabled')`,
		`insert into system_settings values('economy.config',
			'{"checkin":{"enabled":true,"currency":"gold","amount":1,"minimumHours":20},"downloadRewards":[{"objectType":"mod","currency":"diamond","downloadsPerReward":10,"amount":1}]}'::jsonb)`,
		`insert into shop_items values(1,1,'active')`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	payload := economyConfigPayload{
		Checkin: economyCheckinConfig{Enabled: true, Currency: "gold", Amount: 1, MinimumHours: 20},
		DownloadRewards: []economyDownloadReward{{
			ObjectType: "mod", Currency: "diamond", DownloadsPerReward: 10, Amount: 1,
		}},
	}
	if err = validateActiveEconomyCurrencyReferencesTx(ctx, tx, payload); !errors.Is(err, errInactiveEconomyCurrency) {
		t.Fatalf("disabled reward currency returned %v", err)
	}
	if _, err = tx.Exec(ctx, `update currencies set status='active' where code='diamond'`); err != nil {
		t.Fatal(err)
	}
	if err = validateActiveEconomyCurrencyReferencesTx(ctx, tx, payload); err != nil {
		t.Fatalf("active reward currencies were rejected: %v", err)
	}

	blockers, err := economyCurrencyReferenceBlockersTx(ctx, tx, "gold")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"enabled check-in", "active shop items"} {
		if !hasExactString(blockers, expected) {
			t.Fatalf("currency blockers %#v are missing %q", blockers, expected)
		}
	}
}

func TestHeatPromotionSequenceUsesAtomicCounter(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `create temp table content_heat_promotion_counters(
		object_route_id bigint primary key,last_sequence_no bigint not null check(last_sequence_no>0)) on commit drop`); err != nil {
		t.Fatal(err)
	}
	for expected := int64(1); expected <= 3; expected++ {
		sequence, nextErr := nextHeatPromotionSequenceTx(ctx, tx, 42)
		if nextErr != nil || sequence != expected {
			t.Fatalf("sequence call %d returned %d, %v", expected, sequence, nextErr)
		}
	}
}

func TestIndependentEconomyRewardsSharePolicyAndExcludeConfigurationWritesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "economy.checkin")
	server := &Server{db: f.db}
	download, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer download.Rollback(f.ctx)
	if err = server.recordOwnedContentDownloadTx(f.ctx, download, "mod", f.modCode, f.userIDs[f.editor], f.userIDs[f.otherEditor]); err != nil {
		t.Fatal(err)
	}

	// Probe without waiting, before calling a second ordinary reward endpoint.
	var shared, exclusive bool
	probe, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Rollback(f.ctx)
	if err = probe.QueryRow(f.ctx, `select pg_try_advisory_xact_lock_shared(hashtext('mcmods-cn-economy-configuration'))`).Scan(&shared); err != nil {
		t.Fatal(err)
	}
	if err = probe.QueryRow(f.ctx, `select pg_try_advisory_xact_lock(hashtext('mcmods-cn-economy-configuration'))`).Scan(&exclusive); err != nil {
		t.Fatal(err)
	}
	if !shared || exclusive {
		t.Fatalf("overlapping policy access: reader=%v writer=%v", shared, exclusive)
	}
	if err = probe.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}

	f.require(t, f.reviewer, http.MethodPost, "/api/v1/users/me/economy/checkin", nil, http.StatusOK)
	if err = download.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}

	writer, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(f.ctx)
	if err = lockEconomyConfigurationTx(f.ctx, writer); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(f.ctx, `select pg_try_advisory_xact_lock_shared(hashtext('mcmods-cn-economy-configuration'))`).Scan(&shared); err != nil || shared {
		t.Fatalf("configuration writer admitted a concurrent reward: shared=%v err=%v", shared, err)
	}
}

func TestActiveRewardTasksKeepTheirCurrenciesAvailableIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "economy.write", "task.write")
	const currencies = "/api/v1/admin/economy/currencies"
	const tasks = "/api/v1/admin/tasks"
	create := func(path string, payload map[string]any) string {
		t.Helper()
		raw := f.require(t, f.editor, http.MethodPost, path, payload, http.StatusCreated)
		var result struct {
			Data struct {
				PublicID string `json:"publicId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &result); err != nil || !validCatalogPublicID(result.Data.PublicID) {
			t.Fatalf("invalid receipt: %s %v", raw, err)
		}
		return path + "/" + result.Data.PublicID
	}
	currency := map[string]any{"code": "task_coin", "name": "Reward currency", "translations": map[string]any{}}
	currencyPath := create(currencies, currency)
	task := map[string]any{"code": "collect_coins", "name": "Collect coins", "refreshPeriod": "daily", "translations": map[string]any{},
		"condition": map[string]any{"action": "create", "objectType": "user", "metric": "count", "target": 1},
		"rewards":   map[string]any{"currencies": map[string]any{"task_coin": 1}}}
	taskPath := create(tasks, task)
	currency["status"] = "disabled"
	f.require(t, f.editor, http.MethodPut, currencyPath, currency, http.StatusConflict)
	currency["status"] = "active"
	currency["code"] = "changed_coin"
	f.require(t, f.editor, http.MethodPut, currencyPath, currency, http.StatusConflict)
	task["status"] = "disabled"
	f.require(t, f.editor, http.MethodPut, taskPath, task, http.StatusOK)
	currency["code"] = "task_coin"
	currency["status"] = "disabled"
	f.require(t, f.editor, http.MethodPut, currencyPath, currency, http.StatusOK)
	task["name"] = "Retired reward"
	f.require(t, f.editor, http.MethodPut, taskPath, task, http.StatusOK)
	task["status"] = "active"
	f.require(t, f.editor, http.MethodPut, taskPath, task, http.StatusBadRequest)
	var enabled bool
	if err := f.db.QueryRow(f.ctx, `select status='active' from task_definitions where public_id=$1`, strings.TrimPrefix(taskPath, tasks+"/")).Scan(&enabled); err != nil || enabled {
		t.Fatalf("rejected enable changed task: enabled=%v err=%v", enabled, err)
	}
}
