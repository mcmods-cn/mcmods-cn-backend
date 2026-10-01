package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestCurrencyTransferTaxAndPurchaseAmountsDoNotOverflowIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	unique := "audit_amount_" + randomHex(8)
	var senderID, receiverID, currencyID int64
	var receiverPublicID string
	for index, dest := range []*int64{&senderID, &receiverID} {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'test') returning id`,
			fmt.Sprintf("%s_%d", unique, index), fmt.Sprintf("%s_%d@example.test", unique, index)).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, cleanup := range []struct {
			sql  string
			args []any
		}{
			{`delete from shop_purchases where currency_id=$1`, []any{currencyID}},
			{`delete from user_inventory where user_id=$1`, []any{senderID}},
			{`delete from shop_items where price_currency_id=$1`, []any{currencyID}},
			{`delete from currency_transactions where currency_id=$1`, []any{currencyID}},
			{`delete from user_currency_balances where currency_id=$1`, []any{currencyID}},
			{`delete from currencies where id=$1`, []any{currencyID}},
			{`delete from users where id=any($1::bigint[])`, []any{[]int64{senderID, receiverID}}},
		} {
			if _, err := pool.Exec(context.Background(), cleanup.sql, cleanup.args...); err != nil {
				t.Error(err)
			}
		}
	})
	if err = pool.QueryRow(ctx, `select public_id from users where id=$1`, receiverID).Scan(&receiverPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into currencies(code,name,transfer_tax_bps) values($1,'synthetic currency',1000) returning id`, unique).Scan(&currencyID); err != nil {
		t.Fatal(err)
	}
	const amount = int64(1 << 62)
	if _, err = pool.Exec(ctx, `insert into user_currency_balances(user_id,currency_id,balance) values($1,$2,$3)`, senderID, currencyID, amount); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: pool}
	request := func(body any) *http.Request {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/economy", bytes.NewReader(raw))
		return r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: senderID}))
	}
	response := httptest.NewRecorder()
	s.transferCurrency(response, request(map[string]any{"recipient": receiverPublicID, "currency": unique, "amount": amount}))
	if response.Code != http.StatusOK {
		t.Fatalf("transfer failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var transfer struct {
		Data struct{ Tax, Received int64 } `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &transfer); err != nil {
		t.Fatal(err)
	}
	const tax = int64(461168601842738791) // ceil((2^62)/10), exactly.
	if transfer.Data.Tax != tax || transfer.Data.Received != amount-tax {
		t.Fatalf("transfer tax lost integer precision: tax=%d received=%d", transfer.Data.Tax, transfer.Data.Received)
	}
	var senderBalance, receiverBalance int64
	if err = pool.QueryRow(ctx, `select
		(select balance from user_currency_balances where user_id=$1 and currency_id=$3),
		(select balance from user_currency_balances where user_id=$2 and currency_id=$3)`, senderID, receiverID, currencyID).Scan(&senderBalance, &receiverBalance); err != nil {
		t.Fatal(err)
	}
	if senderBalance != 0 || receiverBalance != amount-tax {
		t.Fatalf("persisted transfer does not match the tax: sender=%d receiver=%d", senderBalance, receiverBalance)
	}
	if _, err = pool.Exec(ctx, `update user_currency_balances set balance=$3 where user_id=$1 and currency_id=$2`, senderID, currencyID, amount); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into shop_items(code,item_type,name,price_currency_id,price_amount)
		values($1,'profile_background','synthetic amount fixture',$2,$3)`, unique, currencyID, amount); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	s.purchaseShopItem(response, request(map[string]any{"itemCode": unique, "quantity": 5}))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("overflowing purchase was accepted: status=%d body=%s", response.Code, response.Body.String())
	}
	var purchases int
	if err = pool.QueryRow(ctx, `select (select balance from user_currency_balances where user_id=$1 and currency_id=$2),
		(select count(*) from shop_purchases where user_id=$1 and currency_id=$2)`, senderID, currencyID).Scan(&senderBalance, &purchases); err != nil || senderBalance != amount || purchases != 0 {
		t.Fatalf("rejected purchase changed currency: balance=%d purchases=%d err=%v", senderBalance, purchases, err)
	}
}
