package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestAuditFixRPH001ConcurrentAutoApprovedVersionUpdates(t *testing.T) {
	f := newTEST013Fixture(t)
	v := f.version(t, f.editor, "test013-mod", "Concurrent version initial")
	r := v
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.no-review."+f.modCode)
	var original string
	if err := f.db.QueryRow(f.ctx, `select snapshot::text from content_revisions where public_id=$1`, r.RevisionID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	lock, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(f.ctx)
	key := modContentAggregateVersion + ":" + v.PublicID
	if _, err = lock.Exec(f.ctx, `select pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
		t.Fatal(err)
	}
	type result struct {
		status int
		raw    []byte
		err    error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			edit := modContentVersionEdit{Label: fmt.Sprintf("Concurrent version writer %d", i), MinecraftVersions: []string{"1.21.1"}, Loaders: []string{"neoforge"}, ModVersion: fmt.Sprintf("audit-v%d", i), BaseRevisionID: &v.RevisionID, Reason: fmt.Sprintf("concurrent %d", i)}
			body, _ := json.Marshal(edit)
			req, e := http.NewRequestWithContext(f.ctx, http.MethodPut, f.origin.URL+"/api/v1/mods/test013-mod/content-versions/"+v.PublicID, bytes.NewReader(body))
			if e != nil {
				results <- result{err: e}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+f.editor)
			resp, e := f.origin.Client().Do(req)
			if e != nil {
				results <- result{err: e}
				return
			}
			defer resp.Body.Close()
			raw, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			results <- result{resp.StatusCode, raw, e}
		}(i)
	}
	deadline := time.Now().Add(10 * time.Second)
	waiting := 0
	for time.Now().Before(deadline) {
		if err = f.db.QueryRow(f.ctx, `select count(*) from pg_stat_activity where datname=current_database() and wait_event='advisory' and query like '%pg_advisory_xact_lock%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if waiting < 2 {
		t.Fatal("deterministic barrier not reached by both requests")
	}
	if err = lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	for i := 0; i < 2; i++ {
		x := <-results
		if x.err != nil {
			t.Fatal(x.err)
		}
		counts[x.status]++
		t.Logf("concurrent response: HTTP %d %s", x.status, x.raw)
	}
	var unchanged string
	var approved int
	if err = f.db.QueryRow(f.ctx, `select snapshot::text from content_revisions where public_id=$1`, r.RevisionID).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged != original {
		t.Error("historical snapshot mutated")
	}
	if err = f.db.QueryRow(f.ctx, `select count(*) from content_revisions v join change_requests r on r.proposed_revision_id=v.id where v.aggregate_type=$1 and v.aggregate_key=$2 and v.base_revision_id=(select id from content_revisions where public_id=$3) and r.status='approved'`, modContentAggregateVersion, v.PublicID, v.RevisionID).Scan(&approved); err != nil {
		t.Fatal(err)
	}
	t.Logf("two requests held after initial base read; accepted approved children of same baseline=%d; statuses=%v; historical snapshot unchanged=%t", approved, counts, original == unchanged)
	if counts[200] != 1 || counts[409] != 1 || approved != 1 {
		t.Error("optimistic concurrency must reject one stale writer after publication advances")
	}
}

func auditRPHResource(t *testing.T, f test013Fixture) (modContentMutationResult, modContentMutationResult, modContentMutationResult) {
	t.Helper()
	v := f.version(t, f.editor, "test013-mod", "Audit RPH 1.0")
	s := f.section(t, v.PublicID, f.builtin(t, "item_block"))
	r := f.resource(t, v.PublicID, s.PublicID, "test013:audit_hidden_item", "minecraft.item", "default", map[string]any{})
	f.review(t, r, f.editor, "approved")
	return v, s, r
}

func TestAuditFixRPH001ConcurrentAutoApprovedResourceUpdates(t *testing.T) {
	f := newTEST013Fixture(t)
	v, s, r := auditRPHResource(t, f)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.no-review."+f.modCode)
	var original string
	if err := f.db.QueryRow(f.ctx, `select snapshot::text from content_revisions where public_id=$1`, r.RevisionID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	lock, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(f.ctx)
	key := modContentAggregateResource + ":" + r.PublicID + ":" + v.PublicID
	if _, err = lock.Exec(f.ctx, `select pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
		t.Fatal(err)
	}
	type result struct {
		status int
		raw    []byte
		err    error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			edit := modContentResourceEdit{ResourcePublicID: r.PublicID, VersionPublicID: v.PublicID, SectionPublicID: &s.PublicID, EntryTypeCode: "default", DefaultLocale: "en-US", Definition: map[string]any{}, Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: fmt.Sprintf("Concurrent writer %d", i)}}, BaseRevisionID: &r.RevisionID, Reason: fmt.Sprintf("concurrent %d", i)}
			body, _ := json.Marshal(edit)
			req, e := http.NewRequestWithContext(f.ctx, http.MethodPut, f.origin.URL+"/api/v1/mods/test013-mod/content-resources/"+r.PublicID, bytes.NewReader(body))
			if e != nil {
				results <- result{err: e}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+f.editor)
			resp, e := f.origin.Client().Do(req)
			if e != nil {
				results <- result{err: e}
				return
			}
			defer resp.Body.Close()
			raw, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			results <- result{resp.StatusCode, raw, e}
		}(i)
	}
	deadline := time.Now().Add(10 * time.Second)
	waiting := 0
	for time.Now().Before(deadline) {
		if err = f.db.QueryRow(f.ctx, `select count(*) from pg_stat_activity where datname=current_database() and wait_event='advisory' and query like '%pg_advisory_xact_lock%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if waiting < 2 {
		t.Fatal("deterministic barrier not reached by both requests")
	}
	if err = lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	for i := 0; i < 2; i++ {
		x := <-results
		if x.err != nil {
			t.Fatal(x.err)
		}
		counts[x.status]++
		t.Logf("concurrent response: HTTP %d %s", x.status, x.raw)
	}
	var unchanged string
	var approved int
	if err = f.db.QueryRow(f.ctx, `select snapshot::text from content_revisions where public_id=$1`, r.RevisionID).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged != original {
		t.Error("historical snapshot mutated")
	}
	if err = f.db.QueryRow(f.ctx, `select count(*) from content_revisions v join change_requests r on r.proposed_revision_id=v.id where v.aggregate_type=$1 and v.aggregate_key=$2 and v.base_revision_id=(select id from content_revisions where public_id=$3) and r.status='approved'`, modContentAggregateResource, r.PublicID+":"+v.PublicID, r.RevisionID).Scan(&approved); err != nil {
		t.Fatal(err)
	}
	t.Logf("two requests held after initial base read; accepted approved children of same baseline=%d; statuses=%v; historical snapshot unchanged=%t", approved, counts, original == unchanged)
	if counts[200] != 1 || counts[409] != 1 || approved != 1 {
		t.Error("optimistic concurrency must reject one stale writer after publication advances")
	}
}
