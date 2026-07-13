package httpapi

import (
	"reflect"
	"testing"
)

func TestDiffJSONTracksNestedPathsAndArrayReplacement(t *testing.T) {
	before := map[string]any{
		"summary": "before",
		"author":  map[string]any{"name": "Alex", "role": "owner"},
		"tags":    []any{"technology"},
	}
	after := map[string]any{
		"summary": "after",
		"author":  map[string]any{"name": "Alex", "role": "developer"},
		"tags":    []any{"technology", "automation"},
	}

	changes := diffJSON("", before, after)
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	want := []string{"/author/role", "/summary", "/tags"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("unexpected change paths: got %v want %v", paths, want)
	}
}

func TestTopLevelChangedFieldsCollapsesNestedChanges(t *testing.T) {
	changes := []modRevisionChangeResponse{
		{Path: "/authors/0/name", Operation: "replace"},
		{Path: "/authors/1/role", Operation: "replace"},
		{Path: "/summary", Operation: "replace"},
	}
	if got, want := topLevelChangedFields(changes), []string{"authors", "summary"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected fields: got %v want %v", got, want)
	}
}

func TestSameRevision(t *testing.T) {
	one, anotherOne, two := int64(1), int64(1), int64(2)
	for _, test := range []struct {
		name        string
		left, right *int64
		want        bool
	}{
		{name: "both empty", want: true},
		{name: "same value", left: &one, right: &anotherOne, want: true},
		{name: "different value", left: &one, right: &two, want: false},
		{name: "one empty", left: &one, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sameRevision(test.left, test.right); got != test.want {
				t.Fatalf("sameRevision()=%v want %v", got, test.want)
			}
		})
	}
}
