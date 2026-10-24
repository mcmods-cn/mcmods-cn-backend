package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/searchindex"
)

// PostgreSQL and the application HTTP server are real; this provider freezes
// candidate IDs deliberately to exercise delayed index updates deterministically.
func auditAccessStaleSearch(t *testing.T, f test013Fixture, ids []int64) *searchindex.Client {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		start := min(max(page-1, 0)*perPage, len(ids))
		end := min(start+perPage, len(ids))
		hits := make([]map[string]any, 0, end-start)
		for _, id := range ids[start:end] {
			hits = append(hits, map[string]any{"document": map[string]any{"internal_id": id}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"found": len(ids), "hits": hits})
	}))
	t.Cleanup(provider.Close)
	client := searchindex.New(config.TypesenseConfig{Enabled: true, URL: provider.URL, APIKey: "synthetic-audit-key", CollectionPrefix: "audit", Timeout: time.Second})
	client.SetReady(true)
	return client
}

func TestAuditNEXT002StaleIndexCountsAndPagesUseCurrentVisibilityFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	// FullHTTP fixture's server is retained by the actual listener.
	server := f.origin.Config.Handler.(*Server)
	ids := []int64{f.modID, f.otherModID}
	server.search = auditAccessStaleSearch(t, f, ids)
	check := func(token string, offset, total, count int) {
		t.Helper()
		page := decodeTEST022Data[struct {
			Total int           `json:"total"`
			Items []modResponse `json:"items"`
		}](t, f.require(t, token, http.MethodGet, fmt.Sprintf("/api/v1/mods?q=test013&sort=relevance&limit=1&offset=%d", offset), nil, 200))
		if page.Total != total || len(page.Items) != count {
			t.Fatalf("offset=%d total=%d items=%d want %d/%d", offset, page.Total, len(page.Items), total, count)
		}
	}
	check("", 0, 2, 1)
	check("", 1, 2, 1)
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='rejected' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	check("", 0, 1, 1)
	check("", 1, 1, 0)
	check(f.editor, 0, 2, 1) // established submitter preview scope is retained
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	check("", 0, 2, 1)
	check("", 1, 2, 1)
}

func TestAuditRPH003ResourceSourceVisibilityCountAndIndexFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "global_resource.list")
	version := f.version(t, f.editor, "test013-mod", "Audit source")
	section := f.section(t, version.PublicID, f.builtin(t, "item_block"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:audit_visibility", "minecraft.item", "default", map[string]any{})
	f.review(t, resource, f.editor, "approved")
	var internalID int64
	if err := f.db.QueryRow(f.ctx, `select id from catalog_entities where public_id=$1`, resource.PublicID).Scan(&internalID); err != nil {
		t.Fatal(err)
	}
	server := f.origin.Config.Handler.(*Server)
	for _, indexed := range []bool{false, true} {
		if indexed {
			server.search = auditAccessStaleSearch(t, f, []int64{internalID})
		} else {
			server.search = nil
		}
		for _, state := range []string{"approved", "pending", "rejected", "approved"} {
			if _, err := f.db.Exec(f.ctx, `update mods set review_status=$2 where id=$1`, f.modID, state); err != nil {
				t.Fatal(err)
			}
			page := decodeTEST022Data[struct {
				Total int               `json:"total"`
				Items []json.RawMessage `json:"items"`
			}](t, f.require(t, f.denied, http.MethodGet, "/api/v1/catalog/resources?q="+resource.PublicID, nil, 200))
			want := 0
			if state == "approved" {
				want = 1
			}
			if page.Total != want || len(page.Items) != want {
				t.Fatalf("index=%v state=%s total=%d items=%d want=%d", indexed, state, page.Total, len(page.Items), want)
			}
		}
	}
}
