package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestSimpleProjectTypesComeFromOneOrderedRegistry(t *testing.T) {
	want := []string{"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"}
	values := simpleProjectTypeValues()
	if strings.Join(values, ",") != strings.Join(want, ",") {
		t.Fatalf("simple project type order=%v, want %v", values, want)
	}
	if len(simpleProjectTypes) != len(values) {
		t.Fatalf("simple project membership size=%d, registry size=%d", len(simpleProjectTypes), len(values))
	}
	for _, value := range values {
		if !simpleProjectTypes[value] {
			t.Fatalf("ordered registry value %q is missing from membership set", value)
		}
	}
	raw, err := os.ReadFile("simple_project_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if strings.Contains(source, `var simpleProjectTypes = stringSet("plugin"`) ||
		strings.Contains(source, `return []string{"plugin"`) {
		t.Fatal("simple project handlers still hand-maintain duplicate type literals")
	}
}
