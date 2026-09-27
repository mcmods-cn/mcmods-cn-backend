package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteModpackPreviewUsesTheSupportedTargetNameProjection(t *testing.T) {
	raw, err := os.ReadFile("favorite_modpack_export.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"favoriteTargetNameSQL",
		"when 'mod' then mods.primary_name",
		"when 'modpack' then modpack.primary_name",
		"when 'blueprint' then blueprint.title",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("favorite export name projection is missing %q", required)
		}
	}
}
