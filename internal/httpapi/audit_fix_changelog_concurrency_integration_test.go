package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type auditCompletionHTTPResult struct {
	status int
	raw    []byte
	err    error
}

func auditCompletionAsyncJSON(f test013Fixture, token, method, path string, body any) <-chan auditCompletionHTTPResult {
	out := make(chan auditCompletionHTTPResult, 1)
	go func() {
		b, e := json.Marshal(body)
		if e != nil {
			out <- auditCompletionHTTPResult{err: e}
			return
		}
		req, e := http.NewRequestWithContext(f.ctx, method, f.origin.URL+path, bytes.NewReader(b))
		if e != nil {
			out <- auditCompletionHTTPResult{err: e}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		res, e := f.origin.Client().Do(req)
		if e != nil {
			out <- auditCompletionHTTPResult{err: e}
			return
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, 32768))
		out <- auditCompletionHTTPResult{status: res.StatusCode, raw: raw, err: e}
	}()
	return out
}
func auditCompletionAwaitLocks(t *testing.T, f test013Fixture, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		if err := f.db.QueryRow(f.ctx, "select count(*) from pg_stat_activity where datname=current_database() and wait_event_type='Lock'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("owned concurrent requests reached %d locks; expected %d", n, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestAuditFixRPH001NineChangelogSameBaseConcurrentPublicationIntegration(t *testing.T) {
	for _, mode := range []string{"review-required", "review-disabled", "no-review"} {
		for _, writers := range []string{"two-tabs", "two-editors"} {
			f := newTEST017Fixture(t)
			for _, kind := range []string{"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon", "minecraft_server"} {
				t.Run(mode+"/"+writers+"/"+kind, func(t *testing.T) {
					code := f.modCode
					if kind == "modpack" {
						if err := f.db.QueryRow(f.ctx, "insert into modpacks(slug,primary_name,review_status,submitted_by) values('audit-cas-pack','Audit CAS Pack','approved',$1) returning public_id", f.userIDs[f.editor]).Scan(&code); err != nil {
							t.Fatal(err)
						}
					} else if kind == "minecraft_server" {
						if err := f.db.QueryRow(f.ctx, "insert into minecraft_servers(slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,review_status,submitted_by) values('audit-cas-server','audit-cas.invalid:25565','audit-cas.invalid:25565','audit-cas.invalid','203.0.113.17',25565,'Audit CAS Server','survival','approved',$1) returning public_id", f.userIDs[f.editor]).Scan(&code); err != nil {
							t.Fatal(err)
						}
					} else if kind != "mod" {
						if err := f.db.QueryRow(f.ctx, "insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by) values($1,$2,$2,'approved',$3) returning public_id", kind, "audit-cas-"+kind, f.userIDs[f.editor]).Scan(&code); err != nil {
							t.Fatal(err)
						}
					}
					grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+code)
					queryKind := strings.ReplaceAll(kind, "_", "-")
					if kind == "minecraft_server" {
						queryKind = "server"
					}
					proposal := f.submit(t, f.editor, http.MethodPost, "/api/v1/changelogs?targetType="+queryKind+"&targetId="+code, test017Snapshot("Original "+kind), 201)
					f.review(t, f.reviewer, proposal, "approved", 200)
					f.assertResolution(t, proposal, f.reviewer, "approved")
					path := "/api/v1/changelogs/" + proposal.ID
					for _, actor := range []string{f.editor, f.otherEditor} {
						grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[actor], "project.edit."+code)
						if mode == "no-review" {
							grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[actor], "project.no-review."+code)
						}
						if item := f.detail(t, actor, proposal.ID, 200); item.BodyMarkdown != "Original "+kind {
							t.Fatal("editors did not start with the same published content")
						}
					}
					if mode == "review-disabled" {
						cfg := defaultReviewConfig()
						cfg.ChangelogCreate = true
						cfg.ChangelogEdit = false
						raw, _ := json.Marshal(cfg)
						if _, err := f.db.Exec(f.ctx, `update system_settings set value=$2::jsonb where key=$1`, reviewConfigSettingKey, raw); err != nil {
							t.Fatal(err)
						}
					}
					var id, baseRevision int64
					if err := f.db.QueryRow(f.ctx, "select id,published_revision_id from project_changelogs where public_id=$1", proposal.ID).Scan(&id, &baseRevision); err != nil {
						t.Fatal(err)
					}
					tx, err := f.db.Begin(f.ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(context.Background())
					if _, err = tx.Exec(f.ctx, "select id from project_changelogs where id=$1 for update", id); err != nil {
						t.Fatal(err)
					}
					editA := test017Snapshot("Editor A " + kind)
					editA.BaseRevisionID = f.detail(t, f.editor, proposal.ID, 200).PublishedRevisionID
					editB := test017Snapshot("Editor B " + kind)
					editB.BaseRevisionID = editA.BaseRevisionID
					a := auditCompletionAsyncJSON(f.test013Fixture, f.editor, http.MethodPut, path, editA)
					tokenB := f.editor
					if writers == "two-editors" {
						tokenB = f.otherEditor
					}
					b := auditCompletionAsyncJSON(f.test013Fixture, tokenB, http.MethodPut, path, editB)
					auditCompletionAwaitLocks(t, f.test013Fixture, 2)
					if err = tx.Commit(f.ctx); err != nil {
						t.Fatal(err)
					}
					ra, rb := <-a, <-b
					if ra.err != nil || rb.err != nil {
						t.Fatal("concurrent request transport failure")
					}
					var branches int
					if err = f.db.QueryRow(f.ctx, "select count(*) from content_revisions r join change_requests q on q.proposed_revision_id=r.id where r.entity_type='project_changelog' and r.entity_id=$1 and q.base_revision_id=$2 ", id, baseRevision).Scan(&branches); err != nil {
						t.Fatal(err)
					}
					if mode == "review-required" {
						winner := ra.raw
						if rb.status == 200 {
							winner = rb.raw
						}
						var value struct {
							Data struct {
								RequestID string `json:"changeRequestId"`
							} `json:"data"`
						}
						if err = json.Unmarshal(winner, &value); err != nil {
							t.Fatal(err)
						}
						var revisionID string
						if err = f.db.QueryRow(f.ctx, `select revision.public_id from content_revisions revision join change_requests request on request.proposed_revision_id=revision.id where request.public_id=$1`, value.Data.RequestID).Scan(&revisionID); err != nil {
							t.Fatal(err)
						}
						f.review(t, f.reviewer, test017Mutation{ID: proposal.ID, RequestID: value.Data.RequestID, RevisionID: revisionID}, "approved", 200)
					}
					published := f.detail(t, "", proposal.ID, 200)
					t.Logf("kind=%s statuses=%d/%d approvedChildrenOfSameBase=%d finalBody=%q", kind, ra.status, rb.status, branches, published.BodyMarkdown)
					stale := editA
					stale.Localizations[0].BodyMarkdown = "stale retry"
					f.require(t, f.editor, http.MethodPut, path, stale, 409)
					fresh := test017Snapshot("Fresh baseline retry")
					fresh.BaseRevisionID = published.PublishedRevisionID
					f.require(t, f.editor, http.MethodPut, path, fresh, 200)
					if !((ra.status == 200 && rb.status == 409) || (ra.status == 409 && rb.status == 200)) || branches != 1 {
						t.Errorf("same-base edits must not both publish: statuses=%d/%d approvedChildren=%d", ra.status, rb.status, branches)
					}
				})
			}
		}

	}
}
