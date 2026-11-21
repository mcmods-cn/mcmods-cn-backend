package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"testing"
)

// All creator/review facts in these regressions use actual HTTP. No approved
// creator or avatar binding is seeded directly. OSS transport remains the
// task-owned protocol service; this is not a cloud-provider verification.
func auditOSS001PNG(t *testing.T, red uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, color.NRGBA{red, uint8(x * 8), uint8(y * 8), 200})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func auditOSS001PresignBody(data []byte, source string) map[string]any {
	sum := sha256.Sum256(data)
	return map[string]any{"originalName": "avatar.png", "contentType": "image/png", "sizeBytes": len(data), "sha256": hex.EncodeToString(sum[:]), "source": source, "category": source}
}

type auditOSS001UploadedFile struct {
	ID           string `json:"id"`
	ScanStatus   string `json:"scanStatus"`
	StoredSHA256 string `json:"storedSha256"`
}

func auditOSS001UploadTransport(t *testing.T, f auditFixGRFixture, actor, source string, data []byte) (test037Ticket, auditOSS001UploadedFile) {
	t.Helper()
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, actor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", auditOSS001PresignBody(data, source), 200))
	if !ticket.UploadRequired || ticket.Method != http.MethodPut {
		t.Fatal("must exercise ordinary real PUT instead of substituted completion")
	}
	req, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range ticket.Headers {
		req.Header.Set(k, v)
	}
	response, err := f.provider.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("actual owned OSS PUT status", response.StatusCode)
	}
	result := decodeTEST022Data[auditOSS001UploadedFile](t, f.require(t, actor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	t.Logf("actual completion facts publicIdPresent=%t scanStatus=%q storedHashLength=%d", result.ID != "", result.ScanStatus, len(result.StoredSHA256))
	if result.ID == "" || len(result.StoredSHA256) != 64 {
		t.Fatal("actual completed raster identity missing")
	}
	return ticket, result
}

func auditOSS001Upload(t *testing.T, f auditFixGRFixture, actor, source string, data []byte) (test037Ticket, string) {
	t.Helper()
	ticket, result := auditOSS001UploadTransport(t, f, actor, source, data)
	if result.ScanStatus != "clean" {
		t.Fatal("creator-bindable avatar must have actual synchronous raster validation, got", result.ScanStatus)
	}
	return ticket, result.ID
}

func auditOSS001CreateReviewedCreator(t *testing.T, f auditFixGRFixture, kind string) string {
	t.Helper()
	return auditOSS001CreateReviewedCreatorWithAvatar(t, f, kind, "", "")
}

// Some avatar scenarios authenticate this actor before seeding a grant or
// revoking it. Publish the committed versions as updateUserPermissions does.
func auditOSS001PublishFixturePermissions(t *testing.T, f auditFixGRFixture, actor string) {
	t.Helper()
	if err := errors.Join(f.server.refreshPermissionVersion(f.ctx, f.userIDs[actor]), f.server.refreshRBACVersion(f.ctx)); err != nil {
		t.Fatalf("publish fixture permission versions: %v", err)
	}
}

func auditOSS001CreateReviewedCreatorWithAvatar(t *testing.T, f auditFixGRFixture, kind, fileID, stable string) string {
	t.Helper()
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.create")
	auditOSS001PublishFixturePermissions(t, f, f.editor)
	created := decodeTEST022Data[struct {
		PublicID   string `json:"publicId"`
		RevisionID string `json:"revisionId"`
		Status     string `json:"reviewStatus"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/creators", map[string]any{"kind": kind, "name": "OSS001 " + kind + " " + randomCatalogPublicID(), "defaultLocale": "en-US", "descriptionMarkdown": "Ordinary avatar profile", "localizations": []map[string]any{{"locale": "en-US", "contentMarkdown": "Ordinary avatar profile"}}, "avatarUrl": stable, "avatarFileId": fileID, "links": []any{}}, 201))
	if created.PublicID == "" || created.RevisionID == "" || created.Status != "pending" {
		t.Fatal("ordinary creation must retain independent pending review")
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+created.RevisionID, map[string]any{"status": "approved", "note": "Independent ordinary avatar fixture review"}, 200)
	var status, revision string
	if err := f.db.QueryRow(f.ctx, `select c.review_status,r.public_id from creators c join content_revisions r on r.id=c.published_revision_id where c.public_id=$1`, created.PublicID).Scan(&status, &revision); err != nil || status != "approved" || revision != created.RevisionID {
		t.Fatal("actual independent review did not publish exact creator", err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.edit."+created.PublicID)
	auditOSS001PublishFixturePermissions(t, f, f.editor)
	return created.PublicID
}

func TestAuditOSS001NewCreatorAvatarStagingHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"author", "team"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.create")
			auditOSS001Upload(t, f, f.editor, "creator_avatar:"+kind+":new", auditOSS001PNG(t, 70))
			var count int
			if err := f.db.QueryRow(f.ctx, `select count(*) from creators where created_by=$1`, f.userIDs[f.editor]).Scan(&count); err != nil || count != 0 {
				t.Fatal("staging must not fabricate creator facts", err, count)
			}
		})
	}
}

func TestAuditOSS001ExistingCreatorAvatarRefreshHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"author", "team"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			publicID := auditOSS001CreateReviewedCreator(t, f, kind)
			// Preview authorization is independent of scan completion: an owner can
			// preview an active pending file, but binding still requires clean below.
			ticket, file := auditOSS001UploadTransport(t, f, f.editor, "creator_avatar:"+kind+":"+publicID, auditOSS001PNG(t, 150))
			fileID := file.ID
			var objectKey string
			if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, fileID).Scan(&objectKey); err != nil {
				t.Fatal(err)
			}
			if ticket.ObjectKey == "" || objectKey == "" {
				t.Fatal("real upload identity missing")
			}
			access := decodeTEST022Data[struct {
				URL string `json:"url"`
			}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/files/presign", map[string]any{"objectKey": objectKey}, 200))
			if access.URL == "" {
				t.Fatal("authorized exact creator avatar preview must be renewable")
			}
		})
	}
}

func TestAuditOSS001CreatorAvatarHasRealSynchronousRasterValidationHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"author", "team"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			id := auditOSS001CreateReviewedCreator(t, f, kind)
			auditOSS001Upload(t, f, f.editor, "creator_avatar:"+kind+":"+id, auditOSS001PNG(t, 180))
		})
	}
}
