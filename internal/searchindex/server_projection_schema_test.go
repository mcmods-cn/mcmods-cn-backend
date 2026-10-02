package searchindex

import (
	"encoding/json"
	"testing"
)

func TestServerProjectionContainsEveryCatalogFilterAndStableSortKey(t *testing.T) {
	if projectionSchemaVersion != 5 {
		t.Fatalf("projection schema version=%d want 5", projectionSchemaVersion)
	}
	schema := collectionSchemas()["servers"]
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	definition := string(raw)
	for _, field := range []string{
		`"name":"internal_id","type":"int64"`,
		`"name":"created_at","type":"int64"`,
		`"name":"updated_at","type":"int64"`,
		`"name":"heat_sort_asc","type":"int64"`,
		`"name":"heat_sort_desc","type":"int64"`,
		`"name":"download_count","type":"int64"`,
		`"name":"favorite_count","type":"int64"`,
		`"name":"rating_score","type":"int64"`,
		`"name":"rating_count","type":"int64"`,
		`"name":"view_count","type":"int64"`,
		`"name":"comment_count","type":"int64"`,
	} {
		if !containsJSONFragment(definition, field) {
			t.Fatalf("server projection is missing %s: %s", field, definition)
		}
	}
}

func containsJSONFragment(value, fragment string) bool {
	return len(value) >= len(fragment) && indexJSONFragment(value, fragment) >= 0
}

func indexJSONFragment(value, fragment string) int {
	for start := 0; start+len(fragment) <= len(value); start++ {
		if value[start:start+len(fragment)] == fragment {
			return start
		}
	}
	return -1
}
