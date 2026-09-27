package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModContentSectionCursorIsBoundToQueryRevisionLimitAndMode(t *testing.T) {
	t.Parallel()
	scope := modContentSectionCursorScope(1, 2, 3, "revision-a", "Copper", 120, "cards")
	raw := encodeModContentSectionCursor(modContentSectionPageCursor{
		Version: modContentSectionCursorVersion, Scope: scope,
		SortPath: []int64{0, 2, 4, 8}, Ordinal: 9, ResourceID: 10, Total: 250,
	})
	cursor, err := decodeModContentSectionCursor(raw, scope)
	if err != nil || cursor.Total != 250 || cursor.ResourceID != 10 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	for name, foreign := range map[string]string{
		"query":    modContentSectionCursorScope(1, 2, 3, "revision-a", "Tin", 120, "cards"),
		"revision": modContentSectionCursorScope(1, 2, 3, "revision-b", "Copper", 120, "cards"),
		"limit":    modContentSectionCursorScope(1, 2, 3, "revision-a", "Copper", 80, "cards"),
		"mode":     modContentSectionCursorScope(1, 2, 3, "revision-a", "Copper", 120, "graph"),
	} {
		if _, err = decodeModContentSectionCursor(raw, foreign); err == nil {
			t.Errorf("cursor crossed %s scope", name)
		}
	}
}

func TestModContentSectionCursorRejectsMalformedKeys(t *testing.T) {
	t.Parallel()
	scope := modContentSectionCursorScope(1, 2, 3, "revision", "", 120, "cards")
	for name, cursor := range map[string]modContentSectionPageCursor{
		"empty path": {Version: 1, Scope: scope, Ordinal: 1, ResourceID: 2},
		"zero id":    {Version: 1, Scope: scope, SortPath: []int64{0, 0}, Ordinal: 1, ResourceID: 2},
		"negative":   {Version: 1, Scope: scope, SortPath: []int64{-1, 2}, Ordinal: 1, ResourceID: 2},
	} {
		if _, err := decodeModContentSectionCursor(encodeModContentSectionCursor(cursor), scope); err == nil {
			t.Errorf("accepted %s cursor", name)
		}
	}
	if _, err := decodeModContentSectionCursor("not-base64", scope); err == nil {
		t.Fatal("accepted malformed base64 cursor")
	}
}

func TestModContentLayoutPageEnforcesExactByteBudgetWithContinuation(t *testing.T) {
	t.Parallel()
	scope := modContentSectionCursorScope(1, 2, 3, "revision", "", 10, "graph")
	items := make([]modContentLayoutQueryItem, 0, 10)
	for index := 0; index < 10; index++ {
		items = append(items, modContentLayoutQueryItem{
			ResourcePublicID: "resource1", Label: strings.Repeat("wide-name-", 40),
			Advancement: &modContentLayoutAdvancementSummary{GroupID: strings.Repeat("group-", 20)}, cursor: modContentSectionPageCursor{
				Version: modContentSectionCursorVersion, Scope: scope, SortPath: []int64{0, 2},
				Ordinal: index, ResourceID: int64(index + 1), Total: 10,
			},
		})
	}
	const budget = 1800
	payload, encodedItems, err := encodeBudgetedModContentLayoutPage(map[string]any{"items": items}, items, false, budget)
	if err != nil || len(payload) > budget || encodedItems <= 0 || encodedItems >= len(items) {
		t.Fatalf("bytes=%d items=%d err=%v", len(payload), encodedItems, err)
	}
	var envelope struct {
		Data struct {
			HasMore    bool   `json:"hasMore"`
			NextCursor string `json:"nextCursor"`
			Items      []any  `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.HasMore || envelope.Data.NextCursor == "" || len(envelope.Data.Items) != encodedItems {
		t.Fatalf("budgeted envelope=%+v", envelope.Data)
	}
}
