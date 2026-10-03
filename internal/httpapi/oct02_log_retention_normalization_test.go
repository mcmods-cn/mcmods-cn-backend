package httpapi

import (
	"reflect"
	"testing"
)

func TestOCT02LogRetentionNormalizesKeysWithoutMutatingInput(t *testing.T) {
	original := map[string]int{" api_access ": 45, " custom ": 5000, " ": 7, " invalid ": -1}
	wantOriginal := map[string]int{" api_access ": 45, " custom ": 5000, " ": 7, " invalid ": -1}
	got := normalizeLogConfig(logRetentionConfig{DefaultDays: 5000, CategoryDays: original})
	if got.DefaultDays != 3650 || got.CategoryDays["api_access"] != 45 || got.CategoryDays["custom"] != 3650 {
		t.Fatalf("trimmed retention overrides or bounds were lost: %+v", got)
	}
	for _, key := range []string{" api_access ", " custom ", " ", " invalid ", "invalid"} {
		if _, exists := got.CategoryDays[key]; exists {
			t.Errorf("normalization retained invalid key %q", key)
		}
	}
	if !reflect.DeepEqual(original, wantOriginal) {
		t.Fatal("normalization changed the caller's configuration map")
	}
}

func TestOCT02LogRetentionCanonicalKeyWinsAndAliasesAreDeterministic(t *testing.T) {
	for range 100 {
		got := normalizeLogConfig(logRetentionConfig{CategoryDays: map[string]int{
			"system": 80, " system ": 15, "\tsystem": 20, " custom ": 25, "\tcustom ": 30,
		}})
		if got.CategoryDays["system"] != 80 || got.CategoryDays["custom"] != 30 {
			t.Fatalf("ambiguous aliases did not use stable precedence: %+v", got.CategoryDays)
		}
		if len(got.CategoryDays) != len(defaultLogConfig().CategoryDays)+1 {
			t.Fatalf("aliases became duplicate persisted categories: %+v", got.CategoryDays)
		}
	}
}
