package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This local IPC only directs a separately authorized synthetic reviewer to an
// actual HTTP pending revision submitted by the one ordinary native actor. It
// never creates a revision, grants permission, or changes an asset scan result.
func auditOSS001ReviewNativeRequest(t *testing.T, f auditFixGRFixture, cfg ossConfigPayload, directory string, ordinal int) (map[string]any, bool) {
	t.Helper()
	if ordinal < 1 || ordinal > 12 {
		t.Fatal("bounded owned review IPC ordinal exceeded")
	}
	suffix := fmt.Sprintf("%02d", ordinal)
	requestPath := filepath.Join(directory, "review-request-"+suffix+".json")
	stream, err := os.Open(requestPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	stat, err := stream.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 2048 {
		t.Fatal("bounded regular owned review request required")
	}
	var request struct {
		CreatorID  string `json:"creatorId"`
		RevisionID string `json:"revisionId"`
		FileID     string `json:"fileId"`
	}
	decoder := json.NewDecoder(io.LimitReader(stream, 2048))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil {
		t.Fatal("invalid owned review request shape")
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) || !validCatalogPublicID(request.CreatorID) || !validCatalogPublicID(request.RevisionID) || !validCatalogPublicID(request.FileID) {
		t.Fatal("invalid owned review request identities")
	}
	var kind, requestedFile, requestedReference, requestID string
	var submittedBy, createdBy, revisionActor int64
	err = f.db.QueryRow(f.ctx, `select creator.kind,request.public_id,coalesce(request.submitted_by,0),coalesce(creator.created_by,0),coalesce(revision.created_by,0),revision.snapshot->>'avatarFileId',revision.snapshot->>'avatarUrl'
		from content_revisions revision
		join creators creator on creator.id=revision.entity_id and revision.entity_type=creator.kind and revision.aggregate_type='creator' and revision.aggregate_key=creator.public_id
		join change_requests request on request.proposed_revision_id=revision.id and request.aggregate_type='creator' and request.aggregate_key=creator.public_id and request.entity_id=creator.id
		where creator.public_id=$1 and revision.public_id=$2 and request.status='pending'`, request.CreatorID, request.RevisionID).Scan(&kind, &requestID, &submittedBy, &createdBy, &revisionActor, &requestedFile, &requestedReference)
	if err != nil || (kind != "author" && kind != "team") || submittedBy != f.userIDs[f.editor] || createdBy != submittedBy || revisionActor != submittedBy || requestedFile != request.FileID {
		t.Fatal("owned native review must reference the actual ordinary actor's pending creator/avatar revision", err)
	}
	file, err := resolveTrustedRasterOSSFilePublicID(f.ctx, f.db, request.FileID, ossRasterBindingScope{UploaderID: submittedBy})
	if err != nil {
		t.Fatal("native candidate lacks actual trusted raster proof", err)
	}
	var source, sourceSHA, storedSHA, endpoint, bucket string
	var verified, receipt bool
	err = f.db.QueryRow(f.ctx, `select file.source,file.sha256,file.stored_sha256,file.source_hash_verified,file.endpoint,file.bucket,`+ossCommittedSourceHashProofSQL+` from oss_files file where file.public_id=$1`, request.FileID).Scan(&source, &sourceSHA, &storedSHA, &verified, &endpoint, &bucket, &receipt)
	if err != nil || !verified || !receipt || bucket != cfg.Bucket || endpoint != cfg.displayEndpoint() ||
		(source != "creator_avatar:"+kind+":new" && source != "creator_avatar:"+kind+":"+request.CreatorID) ||
		requestedReference != ossStoredObjectURL(cfg, file.ObjectKey) || temporaryCreatorAvatarReference(requestedReference) {
		t.Fatal("native candidate must preserve current endpoint, byte receipt, exact creator purpose and stable reference", err)
	}
	f.objectMu.Lock()
	object, exists := f.objects[file.ObjectKey]
	storedBytes := bytes.Clone(object.data)
	f.objectMu.Unlock()
	if !exists || fmt.Sprintf("%x", sha256.Sum256(storedBytes)) != storedSHA {
		t.Fatal("actual retained OSS bytes disagree with stored hash")
	}
	pixels, format, err := image.Decode(bytes.NewReader(storedBytes))
	if err != nil || pixels.Bounds().Dx() != 256 || pixels.Bounds().Dy() != 256 {
		t.Fatal("actual native stored avatar must decode at the submitted crop size", err)
	}
	pixelHash := sha256.New()
	for y := pixels.Bounds().Min.Y; y < pixels.Bounds().Max.Y; y++ {
		for x := pixels.Bounds().Min.X; x < pixels.Bounds().Max.X; x++ {
			pixel := color.NRGBAModel.Convert(pixels.At(x, y)).(color.NRGBA)
			pixelHash.Write([]byte{pixel.R, pixel.G, pixel.B, pixel.A})
		}
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+request.RevisionID, map[string]any{"status": "approved", "note": "Independent review of exact native OSS001 avatar revision"}, 200)
	var actualReference, publishedRevision, actualFile, status, reviewerPublicID, submitterPublicID string
	var resolutions, auditResolutions, activityIntents int
	err = f.db.QueryRow(f.ctx, `select creator.avatar_url,published.public_id,file.public_id,request.status,
		(select public_id from users where id=$3),(select public_id from users where id=$4),
		(select count(*) from review_events event where event.change_request_id=request.id and event.event_type='approved' and event.actor_id=$3),
		(select count(*) from audit_events audit where audit.aggregate_type='creator' and audit.aggregate_key=creator.public_id and audit.action='content.revision.approved' and audit.actor_id=$3 and audit.metadata->>'revisionId'=requested.id::text),
		(select count(*) from activity_event_outbox outbox where outbox.object_public_id=creator.public_id)
		from creators creator join content_revisions published on published.id=creator.published_revision_id
		join content_revisions requested on requested.public_id=$2 and requested.entity_id=creator.id and requested.aggregate_type='creator'
		join change_requests request on request.proposed_revision_id=requested.id
		join oss_files file on file.id=creator.avatar_file_id
		where creator.public_id=$1 and creator.review_status='approved'`, request.CreatorID, request.RevisionID, f.userIDs[f.reviewer], submittedBy).Scan(&actualReference, &publishedRevision, &actualFile, &status, &reviewerPublicID, &submitterPublicID, &resolutions, &auditResolutions, &activityIntents)
	if err != nil || actualReference != requestedReference || actualFile != request.FileID || publishedRevision != request.RevisionID || status != "approved" || resolutions != 1 || auditResolutions != 1 || activityIntents < 1 || reviewerPublicID == submitterPublicID {
		t.Fatal("actual independent review failed exact published pointer, asset, audit or durable activity postconditions", err)
	}
	report := map[string]any{"creatorId": request.CreatorID, "revisionId": request.RevisionID, "fileId": request.FileID, "changeRequestId": requestID, "status": status, "publishedRevisionId": publishedRevision,
		"sourceSHA256": sourceSHA, "storedSHA256": storedSHA, "actualStoredBytesSHA256": fmt.Sprintf("%x", sha256.Sum256(storedBytes)), "sourceHashVerified": verified, "committedByteReceipt": receipt,
		"width": pixels.Bounds().Dx(), "height": pixels.Bounds().Dy(), "storedFormat": format, "storedBytes": len(storedBytes), "pixelNRGBASHA256": fmt.Sprintf("%x", pixelHash.Sum(nil)),
		"stableReferenceSHA256": fmt.Sprintf("%x", sha256.Sum256([]byte(actualReference))), "temporarySignaturePersisted": temporaryCreatorAvatarReference(actualReference), "submitterPublicId": submitterPublicID, "reviewerPublicId": reviewerPublicID,
		"approvalEventsForExactReviewer": resolutions, "auditApprovalEventsForExactRevisionAndReviewer": auditResolutions, "durableActivityIntentsForCreator": activityIntents, "kind": kind, "purpose": source,
		"layer": "Actual HTTP+PostgreSQL independent reviewer and actual retained bytes; reviewer is isolated synthetic identity, not native reviewer UI or real cloud"}
	auditJourneyWriteJSON(t, filepath.Join(directory, "review-result-"+suffix+".json"), report)
	return report, true
}

func TestAuditOSS001NativeReviewIPCProducesExactActualHTTPDatabaseAndRasterReceipt(t *testing.T) {
	f := newAuditFixGRFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.create")
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 180, A: 200})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	_, fileID := auditOSS001Upload(t, f, f.editor, "creator_avatar:author:new", encoded.Bytes())
	var key string
	if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, fileID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	cfg := f.server.ossConfigFromSettings(f.ctx)
	created := decodeTEST022Data[struct {
		PublicID   string `json:"publicId"`
		RevisionID string `json:"revisionId"`
		Status     string `json:"reviewStatus"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/creators", map[string]any{"kind": "author", "name": "Native IPC fixture author", "defaultLocale": "en-US", "descriptionMarkdown": "Actual ordinary pending avatar", "localizations": []map[string]any{{"locale": "en-US", "contentMarkdown": "Actual ordinary pending avatar"}}, "avatarUrl": ossStoredObjectURL(cfg, key), "avatarFileId": fileID, "links": []any{}}, 201))
	if created.Status != "pending" {
		t.Fatal("fixture must not replace independent review")
	}
	directory := t.TempDir()
	auditJourneyWriteJSON(t, filepath.Join(directory, "review-request-01.json"), map[string]any{"creatorId": created.PublicID, "revisionId": created.RevisionID, "fileId": fileID})
	receipt, processed := auditOSS001ReviewNativeRequest(t, f, cfg, directory, 1)
	if !processed || receipt["status"] != "approved" || receipt["sourceSHA256"] != fmt.Sprintf("%x", sha256.Sum256(encoded.Bytes())) || receipt["temporarySignaturePersisted"] != false || receipt["publishedRevisionId"] != created.RevisionID {
		t.Fatal("IPC cannot manufacture an approval, wrong byte proof or pointer")
	}
	facts := auditOSS001NativeFinalFacts(t, f)
	if facts["referencesContainNoKnownSigningKeys"] != true {
		t.Fatal("final facts must query actual stable state")
	}
}

func auditOSS001NativeFinalFacts(t *testing.T, f auditFixGRFixture) map[string]any {
	t.Helper()
	var creatorFacts []byte
	err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(facts order by facts->>'creatorId'),'[]'::jsonb) from (
		select jsonb_build_object('creatorId',creator.public_id,'kind',creator.kind,'reviewStatus',creator.review_status,
		'publishedRevisionId',revision.public_id,'fileId',file.public_id,'stableReferenceSHA256',encode(sha256(convert_to(creator.avatar_url,'UTF8')),'hex'),
		'revisions',(select count(*) from content_revisions where aggregate_type='creator' and aggregate_key=creator.public_id),
		'pendingRequests',(select count(*) from change_requests where aggregate_type='creator' and aggregate_key=creator.public_id and status='pending'),
		'approvedRequests',(select count(*) from change_requests where aggregate_type='creator' and aggregate_key=creator.public_id and status='approved'),
		'activityIntents',(select count(*) from activity_event_outbox where object_public_id=creator.public_id)) facts
		from creators creator left join content_revisions revision on revision.id=creator.published_revision_id
		left join oss_files file on file.id=creator.avatar_file_id where creator.created_by=$1) owned`, f.userIDs[f.editor]).Scan(&creatorFacts)
	if err != nil {
		t.Fatal("final native creator facts query", err)
	}
	var creators any
	if err = json.Unmarshal(creatorFacts, &creators); err != nil {
		t.Fatal(err)
	}
	var temporaryDrafts, temporaryCreators, totalDrafts int
	err = f.db.QueryRow(f.ctx, `select
		(select count(*) from user_drafts where user_id=$1 and kind='creator'),
		(select count(*) from user_drafts where user_id=$1 and kind='creator' and coalesce(payload->>'avatarUrl','') ~* '[?&](x-oss-signature|x-amz-signature|signature|auth_key)='),
		(select count(*) from creators where created_by=$1 and avatar_url ~* '[?&](x-oss-signature|x-amz-signature|signature|auth_key)=')`, f.userIDs[f.editor]).Scan(&totalDrafts, &temporaryDrafts, &temporaryCreators)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"creators": creators, "remainingCreatorDrafts": totalDrafts, "temporaryAvatarDrafts": temporaryDrafts, "temporaryCreatorStoredReferences": temporaryCreators, "referencesContainNoKnownSigningKeys": temporaryDrafts == 0 && temporaryCreators == 0,
		"scope": strings.TrimSpace("Owned isolated actor rows only; no production diagnosis/migration and no assertion that submitted-and-deleted drafts remain present")}
}
