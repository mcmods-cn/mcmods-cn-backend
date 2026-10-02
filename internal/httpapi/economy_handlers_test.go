package httpapi

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestCalculateTransferAmountsPreservesCurrency(t *testing.T) {
	for _, amount := range []int64{2, 9_999, 10_000, 10_001, maxCurrencyAmount} {
		for _, taxBPS := range []int{0, 1, 333, 2_500} {
			tax, received, err := calculateTransferAmounts(amount, taxBPS)
			if err != nil {
				t.Fatalf("calculateTransferAmounts(%d, %d): %v", amount, taxBPS, err)
			}
			if tax < 0 || received <= 0 || tax > amount || received > amount || tax+received != amount {
				t.Fatalf("currency was not conserved for amount=%d taxBPS=%d: tax=%d received=%d", amount, taxBPS, tax, received)
			}
		}
	}
	tax, received, err := calculateTransferAmounts(10_000, 9_999)
	if err != nil || tax != 9_999 || received != 1 {
		t.Fatalf("high-tax transfer = tax %d, received %d, error %v", tax, received, err)
	}
}

func TestCalculateTransferAmountsRejectsOutOfDomainAndOverflowInputs(t *testing.T) {
	for _, test := range []struct {
		amount int64
		taxBPS int
	}{
		{0, 100},
		{-1, 100},
		{maxCurrencyAmount + 1, 100},
		{math.MaxInt64, 10_000},
		{100, -1},
		{100, 10_001},
		{100, 10_000},
	} {
		if _, _, err := calculateTransferAmounts(test.amount, test.taxBPS); err == nil {
			t.Fatalf("calculateTransferAmounts(%d, %d) accepted an invalid input", test.amount, test.taxBPS)
		}
	}
}

func TestCalculateShopTotalUsesCheckedMultiplication(t *testing.T) {
	if total, err := calculateShopTotal(maxCurrencyAmount, maxShopPurchaseQuantity); err != nil || total != maxCurrencyBalance {
		t.Fatalf("maximum valid shop total = %d, %v", total, err)
	}
	for _, test := range []struct {
		unitPrice int64
		quantity  int
	}{
		{math.MaxInt64, 2},
		{maxCurrencyAmount + 1, 1},
		{1, 0},
		{1, maxShopPurchaseQuantity + 1},
	} {
		if _, err := calculateShopTotal(test.unitPrice, test.quantity); err == nil {
			t.Fatalf("calculateShopTotal(%d, %d) accepted an invalid input", test.unitPrice, test.quantity)
		}
	}
}

func TestCheckedCurrencyBalanceRejectsWraparoundAndDomainOverflow(t *testing.T) {
	if next, err := checkedCurrencyBalance(maxCurrencyBalance-1, 1); err != nil || next != maxCurrencyBalance {
		t.Fatalf("maximum balance transition = %d, %v", next, err)
	}
	for _, test := range []struct {
		current int64
		delta   int64
	}{
		{maxCurrencyBalance, 1},
		{math.MaxInt64, 1},
		{0, math.MinInt64},
		{-1, 1},
	} {
		if _, err := checkedCurrencyBalance(test.current, test.delta); err == nil {
			t.Fatalf("checkedCurrencyBalance(%d, %d) accepted an invalid transition", test.current, test.delta)
		}
	}
}

func TestNormalizeEconomyConfigRejectsOutOfDomainRewards(t *testing.T) {
	cfg := defaultEconomyConfig()
	cfg.Checkin.Amount = maxCurrencyAmount + 1
	if _, err := normalizeEconomyConfig(cfg); err == nil {
		t.Fatal("oversized check-in reward was accepted")
	}
	cfg = defaultEconomyConfig()
	cfg.DownloadRewards = []economyDownloadReward{{
		ObjectType: "mod", Currency: "diamond", DownloadsPerReward: 1, Amount: maxCurrencyAmount + 1,
	}}
	if _, err := normalizeEconomyConfig(cfg); err == nil {
		t.Fatal("oversized download reward was accepted")
	}
}

func TestSupportedShopItemTypesMatchExecutableUsePaths(t *testing.T) {
	for _, itemType := range []string{"profile_background", "project_heat_boost", "server_heat_boost"} {
		if !isSupportedShopItemType(itemType) {
			t.Fatalf("executable shop item type %q was rejected", itemType)
		}
	}
	for _, itemType := range []string{"", "custom", "profile-background", "PROJECT_HEAT_BOOST"} {
		if isSupportedShopItemType(itemType) {
			t.Fatalf("non-executable shop item type %q was accepted", itemType)
		}
	}
}

func TestEconomyHandlersUseTransactionalConfigurationInvariants(t *testing.T) {
	raw, err := os.ReadFile("economy_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"nextHeatPromotionSequenceTx(r.Context(), tx, routeID)",
		"validateActiveEconomyCurrencyReferencesTx(r.Context(), tx, payload)",
		"economyCurrencyReferenceBlockersTx(r.Context(), tx, currentCode)",
		"!isSupportedShopItemType(payload.ItemType)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("economy handler is missing invariant %q", required)
		}
	}
	if strings.Contains(source, "select count(*) from content_heat_promotions where object_route_id=$1") {
		t.Fatal("heat-promotion sequence still depends on a racy count query")
	}
	for _, functionName := range []string{"func (s *Server) checkIn", "func (s *Server) recordOwnedContentDownload"} {
		start := strings.Index(source, functionName)
		if start < 0 {
			t.Fatalf("could not find %s", functionName)
		}
		segment := source[start:]
		lockIndex := strings.Index(segment, "lockEconomyConfigurationTx")
		loadIndex := strings.Index(segment, "loadEconomyConfig")
		if lockIndex < 0 || loadIndex < 0 || lockIndex > loadIndex {
			t.Fatalf("%s does not lock before reading economy configuration", functionName)
		}
	}
}
