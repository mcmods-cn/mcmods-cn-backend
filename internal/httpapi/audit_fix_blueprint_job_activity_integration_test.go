package httpapi

import (
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func TestAuditFixCMP003BlueprintConversionRetryActivitySharesJobOutboxTransaction(t *testing.T) {
	for _, operation := range []string{"convert", "retry"} {
		t.Run(operation, func(t *testing.T) {
			f := newTEST036Fixture(t)
			raw, _, err := encodeBlueprint(test036Document(), "nbt")
			if err != nil {
				t.Fatal(err)
			}
			upload := f.upload(t, f.editor, "atomic-job.nbt", raw)
			worker := f.worker("atomic-job")
			path := "/api/v1/blueprints/" + upload.publicID + "/" + operation
			var body any
			actor := f.editor
			if operation == "convert" {
				if err = worker.processJob(f.ctx, upload.jobID); err != nil {
					t.Fatal(err)
				}
				f.approve(t, upload.publicID)
				actor = f.otherEditor
				body = map[string]string{"format": "schem"}
			} else {
				f.failGET.Store(true)
				for range 3 {
					if err = worker.processJob(f.ctx, upload.jobID); err == nil {
						t.Fatal("controlled actual source GET503 did not fail worker")
					}
				}
				f.failGET.Store(false)
				var state string
				if err = f.db.QueryRow(f.ctx, `select status from blueprints where public_id=$1`, upload.publicID).Scan(&state); err != nil || state != "failed" {
					t.Fatalf("retry source state=%s err=%v", state, err)
				}
			}
			prior := auditFixActivityIntentCount(t, f.test013Fixture, actor, activity.ActionCreate, activity.ObjectBlueprint)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[actor], true)
			before := f.facts(t)
			response := f.require(t, actor, http.MethodPost, path, body, 500)
			auditFixRequireSpecificActivityError(t, response, "failed to persist blueprint activity")
			f.unchanged(t, before)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[actor], false)
			queued := decodeTEST022Data[map[string]string](t, f.require(t, actor, http.MethodPost, path, body, 202))
			if count := auditFixActivityIntentCount(t, f.test013Fixture, actor, activity.ActionCreate, activity.ObjectBlueprint); count != prior+1 {
				t.Fatalf("job activity=%d prior=%d", count, prior)
			}
			var job int64
			var intents int
			if err = f.db.QueryRow(f.ctx, `select id,(select count(*) from nats_outbox where aggregate_id=job.public_id and event_type='blueprint.conversion.requested') from blueprint_jobs job where public_id=$1 and status='queued'`, queued["jobId"]).Scan(&job, &intents); err != nil || intents != 1 {
				t.Fatalf("job/outbox id=%d intents=%d err=%v", job, intents, err)
			}
			if err = worker.processJob(f.ctx, job); err != nil {
				t.Fatal(err)
			}
			if operation == "retry" {
				f.approve(t, upload.publicID)
			}
			f.require(t, "", http.MethodGet, "/api/v1/blueprints/"+upload.publicID, nil, 200)
		})
	}
}
