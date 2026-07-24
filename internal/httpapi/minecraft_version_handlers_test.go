package httpapi

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestMinecraftVersionType(t *testing.T) {
	tests := map[string]string{
		"release":   "release",
		"snapshot":  "snapshot",
		"old_alpha": "legacy",
		"old_beta":  "legacy",
	}
	for input, expected := range tests {
		if actual := minecraftVersionType(input); actual != expected {
			t.Fatalf("minecraftVersionType(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestNextMinecraftVersionSync(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	before := time.Date(2026, time.July, 15, 3, 30, 0, 0, location)
	if actual := nextMinecraftVersionSync(before); actual.Hour() != 4 || actual.Day() != 15 {
		t.Fatalf("next sync before 04:00 = %s", actual)
	}
	after := time.Date(2026, time.July, 15, 4, 30, 0, 0, location)
	if actual := nextMinecraftVersionSync(after); actual.Hour() != 4 || actual.Day() != 16 {
		t.Fatalf("next sync after 04:00 = %s", actual)
	}
}

func TestStoredMinecraftVersionOrderIsAuthoritative(t *testing.T) {
	base := minecraftVersionConfig{Versions: []minecraftVersionOption{{Code: "1.20.1", Type: "release"}}}
	stored := minecraftVersionConfig{Versions: []minecraftVersionOption{
		{Code: "26.2", Type: "release"},
		{Code: "26.3-snapshot-3", Type: "snapshot"},
	}}
	merged := mergeMinecraftVersionConfig(base, stored)
	if len(merged.Versions) != 2 || merged.Versions[0].Code != "26.2" {
		t.Fatalf("stored version order was not preserved: %#v", merged.Versions)
	}
}

func TestDecodeLoaderVersionSources(t *testing.T) {
	maven, err := decodeMavenMetadataVersions([]byte(`<metadata><versioning><versions><version>1.20.1-47.4.10</version><version>1.21.1-52.0.1</version></versions></versioning></metadata>`))
	if err != nil || !reflect.DeepEqual(maven, []string{"1.20.1-47.4.10", "1.21.1-52.0.1"}) {
		t.Fatalf("decode Maven metadata = %#v, %v", maven, err)
	}
	fabric, err := decodeFabricGameVersions([]byte(`[{"version":"1.21.1","stable":true},{"version":"26.3-snapshot-4","stable":false}]`))
	if err != nil || !reflect.DeepEqual(fabric, []string{"1.21.1", "26.3-snapshot-4"}) {
		t.Fatalf("decode Fabric game versions = %#v, %v", fabric, err)
	}
	liteLoader, err := decodeLiteLoaderMinecraftVersions([]byte(`{"versions":{"1.12.2":{},"1.7.10":{}}}`))
	if err != nil || len(liteLoader) != 2 {
		t.Fatalf("decode LiteLoader versions = %#v, %v", liteLoader, err)
	}
}

func TestResolveForgeMinecraftVersions(t *testing.T) {
	known := []string{"1.21.1", "1.20.1", "1.8.9", "26.2"}
	artifacts := []string{"1.20.1-47.4.10", "11.15.1.2318-1.8.9", "26.2-65.0.6", "not-a-version"}
	actual := resolveArtifactMinecraftVersions(artifacts, known)
	expected := []string{"1.20.1", "1.8.9", "26.2"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("resolved Forge versions = %#v, want %#v", actual, expected)
	}
}

func TestResolveNeoForgeMinecraftVersions(t *testing.T) {
	known := []string{"1.20.1", "1.20.4", "1.21", "1.21.1", "25w14craftmine", "26.2"}
	artifacts := []string{"1.20.1-47.1.106", "20.4.251", "21.0.167", "21.1.241", "0.25w14craftmine.3-beta", "26.2.18-beta"}
	actual := resolveNeoForgeMinecraftVersions(artifacts, known)
	expected := []string{"1.20.1", "1.20.4", "1.21", "1.21.1", "25w14craftmine", "26.2"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("resolved NeoForge versions = %#v, want %#v", actual, expected)
	}
}

func TestOrderSupportedMinecraftVersionsUsesCatalogOrder(t *testing.T) {
	all := []minecraftVersionOption{{Code: "26.2"}, {Code: "1.21.1"}, {Code: "1.20.1"}}
	actual := orderSupportedMinecraftVersions(all, []string{"1.20.1", "26.2", "unknown"})
	expected := []string{"26.2", "1.20.1"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("ordered supported versions = %#v, want %#v", actual, expected)
	}
}

func TestFailedLoaderSyncPreservesPreviousVersions(t *testing.T) {
	config := minecraftVersionConfig{
		Versions:     []minecraftVersionOption{{Code: "1.21.1"}, {Code: "1.20.1"}},
		Loaders:      []minecraftLoaderOption{{Code: "Forge", Versions: []string{"1.20.1"}}, {Code: "Fabric", Versions: []string{"1.20.1"}}},
		LastSyncedAt: "2026-07-20T08:00:00Z",
	}
	sources := []minecraftLoaderVersionSource{{code: "Forge", primaryURL: forgeMavenMetadataURL}, {code: "Fabric", primaryURL: fabricGameVersionsURL}}
	results := map[string]minecraftLoaderVersionResult{
		"forge":  {source: sources[0], err: errors.New("temporary source failure")},
		"fabric": {source: sources[1], versions: []string{"1.21.1"}, sourceURL: fabricGameVersionsURL},
	}
	applyMinecraftLoaderVersionResults(&config, sources, results)
	if !reflect.DeepEqual(config.Loaders[0].Versions, []string{"1.20.1"}) {
		t.Fatalf("failed Forge sync replaced previous versions: %#v", config.Loaders[0].Versions)
	}
	if !reflect.DeepEqual(config.Loaders[1].Versions, []string{"1.21.1"}) {
		t.Fatalf("successful Fabric sync did not replace versions: %#v", config.Loaders[1].Versions)
	}
	if config.LoaderSyncs[0].Status != "failed" || config.LoaderSyncs[1].Status != "synced" {
		t.Fatalf("unexpected loader sync statuses: %#v", config.LoaderSyncs)
	}
}
