package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func auditCapabilityWrapperFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditImportActivityFacts(t, f)
	for _, table := range []string{"oss_user_upload_quota_reservations", "oss_multipart_sessions", "oss_download_stats", "creator_claim_attachments", "project_editor_application_attachments", "minecraft_server_proof_files", "minecraft_servers", "creator_claims", "project_editor_applications"} {
		var text string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&text); err != nil {
			t.Fatal(err)
		}
		facts[table] = text
	}
	return facts
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
			failed = f.require(t, f.editor, http.MethodPost, base+"complete", ticket.completion(), 500)
			auditFixRequireSpecificActivityError(t, failed, "failed to persist upload activity")
			auditFixRequireProjectFactsUnchanged(t, before, auditCapabilityWrapperFacts(t, f.test013Fixture))
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
