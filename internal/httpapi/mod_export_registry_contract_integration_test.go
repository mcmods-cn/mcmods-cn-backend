package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
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
	_, revision := f.imported(t, auditFixGRZip(t, map[string]string{
		"registries/items.json": string(payload),
		"registries/blocks.json": `{"registry":"blocks","entries":[
			{"id":"example:small_block","namespace":"example","names":{"en-US":"Small block"},"hardness":1}
		]}`,
	}))
	base := "/api/v1/export-revisions/" + revision
	large := base + "/registries/items"
	small := base + "/registries/blocks"
	pages := base + "/registry-entries?registries=items"
	for _, path := range []string{large, small, pages} {
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
	assertLegacyRegistryLimit(t, f, large, pages)
	all := read(small)
	if all.Registry != "blocks" || !slices.Equal(ids(all.Items), []string{"example:small_block"}) {
		t.Fatalf("small full response lost its complete stored membership: %#v", all)
	}
	empty := read(base + "/registries/unknown_fixture_registry")
	if empty.Registry != "unknown_fixture_registry" || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("small full response lost its empty list contract: %#v", empty)
	}
	first, second := read(pages+"&limit=101"), read(pages+"&limit=100&offset=100")
	if first.Limit != 100 || first.Offset != 0 || first.Total != count || second.Offset != 100 || second.Total != count ||
		!slices.Equal(ids(first.Items), want[:100]) || !slices.Equal(ids(second.Items), want[100:]) {
		t.Fatalf("paged lengths, offsets or totals changed: %d/%d, %d/%d, %d/%d", len(first.Items), len(second.Items), first.Offset, second.Offset, first.Total, second.Total)
	}
	summary := read(pages + "&summary=1")
	if summary.Limit != 80 || !slices.Equal(ids(summary.Items), want[:80]) || summary.Total != count ||
		len(summary.Items[0].Data) != 0 || len(all.Items[0].Data) == 0 || all.Items[0].Names["en-US"] != "Small block" {
		t.Fatal("default summary or full snapshot projection changed")
	}

	// Put an oversized value into one fixture snapshot to exercise the byte
	// budget independently of the row ceiling. The approved fixture scope is owned.
	updated, err := f.db.Exec(f.ctx, `update resource_import_snapshots set
		data = data || jsonb_build_object('fixtureDescription', repeat('x',$2::integer))
		where revision_id=$1 and registry='blocks'`, revision, maxCatalogResponseBytes)
	if err != nil || updated.RowsAffected() != 1 {
		t.Fatalf("prepare registry byte-limit fixture: rows=%d err=%v", updated.RowsAffected(), err)
	}
	blockPages := base + "/registry-entries?registries=blocks"
	assertLegacyRegistryLimit(t, f, small, blockPages)
	blockSummaries := read(blockPages + "&summary=1")
	if blockSummaries.Total != 1 || !slices.Equal(ids(blockSummaries.Items), []string{"example:small_block"}) {
		t.Fatalf("paginated summaries could not read the large entry: %#v", blockSummaries)
	}
}

func assertLegacyRegistryLimit(t *testing.T, f auditFixGRFixture, path, pageURL string) {
	t.Helper()
	request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origin.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.origin.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var problem struct {
		Code    string          `json:"code"`
		Data    json.RawMessage `json:"data"`
		Details struct {
			PageURL          string `json:"pageURL"`
			MaxEntries       int    `json:"maxEntries"`
			MaxResponseBytes int    `json:"maxResponseBytes"`
		} `json:"details"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&problem); err != nil {
		t.Fatalf("read registry limit response: %v", err)
	}
	if response.StatusCode != http.StatusUnprocessableEntity || problem.Code != "registry_response_too_large" ||
		len(problem.Data) != 0 || problem.Details.PageURL != pageURL ||
		problem.Details.MaxEntries != 100 || problem.Details.MaxResponseBytes != maxCatalogResponseBytes ||
		response.Header.Get("Link") != "<"+pageURL+`>; rel="alternate"` {
		t.Fatalf("legacy registry must return an explicit pagination fallback: status=%d code=%q details=%#v",
			response.StatusCode, problem.Code, problem.Details)
	}
}
