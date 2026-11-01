package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func auditCapabilityWrapperFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditImportActivityFacts(t, f)
	for _, table := range []string{"oss_user_upload_quota_reservations", "oss_multipart_sessions", "oss_download_stats", "creator_claim_attachments", "project_editor_application_attachments", "minecraft_server_proof_files", "minecraft_servers", "creator_claims", "project_editor_applications", "oss_upload_capabilities", "oss_rehome_jobs", "report_evidence", "reports", "report_snapshots", "mod_gallery_images", "blueprints", "blueprint_jobs", "blueprint_variants", "content_localizations", "content_subjects", "oss_user_quota_usage", "oss_user_daily_quota_usage"} {
		var text string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&text); err != nil {
			t.Fatal(err)
		}
		facts[table] = text
	}
	return facts
}

func auditCMP003WrapperReceiptRows(t *testing.T, f auditFixGRFixture) map[string]json.RawMessage {
	t.Helper()
	var raw []byte
	if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_object_agg(id::text,to_jsonb(binding)),'{}'::jsonb) from oss_upload_byte_bindings binding`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var rows map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func auditCMP003WrapperObjects(f auditFixGRFixture) map[string]test036Object {
	f.objectMu.Lock()
	defer f.objectMu.Unlock()
	result := make(map[string]test036Object, len(f.objects))
	for key, object := range f.objects {
		object.data = bytes.Clone(object.data)
		result[key] = object
	}
	return result
}

// A failed complete may retain only its exact private, unadopted byte-binding
// candidate and its durable compensation. Business/activity/quota facts and
// every pre-existing provider object, receipt and cleanup row stay unchanged.
func auditCMP003WrapperFailureCompensation(t *testing.T, f auditFixGRFixture, ticket test037Ticket, raw []byte, before map[string]string, receiptsBefore map[string]json.RawMessage, objectsBefore map[string]test036Object) {
	t.Helper()
	receiptsAfter := auditCMP003WrapperReceiptRows(t, f)
	var candidate string
	newReceipts := 0
	for id, row := range receiptsAfter {
		if prior, exists := receiptsBefore[id]; exists {
			if !bytes.Equal(prior, row) {
				t.Fatal("failed completion changed a pre-existing byte receipt")
			}
			continue
		}
		newReceipts++
		var exact bool
		if err := f.db.QueryRow(f.ctx, `select sealed_source_key,
 owner_id=$2 and bucket='audit-owned' and endpoint=$3 and file_endpoint=$3
 and upload_object_key=$4 and logical_category=$5 and source=$6 and original_name=$7
 and declared_sha256=$8 and declared_size_bytes=$9
 and status='cleanup_pending' and file_id is null and stored_object_key=sealed_source_key
 from oss_upload_byte_bindings where id=$1::uuid`, id, f.userIDs[f.editor], f.provider.URL, ticket.ObjectKey, ticket.Category, ticket.Source, ticket.OriginalName, ticket.SHA256, ticket.SizeBytes).Scan(&candidate, &exact); err != nil || !exact {
			t.Fatalf("failed completion lacks the exact current ticket's unadopted candidate: %v", err)
		}
		if !ownedOSSByteBindingCandidate(&ossUploadByteBinding{ID: id, SourceKey: candidate}, candidate) || candidate == ticket.ObjectKey {
			t.Fatal("compensation candidate escaped the current private attempt")
		}
		var exactIntent int
		if err := f.db.QueryRow(f.ctx, `select count(*) from oss_object_deletion_outbox where bucket='audit-owned' and endpoint=$1 and object_key=$2 and oss_file_id is null and raw_upload_binding_id is null and status='pending' and attempts=0 and reason='upload-byte-binding-not-adopted'`, f.provider.URL, candidate).Scan(&exactIntent); err != nil || exactIntent != 1 {
			t.Fatalf("failed complete candidate's exact compensating intent=%d err=%v", exactIntent, err)
		}
	}
	if len(receiptsAfter) != len(receiptsBefore)+1 || newReceipts != 1 {
		t.Fatal("failed completion removed old receipts or created extra attempts")
	}
	objectsAfter := auditCMP003WrapperObjects(f)
	if len(objectsAfter) != len(objectsBefore)+1 {
		t.Fatal("failed completion changed objects beyond its one private candidate")
	}
	for key, old := range objectsBefore {
		if current, exists := objectsAfter[key]; !exists || !reflect.DeepEqual(old, current) {
			t.Fatal("failed completion changed a pre-existing provider object")
		}
	}
	if original, exists := objectsAfter[ticket.ObjectKey]; !exists || !bytes.Equal(original.data, raw) {
		t.Fatal("failed completion changed the actual client source bytes")
	}
	if copied, exists := objectsAfter[candidate]; !exists || !reflect.DeepEqual(copied, objectsBefore[ticket.ObjectKey]) {
		t.Fatal("private candidate does not retain the exact source bytes and metadata")
	}
	// This shared assertion compares the entire original business fact map and
	// permits only exact owned pending candidate rows; it keeps all old outbox
	// rows byte-for-byte, including any unrelated cleanup.
	auditOSSAssertFailureOnlyCompensatesOwnCandidates(t, test037Fixture{test013Fixture: f.test013Fixture}, before)
	t.Logf("CMP003_WRAPPER_COMPENSATION ticket_category=%s source=%s exact_unadopted_receipt=1 exact_private_intent=1 actual_source_sha256=%s original_objects_and_all_business_facts_unchanged=true", ticket.Category, ticket.Source, sha256Hex(raw))
}

func TestAuditFixCMP003ImportUploadWrappersPreserveAtomicOriginalActivity(t *testing.T) {
	for _, scope := range []string{"catalog", "export"} {
		t.Run(scope, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			raw := auditFixGRZip(t, nil)
			name, kind, source := "wrapper.zip", "application/zip", "mcmods_exporter"
			if scope == "catalog" {
				raw = []byte("{\"Name\":\"Wrapper source\",\"RegisterName\":\"example:wrapper\",\"Type\":\"Item\"}\n")
				name = "wrapper.json"
				kind = "application/json"
				source = iconRendererImportSource
			}
			hash := sha256.Sum256(raw)
			request := ossDirectUploadRequest{OriginalName: name, ContentType: kind, Source: source, SizeBytes: int64(len(raw)), SHA256: hex.EncodeToString(hash[:])}
			base := "/api/v1/mods/audit-gr-" + f.modCode + "/" + scope + "-imports/uploads/"
			// The actual catalog route has the shared spelling catalog-imports;
			// exporter route retains its existing export-imports identity.
			before := auditCapabilityWrapperFacts(t, f.test013Fixture)
			f.require(t, f.otherEditor, http.MethodPost, base+"presign", request, 403)
			auditFixRequireProjectFactsUnchanged(t, before, auditCapabilityWrapperFacts(t, f.test013Fixture))
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			failed := f.require(t, f.editor, http.MethodPost, base+"presign", request, 500)
			auditFixRequireSpecificActivityError(t, failed, "failed to persist OSS upload ticket activity")
			auditFixRequireProjectFactsUnchanged(t, before, auditCapabilityWrapperFacts(t, f.test013Fixture))
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			prior := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionUpload, activity.ObjectMod)
			ticket := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, http.MethodPost, base+"presign", request, 200))
			if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionUpload, activity.ObjectMod); got != prior+1 {
				t.Fatalf("presign intents=%d prior=%d", got, prior)
			}
			put, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(raw))
			if err != nil {
				t.Fatal("prepare owned upload")
			}
			for k, v := range ticket.Headers {
				put.Header.Set(k, v)
			}
			res, err := f.provider.Client().Do(put)
			if err != nil {
				t.Fatal("owned upload unavailable")
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatalf("owned PUT status=%d", res.StatusCode)
			}
			before = auditCapabilityWrapperFacts(t, f.test013Fixture)
			f.require(t, f.otherEditor, http.MethodPost, base+"complete", ticket.completion(), 403)
			auditFixRequireProjectFactsUnchanged(t, before, auditCapabilityWrapperFacts(t, f.test013Fixture))
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			receiptsBefore := auditCMP003WrapperReceiptRows(t, f)
			objectsBefore := auditCMP003WrapperObjects(f)
			failed = f.require(t, f.editor, http.MethodPost, base+"complete", ticket.completion(), 500)
			auditFixRequireSpecificActivityError(t, failed, "failed to persist upload activity")
			auditCMP003WrapperFailureCompensation(t, f, ticket, raw, before, receiptsBefore, objectsBefore)
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			file := decodeTEST022Data[struct {
				ID string `json:"id"`
			}](t, f.require(t, f.editor, http.MethodPost, base+"complete", ticket.completion(), 201))
			var digest, key string
			var owner int64
			if err = f.db.QueryRow(f.ctx, `select sha256,object_key,uploader_id from oss_files where public_id=$1`, file.ID).Scan(&digest, &key, &owner); err != nil || owner != f.userIDs[f.editor] || digest != request.SHA256 {
				t.Fatalf("registered source owner/digest %d/%s err=%v", owner, digest, err)
			}
			actual, err := f.server.readLogOSSObject(f.ctx, key, int64(len(raw)), digest)
			if err != nil || !bytes.Equal(actual, raw) {
				t.Fatalf("registered source bytes differ err=%v", err)
			}
			if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionUpload, activity.ObjectMod); got != prior+2 {
				t.Fatalf("complete duplicated/changed activity %d prior=%d", got, prior)
			}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before = auditCapabilityWrapperFacts(t, f.test013Fixture)
			f.require(t, f.editor, http.MethodPost, base+"complete", ticket.completion(), 200)
			auditFixRequireProjectFactsUnchanged(t, before, auditCapabilityWrapperFacts(t, f.test013Fixture))
		})
	}
}

func TestAuditFixCMP003ReviewAttachmentWrappersPersistBeforeSignedDisclosure(t *testing.T) {
	for _, scope := range []string{"creator-claim", "editor-application", "server-proof"} {
		t.Run(scope, func(t *testing.T) {
			f := newTEST037Fixture(t)
			actor := f.editor
			if scope == "editor-application" {
				actor = f.otherEditor
			}
			source := auditFixGRZip(t, nil)
			ticket := f.ticket(t, actor, "/api/v1/users/me/oss/uploads/presign", "review-proof.zip", "application/zip", "test037-review-proof", source)
			f.put(t, ticket, source)
			file := decodeTEST022Data[struct {
				ID string `json:"id"`
			}](t, auditOSSRequire(t, f, actor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)).ID
			// Ordinary upload completion leaves the safety scan pending. Exercise
			// the real authorized scan endpoint before binding review evidence.
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "oss.write")
			auditOSSRequire(t, f, f.denied, http.MethodPatch, "/api/v1/admin/oss/files/"+file+"/scan", map[string]string{"status": "clean", "note": "Owned synthetic evidence safety decision"}, 200)
			path, permission := "", ""
			switch scope {
			case "creator-claim":
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[actor], "creator.claim")
				var creator string
				if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values('author','Wrapper proof author','wrapper proof author','approved') returning public_id`).Scan(&creator); err != nil {
					t.Fatal(err)
				}
				claim := decodeTEST022Data[struct {
					ID string `json:"id"`
				}](t, auditOSSRequire(t, f, actor, http.MethodPost, "/api/v1/creators/"+creator+"/claims", map[string]any{"proofMarkdown": "Owned real uploaded proof", "proofFileIds": []string{file}}, 201))
				path = "/api/v1/admin/creator-claims/" + claim.ID + "/attachments/" + file + "/presign"
				permission = "creator.claim.review"
			case "editor-application":
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[actor], "project.editor.apply")
				application := decodeTEST022Data[projectEditorApplicationResponse](t, auditOSSRequire(t, f, actor, http.MethodPost, "/api/v1/projects/mod/"+f.modCode+"/editor-applications", createProjectEditorApplicationRequest{ProofMarkdown: "Owned real uploaded proof", AttachmentIDs: []string{file}}, 201))
				path = "/api/v1/admin/project-editor-applications/" + application.ID + "/attachments/" + file + "/presign"
				permission = "project.editor.review"
			case "server-proof":
				// A typed pending-review stage isolates this capability wrapper; the
				// actual server create/probe journey is separately covered by TEST015.
				var server int64
				var public string
				if err := f.db.QueryRow(f.ctx, `insert into minecraft_servers(slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,submitted_by) values('wrapper-proof','wrapper.invalid','wrapper.invalid:25565','wrapper.invalid','wrapper.invalid',25565,'Wrapper proof server','survival',$1) returning id,public_id`, f.userIDs[actor]).Scan(&server, &public); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Exec(f.ctx, `insert into minecraft_server_proof_files(server_id,oss_file_id) select $1,id from oss_files where public_id=$2`, server, file); err != nil {
					t.Fatal(err)
				}
				path = "/api/v1/admin/server-reviews/" + public + "/attachments/" + file + "/presign"
				permission = "server.review"
			}
			unboundSource := append(append([]byte(nil), source...), '\n')
			unboundTicket := f.ticket(t, actor, "/api/v1/users/me/oss/uploads/presign", "unbound-proof.zip", "application/zip", "test037-review-proof", unboundSource)
			f.put(t, unboundTicket, unboundSource)
			unbound := decodeTEST022Data[struct {
				ID string `json:"id"`
			}](t, auditOSSRequire(t, f, actor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", unboundTicket.completion(), 201)).ID
			auditOSSRequire(t, f, f.denied, http.MethodPatch, "/api/v1/admin/oss/files/"+unbound+"/scan", map[string]string{"status": "clean", "note": "Owned unbound evidence safety decision"}, 200)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], permission)
			before := auditCapabilityWrapperFacts(t, f.test013Fixture)
			auditOSSRequire(t, f, actor, http.MethodPost, path, nil, 403)
			auditOSSRequire(t, f, f.reviewer, http.MethodPost, strings.Replace(path, "/attachments/"+file+"/", "/attachments/"+unbound+"/", 1), nil, 404)
			auditFixRequireProjectFactsUnchanged(t, before, auditCapabilityWrapperFacts(t, f.test013Fixture))
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.reviewer], true)
			failed := auditOSSRequire(t, f, f.reviewer, http.MethodPost, path, nil, 500)
			auditFixRequireSpecificActivityError(t, failed, "failed to persist OSS download")
			auditFixRequireProjectFactsUnchanged(t, before, auditCapabilityWrapperFacts(t, f.test013Fixture))
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.reviewer], false)
			prior := auditFixActivityIntentCount(t, f.test013Fixture, f.reviewer, activity.ActionCreate, activity.ObjectReview)
			result := decodeTEST022Data[struct {
				URL string `json:"url"`
			}](t, auditOSSRequire(t, f, f.reviewer, http.MethodPost, path, nil, 200))
			if got := auditFixActivityIntentCount(t, f.test013Fixture, f.reviewer, activity.ActionCreate, activity.ObjectReview); got != prior+1 {
				t.Fatalf("review wrapper activity=%d prior=%d", got, prior)
			}
			get, err := http.NewRequestWithContext(f.ctx, http.MethodGet, result.URL, nil)
			if err != nil {
				t.Fatal("prepare owned signed capability")
			}
			res, err := f.provider.Client().Do(get)
			if err != nil {
				t.Fatal("owned signed capability unavailable")
			}
			body, readErr := io.ReadAll(io.LimitReader(res.Body, int64(len(source)+1)))
			res.Body.Close()
			if res.StatusCode != 200 || readErr != nil || !bytes.Equal(body, source) {
				t.Fatalf("signed source bytes/status changed status=%d err=%v", res.StatusCode, readErr)
			}
			auditOSSRequire(t, f, f.denied, http.MethodPatch, "/api/v1/admin/oss/files/"+file+"/scan", map[string]string{"status": "pending", "note": "Owned rescan state"}, 200)
			before = auditCapabilityWrapperFacts(t, f.test013Fixture)
			auditOSSRequire(t, f, f.reviewer, http.MethodPost, path, nil, 404)
			auditFixRequireProjectFactsUnchanged(t, before, auditCapabilityWrapperFacts(t, f.test013Fixture))
		})
	}
}
