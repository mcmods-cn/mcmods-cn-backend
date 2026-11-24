package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func auditOSS001PublishedCreatorBody(t *testing.T, f auditFixGRFixture, id string) map[string]any {
	t.Helper()
	var raw []byte
	var revisionID string
	if err := f.db.QueryRow(f.ctx, `select revision.snapshot,revision.public_id from creators creator join content_revisions revision on revision.id=creator.published_revision_id where creator.public_id=$1`, id).Scan(&raw, &revisionID); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	body["baseRevisionId"] = revisionID
	return body
}

func TestAuditOSS001DifferentAvatarSameTextIsBusinessChangeAndExactRepeatIsRejectedHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"author", "team"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			id, oldFile, _ := auditOSS001RetainedAvatarSetup(t, f, kind)
			data := auditOSS001PNG(t, 200)
			source := "creator_avatar:" + kind + ":" + id
			_, newFile := auditOSS001Upload(t, f, f.editor, source, data)
			if newFile == oldFile {
				t.Fatal("must use a distinct actual avatar, not alter the text or add a nonce")
			}
			repeat := decodeTEST022Data[struct {
				UploadRequired bool `json:"uploadRequired"`
				File           struct {
					ID string `json:"id"`
				} `json:"file"`
			}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", auditOSS001PresignBody(data, source), 200))
			if repeat.UploadRequired || repeat.File.ID != newFile {
				t.Fatal("same bytes/source/uploader must retain the real file identity instead of random dedup salt")
			}
			body := auditOSS001PublishedCreatorBody(t, f, id)
			body["avatarFileId"] = newFile
			var key string
			if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, newFile).Scan(&key); err != nil {
				t.Fatal(err)
			}
			body["avatarUrl"] = ossStoredObjectURL(f.server.ossConfigFromSettings(f.ctx), key)
			changed := decodeTEST022Data[struct {
				Revision string `json:"revisionId"`
				Status   string `json:"reviewStatus"`
			}](t, f.require(t, f.editor, http.MethodPut, "/api/v1/creators/"+id, body, 200))
			if changed.Status != "pending" || changed.Revision == "" {
				t.Fatal("actual avatar change must retain independent review")
			}
			before := auditOSS001CreatorWriteFacts(t, f, id)
			raw := f.require(t, f.editor, http.MethodPut, "/api/v1/creators/"+id, body, 403)
			var problem struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(raw, &problem); err != nil || problem.Code != "duplicate_content" {
				t.Fatal("same normalized avatar and same text must hit the exact duplicate gate", err, problem.Code)
			}
			after := auditOSS001CreatorWriteFacts(t, f, id)
			if !bytes.Equal(before, after) {
				t.Fatal("duplicate must not create a second revision/request/activity intent")
			}
		})
	}
}

func TestAuditOSS001AvatarReferenceNeverMakesInvalidOrForeignFilesBindableHTTPPostgresIntegration(t *testing.T) {
	for _, variant := range []string{"invalid", "foreign"} {
		t.Run(variant, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			id, _, stable := auditOSS001RetainedAvatarSetup(t, f, "author")
			fileID := "zzzzzzzzz"
			if variant == "foreign" {
				_, fileID = auditOSS001Upload(t, f, f.otherEditor, "mod_resource:audit-gr-"+f.otherModCode+":item:icon", auditOSS001PNG(t, 220))
			}
			body := auditOSS001PublishedCreatorBody(t, f, id)
			body["avatarFileId"] = fileID
			body["avatarUrl"] = stable
			before := auditOSS001CreatorWriteFacts(t, f, id)
			raw := f.require(t, f.editor, http.MethodPut, "/api/v1/creators/"+id, body, 400)
			var problem struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(raw, &problem); err != nil || problem.Error != "creator avatar file was not found" {
				t.Fatal("must reach exact trusted-raster ownership rejection, not an abuse or SQL error", err, problem.Error)
			}
			after := auditOSS001CreatorWriteFacts(t, f, id)
			if !bytes.Equal(before, after) {
				t.Fatal("registered but invalid/unowned avatar must cause zero creator/revision/request/activity writes")
			}
		})
	}
}
