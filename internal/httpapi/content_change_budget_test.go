package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestStoredContentChangesHaveItemDepthPathAndValueBudgets(t *testing.T) {
	beforeFields := make(map[string]any, 1000)
	afterFields := make(map[string]any, 1000)
	for index := 0; index < 1000; index++ {
		key := fmt.Sprintf("field%04d", index)
		beforeFields[key] = index
		afterFields[key] = index + 1
	}
	changes := boundedContentChanges(map[string]any{"fields": beforeFields}, map[string]any{"fields": afterFields})
	if len(changes) != maximumStoredContentChangeItems {
		t.Fatalf("stored changes=%d want hard limit %d", len(changes), maximumStoredContentChangeItems)
	}
	if changes[len(changes)-1].Path != "/" {
		t.Fatalf("last change path=%q want root truncation summary", changes[len(changes)-1].Path)
	}
	for _, change := range changes {
		if len(change.Path) > maximumStoredContentChangePathBytes {
			t.Fatalf("change path bytes=%d exceeds %d", len(change.Path), maximumStoredContentChangePathBytes)
		}
	}
	longKey := strings.Repeat("界", maximumStoredContentChangePathBytes)
	longPathChanges := boundedContentChanges(map[string]any{longKey: "before"}, map[string]any{longKey: "after"})
	if len(longPathChanges) != 1 || len(longPathChanges[0].Path) > maximumStoredContentChangePathBytes || !strings.Contains(longPathChanges[0].Path, "~h") {
		t.Fatalf("long path change=%+v", longPathChanges)
	}

	deepBefore, deepAfter := any("before"), any("after")
	for depth := 0; depth < maximumStoredContentChangeDepth+20; depth++ {
		deepBefore = map[string]any{"child": deepBefore}
		deepAfter = map[string]any{"child": deepAfter}
	}
	deepChanges := boundedContentChanges(deepBefore, deepAfter)
	if len(deepChanges) != 1 || strings.Count(deepChanges[0].Path, "/") > maximumStoredContentChangeDepth {
		t.Fatalf("deep changes=%+v", deepChanges)
	}
}

func TestStoredContentChangeValuesSummarizeLargeArraysAndStrings(t *testing.T) {
	values := []any{
		make([]any, 10000),
		strings.Repeat("界", maximumStoredContentChangeValueBytes),
	}
	for _, value := range values {
		encoded, err := encodeStoredContentChangeValue(value)
		if err != nil || encoded == nil {
			t.Fatalf("encoded=%v err=%v", encoded, err)
		}
		if len(*encoded) > maximumStoredContentChangeValueBytes {
			t.Fatalf("stored value bytes=%d exceeds %d", len(*encoded), maximumStoredContentChangeValueBytes)
		}
		var summary map[string]any
		if err = json.Unmarshal([]byte(*encoded), &summary); err != nil || summary["$summary"] == nil || summary["sha256"] == nil {
			t.Fatalf("summary=%v err=%v", summary, err)
		}
	}
	if !strings.Contains(contentChangeBatchInsertSQL, "unnest($2::text[],$3::text[],$4::text[],$5::text[])") {
		t.Fatal("content changes are not inserted as one parallel-array batch")
	}
}
