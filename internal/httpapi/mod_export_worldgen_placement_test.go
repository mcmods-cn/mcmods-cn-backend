package httpapi

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

// Placement fragments from Minecraft 1.20.1, exporter archive SHA-256
// 5f215422cd0cdb659f7cada4886524cc4ef619cecf7d2ed369935d0f39532901.
// The custom count is from the exporter's natural-generation-v2 schema fixture.
func TestNaturalGenerationPlacementProjections(t *testing.T) {
	for name, raw := range map[string]string{
		"trees_plains":          `{"count":{"distribution":[{"data":0,"weight":19},{"data":1,"weight":1}],"type":"minecraft:weighted_list"}}`,
		"lake_lava_underground": `{"modifiers":[{"direction_of_search":"down","target_condition":{"predicates":[{"predicate":{"blocks":"minecraft:air","type":"minecraft:matching_blocks"},"type":"minecraft:not"},{"offset":[0,-5,0],"type":"minecraft:inside_world_bounds"}],"type":"minecraft:all_of"},"max_steps":32,"type":"minecraft:environment_scan"}]}`,
		"pointed_dripstone":     `{"modifiers":[{"xz_spread":{"type":"minecraft:clamped_normal","value":{"mean":0.0,"deviation":3.0,"min_inclusive":-10,"max_inclusive":10}},"y_spread":{"type":"minecraft:clamped_normal","value":{"mean":0.0,"deviation":0.6,"min_inclusive":-2,"max_inclusive":2}},"type":"minecraft:random_offset"}]}`,
		"spring_lava_frozen":    `{"height":{"min_inclusive":{"above_bottom":0},"max_inclusive":{"below_top":8},"inner":8,"type":"minecraft:very_biased_to_bottom"}}`,
		"patch_grass_plain":     `{"modifiers":[{"noise_level":-0.8,"below_noise":5,"above_noise":10,"type":"minecraft:noise_threshold_count"}]}`,
		"warm_ocean_vegetation": `{"modifiers":[{"noise_to_count_ratio":20,"noise_factor":400.0,"noise_offset":0.0,"type":"minecraft:noise_based_count"}]}`,
		"seagrass_simple":       `{"modifiers":[{"step":"liquid","type":"minecraft:carving_mask"}]}`,
		"acacia_checked":        `{"biome_filter":false,"modifier_types":["minecraft:block_predicate_filter"],"modifiers":[{"predicate":{"state":{"Properties":{"stage":"0"},"Name":"minecraft:acacia_sapling"},"type":"minecraft:would_survive"},"type":"minecraft:block_predicate_filter"}]}`,
		"schema_custom_count":   `{"count":{"type":"example:custom_count","value":12},"modifier_types":["example:custom_count"],"modifiers":[{"type":"example:custom_count","value":12}],"biome_filter":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			var placement map[string]any
			if err := json.Unmarshal([]byte(raw), &placement); err != nil {
				t.Fatal(err)
			}
			stored, err := registeredImportedCatalogJSONRule("placement").project(placement)
			if err != nil || !reflect.DeepEqual(stored, placement) {
				t.Fatalf("stored placement = %#v, err = %v", stored, err)
			}
			entry := map[string]any{"entries": []any{map[string]any{"placement": placement}}}
			projected, err := exportPartialCatalogJSONRule("worldgen/natural_generation.json").project(entry)
			if err != nil || !reflect.DeepEqual(projected, entry) {
				t.Fatalf("partial catalog = %#v, err = %v", projected, err)
			}
		})
	}
}

func TestNaturalGenerationPlacementFieldPurposes(t *testing.T) {
	for _, raw := range []string{
		`{"modifiers":[{"type":"minecraft:environment_scan","target_condition":"down"}]}`,
		`{"count":{"distribution":[{"data":0,"weight":{"value":2}}]}}`,
		`{"height":{"min_inclusive":{"above_bottom":0,"block_id":"minecraft:stone"}}}`,
		`{"modifiers":[{"predicate":{"offset":[0,0,0,0]}}]}`,
		`{"modifiers":[{"predicate":{"state":{"Properties":{"stage":{"min":0}}}}}]}`,
		`{"modifiers":[{"type":"example:custom_count","unregistered_property":1}]}`,
	} {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if _, err := registeredImportedCatalogJSONRule("placement").project(value); err == nil {
			t.Errorf("unregistered placement shape accepted: %s", raw)
		}
	}
}

func TestNaturalGenerationCarverPartialProjection(t *testing.T) {
	// These fields come from the same archive's minecraft:cave carver.
	const source = `{"entries":[{"generation_step":"air","probability":0.15,"y":{"min_inclusive":{"above_bottom":8},"max_inclusive":{"absolute":180},"type":"minecraft:uniform"},"yScale":{"type":"minecraft:uniform","value":{"min_inclusive":0.1,"max_exclusive":0.9}},"lava_level":{"above_bottom":8},"replaceable":"#minecraft:overworld_carver_replaceables","debug_settings":{"water_state":{"Properties":{"waterlogged":"false","lit":"false","candles":"1"},"Name":"minecraft:candle"}}}]}`
	var catalog any
	if err := json.Unmarshal([]byte(source), &catalog); err != nil {
		t.Fatal(err)
	}
	got, err := exportPartialCatalogJSONRule("worldgen/natural_generation.json").project(catalog)
	if err != nil || !reflect.DeepEqual(catalog, got) {
		t.Fatalf("carver changed in partial catalog: %v", err)
	}
}

func TestNaturalGenerationPlacementImportAndReviewIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	// The exporter emits the same count provider in its summary and the
	// original placed feature. A partial record declares the fallback path.
	const count = `{"type":"minecraft:weighted_list","distribution":[{"data":0,"weight":19},{"data":1,"weight":1}]}`
	const modifiers = `[{"type":"minecraft:count","count":` + count + `}]`
	const placement = `{"count":` + count + `,"modifiers":` + modifiers + `,"biome_filter":false}`
	const sourcePath = "data/example/worldgen/placed_feature/trees.json"
	const definition = `{"feature":"example:trees","placement":` + modifiers + `}`
	_, revision := f.imported(t, auditFixGRZip(t, map[string]string{
		"worldgen/natural_generation.json": `{"schema_version":"mcmods-natural-generation/v2","export_mode":"normalized_catalog","entries":[{"id":"example:trees","generation_id":"example:trees","namespace":"example","entry_kind":"placed_feature","normalization_status":"partial","generation_step":"vegetal_decoration","placed_feature_definition_path":"` + sourcePath + `","placement":` + placement + `}]}`,
		sourcePath:                         definition,
	}))
	var resourceID string
	if err := f.db.QueryRow(f.ctx, `select entity.public_id from resource_import_snapshots snapshot
		join catalog_entities entity on entity.id=snapshot.resource_id
		where snapshot.revision_id=$1 and snapshot.registry='natural_generation' and snapshot.data->'placement'=$2::jsonb`,
		revision, placement).Scan(&resourceID); err != nil {
		t.Fatalf("stored placement: %v", err)
	}
	assetURL := "/api/v1/export-revisions/" + revision + "/assets/content?path=" + url.QueryEscape(sourcePath)
	f.require(t, "", "GET", assetURL, nil, 403)
	f.reviewGR(t, revision, "approved")
	got := f.require(t, "", "GET", assetURL, nil, 200)
	var wantAsset, gotAsset, wantPlacement any
	if err := json.Unmarshal([]byte(definition), &wantAsset); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &gotAsset); err != nil || !reflect.DeepEqual(wantAsset, gotAsset) {
		t.Fatalf("published placed feature = %#v, err = %v", gotAsset, err)
	}
	got = f.require(t, "", "GET", "/api/v1/mods/audit-gr-"+f.modCode+"/content-resources/"+resourceID+"?version="+f.versionPublic, nil, 200)
	var page struct {
		Data struct {
			Details []struct{ Definition map[string]any }
		}
	}
	if err := json.Unmarshal([]byte(placement), &wantPlacement); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &page); err != nil || len(page.Data.Details) != 1 || !reflect.DeepEqual(page.Data.Details[0].Definition["placement"], wantPlacement) {
		t.Fatalf("published resource did not preserve reviewed placement, err = %v", err)
	}
}
