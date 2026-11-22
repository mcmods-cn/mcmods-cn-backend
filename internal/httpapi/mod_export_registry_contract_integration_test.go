package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
)

func TestPublishedRegistryAndPagedListKeepDistinctContractsIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	const count = 103
	want := make([]string, count)
	entries := make([]map[string]any, count)
	for i := range want {
		want[i] = fmt.Sprintf("example:list_%03d", i)
		entries[count-1-i] = map[string]any{"id": want[i], "namespace": "example", "names": map[string]string{"en-US": "Example"}, "max_stack_size": 64}
	}
	payload, err := json.Marshal(map[string]any{"registry": "items", "entries": entries})
	if err != nil {
		t.Fatal(err)
	}
	_, revision := f.imported(t, auditFixGRZip(t, map[string]string{"registries/items.json": string(payload)}))
	base := "/api/v1/export-revisions/" + revision
	full := base + "/registries/items"
	pages := base + "/registry-entries?registries=items"
	for _, path := range []string{full, pages} {
		f.require(t, "", http.MethodGet, path, nil, http.StatusForbidden)
	}
	f.reviewGR(t, revision, "approved")
	type item struct {
		ID    string                     `json:"id"`
		Names map[string]string          `json:"names"`
		Data  map[string]json.RawMessage `json:"data"`
	}
	type response struct {
		Registry string `json:"registry"`
		Items    []item `json:"items"`
		Total    int    `json:"total"`
		Limit    int    `json:"limit"`
		Offset   int    `json:"offset"`
	}
	read := func(path string) response {
		t.Helper()
		return decodeTEST022Data[response](t, f.require(t, "", http.MethodGet, path, nil, http.StatusOK))
	}
	ids := func(items []item) []string {
		result := make([]string, len(items))
		for i, item := range items {
			result[i] = item.ID
		}
		return result
	}
	all := read(full)
	if all.Registry != "items" || !slices.Equal(ids(all.Items), want) {
		t.Fatalf("full response registry=%q length=%d; expected all %d sorted items", all.Registry, len(all.Items), count)
	}
	first, second := read(pages+"&limit=101"), read(pages+"&limit=100&offset=100")
	if first.Limit != 100 || first.Offset != 0 || first.Total != count || second.Offset != 100 || second.Total != count ||
		!slices.Equal(ids(first.Items), want[:100]) || !slices.Equal(ids(second.Items), want[100:]) {
		t.Fatalf("paged lengths, offsets or totals changed: %d/%d, %d/%d, %d/%d", len(first.Items), len(second.Items), first.Offset, second.Offset, first.Total, second.Total)
	}
	summary := read(pages + "&summary=1")
	if summary.Limit != 80 || !slices.Equal(ids(summary.Items), want[:80]) || summary.Total != count ||
		len(summary.Items[0].Data) != 0 || len(all.Items[0].Data) == 0 || all.Items[0].Names["en-US"] != "Example" {
		t.Fatal("default summary or full snapshot projection changed")
	}
}
