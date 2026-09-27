package httpapi

import (
	"context"
	"errors"
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
