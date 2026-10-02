package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestAnalyzeImportMODIDs(t *testing.T) {
	tests := []struct {
		name       string
		configured []string
		counts     map[string]int
		required   bool
		primary    string
	}{
		{name: "matching", configured: []string{"ExampleMod"}, counts: map[string]int{"examplemod": 92, "minecraft": 300, "dependency": 8}, primary: "examplemod"},
		{name: "mismatch", configured: []string{"anothermod"}, counts: map[string]int{"examplemod": 120}, required: true, primary: "examplemod"},
		{name: "missing configured", counts: map[string]int{"examplemod": 1}, required: true, primary: "examplemod"},
		{name: "multiple major", configured: []string{"examplemod", "dependency"}, counts: map[string]int{"examplemod": 6, "dependency": 4}, required: true, primary: "examplemod"},
		{name: "unknown", configured: []string{"examplemod"}, counts: map[string]int{"minecraft": 10}, primary: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := analyzeImportMODIDs(test.configured, test.counts)
			if result.Required != test.required || result.Primary != test.primary {
				t.Fatalf("unexpected analysis: %#v", result)
			}
		})
	}
}

func TestAnalyzeImportMODIDsRatiosAndNormalization(t *testing.T) {
	result := analyzeImportMODIDs([]string{" ExampleMod ", "examplemod"}, map[string]int{"EXAMPLEMOD": 9, "dependency": 1})
	if len(result.Configured) != 1 || len(result.Detected) != 2 || result.Detected[0].Ratio != 0.9 {
		t.Fatalf("unexpected normalized analysis: %#v", result)
	}
}

func TestMODIDConfirmationProtocolBindsEveryImporterAndJobMod(t *testing.T) {
	source, err := os.ReadFile("modid_validation.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(source), "and mod_id=$") < 3 {
		t.Fatal("MODID analysis reads and transitions are not all bound to the job's Mod")
	}
	for _, file := range []string{"mod_export_handlers.go", "mod_embedded_icon_import.go"} {
		entrypoint, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Count(string(entrypoint), "pauseCatalogImportForMODIDConfirmation(") != 1 {
			t.Fatalf("%s does not use exactly one shared MODID confirmation gate", file)
		}
	}
}
