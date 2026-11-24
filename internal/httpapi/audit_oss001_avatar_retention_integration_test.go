package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func auditOSS001ProfilePayload(t *testing.T, f auditFixGRFixture, kind, id, fileID, reference string) map[string]any {
	t.Helper()
	body := map[string]any{"baseRevisionId": auditOSS001PublishedCreatorBody(t, f, id)["baseRevisionId"], "kind": kind, "name": "Retained profile biography " + id, "descriptionMarkdown": "A requested ordinary biography update", "defaultLocale": "en-US", "localizations": []map[string]any{{"locale": "en-US", "contentMarkdown": "A requested ordinary biography update"}}, "avatarUrl": reference, "links": []any{}}
	if fileID != "" {
		body["avatarFileId"] = fileID
	}
	return body
}
func auditOSS001RetainedAvatarSetup(t *testing.T, f auditFixGRFixture, kind string) (string, string, string) {
	t.Helper()
	_, fileID := auditOSS001Upload(t, f, f.editor, "mod_resource:audit-gr-"+f.modCode+":item:icon", auditOSS001PNG(t, 130))
	var key string
	if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, fileID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	stable := ossStoredObjectURL(f.server.ossConfigFromSettings(f.ctx), key)
	id := auditOSS001CreateReviewedCreatorWithAvatar(t, f, kind, fileID, stable)
	return id, fileID, stable
}
func auditOSS001CreatorWriteFacts(t *testing.T, f auditFixGRFixture, id string) []byte {
	t.Helper()
	var raw []byte
	if err := f.db.QueryRow(f.ctx, `select jsonb_build_object('creator',(select jsonb_build_object('name',name,'avatarUrl',avatar_url,'avatarFileId',avatar_file_id,'published',published_revision_id) from creators where public_id=$1),'revisions',(select count(*) from content_revisions where aggregate_key=$1),'requests',(select count(*) from change_requests where aggregate_key=$1),'outbox',(select count(*) from activity_event_outbox where object_public_id=$1))`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestAuditOSS001LegacyStoredAvatarSurvivesUnrelatedEditHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"author", "team"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			id, _, stable := auditOSS001RetainedAvatarSetup(t, f, kind)
			// Explicit old-data fixture only: the creator was really created/reviewed and
			// the raster really uploaded. Historical versions may lack this FK. This
			// fixture mutation is not a production migration or new ownership proof.
			if _, err := f.db.Exec(f.ctx, `update creators set avatar_file_id=null where public_id=$1`, id); err != nil {
				t.Fatal(err)
			}
			response := decodeTEST022Data[struct {
				Revision string `json:"revisionId"`
				Status   string `json:"reviewStatus"`
			}](t, f.require(t, f.editor, http.MethodPut, "/api/v1/creators/"+id, auditOSS001ProfilePayload(t, f, kind, id, "", stable), 200))
			if response.Status != "pending" {
				t.Fatal("legacy unrelated edit must remain pending")
			}
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+response.Revision, map[string]any{"status": "approved", "note": "Preserve the unchanged legacy reference"}, 200)
			var stored string
			var fileID *int64
			if err := f.db.QueryRow(f.ctx, `select avatar_url,avatar_file_id from creators where public_id=$1`, id).Scan(&stored, &fileID); err != nil || stored != stable || fileID != nil {
				t.Fatal("unrelated approved edit cleared or rebound existing legacy avatar", err)
			}
			detail := decodeTEST022Data[struct {
				AvatarStorageURL string `json:"avatarStorageUrl"`
			}](t, f.require(t, f.editor, http.MethodGet, "/api/v1/creators/"+id, nil, 200))
			if detail.AvatarStorageURL != stable {
				t.Fatal("editor must receive explicit stored reference separately from signed presentation")
			}
		})
	}
}
func TestAuditOSS001DifferentEditorRetainsOnlyCurrentBoundAvatarHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"author", "team"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			id, fileID, stable := auditOSS001RetainedAvatarSetup(t, f, kind)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "creator.edit."+id)
			changed := decodeTEST022Data[struct {
				Revision string `json:"revisionId"`
				Status   string `json:"reviewStatus"`
			}](t, f.require(t, f.otherEditor, http.MethodPut, "/api/v1/creators/"+id, auditOSS001ProfilePayload(t, f, kind, id, fileID, stable), 200))
			if changed.Status != "pending" {
				t.Fatal("ordinary other editor must retain independent review")
			}
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+changed.Revision, map[string]any{"status": "approved", "note": "Unchanged currently bound avatar can survive another editor"}, 200)
			var current string
			if err := f.db.QueryRow(f.ctx, `select f.public_id from creators c join oss_files f on f.id=c.avatar_file_id where c.public_id=$1`, id).Scan(&current); err != nil || current != fileID {
				t.Fatal("current shared avatar changed unexpectedly", err)
			}
			_, foreignFile := auditOSS001Upload(t, f, f.editor, "mod_resource:audit-gr-"+f.modCode+":item:icon", auditOSS001PNG(t, 210))
			before := auditOSS001CreatorWriteFacts(t, f, id)
			body := auditOSS001ProfilePayload(t, f, kind, id, foreignFile, stable)
			body["name"] = "Reject unauthorized avatar replacement " + id
			raw := f.require(t, f.otherEditor, http.MethodPut, "/api/v1/creators/"+id, body, 400)
			var problem struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(raw, &problem); err != nil || problem.Error != "creator avatar file was not found" {
				t.Fatal("must reject exact foreign-file ownership instead of an unrelated validation or abuse error", problem.Error, err)
			}
			after := auditOSS001CreatorWriteFacts(t, f, id)
			if !bytes.Equal(before, after) {
				t.Fatal("foreign new file must not create revisions, requests, activity intent or change the current profile")
			}
		})
	}
}
