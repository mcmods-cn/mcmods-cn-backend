package searchindex

import (
	"reflect"
	"testing"
)

func TestSearchProjectionRetainsTextAfterEmptyOptionalFields(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		source, expected []string
	}{
		{"unnamed-source", []string{"", "Localized name"}, []string{"Localized name"}},
		{"optional-secondary-name", []string{"Primary", "", " ", "Translated", "Primary"}, []string{"Primary", "Translated"}},
		{"missing-summary", []string{"", "Searchable body", "Localized body"}, []string{"Searchable body", "Localized body"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if got := compactStrings(scenario.source); !reflect.DeepEqual(got, scenario.expected) {
				t.Fatalf("indexed text=%q; want %q", got, scenario.expected)
			}
		})
	}
}
