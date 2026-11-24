package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestImportedAdvancementIconPublishesStackIdentityAndTagPresence(t *testing.T) {
	for _, tag := range []string{`{Damage:0}`, `{BlockEntityTag:{id:"minecraft:decorated_pot",sherds:["minecraft:brick"]}}`} {
		icon := map[string]any{"item": "minecraft:decorated_pot", "count": float64(1), "nbt_snbt": tag}
		source := map[string]any{"icon": icon}
		for _, input := range []any{source, minimizeAdvancementDisplay(source)} {
			value, err := exportDisplayJSONRule().project(input)
			if err != nil {
				t.Fatal(err)
			}
			got := value.(map[string]any)["icon"].(map[string]any)
			want := map[string]any{"item": icon["item"], "count": float64(1), "has_stack_tag": true}
			if !reflect.DeepEqual(got, want) || icon["nbt_snbt"] != tag {
				t.Fatal("public icon must retain the item, count and tag presence, and leave the source intact")
			}
		}
	}
	for _, raw := range []any{map[string]any{"unexpected": true}, strings.Repeat("x", (256<<10)+1)} {
		if _, err := exportDisplayJSONRule().project(map[string]any{"icon": map[string]any{"nbt_snbt": raw}}); err == nil {
			t.Fatal("accepted an invalid or oversized stack tag")
		}
	}
	legacy := map[string]any{"icon": map[string]any{"item": "minecraft:stick", "nbt": "{}"}}
	got, err := exportDisplayJSONRule().project(legacy)
	if err != nil || !reflect.DeepEqual(got, legacy) {
		t.Fatal("legacy icon changed")
	}
}

func TestFlatGeneratorStructureOverridesUseRegistryReferences(t *testing.T) {
	var settings map[string]any
	if err := json.Unmarshal([]byte(`{"biome":"minecraft:plains","structure_overrides":["minecraft:strongholds","minecraft:villages"],"layers":[{"height":1,"block":"minecraft:bedrock"}],"features":false,"lakes":false}`), &settings); err != nil {
		t.Fatal(err)
	}
	rule := registeredImportedCatalogJSONRule("generatorSettings")
	for _, overrides := range []any{settings["structure_overrides"], []any{}, "#minecraft:in_overworld"} {
		settings["structure_overrides"] = overrides
		projected, err := rule.project(settings)
		if err != nil || !reflect.DeepEqual(projected, settings) {
			t.Fatalf("valid flat generator settings did not round trip: %v", err)
		}
	}
	for _, overrides := range []any{map[string]any{"entry": "minecraft:villages"}, []any{float64(2)}, "minecraft:villages", []any{"#minecraft:villages"}, []any{"missing namespace"}} {
		settings["structure_overrides"] = overrides
		if _, err := rule.project(settings); err == nil {
			t.Fatal("accepted an unsupported structure holder")
		}
	}
	if _, err := registeredImportedCatalogJSONRule("biomeSource").project(map[string]any{"structure_overrides": []any{}}); err == nil {
		t.Fatal("dimension settings leaked into another codec")
	}
}

func TestImportedAdvancementTitleRetainsCanonicalLocaleWhenAliasIsPresent(t *testing.T) {
	source := map[string]any{"title_names": map[string]string{"en_us": "Fallback title", "en-US": "Published title", "ja_jp": "日本語", "es-MX": "unavailable"}}
	for range 100 {
		display := minimizeAdvancementDisplay(source).(map[string]any)
		var titles map[string]string
		if err := json.Unmarshal(display["title_names"].(json.RawMessage), &titles); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(titles, map[string]string{"en-US": "Published title", "ja-JP": "日本語"}) {
			t.Fatalf("stored display titles = %#v", titles)
		}
	}
}
