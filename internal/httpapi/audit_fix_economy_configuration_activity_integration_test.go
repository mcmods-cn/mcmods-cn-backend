package httpapi

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func auditFixEconomyConfigurationFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditFixEconomyActivityFacts(t, f)
	for _, table := range []string{"system_settings", "currencies", "shop_items", "permissions"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func TestAuditFixCMP003EconomyConfigurationActivitySharesCurrencyShopAndPermissionTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "economy.write", "economy.read", "shop.write", "shop.read")
	recover := func(method, path string, body any, want int, action, object int16) []byte {
		t.Helper()
		prior := auditFixActivityIntentCount(t, f, f.reviewer, action, object)
		auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], true)
		before := auditFixEconomyConfigurationFacts(t, f)
		raw := f.require(t, f.reviewer, method, path, body, 500)
		auditFixRequireSpecificActivityError(t, raw, "failed to persist economy configuration activity")
		auditFixRequireProjectFactsUnchanged(t, before, auditFixEconomyConfigurationFacts(t, f))
		auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], false)
		result := f.require(t, f.reviewer, method, path, body, want)
		if count := auditFixActivityIntentCount(t, f, f.reviewer, action, object); count != prior+1 {
			t.Fatalf("configuration activity=%d prior=%d", count, prior)
		}
		return result
	}
	currency := currencyPayload{Code: "audit_atomic_coin", Name: "Synthetic atomic coin", Translations: map[string]any{}, Status: "active", TransferTaxBPS: 125}
	created := decodeTEST022Data[currencyPayload](t, recover(http.MethodPost, "/api/v1/admin/economy/currencies", currency, 201, activity.ActionCreate, activity.ObjectEconomy))
	currency.Name = "Updated synthetic coin"
	recover(http.MethodPut, "/api/v1/admin/economy/currencies/"+created.PublicID, currency, 200, activity.ActionEdit, activity.ObjectEconomy)
	config := economyConfigPayload{Checkin: economyCheckinConfig{Enabled: true, Currency: currency.Code, Amount: 7, MinimumHours: 20}, DownloadRewards: []economyDownloadReward{}}
	recover(http.MethodPut, "/api/v1/admin/economy/config", config, 200, activity.ActionEdit, activity.ObjectEconomy)
	before := auditFixEconomyConfigurationFacts(t, f)
	disabled := currency
	disabled.Status = "disabled"
	f.require(t, f.reviewer, http.MethodPut, "/api/v1/admin/economy/currencies/"+created.PublicID, disabled, 409)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixEconomyConfigurationFacts(t, f))
	var rawConfig []byte
	if err := f.db.QueryRow(f.ctx, `select config from shop_items where code='project_heat_boost'`).Scan(&rawConfig); err != nil {
		t.Fatal(err)
	}
	var shopConfig map[string]any
	if err := json.Unmarshal(rawConfig, &shopConfig); err != nil {
		t.Fatal(err)
	}
	item := shopItemPayload{Code: "audit_atomic_item", ItemType: "project_heat_boost", Name: "Synthetic atomic shop item", Translations: map[string]any{}, Config: shopConfig, Status: "active", PriceCurrency: currency.Code, PriceAmount: 5, PurchasePermission: "audit.atomic_purchase", UsePermission: "audit.atomic_use"}
	saved := decodeTEST022Data[shopItemPayload](t, recover(http.MethodPost, "/api/v1/admin/shop/items", item, 201, activity.ActionCreate, activity.ObjectShopItem))
	item.Name = "Updated synthetic shop item"
	item.PurchasePermission = "audit.atomic_purchase2"
	item.UsePermission = "audit.atomic_use2"
	recover(http.MethodPut, "/api/v1/admin/shop/items/"+saved.PublicID, item, 200, activity.ActionEdit, activity.ObjectShopItem)
	var declared int
	if err := f.db.QueryRow(f.ctx, `select count(*) from permissions where code=any($1::text[])`, []string{"audit.atomic_purchase", "audit.atomic_use", "audit.atomic_purchase2", "audit.atomic_use2"}).Scan(&declared); err != nil || declared != 4 {
		t.Fatalf("committed declared permissions=%d err=%v", declared, err)
	}
	list := f.require(t, f.reviewer, http.MethodGet, "/api/v1/admin/shop/items", nil, 200)
	if !json.Valid(list) {
		t.Fatal("actual admin shop projection invalid")
	}
}
