package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func TestIndependentP1RPH001LosingTransactionHasNoAdditionalFacts(t *testing.T) {
	for _, kind := range []string{"version", "resource", "template", "layout", "changelog"} {
		t.Run(kind, func(t *testing.T) {
			var f test013Fixture
			var target modContentMutationResult
			var aggregate, path, method string
			var build func(int) any
			if kind == "changelog" {
				g := newTEST017Fixture(t)
				f = g.test013Fixture
				proposal := g.submit(t, f.editor, http.MethodPost, "/api/v1/changelogs?targetType=mod&targetId="+f.modCode, test017Snapshot("Independent original"), 201)
				g.review(t, f.reviewer, proposal, "approved", 200)
				base := g.detail(t, f.editor, proposal.ID, 200).PublishedRevisionID
				aggregate, path, method = projectChangelogAggregate+":"+proposal.ID, "/api/v1/changelogs/"+proposal.ID, http.MethodPut
				build = func(i int) any {
					edit := test017Snapshot(fmt.Sprintf("Independent writer %d", i))
					edit.BaseRevisionID = base
					return edit
				}
			} else {
				f = newTEST013Fixture(t)
				method = http.MethodPut
				switch kind {
				case "version":
					target = f.version(t, f.editor, "test013-mod", "Independent initial")
					aggregate = modContentAggregateVersion + ":" + target.PublicID
					path = "/api/v1/mods/test013-mod/content-versions/" + target.PublicID
					build = func(i int) any {
						return modContentVersionEdit{BaseRevisionID: &target.RevisionID, MinecraftVersions: []string{"1.21.1"}, Loaders: []string{"neoforge"}, ModVersion: fmt.Sprintf("independent-%d", i), Reason: "Independent transaction check"}
					}
				case "resource":
					version, section, resource := auditRPHResource(t, f)
					target = resource
					aggregate = modContentAggregateResource + ":" + target.PublicID + ":" + version.PublicID
					path = "/api/v1/mods/test013-mod/content-resources/" + target.PublicID
					build = func(i int) any {
						return modContentResourceEdit{BaseRevisionID: &target.RevisionID, ResourcePublicID: target.PublicID, VersionPublicID: version.PublicID, SectionPublicID: &section.PublicID, EntryTypeCode: "default", DefaultLocale: "en-US", Definition: map[string]any{}, Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: fmt.Sprintf("Independent writer%d", i)}}, Reason: "Independent transaction check"}
					}
				case "template":
					edit := modContentTemplateEdit{Code: "independentmatrix", DefaultLocale: "en-US", DefaultDisplayMode: "compact", Definition: templateSchemaGuardMap(false, "number", false), Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Independent template"}}, Reason: "Independent transaction check"}
					target = f.submit(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/content-templates", edit, 201)
					f.review(t, target, f.editor, "approved")
					aggregate = modContentAggregateTemplate + ":" + target.PublicID
					path = "/api/v1/mods/test013-mod/content-templates/" + target.PublicID
					build = func(i int) any {
						e := edit
						e.BaseRevisionID = &target.RevisionID
						e.Localizations = []catalogLocalizationEdit{{Locale: "en-US", Name: fmt.Sprintf("Independent writer%d", i)}}
						return e
					}
				case "layout":
					version := f.version(t, f.editor, "test013-mod", "Independent layout")
					target = f.section(t, version.PublicID, f.builtin(t, "item_block"))
					aggregate = modContentAggregateSection + ":" + target.PublicID
					path = "/api/v1/mods/test013-mod/content-sections/" + target.PublicID + "/layout"
					method = http.MethodPatch
					build = func(i int) any {
						return modContentLayoutPatch{BaseRevisionID: &target.RevisionID, VersionPublicID: version.PublicID, RootSectionPublicID: target.PublicID, DisplayMode: "large", Resources: []modContentLayoutResourceEdit{}, Reason: fmt.Sprintf("Independent writer%d", i)}
					}
				}
			}
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.no-review."+f.modCode)
			reached, release := make(chan struct{}), make(chan struct{})
			var releaseOnce, stopOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			cfg := f.db.Config()
			cfg.ConnConfig.Tracer = independentQueryStartHook(func(data pgx.TraceQueryStartData) {
				if data.SQL == `select pg_advisory_xact_lock(hashtext($1))` && len(data.Args) > 0 && data.Args[0] == aggregate {
					stopOnce.Do(func() {
						close(reached)
						select {
						case <-release:
						case <-f.ctx.Done():
						}
					})
				}
			})
			pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			original := f.origin.Config.Handler.(*Server)
			server := NewServer(f.ctx, original.cfg, pool, nil, original.cache, nil, nil)
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := server.Shutdown(ctx); err != nil {
					t.Error(err)
				}
			}()
			origin := httptest.NewServer(server)
			defer origin.Close()
			loserFixture := f
			loserFixture.origin = origin
			loser := auditCompletionAsyncJSON(loserFixture, f.editor, method, path, build(2))
			select {
			case <-reached:
			case <-f.ctx.Done():
				t.Fatal("loser did not reach pre-advisory SQL barrier")
			}
			f.require(t, f.editor, method, path, build(1), 200)
			before := auditAccessActivityFacts(t, f)
			releaseOnce.Do(func() { close(release) })
			got := <-loser
			if got.err != nil || got.status != 409 {
				t.Fatalf("loser status=%d err=%v body=%s", got.status, got.err, got.raw)
			}
			auditFixRequireProjectFactsUnchanged(t, before, auditAccessActivityFacts(t, f))
			t.Logf("%s actual HTTP old contender passed early check, winner committed, loser409; full published pointers/revisions/audit/outbox/notifications/balances/ledgers unchanged", kind)
		})
	}
}

func TestIndependentP1CONT001StaleBaselineConflictsWithoutPublication(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "content.write", "content.no-review")
	s := f.origin.Config.Handler.(*Server)
	base := func(name string) map[string]any {
		return map[string]any{"reason": "Independent stale baseline", "defaultLocale": "en-US", "localizations": []map[string]any{{"locale": "en-US", "name": name}}}
	}
	p := base("Independent parent")
	p["canonicalId"] = "independent:parent"
	p["definition"] = map[string]any{}
	parent := decodeTEST022Data[catalogEditResult](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/recipe-types", p, 202))
	p = base("Independent template")
	p["templateKey"] = "independent-parent"
	p["canvas"] = map[string]any{"width": 80, "height": 40, "imageScale": 1}
	p["definition"] = map[string]any{}
	p["slots"] = []map[string]any{{"slotKey": "output", "role": "output", "ordinal": 0, "rect": map[string]any{"x": 10, "y": 10, "width": 16, "height": 16}}}
	template := decodeTEST022Data[catalogEditResult](t, f.require(t, f.otherEditor, http.MethodPost, "/api/v1/recipe-types/"+parent.ObjectPublicID+"/templates", p, 202))
	cases := []struct {
		kind, path string
		extra      map[string]any
	}{
		{"resource", "/api/v1/catalog/resources", map[string]any{"kindCode": "minecraft.item", "canonicalId": "independent:stale", "definition": map[string]any{}}},
		{"tag", "/api/v1/tags", map[string]any{"registry": "minecraft:item", "canonicalId": "independent:stale", "memberResourcePublicIds": []string{}}},
		{"recipe_type", "/api/v1/recipe-types", map[string]any{"canonicalId": "independent:stale", "definition": map[string]any{}, "catalystResourcePublicIds": []string{}}},
		{"recipe_template", "/api/v1/recipe-types/" + parent.ObjectPublicID + "/templates", map[string]any{"templateKey": "independent-stale", "canvas": p["canvas"], "slots": p["slots"], "definition": map[string]any{}}},
		{"recipe", "/api/v1/recipe-types/" + parent.ObjectPublicID + "/recipes", map[string]any{"canonicalSourceId": "independent:stale", "templatePublicId": template.ObjectPublicID, "applicableVersionIds": []string{"1.21.1"}, "definition": map[string]any{}, "bindings": map[string]any{"output": map[string]any{"candidates": []map[string]any{{"rawResourceId": "independent:unresolved", "kindCode": "minecraft.item", "amount": 1}}}}}},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			payload := base("Independent original " + c.kind)
			for k, v := range c.extra {
				payload[k] = v
			}
			first := decodeTEST022Data[catalogEditResult](t, f.require(t, f.otherEditor, http.MethodPost, c.path, payload, 202))
			var firstID int64
			var firstRaw []byte
			if err := f.db.QueryRow(f.ctx, `select id,snapshot from content_revisions where public_id=$1`, first.RevisionID).Scan(&firstID, &firstRaw); err != nil {
				t.Fatal(err)
			}
			var snapshot catalogEditorSnapshot
			if err := json.Unmarshal(firstRaw, &snapshot); err != nil {
				t.Fatal(err)
			}
			snapshot.Operation = "edit"
			if err := f.db.QueryRow(f.ctx, `select id from catalog_entities where public_id=$1`, first.ObjectPublicID).Scan(&snapshot.EntityID); err != nil {
				t.Fatal(err)
			}
			if snapshot.ParentPublicID != "" {
				if err := f.db.QueryRow(f.ctx, `select id from catalog_entities where public_id=$1`, snapshot.ParentPublicID).Scan(&snapshot.ParentEntityID); err != nil {
					t.Fatal(err)
				}
			}
			mutate := func(token, base string) catalogEditResult {
				claims, err := security.ParseToken(s.cfg.JWTSecret, token)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(http.MethodPut, "/internal/independent-existing-history", nil).WithContext(context.WithValue(f.ctx, claimsContextKey, claims))
				got, err := s.submitCatalogEditorMutation(r, snapshot, &base)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			second := mutate(f.otherEditor, first.RevisionID)
			pending := mutate(f.editor, second.RevisionID)
			if pending.ReviewStatus != "pending" {
				t.Fatal("ordinary pending fixture bypassed review")
			}
			// Restore an actually created historical revision with the production
			// publisher. No immutable snapshot/base is edited, and no arbitrary SQL
			// error may satisfy the review's explicit stale-baseline contract.
			tx, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if err = publishCatalogEditorSnapshotTx(f.ctx, tx, firstID, firstRaw, f.userIDs[f.otherEditor]); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			before := auditAccessActivityFacts(t, f)
			raw := f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+pending.RevisionID, map[string]string{"status": "approved"}, 409)
			if !containsIndependentText(raw, "catalog content changed after this request was submitted") {
				t.Fatalf("wrong conflict reason: %s", raw)
			}
			after := auditAccessActivityFacts(t, f)
			for _, allowed := range []string{"change_requests", "review_events", "audit_events"} {
				delete(before, allowed)
				delete(after, allowed)
			}
			if !reflect.DeepEqual(before, after) {
				for table, value := range before {
					if after[table] != value {
						t.Errorf("stale approval changed %s", table)
					}
				}
			}
			var state string
			var conflicts, approved int
			if err = f.db.QueryRow(f.ctx, `select status,(select count(*) from review_events where change_request_id=q.id and event_type='conflicted'),(select count(*) from review_events where change_request_id=q.id and event_type='approved') from change_requests q where public_id=$1`, pending.ChangeRequestID).Scan(&state, &conflicts, &approved); err != nil || state != "conflicted" || conflicts != 1 || approved != 0 {
				t.Fatalf("state=%s conflicts=%d approved=%d err=%v", state, conflicts, approved, err)
			}
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+pending.RevisionID, map[string]string{"status": "approved"}, 409)
			t.Log("Real HTTP review stale409, exact conflict audit retained once; no publication/projection/outbox/notification/reward/balance side effects")
		})
	}
}

func containsIndependentText(raw []byte, text string) bool {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value["error"] == text
}
