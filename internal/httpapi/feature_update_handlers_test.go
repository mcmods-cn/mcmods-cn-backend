package httpapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCatalogApplicableVersionsUseAuthoritativeOrder(t *testing.T) {
	raw, _ := json.Marshal([]string{"1.20.1", "unknown", "1.21.1"})
	items := catalogApplicableVersions(raw, map[string]int{"1.21.1": 0, "1.20.1": 1})
	want := []map[string]string{
		{"id": "1.21.1", "name": "1.21.1", "group": "1.21.X"},
		{"id": "1.20.1", "name": "1.20.1", "group": "1.20.X"},
		{"id": "unknown", "name": "unknown", "group": "其他"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("unexpected applicable versions: %#v", items)
	}
}

func TestMinecraftVersionOrderKeepsConfiguredSequence(t *testing.T) {
	order := minecraftVersionOrder(minecraftVersionConfig{Versions: []minecraftVersionOption{{Code: "26.1"}, {Code: "1.21.11"}}})
	if order["26.1"] != 0 || order["1.21.11"] != 1 {
		t.Fatalf("unexpected version ranks: %#v", order)
	}
}
