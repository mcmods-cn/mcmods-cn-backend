package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

type auditGR004QueueEnvelope struct {
	Data struct {
		Items  []modContentReviewItem `json:"items"`
		Total  int                    `json:"total"`
		Facets map[string][]struct {
			Value string `json:"value"`
			Count int    `json:"count"`
		} `json:"facets"`
	} `json:"data"`
}

func auditGR004RequireQueueTotals(t *testing.T, response auditGR004QueueEnvelope, want int) {
	t.Helper()
	if response.Data.Total != want || len(response.Data.Items) != want {
		t.Fatalf("queue total/items=%d/%d want%d", response.Data.Total, len(response.Data.Items), want)
	}
	for _, key := range []string{"categories", "operations", "projectTypes"} {
		rows, exists := response.Data.Facets[key]
		if !exists || rows == nil {
			t.Fatalf("queue omitted exact empty facet array %s", key)
		}
		count := 0
		for _, row := range rows {
			count += row.Count
		}
		if count != want || len(rows) != want {
			t.Fatalf("queue facet %s rows=%d/count=%d want%d", key, len(rows), count, want)
		}
	}
}

// Queue visibility, pending payload permission and the decision endpoint must
// agree for each currently authorized scope. Independent fixtures keep the
// ordinary write budget and published state of each role unrelated.
func TestAuditGR004IndependentQueueReadDecisionRoleMatrixHTTPPostgresIntegration(t *testing.T) {
	for _, role := range []string{"anonymous", "ordinary", "readonly", "creator", "content-review", "project-review", "scoped-project-review", "other-project-review", "scoped-self-review", "global-self-review", "admin", "revoked-content-review"} {
		t.Run(role, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			_, revision := f.imported(t, auditFixGRZip(t, nil))
			actor := ""
			actorID := int64(0)
			if role == "creator" || role == "scoped-self-review" || role == "global-self-review" {
				actor = f.editor
				actorID = f.userIDs[f.editor]
			} else if role != "anonymous" {
				actorID, _, actor = createTEST044User(t, f.ctx, f.db, f.server.cfg, fmt.Sprintf("gr004-%s-%d", role, time.Now().UnixNano()), "UTC")
				if _, e := f.db.Exec(f.ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, actorID); e != nil {
					t.Fatal(e)
				}
			}
			perms := map[string][]string{"readonly": {"global_resource.list", "global_resource.view"}, "content-review": {"content.review"}, "project-review": {"project.review"}, "scoped-project-review": {"project.review." + f.modCode}, "other-project-review": {"project.review." + f.otherModCode}, "scoped-self-review": {"project.review." + f.modCode}, "global-self-review": {"content.review"}, "admin": {"admin.*"}, "revoked-content-review": {"content.review"}}
			if len(perms[role]) > 0 {
				grantTEST044Permissions(t, f.ctx, f.db, actorID, perms[role]...)
				// The creator authenticated during upload. Publish this grant just
				// as the authorization endpoint does after its database commit.
				if err := f.server.refreshPermissionVersion(f.ctx, actorID); err != nil {
					t.Fatal(err)
				}
			}
			canDecide := role == "content-review" || role == "project-review" || role == "scoped-project-review" || role == "global-self-review" || role == "admin" || role == "revoked-content-review"
			canQueue := canDecide || role == "other-project-review" || role == "scoped-self-review"
			queueStatus := 403
			if canQueue {
				queueStatus = 200
			}
			if role == "anonymous" {
				queueStatus = 401
			}
			readStatus := 403
			if canDecide || role == "creator" || role == "scoped-self-review" {
				readStatus = 200
			}
			raw := f.require(t, actor, http.MethodGet, "/api/v1/export-revisions/"+revision, nil, readStatus)
			if readStatus == 200 {
				var envelope struct {
					Data struct {
						CanReview bool `json:"canReview"`
					} `json:"data"`
				}
				if e := json.Unmarshal(raw, &envelope); e != nil {
					t.Fatal(e)
				}
				t.Logf("GR004_DETAIL role=%s canReview=%v", role, envelope.Data.CanReview)
				if envelope.Data.CanReview != canDecide {
					t.Fatalf("detail canReview=%v role=%s want=%v", envelope.Data.CanReview, role, canDecide)
				}
			}
			queue := f.require(t, actor, http.MethodGet, "/api/v1/admin/mod-content-reviews?category=catalog_import&limit=100", nil, queueStatus)
			if canQueue {
				var envelope auditGR004QueueEnvelope
				if e := json.Unmarshal(queue, &envelope); e != nil {
					t.Fatal(e)
				}
				expected := 0
				if canDecide {
					expected = 1
				}
				auditGR004RequireQueueTotals(t, envelope, expected)
				found := false
				for _, item := range envelope.Data.Items {
					if item.ID == revision {
						found = true
						if item.ReviewURL != "/api/v1/admin/export-revisions/"+revision+"/activate" {
							t.Fatal("wrong exact decision target")
						}
					}
				}
				if found != canDecide {
					t.Fatalf("queue and decision scope disagree: role=%s queued=%v canDecide=%v total=%d", role, found, canDecide, envelope.Data.Total)
				}
			}
			if role == "revoked-content-review" {
				if _, e := f.db.Exec(f.ctx, `update user_permissions set allow=false where user_id=$1 and permission_id=(select id from permissions where code='content.review')`, actorID); e != nil {
					t.Fatal(e)
				}
				if e := f.server.refreshPermissionVersion(f.ctx, actorID); e != nil {
					t.Fatal(e)
				}
				f.require(t, actor, http.MethodGet, "/api/v1/admin/mod-content-reviews?category=catalog_import", nil, 403)
				f.require(t, actor, http.MethodGet, "/api/v1/export-revisions/"+revision, nil, 403)
				canDecide = false
			}
			before := auditImportActivityFacts(t, f.test013Fixture)
			status := 403
			if canDecide {
				status = 200
			}
			if role == "anonymous" {
				status = 401
			}
			f.require(t, actor, http.MethodPatch, "/api/v1/admin/export-revisions/"+revision+"/activate", map[string]string{"status": "approved", "note": "Independent current-role matrix"}, status)
			if !canDecide {
				if after := auditImportActivityFacts(t, f.test013Fixture); !reflect.DeepEqual(before, after) {
					t.Fatal("denied decision changed task, source, projection or outbox")
				}
			} else {
				if f.number(t, `select count(*) from catalog_import_revisions where id=$1 and is_active and status='ready'`, revision) != 1 || f.number(t, `select count(*) from audit_events where aggregate_key=$1 and action='review_approved'`, revision) != 1 {
					t.Fatal("allowed role failed to publish exact one decision")
				}
			}
		})
	}
}
