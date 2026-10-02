package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	appconfig "mcmods-cn-backend/internal/config"
)

func TestOCT02TransferResponseKeepsRecipientWalletPrivateIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newTEST044IsolatedDatabase(t, ctx)
	cfg := appconfig.Load()
	server := &Server{db: pool, cfg: cfg}
	senderID, _, token := createTEST044User(t, ctx, pool, cfg, "privacy-sender", "UTC")
	recipientID, recipientPublicID, _ := createTEST044User(t, ctx, pool, cfg, "privacy-recipient", "UTC")
	grantTEST044Permissions(t, ctx, pool, senderID, "economy.transfer")
	var currencyID int64
	if err := pool.QueryRow(ctx, `update currencies set transfer_tax_bps=1000 where code='diamond' returning id`).Scan(&currencyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into user_currency_balances(user_id,currency_id,balance) values($1,$3,1000),($2,$3,987654)`, senderID, recipientID, currencyID); err != nil {
		t.Fatal(err)
	}
	response := performTEST044Request(ctx, server.requirePermission("economy.transfer", server.transferCurrency), token, http.MethodPost, "/api/v1/users/me/economy/transfer", fmt.Sprintf(`{"recipient":%q,"currency":"diamond","amount":100}`, recipientPublicID), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("transfer status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if _, exists := result.Data["recipientBalance"]; exists {
		t.Fatal("transfer reveals another user's existing wallet balance")
	}
	for key, want := range map[string]int64{"tax": 10, "received": 90, "senderBalance": 900} {
		var got int64
		if err := json.Unmarshal(result.Data[key], &got); err != nil || got != want {
			t.Fatalf("%s = %d/%v want %d", key, got, err, want)
		}
	}
	assertTEST044Balances(t, ctx, pool, currencyID, map[int64]int64{senderID: 900, recipientID: 987744})
	assertTEST044TransferLedger(t, ctx, pool, currencyID, 2)
}
