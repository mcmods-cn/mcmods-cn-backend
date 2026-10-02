package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogPresentationBatchNormalizesAndBoundsReferences(t *testing.T) {
	request := catalogPresentationBatchRequest{
		Locale: "zh_cn",
		Items: []catalogPresentationBatchReference{
			{PublicID: " ABC123456 ", ID: " Minecraft:Stone ", Kind: " Minecraft.Item ", Registry: " Minecraft "},
			{PublicID: "abc123456", ID: "minecraft:stone", Kind: "minecraft.item", Registry: "minecraft"},
		},
	}
	primary, secondary, items, err := normalizeCatalogPresentationBatchRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if primary != "zh-CN" || secondary != "zh-TW" || len(items) != 1 || items[0].PublicID != "abc123456" || items[0].ID != "minecraft:stone" {
		t.Fatalf("normalized primary=%q secondary=%q items=%+v", primary, secondary, items)
	}

	tooMany := catalogPresentationBatchRequest{Items: make([]catalogPresentationBatchReference, maxCatalogPresentationBatchItems+1)}
	for index := range tooMany.Items {
		tooMany.Items[index] = catalogPresentationBatchReference{ID: "minecraft:item_" + strings.Repeat("x", index%4), Kind: "minecraft.item"}
	}
	if _, _, _, err = normalizeCatalogPresentationBatchRequest(tooMany); err == nil {
		t.Fatal("oversized presentation batch was accepted")
	}
}

func TestCatalogPresentationBatchRouteIsRegistered(t *testing.T) {
	raw, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `POST /api/v1/catalog/resource-presentations`) {
		t.Fatal("catalog presentation batch route is not registered")
	}
}
