package httpapi

import (
	"reflect"
	"testing"
)

func TestREUSE003ProjectFileFiltersUseAuthoritativeMinecraftVersionOrder(t *testing.T) {
	config := minecraftVersionConfig{Versions: []minecraftVersionOption{
		{Code: "25w10a", Type: "snapshot"},
		{Code: "1.20.6", Type: "release"},
		{Code: "1.21.1", Type: "release"},
	}}
	items := []projectFileItem{
		{GameVersions: []string{"1.21.1", "unknown-beta"}},
		{GameVersions: []string{"25w10a", "1.20.6", "1.21.1"}},
	}

	versions, _ := projectFileFilters(items, minecraftVersionOrder(config))
	want := []string{"25w10a", "1.20.6", "1.21.1", "unknown-beta"}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("project file versions=%#v, want authoritative order %#v", versions, want)
	}
}
