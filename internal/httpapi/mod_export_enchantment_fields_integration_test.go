package httpapi

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Compare every supplied storage-contract field, including exact false/zero
// values. Do not demand supported_items when the archival exporter omitted it.
func assertExporterEnchantmentSourceFields(t *testing.T, ctx context.Context, tx pgx.Tx, versionID int64, raw []byte) {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	entries := exportObjectArray(document["entries"])
	if len(entries) == 0 {
		t.Fatal("enchantment field contract needs nonempty source data")
	}
	fields := map[string]string{
		"min_level": "minimumLevel", "max_level": "maximumLevel", "rarity": "rarity", "rarity_weight": "rarityWeight",
		"anvil_cost": "anvilCost", "treasure_only": "treasureOnly", "curse": "curse", "tradeable": "tradeable", "discoverable": "discoverable",
		"slots": "slots", "supported_items_tag": "supportedItemsTag", "supported_items": "supportedItems", "exclusive_with": "exclusiveWith", "costs": "costs", "effect_component_count": "effectComponentCount",
	}
	for _, source := range entries {
		var definitionRaw []byte
		if err := tx.QueryRow(ctx, `select detail.definition from mod_resource_version_details detail join game_resources resource on resource.entity_id=detail.resource_id where detail.version_id=$1 and resource.kind_code='minecraft.enchantment' and resource.canonical_id=$2 and detail.status='active'`, versionID, exportString(source["id"])).Scan(&definitionRaw); err != nil {
			t.Fatal(err)
		}
		var definition map[string]any
		if err := json.Unmarshal(definitionRaw, &definition); err != nil {
			t.Fatal(err)
		}
		for sourceKey, websiteKey := range fields {
			expected, supplied := source[sourceKey]
			actual, present := definition[websiteKey]
			if supplied && (!present || !reflect.DeepEqual(actual, expected)) {
				t.Fatalf("%s field %s: source=%#v persisted=%#v present=%v", source["id"], sourceKey, expected, actual, present)
			}
			if !supplied && present {
				t.Fatalf("%s invented unsupplied field %s=%#v", source["id"], websiteKey, actual)
			}
		}
	}
	t.Logf("exact_enchantment_source_fields=%d entries (supplied preserved, absent not fabricated)", len(entries))
}

func TestExporterEnchantmentFieldContractIntegration(t *testing.T) {
	pool := openModExportConcurrentTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	revisionID, versionID := createRealKeyMappingImportFacts(t, ctx, tx)
	// Explicit synthetic contract fixture, not an actual exported package.
	raw := []byte(`{"registry":"enchantments","entries":[{"id":"minecraft:field_contract_test","min_level":1,"max_level":4,"rarity":"rare","rarity_weight":2,"anvil_cost":0,"treasure_only":false,"curse":false,"tradeable":true,"discoverable":true,"slots":["head"],"supported_items_tag":"minecraft:head_armor","supported_items":["minecraft:stone"],"exclusive_with":["minecraft:protection"],"costs":{"base":3},"effect_component_count":0}]}`)
	rows, err := prepareExportRegistryResources(nil, map[string]string{"minecraft": revisionID}, "registries/enchantments.json", raw)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	batch := newModExportWriteBatch()
	queueCatalogResource(batch, rows[0])
	if err = batch.flush(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = syncImportedResourcesToContentVersionTx(ctx, tx, []string{revisionID}, versionID, false, 0); err != nil {
		t.Fatal(err)
	}
	assertExporterEnchantmentSourceFields(t, ctx, tx, versionID, raw)
}
