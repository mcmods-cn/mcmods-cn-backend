package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEconomyItemOperationsCompleteWithSinglePoolConnectionIntegration(t *testing.T) {
	s, actor := ossUploadTestServer(t, 1024)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unique := "economy_pool_" + randomHex(8)
	var currencyID int64
	if err := s.db.QueryRow(ctx, `insert into currencies(code,name) values($1,'synthetic connection fixture') returning id`, unique).Scan(&currencyID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, query := range []string{
			`delete from shop_purchases where currency_id=$1`,
			`delete from user_inventory where shop_item_id in(select id from shop_items where price_currency_id=$1)`,
			`delete from shop_items where price_currency_id=$1`,
			`delete from currency_transactions where currency_id=$1`,
			`delete from user_currency_balances where currency_id=$1`,
			`delete from currencies where id=$1`,
		} {
			if _, err := s.db.Exec(context.Background(), query, currencyID); err != nil {
				t.Error(err)
			}
		}
	})
	if tag, err := s.db.Exec(ctx, `insert into user_permissions(user_id,permission_id)
		select $1,id from permissions where code='economy.checkin' on conflict do nothing`, actor.Subject); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("synthetic permission grant failed: err=%v", err)
	}
	if _, err := s.db.Exec(ctx, `insert into shop_items(code,item_type,name,price_currency_id,price_amount,purchase_permission,use_permission)
		values($1,'profile_background','synthetic connection fixture',$2,0,'economy.checkin','economy.checkin')`, unique, currencyID); err != nil {
		t.Fatal(err)
	}
	poolConfig := s.db.Config().Copy()
	poolConfig.MaxConns, poolConfig.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	single := &Server{db: pool, cfg: s.cfg}
	response := httptest.NewRecorder()
	r := ossUploadTestRequest(t, map[string]any{"itemCode": unique, "quantity": 1}, actor).WithContext(context.WithValue(ctx, claimsContextKey, actor))
	single.purchaseShopItem(response, r)
	if response.Code != http.StatusOK {
		t.Fatalf("single-connection purchase failed: status=%d body=%s", response.Code, response.Body.String())
	}
	items, err := single.loadShopItems(ctx, false, actor.Subject)
	if err != nil || len(items) == 0 {
		t.Fatalf("single-connection catalog failed: count=%d err=%v", len(items), err)
	}
	response = httptest.NewRecorder()
	r = ossUploadTestRequest(t, map[string]any{"itemCode": unique}, actor).WithContext(context.WithValue(ctx, claimsContextKey, actor))
	single.useShopItem(response, r)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "profile background file is required") {
		t.Fatalf("item use did not reach the actual missing-file validation: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEconomyInvalidConfigurationCannotEnableMintingIntegration(t *testing.T) {
	s, actor := ossUploadTestServer(t, 1024)
	for _, value := range []string{`42`, `null`, `{"checkin":null}`, `{"checkin":{"enabled":null}}`} {
		if _, err := s.db.Exec(context.Background(), `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, economyConfigSettingKey, value); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		s.checkIn(response, ossUploadTestRequest(t, map[string]any{}, actor))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("invalid settings enabled a default reward: status=%d body=%s", response.Code, response.Body.String())
		}
	}
	var count int
	if err := s.db.QueryRow(context.Background(), `select count(*) from user_checkins where user_id=$1`, actor.Subject).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed configuration minted a reward: count=%d err=%v", count, err)
	}
}
