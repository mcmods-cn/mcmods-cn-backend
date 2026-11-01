package httpapi

import (
	"bytes"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The prior eleven native positive rows remain sealed at their original tuple.
// This independent real HTTP/PostgreSQL regression closes only the remaining
// revoke/foreign-uploader/already-bound-gallery rejection assertions.
func TestAuditTP008SixRevokedCreatesAndBoundGalleryRejectWithoutBusinessWritesHTTPIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || os.Getenv("MCMODS_RUN_TP008_NEGATIVE") != "1" {
		t.Skip("opt-in short TP008 actual HTTP/PostgreSQL negative matrix with normal anti-abuse/Redis")
	}
	f := newAuditTP008NegativeFixture(t)
	fileID := f.normalGalleryPrecondition(t)
	gallery := []modGalleryImagePayload{{FileID: fileID, Name: "TP008 owned inspected PNG", ContentType: "image/png"}}
	project := f.draft(t, "plugin", "bound_owner")
	project.GalleryImages = gallery
	created := auditTP008NegativeData[simpleProjectResponse](t, f.request(t, "owner", http.MethodPost, "/api/v1/content-projects/plugin", project, 201))
	if created.ReviewStatus != "pending" || created.SubmissionRevisionID == "" || created.ChangeRequestID == "" || created.PublicID == "" {
		t.Fatal("normal prerequisite did not enter independent pending review")
	}
	f.request(t, "", http.MethodGet, "/api/v1/content-projects/plugin/"+created.SiteID, nil, 404)
	f.request(t, "reviewer", http.MethodPatch, "/api/v1/content-revisions/"+created.SubmissionRevisionID, map[string]string{"status": "approved", "note": "TP008 negative phase normal independent prerequisite"}, 200)
	var bound, independent, pendingAuthors int
	if err := f.db.QueryRow(f.ctx, `select
		(select count(*) from simple_project_gallery_images gallery join simple_projects project on project.id=gallery.project_id join oss_files file on file.id=gallery.oss_file_id where project.public_id=$1 and project.review_status='approved' and gallery.published_revision_id=project.published_revision_id and file.public_id=$2),
		(select count(*) from review_events event join change_requests request on request.id=event.change_request_id join content_revisions revision on revision.id=request.proposed_revision_id where revision.public_id=$3 and revision.created_by=$4 and event.actor_id=$5 and event.actor_id<>revision.created_by and event.event_type='approved'),
		(select count(*) from content_creator_bindings binding where binding.subject_id=(select id from simple_projects where public_id=$1) and binding.subject_type='plugin' and binding.status='pending')`,
		created.PublicID, fileID, created.SubmissionRevisionID, f.ids["owner"], f.ids["reviewer"]).Scan(&bound, &independent, &pendingAuthors); err != nil || bound != 1 || independent != 1 || pendingAuthors != 1 {
		t.Fatalf("normal bound gallery/review/author prerequisites: bound=%d independent=%d pendingAuthors=%d err=%v", bound, independent, pendingAuthors, err)
	}
	publicRaw := f.request(t, "", http.MethodGet, "/api/v1/content-projects/plugin/"+created.SiteID, nil, 200)
	public := auditTP008NegativeData[simpleProjectResponse](t, publicRaw)
	if public.ReviewStatus != "approved" || public.PublishedRevisionID == nil || *public.PublishedRevisionID != created.SubmissionRevisionID || len(public.GalleryImages) != 1 || public.GalleryImages[0].FileID != fileID || len(public.Authors) != 0 {
		t.Fatal("normal prerequisite public projection does not match independent approval")
	}
	if err := os.WriteFile(filepath.Join(f.directory, "new-prerequisite-public.json"), publicRaw, 0600); err != nil {
		t.Fatal(err)
	}
	f.report["normalBoundProject"] = map[string]any{"projectId": created.PublicID, "siteId": created.SiteID, "revisionId": created.SubmissionRevisionID, "changeRequestId": created.ChangeRequestID, "uploader": f.publicIDs["owner"], "reviewer": f.publicIDs["reviewer"], "approvedGalleryBindings": bound, "independentApprovals": independent, "pendingHiddenAuthorBindings": pendingAuthors, "publicBodySha256": sha256Hex(publicRaw)}
	for _, item := range []struct{ name, role, message string }{
		{"foreign_uploader", "foreign", "project gallery image is unavailable or not owned by the editor"},
		{"same_owner_already_bound_other_project", "owner", "project gallery image is already used"},
	} {
		t.Run(item.name, func(t *testing.T) {
			draft := f.draft(t, "plugin", item.name)
			draft.GalleryImages = gallery // no existing gallery public ID: new-project binding path
			f.deny(t, item.name, item.role, "plugin", item.message, draft, 400)
		})
	}
	if t.Failed() {
		t.Fatal("gallery negative prerequisite phase failed; not replaying writes")
	}
	kinds := simpleProjectTypeValues()
	if len(kinds) != 6 {
		t.Fatal("all six project types required")
	}
	for _, kind := range kinds {
		f.permission(t, kind, true)
	}
	var oldPermission, oldAuth, newPermission, newAuth int64
	if err := f.db.QueryRow(f.ctx, `select permission_version,auth_version from users where id=$1`, f.ids["revoked"]).Scan(&oldPermission, &oldAuth); err != nil {
		t.Fatal(err)
	}
	f.request(t, "governor", http.MethodPut, "/api/v1/admin/users/"+f.publicIDs["revoked"]+"/permissions", updateUserPermissionsRequest{Permissions: []userPermissionEntry{}}, 200)
	f.request(t, "revoked", http.MethodGet, "/api/v1/auth/me", nil, 200)
	for _, kind := range kinds {
		f.permission(t, kind, false)
	}
	var revocations int
	if err := f.db.QueryRow(f.ctx, `select permission_version,auth_version,(select count(*) from permission_audit_logs where operator_id=$2 and target_user_id=$1 and action='update_user_permissions') from users where id=$1`, f.ids["revoked"], f.ids["governor"]).Scan(&newPermission, &newAuth, &revocations); err != nil || newPermission <= oldPermission || newAuth != oldAuth || revocations != 1 {
		t.Fatalf("actual revocation version/audit/session prerequisites: permissions=%d→%d auth=%d→%d audits=%d err=%v", oldPermission, newPermission, oldAuth, newAuth, revocations, err)
	}
	f.report["revocation"] = map[string]any{"actor": f.publicIDs["revoked"], "operator": f.publicIDs["governor"], "permissionVersionBefore": oldPermission, "permissionVersionAfter": newPermission, "authVersionUnchanged": oldAuth == newAuth, "sameOriginalSessionRemainsAuthenticated": true, "auditCount": revocations, "allSixEvaluatedAllowedBeforeAndDeniedAfter": true}
	first := time.Now()
	for index, kind := range kinds {
		if index == 3 {
			deadline := first.Add(61 * time.Second)
			f.report["originalRateWindowWait"] = map[string]any{"firstRequestAt": first.UTC(), "waitStartedAt": time.Now().UTC(), "nextNotBefore": deadline.UTC(), "originalWindowSeconds": 60, "sameFixedActor": true}
			f.write(t)
			t.Log("waiting original review.submit 60s window before the same actor's remaining three denied requests")
			timer := time.NewTimer(time.Until(deadline))
			select {
			case <-f.ctx.Done():
				timer.Stop()
				t.Fatal("declared short negative phase expired while waiting original rate window")
			case <-timer.C:
			}
		}
		t.Run("revoked_"+kind, func(t *testing.T) {
			f.deny(t, "revoked_"+kind, "revoked", kind, "project creation permission is required", f.draft(t, kind, "revoked_"+kind), 403)
		})
	}
	f.report["finishedBusinessFacts"] = f.facts(t)
	f.report["ownedStorageProtocol"] = f.storage.snapshot()
	f.write(t)
}

func (f *auditTP008NegativeFixture) permission(t *testing.T, kind string, want bool) {
	t.Helper()
	raw := f.request(t, "revoked", http.MethodGet, "/api/v1/auth/permissions/evaluate?code=project.create."+kind, nil, 200)
	result := auditTP008NegativeData[struct{ Allowed bool }](t, raw)
	if result.Allowed != want {
		t.Fatalf("same actor's live %s permission=%v want=%v", kind, result.Allowed, want)
	}
}

func (f *auditTP008NegativeFixture) normalGalleryPrecondition(t *testing.T) string {
	t.Helper()
	raw := auditTP008NegativePNG(t)
	digest := sha256Hex(raw)
	source := "simple_project_gallery:plugin:draft"
	ticket := auditTP008NegativeData[test037Ticket](t, f.request(t, "owner", http.MethodPost, "/api/v1/users/me/oss/uploads/presign", map[string]any{"originalName": "tp008-negative-owned.png", "contentType": "image/png", "category": source, "source": source, "sizeBytes": len(raw), "sha256": digest}, 200))
	parsed, err := url.Parse(ticket.URL)
	owned, ownedErr := url.Parse(f.storage.server.URL)
	if err != nil || ownedErr != nil || parsed.Scheme != owned.Scheme || parsed.Host != owned.Host || parsed.User != nil || ticket.Method != http.MethodPut || !ticket.UploadRequired || ticket.SHA256 != digest || ticket.SizeBytes != int64(len(raw)) {
		t.Fatal("normal single-PUT ticket is not bound to the declared owned asset and bytes")
	}
	request, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(raw))
	if err != nil {
		t.Fatal("cannot construct owned initial PUT")
	}
	for key, value := range ticket.Headers {
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("normal owned initial PUT transport failed")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("normal owned initial PUT status=%d", response.StatusCode)
	}
	completed := auditTP008NegativeData[struct{ ID string }](t, f.request(t, "owner", http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	var key, storedHash, scanState string
	var size int64
	var proof bool
	if err := f.db.QueryRow(f.ctx, `select file.object_key,file.stored_sha256,file.size_bytes,file.scan_status,file.source_hash_verified and exists(select 1 from oss_upload_byte_bindings binding where binding.file_id=file.id and binding.status='committed' and `+ossByteBindingCurrentFileProofSQL+`) from oss_files file where file.public_id=$1 and file.uploader_id=$2 and file.status='active'`, completed.ID, f.ids["owner"]).Scan(&key, &storedHash, &size, &scanState, &proof); err != nil || !proof || scanState != "pending" || size != int64(len(raw)) || storedHash != digest || !isOSSSealedObjectKey(key) {
		t.Fatal("normal upload lacks pending scan and current server-verified sealed byte proof")
	}
	sdk, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal("owned manual inspection client unavailable")
	}
	inspected, err := readOSSUploadedRaster(f.ctx, sdk, cfg, key, size, storedHash)
	picture, format, decodeErr := image.Decode(bytes.NewReader(inspected))
	if err != nil || decodeErr != nil || format != "png" || picture.Bounds().Dx() != 8 || picture.Bounds().Dy() != 8 || !bytes.Equal(raw, inspected) {
		t.Fatal("normal independent scan precondition did not inspect the exact PNG bytes")
	}
	f.request(t, "scan", http.MethodPatch, "/api/v1/admin/oss/files/"+completed.ID+"/scan", map[string]string{"status": "clean", "note": "TP008 owned PNG exact bytes/hash/8x8 decoded; explicit manual precondition, not antivirus engine"}, 200)
	var audits int
	if err = f.db.QueryRow(f.ctx, `select file.scan_status,(select count(*) from audit_events where aggregate_type='oss_file' and aggregate_key=file.public_id and action='scan_status_update' and actor_id=$2) from oss_files file where file.public_id=$1`, completed.ID, f.ids["scan"]).Scan(&scanState, &audits); err != nil || scanState != "clean" || audits != 1 {
		t.Fatal("normal scan API did not persist its independent exact audit and clean state")
	}
	if err = os.WriteFile(filepath.Join(f.directory, "new-prerequisite-input.png"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.report["normalOwnedUpload"] = map[string]any{"fileId": completed.ID, "sourceSha256": digest, "storedSha256": storedHash, "bytes": size, "dimensions": []int{8, 8}, "source": source, "clientPUTCount": 1, "currentServerSealedByteProof": proof, "manualScanActor": f.publicIDs["scan"], "manualScanAuditCount": audits, "scanStatus": scanState, "inspectionAuthority": "oss.write only; existing real HTTP scan-status API, not antivirus engine"}
	f.write(t)
	return completed.ID
}
