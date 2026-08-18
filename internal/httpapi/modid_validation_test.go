package httpapi

import "testing"

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
