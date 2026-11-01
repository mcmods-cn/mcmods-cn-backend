package httpapi

import (
	"bytes"
	"encoding/xml"
	"io"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"strings"
	"testing"
)

// Exercise the corrected provider protocol through an actual normal blueprint
// upload, then prove wrong source/private-target/overwrite requests cannot
// mutate its bytes. This does not substitute an API response or bypass sealing.
func TestAuditTEST036OwnedCopyProtocolPreservesActualBytesAndRejectsWrongTargetsIntegration(t *testing.T) {
	f := newTEST036Fixture(t)
	raw, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "copy-protocol.nbt", raw)
	var originalKey, sealedKey string
	if err = f.db.QueryRow(f.ctx, `select binding.upload_object_key,binding.sealed_source_key from blueprints blueprint join oss_upload_byte_bindings binding on binding.file_id=blueprint.original_file_id where blueprint.public_id=$1 and binding.owner_id=$2 and binding.status='committed'`, upload.publicID, f.userIDs[f.editor]).Scan(&originalKey, &sealedKey); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]test036Object {
		f.mu.Lock()
		defer f.mu.Unlock()
		result := make(map[string]test036Object, len(f.objects))
		for key, object := range f.objects {
			object.data = bytes.Clone(object.data)
			result[key] = object
		}
		return result
	}
	beforeObjects := snapshot()
	if !bytes.Equal(beforeObjects["/"+originalKey].data, raw) || !reflect.DeepEqual(beforeObjects["/"+originalKey], beforeObjects["/"+sealedKey]) {
		t.Fatal("normal production completion did not copy actual source bytes and metadata into its own sealed key")
	}
	f.mu.Lock()
	initialCopies := append([]test036CopyRequest(nil), (*f.copyRequests)...)
	f.mu.Unlock()
	if len(initialCopies) != 1 || initialCopies[0].Source != "/test036-owned/"+originalKey || initialCopies[0].Destination != "/"+sealedKey || initialCopies[0].ForbidOverwrite != "true" || initialCopies[0].BodyBytes != 0 || initialCopies[0].Bytes != len(raw) || initialCopies[0].SHA256 != sha256Hex(raw) {
		t.Fatal("actual SDK copy did not use the exact source/private destination/overwrite contract")
	}
	newTarget := path.Join(path.Dir(originalKey), ossSealedUploadComponent, newExportID(), path.Base(originalKey))
	beforeFacts := f.facts(t)
	for _, tc := range []struct {
		name, source, target, forbid string
		status                       int
		code                         string
		body                         string
	}{
		{"foreign-bucket", "/foreign-owned/" + originalKey, newTarget, "true", 400, "InvalidArgument", ""},
		{"missing-source", "/test036-owned/" + path.Join(path.Dir(originalKey), "missing.nbt"), path.Join(path.Dir(originalKey), ossSealedUploadComponent, newExportID(), "missing.nbt"), "true", 404, "NoSuchKey", ""},
		{"outside-private-attempt", "/test036-owned/" + originalKey, path.Join(path.Dir(originalKey), "unreserved.nbt"), "true", 400, "InvalidArgument", ""},
		{"different-source-directory", "/test036-owned/" + originalKey, path.Join("foreign", ossSealedUploadComponent, newExportID(), path.Base(originalKey)), "true", 400, "InvalidArgument", ""},
		{"overwrite-not-forbidden", "/test036-owned/" + originalKey, newTarget, "", 400, "InvalidArgument", ""},
		{"existing-private-target", "/test036-owned/" + originalKey, sealedKey, "true", 409, "FileAlreadyExists", ""},
		{"nonempty-copy-body", "/test036-owned/" + originalKey, newTarget, "true", 400, "InvalidArgument", "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(f.ctx, http.MethodPut, f.provider.URL+"/test036-owned/"+tc.target, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("x-oss-copy-source", url.PathEscape(tc.source))
			req.Header.Set("x-oss-forbid-overwrite", tc.forbid)
			res, err := f.provider.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(io.LimitReader(res.Body, 4097))
			res.Body.Close()
			var failure struct {
				Code string `xml:"Code"`
			}
			if readErr != nil || len(body) > 4096 || res.StatusCode != tc.status || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/xml") || xml.Unmarshal(body, &failure) != nil || failure.Code != tc.code {
				t.Fatalf("owned copy boundary status=%d want=%d code=%s want=%s read=%v", res.StatusCode, tc.status, failure.Code, tc.code, readErr)
			}
			if !reflect.DeepEqual(snapshot(), beforeObjects) {
				t.Fatal("rejected copy mutated a pre-existing object or created another target")
			}
			f.unchanged(t, beforeFacts)
		})
	}
}

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
