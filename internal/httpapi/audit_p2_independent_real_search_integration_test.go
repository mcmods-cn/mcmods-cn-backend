package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/searchindex"
	"mcmods-cn-backend/internal/security"
)

// Real Typesense deliberately retains its old approved documents while the
// authoritative PostgreSQL rows change. No fake provider or running indexing
// Worker is used, so this proves delayed-index reads, not Worker delivery.
func TestAuditP2NEXT002RealTypesenseStaleIndexHTTPPostgresIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_TYPESENSE_INTEGRATION") != "1" {
		t.Skip("requires explicit task-owned real Typesense integration endpoint")
	}
	address, err := url.Parse(os.Getenv("MCMODS_TEST_TYPESENSE_URL"))
	if err != nil || address.Hostname() != "127.0.0.1" || address.Scheme != "http" || address.User != nil {
		t.Fatal("explicit task-owned loopback Typesense HTTP endpoint required")
	}
	key := os.Getenv("MCMODS_TEST_TYPESENSE_API_KEY")
	if key == "" {
		t.Fatal("task-owned synthetic Typesense key required")
	}
	var f test013Fixture
	if os.Getenv("MCMODS_RUN_LIVE_P2_SEARCH") == "1" {
		f = newAuditFixGRFixture(t, 25*time.Minute).test013Fixture
	} else {
		f = newTEST013Fixture(t)
	}
	prefix := "p2_exporter_" + fmt.Sprint(time.Now().UnixNano())
	client := searchindex.New(config.TypesenseConfig{Enabled: true, URL: address.String(), APIKey: key, CollectionPrefix: prefix, Timeout: 3 * time.Second})
	if err = client.Health(f.ctx); err != nil {
		t.Fatal("task-owned Typesense health failed")
	}
	collection := client.VersionedCollection("projects", 6)
	alias := client.Alias("projects")
	fields := []searchindex.Field{{Name: "internal_id", Type: "int64"}, {Name: "entity_type", Type: "string", Facet: true}, {Name: "updated_at", Type: "int64"}}
	for _, name := range []string{"names", "identifiers", "creators", "keywords", "text"} {
		fields = append(fields, searchindex.Field{Name: name, Type: "string[]", Infix: true})
	}
	for _, name := range []string{"slug", "public_id"} {
		fields = append(fields, searchindex.Field{Name: name, Type: "string", Infix: true})
	}
	if err = client.CreateCollection(f.ctx, searchindex.CollectionSchema{Name: collection, Fields: fields, DefaultSortingField: "updated_at"}); err != nil {
		t.Fatal("create owned unique collection failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Both names were generated and created by this invocation; no shared
		// alias, collection discovery or broad cleanup is performed.
		request, e := http.NewRequestWithContext(ctx, http.MethodDelete, address.String()+"/aliases/"+url.PathEscape(alias), nil)
		if e != nil {
			t.Error(e)
			return
		}
		request.Header.Set("X-TYPESENSE-API-KEY", key)
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Error("delete owned alias failed")
			return
		}
		response.Body.Close()
		if response.StatusCode != 200 && response.StatusCode != 404 {
			t.Errorf("delete owned alias status=%d", response.StatusCode)
		}
		if e = client.DeleteCollection(ctx, collection); e != nil {
			t.Error("delete owned unique collection failed")
		}
	})
	var third int64
	thirdPublic := randomCatalogPublicID()
	if err = f.db.QueryRow(f.ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values($1,$2,'P2 third','approved',$3) returning id`, thirdPublic, "p2-third-"+thirdPublic, f.userIDs[f.otherEditor]).Scan(&third); err != nil {
		t.Fatal(err)
	}
	ids := []int64{f.modID, f.otherModID, third}
	publicIDs := []string{f.modCode, f.otherModCode, thirdPublic}
	documents := make([]map[string]any, 0, 3)
	for i, id := range ids {
		names := []string{"P2StaleIndexFixture"}
		if i == 0 {
			names = append(names, "P2OnlyFirst")
		}
		documents = append(documents, map[string]any{"id": fmt.Sprintf("mod_%d", id), "internal_id": id, "entity_type": "mod", "updated_at": int64(100 + i), "names": names, "identifiers": []string{}, "creators": []string{}, "keywords": []string{}, "text": []string{}, "slug": fmt.Sprintf("p2-%d", i), "public_id": publicIDs[i]})
	}
	if err = client.ImportDocuments(f.ctx, collection, documents); err != nil {
		t.Fatal("import owned synthetic candidate documents failed")
	}
	if err = client.UpsertAlias(f.ctx, alias, collection); err != nil {
		t.Fatal("set owned unique alias failed")
	}
	client.SetReady(true)
	server := f.origin.Config.Handler.(*Server)
	server.search = client
	check := func(token, query string, total int) {
		t.Helper()
		seen := map[string]bool{}
		for offset := 0; offset <= total; offset++ {
			page := decodeTEST022Data[struct {
				Total int           `json:"total"`
				Items []modResponse `json:"items"`
			}](t, f.require(t, token, http.MethodGet, fmt.Sprintf("/api/v1/mods?q=%s&sort=relevance&limit=1&offset=%d", url.QueryEscape(query), offset), nil, 200))
			want := 1
			if offset == total {
				want = 0
			}
			if page.Total != total || len(page.Items) != want {
				t.Fatalf("query=%s offset=%d total=%d/items=%d want=%d/%d", query, offset, page.Total, len(page.Items), total, want)
			}
			for _, item := range page.Items {
				if seen[item.UniqueID] {
					t.Fatal("duplicate public item across pages")
				}
				seen[item.UniqueID] = true
			}
		}
		if len(seen) != total {
			t.Fatalf("unique page union=%d want=%d", len(seen), total)
		}
	}
	// Verify actual Typesense was used rather than a silent SQL fallback.
	if page := server.searchProjectPage(f.ctx, "P2OnlyFirst", "mod", "", "", "", security.Claims{}, 1, 0); !page.Used || page.Total != 1 {
		t.Fatalf("actual indexed page not used: %+v", page)
	}
	check("", "P2OnlyFirst", 1)
	check("", "P2StaleIndexFixture", 3)
	for _, state := range []string{"pending", "rejected"} {
		if _, err = f.db.Exec(f.ctx, `update mods set review_status=$2 where id=$1`, f.modID, state); err != nil {
			t.Fatal(err)
		}
		stale, e := client.Search(f.ctx, searchindex.SearchRequest{Collection: "projects", Query: "P2OnlyFirst", QueryBy: []string{"names"}})
		if e != nil || stale.Found != 1 {
			t.Fatalf("stale real index prerequisite found=%d err=%v", stale.Found, e)
		}
		check("", "P2OnlyFirst", 0)
		check(f.otherEditor, "P2OnlyFirst", 0)
		check(f.editor, "P2OnlyFirst", 1)
		check("", "P2StaleIndexFixture", 2)
	}
	if _, err = f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	check("", "P2OnlyFirst", 1)
	check("", "P2StaleIndexFixture", 3)
	if _, err = f.db.Exec(f.ctx, `delete from mods where id=$1`, third); err != nil {
		t.Fatal(err)
	}
	check("", "P2StaleIndexFixture", 2)
	t.Log("REAL_TYPESENSE delayed-index single hidden total0; restore no duplicates; multi-page/last-page, deleted candidate and owner/other isolation PASS; no browser/Worker claim")
	if os.Getenv("MCMODS_RUN_LIVE_P2_SEARCH") == "1" {
		auditP2HoldRealSearchForBrowser(t, f, prefix, address.String(), collection, client)
	}
}

// Opt-in test controller only: no control route or policy override is exposed
// by the product server. The frontend driver may observe real public responses
// while an owned file requests the same isolated SQL state changes as above.
func auditP2HoldRealSearchForBrowser(t *testing.T, f test013Fixture, prefix, typesenseOrigin, collection string, client *searchindex.Client) {
	t.Helper()
	directory := os.Getenv("MCMODS_AUDIT_P2_SEARCH_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("fresh absolute task-owned live search output directory required")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal("live search output directory already exists or unavailable")
	}
	// The real browser exposes page sizes 20/40/60. Keep the original tiny
	// counterexample separate, and seed a distinct 21-row group for actual
	// Next/Previous navigation with its unchanged minimum page size.
	var pagingControlledID int64
	var pagingControlledPublic string
	pagingDocuments := make([]map[string]any, 0, 21)
	for i := 0; i < 21; i++ {
		public := randomCatalogPublicID()
		var id int64
		if err := f.db.QueryRow(f.ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values($1,$2,$3,'approved',$4) returning id`, public, "p2-browser-"+public, fmt.Sprintf("P2BrowserPagingFixture %02d", i), f.userIDs[f.otherEditor]).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			pagingControlledID, pagingControlledPublic = id, public
		}
		pagingDocuments = append(pagingDocuments, map[string]any{"id": fmt.Sprintf("mod_%d", id), "internal_id": id, "entity_type": "mod", "updated_at": int64(1000 + i), "names": []string{"P2BrowserPagingFixture"}, "identifiers": []string{}, "creators": []string{}, "keywords": []string{}, "text": []string{}, "slug": "p2-browser-" + public, "public_id": public})
	}
	if err := client.ImportDocuments(f.ctx, collection, pagingDocuments); err != nil {
		t.Fatalf("import owned synthetic browser paging documents: %v", err)
	}
	write := func(name string, value any) {
		t.Helper()
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, name), append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	write("runtime.json", map[string]any{"owner": "exporter-independent-next002-20261024", "apiOrigin": f.origin.URL, "backendSource": auditJourneyRevision(cwd), "typesenseOrigin": typesenseOrigin, "ownedTypesensePrefix": prefix, "singleQuery": "P2OnlyFirst", "multiQuery": "P2StaleIndexFixture", "singleProjectPublicId": f.modCode, "initialMultiTotal": 2, "pagingQuery": "P2BrowserPagingFixture", "pagingTotal": 21, "pagingControlledPublicId": pagingControlledPublic, "pagingHiddenTotal": 20, "pagingControlInitiallyLastPage": true, "sourceState": "approved", "indexDocumentCount": 24, "commandPath": filepath.Join(directory, "command.json"), "ackPath": filepath.Join(directory, "command-ack.json"), "stopPath": filepath.Join(directory, "STOP"), "layer": "actual mounted API + PostgreSQL18 + actual Typesense29; no index Worker; external actual browser required"})
	last := ""
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal("live search browser fixture timed out")
		case <-ticker.C:
		}
		if _, err := os.Stat(filepath.Join(directory, "STOP")); err == nil {
			return
		}
		raw, err := os.ReadFile(filepath.Join(directory, "command.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var command struct {
			ID    string `json:"commandId"`
			State string `json:"state"`
			Scope string `json:"scope"`
		}
		if json.Unmarshal(raw, &command) != nil || command.ID == "" || command.ID == last {
			continue
		}
		if command.State != "approved" && command.State != "pending" && command.State != "rejected" {
			t.Fatal("invalid owned test state command")
		}
		id := f.modID
		if command.Scope == "paging" {
			id = pagingControlledID
		} else if command.Scope != "" && command.Scope != "single" {
			t.Fatal("invalid owned source scope")
		}
		if _, err = f.db.Exec(f.ctx, `update mods set review_status=$2 where id=$1`, id, command.State); err != nil {
			t.Fatal(err)
		}
		last = command.ID
		write("command-ack.json", map[string]any{"commandId": command.ID, "state": command.State, "acknowledgedAt": time.Now().UTC().Format(time.RFC3339Nano), "scope": command.Scope, "syntheticSourceOnly": true})
	}
}
