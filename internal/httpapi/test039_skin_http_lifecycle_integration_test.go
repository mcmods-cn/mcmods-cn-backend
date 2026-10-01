package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

type test039Fixture struct{ test038Fixture }

func newTEST039Fixture(t *testing.T) test039Fixture {
	t.Helper()
	f := test039Fixture{newTEST038Fixture(t)}
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "skin.library.upload", "skin.profile.create", "skin.profile.write")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "skin.admin")
	review := defaultReviewConfig()
	review.CatalogCreate = true
	review.CatalogEdit = false
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update system_settings set value=$2::jsonb where key=$1", reviewConfigSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f test039Fixture) facts(t *testing.T) map[string]string {
	t.Helper()
	result := f.test038Fixture.facts(t)
	for _, table := range []string{"skin_texture_blobs", "skin_assets", "skin_wardrobe", "skin_asset_adoptions", "skin_public_catalog", "player_profiles", "player_profile_textures", "player_profile_name_history", "yggdrasil_accounts", "yggdrasil_tokens", "yggdrasil_join_sessions"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(fact_row) order by to_jsonb(fact_row)::text),'[]'::jsonb)::text from "+pgx.Identifier{table}.Sanitize()+" fact_row").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		result[table] = raw
	}
	return result
}

func (f test039Fixture) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	if !reflect.DeepEqual(before, after) {
		for table, raw := range before {
			if after[table] != raw {
				t.Errorf("denied/failed skin operation mutated %s", table)
			}
		}
		t.FailNow()
	}
}

func test039PNG(t *testing.T, width, height int, variation uint8) []byte {
	t.Helper()
	bitmap := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			bitmap.SetNRGBA(x, y, color.NRGBA{R: uint8(x) + variation, G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, bitmap); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

func (f test039Fixture) upload(t *testing.T, token, name string, raw []byte) string {
	t.Helper()
	ticket := f.ticket(t, token, "/api/v1/users/me/oss/uploads/presign", name, "image/png", "skin", raw)
	f.put(t, ticket, raw)
	file := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, token, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	if file.ID == "" {
		t.Fatal("actual PNG upload did not register source")
	}
	// This is an owned clean scan state, not a claim of running a virus scanner.
	if _, err := f.db.Exec(f.ctx, "update oss_files set scan_status='clean' where public_id=$1", file.ID); err != nil {
		t.Fatal(err)
	}
	return file.ID
}

func (f test039Fixture) asset(t *testing.T, token, file, name, visibility string) playerTextureResponse {
	t.Helper()
	request := skinAssetCreateRequest{FileID: file, Kind: "skin", Model: "default", Name: name, Visibility: visibility, DefaultLocale: "en-US", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: name, Summary: "actual TEST039 lifecycle"}}}
	asset := decodeTEST022Data[playerTextureResponse](t, f.require(t, token, http.MethodPost, "/api/v1/skins", request, 201))
	if asset.PublicID == "" || len(asset.TextureHash) != 64 || !asset.CanEdit || asset.Owner.ID == "" {
		t.Fatal("actual skin creation lost owner/hash/capability")
	}
	return asset
}

func (f test039Fixture) pendingRevision(t *testing.T, asset string) string {
	t.Helper()
	var revision string
	if err := f.db.QueryRow(f.ctx, `select revision.public_id from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id
		where request.aggregate_type='skin' and request.aggregate_key=$1 and request.status='pending' order by request.id desc limit 1`, asset).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return revision
}

func (f test039Fixture) review(t *testing.T, asset, status string) {
	t.Helper()
	revision := f.pendingRevision(t, asset)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+revision, map[string]any{"status": status, "note": "test039 independent actual review"}, 200)
}

func (f test039Fixture) profile(t *testing.T, token, name, visibility string) playerProfileResponse {
	t.Helper()
	profile := decodeTEST022Data[playerProfileResponse](t, f.require(t, token, http.MethodPost, "/api/v1/users/me/player-profiles", playerProfileCreateRequest{Name: name, Visibility: visibility}, 201))
	if profile.PublicID == "" || profile.UUID == "" {
		t.Fatal("actual profile creation lost identity")
	}
	return profile
}

func assertTEST039TextureBytes(t *testing.T, raw []byte, hash string) {
	t.Helper()
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != hash {
		t.Fatal("actual texture bytes disagree with canonical content hash")
	}
	bitmap, err := png.Decode(bytes.NewReader(raw))
	if err != nil || bitmap.Bounds().Dx() != 64 || bitmap.Bounds().Dy() != 64 {
		t.Fatalf("actual normalized PNG dimensions/decoder error=%v", err)
	}
}

func (f test039Fixture) conditionalTexture(t *testing.T, route, hash string, want int) {
	t.Helper()
	request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origin.URL+route, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("If-None-Match", `"`+hash+`"`)
	response, err := f.origin.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode != want {
		t.Fatalf("conditional texture status=%d want=%d err=%v", response.StatusCode, want, err)
	}
	if want == 304 && (len(raw) != 0 || response.Header.Get("ETag") != `"`+hash+`"` || response.Header.Get("Cache-Control") != "public, max-age=31536000, immutable") {
		t.Fatal("healthy immutable launcher revalidation contract changed")
	}
}

func TestTEST039ActualUploadCreateReviewAndPrivateProfileFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "private.png", test039PNG(t, 64, 64, 1))
	asset := f.asset(t, f.editor, file, "private skin", "private")
	if asset.ReviewStatus != "pending" {
		t.Fatal("normal skin upload bypassed CatalogCreate review")
	}
	before := f.facts(t)
	f.require(t, "", http.MethodGet, "/api/v1/skins/"+asset.PublicID, nil, 404)
	f.require(t, f.otherEditor, http.MethodGet, "/api/v1/skins/"+asset.PublicID, nil, 404)
	f.require(t, f.editor, http.MethodGet, "/api/v1/skins/"+asset.PublicID, nil, 200)
	f.unchanged(t, before)
	f.review(t, asset.PublicID, "approved")
	publicProfile := f.profile(t, f.editor, "Test039Public", "public")
	privateProfile := f.profile(t, f.editor, "Test039Private", "private")
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+publicProfile.PublicID+"/textures", map[string]any{"skinPublicId": asset.PublicID}, 409)
	f.require(t, f.otherEditor, http.MethodPut, "/api/v1/users/me/player-profiles/"+privateProfile.PublicID+"/textures", map[string]any{"skinPublicId": asset.PublicID}, 400)
	f.unchanged(t, before)
	equipped := decodeTEST022Data[playerProfileResponse](t, f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+privateProfile.PublicID+"/textures", map[string]any{"skinPublicId": asset.PublicID}, 200))
	if equipped.Skin == nil || equipped.Skin.PublicID != asset.PublicID {
		t.Fatal("actual private profile did not equip owned approved private skin")
	}
	before = f.facts(t)
	f.require(t, "", http.MethodGet, "/api/v1/player-profiles/"+privateProfile.PublicID, nil, 404)
	f.require(t, f.otherEditor, http.MethodGet, "/api/v1/player-profiles/"+privateProfile.PublicID, nil, 404)
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+privateProfile.PublicID, map[string]any{"visibility": "public"}, 409)
	public := decodeTEST022Data[playerProfileResponse](t, f.require(t, "", http.MethodGet, "/api/v1/player-profiles/"+publicProfile.PublicID, nil, 200))
	if public.Skin != nil {
		t.Fatal("public profile inherited private texture")
	}
	f.unchanged(t, before)
}

func TestTEST039PrivateMetadataAndDeletedBlobContentRemainDistinctFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "private-bytes.png", test039PNG(t, 64, 64, 2))
	asset := f.asset(t, f.editor, file, "private bytes", "private")
	before := f.facts(t)
	// Existing launcher hash URLs are content-addressed bearers, not asset ACLs.
	// The private metadata boundary must not be replaced by a shared-blob owner.
	f.require(t, "", http.MethodGet, "/api/v1/skins/"+asset.PublicID, nil, 404)
	f.require(t, f.otherEditor, http.MethodGet, "/api/v1/skins/"+asset.PublicID, nil, 404)
	assertTEST039TextureBytes(t, f.require(t, "", http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
	f.conditionalTexture(t, asset.TextureURL, asset.TextureHash, 304)
	assertTEST039TextureBytes(t, f.require(t, f.editor, http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
	f.unchanged(t, before)
	f.review(t, asset.PublicID, "approved")
	before = f.facts(t)
	f.require(t, "", http.MethodGet, "/api/v1/skins/"+asset.PublicID, nil, 404)
	f.unchanged(t, before)
	f.require(t, f.editor, http.MethodDelete, "/api/v1/skins/"+asset.PublicID, nil, 200)
	before = f.facts(t)
	for _, token := range []string{"", f.editor, f.otherEditor, f.reviewer} {
		f.require(t, token, http.MethodGet, asset.TextureURL, nil, 404)
	}
	f.conditionalTexture(t, asset.TextureURL, asset.TextureHash, 404)
	f.unchanged(t, before)
}

func TestTEST039FullHTTPAdministratorCapabilitiesAndPublicPrivatePublicationInvariantIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "public.png", test039PNG(t, 64, 64, 3))
	asset := f.asset(t, f.editor, file, "public skin", "public")
	f.review(t, asset.PublicID, "approved")
	admin := decodeTEST022Data[playerTextureResponse](t, f.require(t, f.reviewer, http.MethodGet, "/api/v1/skins/"+asset.PublicID, nil, 200))
	if !admin.CanEdit || !admin.CanUse {
		t.Fatal("skin.admin actual session lost authorized capability")
	}
	profile := f.profile(t, f.editor, "Test039Equip", "public")
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+profile.PublicID+"/textures", map[string]any{"skinPublicId": asset.PublicID}, 200)
	before := f.facts(t)
	f.require(t, f.editor, http.MethodPut, "/api/v1/skins/"+asset.PublicID, map[string]any{"visibility": "private"}, 409)
	f.require(t, f.otherEditor, http.MethodPut, "/api/v1/skins/"+asset.PublicID, map[string]any{"name": "cross owner"}, 403)
	f.unchanged(t, before)
	f.require(t, f.reviewer, http.MethodPut, "/api/v1/skins/"+asset.PublicID, map[string]any{"name": "administrator edit"}, 200)
	updated := decodeTEST022Data[playerTextureResponse](t, f.require(t, f.editor, http.MethodGet, "/api/v1/skins/"+asset.PublicID, nil, 200))
	if updated.Name != "administrator edit" {
		t.Fatal("authorized administrator edit was not published")
	}
	assertTEST039TextureBytes(t, f.require(t, "", http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
}

func TestTEST039FullHTTPSkinReviewNotificationFailureRollsBackAllFactsIntegration(t *testing.T) {
	for _, status := range []string{"approved", "rejected"} {
		t.Run(status, func(t *testing.T) {
			f := newTEST039Fixture(t)
			file := f.upload(t, f.editor, "review.png", test039PNG(t, 64, 64, 4))
			asset := f.asset(t, f.editor, file, "review skin", "public")
			revision := f.pendingRevision(t, asset.PublicID)
			if _, err := f.db.Exec(f.ctx, `alter table nats_outbox add constraint test039_reject_review_notification
			check(coalesce(payload->>'templateKey','') not in ('review_approved','review_rejected'))`); err != nil {
				t.Fatal(err)
			}
			before := f.facts(t)
			row := f.parallel(t, f.reviewer, []test038Operation{{http.MethodPatch, "/api/v1/content-revisions/" + revision, map[string]any{"status": status, "note": "test039 notification failure"}}})[0]
			if row.status != 500 || !reflect.DeepEqual(before, f.facts(t)) {
				var persisted string
				if err := f.db.QueryRow(f.ctx, "select review_status from skin_assets where public_id=$1", asset.PublicID).Scan(&persisted); err != nil {
					t.Fatal(err)
				}
				t.Fatalf("failed review intent status=%d persisted=%s factsUnchanged=%v body=%s", row.status, persisted, reflect.DeepEqual(before, f.facts(t)), row.raw)
			}
			if _, err := f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test039_reject_review_notification"); err != nil {
				t.Fatal(err)
			}
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+revision, map[string]any{"status": status, "note": "recovered independent skin review"}, 200)
			var stored string
			var intents int
			if err := f.db.QueryRow(f.ctx, `select review_status,(select count(*) from nats_outbox where payload->>'templateKey'=$2)
			from skin_assets where public_id=$1`, asset.PublicID, "review_"+status).Scan(&stored, &intents); err != nil {
				t.Fatal(err)
			}
			if stored != status || intents != 1 {
				t.Fatalf("recovered skin review state=%s intents=%d", stored, intents)
			}
			before = f.facts(t)
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+revision, map[string]any{"status": status, "note": "duplicate"}, 409)
			f.unchanged(t, before)
		})
	}
}

func TestTEST039SharedBlobQuotaLastReferenceDeletionAndFreshKeyFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	raw := test039PNG(t, 64, 64, 5)
	firstFile := f.upload(t, f.editor, "shared-first.png", raw)
	secondFile := f.upload(t, f.otherEditor, "shared-second.png", raw)
	first := f.asset(t, f.editor, firstFile, "shared first", "public")
	second := f.asset(t, f.otherEditor, secondFile, "shared second", "public")
	if first.TextureHash != second.TextureHash {
		t.Fatal("identical canonical PNG was not shared")
	}
	f.review(t, first.PublicID, "approved")
	f.review(t, second.PublicID, "approved")
	// A private asset must not make another user's identical public hash unavailable.
	f.require(t, f.editor, http.MethodPut, "/api/v1/skins/"+first.PublicID, map[string]any{"visibility": "private"}, 200)
	f.require(t, "", http.MethodGet, "/api/v1/skins/"+first.PublicID, nil, 404)
	assertTEST039TextureBytes(t, f.require(t, "", http.MethodGet, second.TextureURL, nil, 200), second.TextureHash)
	var oldKey string
	var oldFileID, refs int64
	var owner *int64
	var source int64
	if err := f.db.QueryRow(f.ctx, `select blob.object_key,blob.oss_file_id,blob.active_reference_count,file.uploader_id,file.source_size_bytes
		from skin_texture_blobs blob join oss_files file on file.id=blob.oss_file_id where blob.hash=$1`, first.TextureHash).Scan(&oldKey, &oldFileID, &refs, &owner, &source); err != nil {
		t.Fatal(err)
	}
	if refs != 2 || owner != nil || source != 0 {
		t.Fatalf("shared derivative references/owner/source=%d/%v/%d", refs, owner, source)
	}
	for _, token := range []string{f.editor, f.otherEditor} {
		var active, stored, reserved int64
		if err := f.db.QueryRow(f.ctx, "select active_source_bytes,active_stored_bytes,reserved_stored_bytes from oss_user_quota_usage where user_id=$1", f.userIDs[token]).Scan(&active, &stored, &reserved); err != nil {
			t.Fatal(err)
		}
		if active != int64(len(raw)) || stored != int64(len(raw)) || reserved != 0 {
			t.Fatalf("shared derivative charged uploader quota=%d/%d/%d", active, stored, reserved)
		}
	}
	f.require(t, f.editor, http.MethodDelete, "/api/v1/skins/"+first.PublicID, nil, 200)
	var state string
	var queued int
	if err := f.db.QueryRow(f.ctx, "select blob.active_reference_count,file.status,(select count(*) from oss_object_deletion_outbox where oss_file_id=file.id) from skin_texture_blobs blob join oss_files file on file.id=blob.oss_file_id where blob.hash=$1", first.TextureHash).Scan(&refs, &state, &queued); err != nil {
		t.Fatal(err)
	}
	if refs != 1 || state != "active" || queued != 0 {
		t.Fatalf("first reference deletion retired shared file refs/state/queued=%d/%s/%d", refs, state, queued)
	}
	assertTEST039TextureBytes(t, f.require(t, "", http.MethodGet, second.TextureURL, nil, 200), second.TextureHash)
	f.require(t, f.otherEditor, http.MethodDelete, "/api/v1/skins/"+second.PublicID, nil, 200)
	if err := f.db.QueryRow(f.ctx, "select blob.active_reference_count,file.status,(select count(*) from oss_object_deletion_outbox where oss_file_id=file.id) from skin_texture_blobs blob join oss_files file on file.id=blob.oss_file_id where blob.hash=$1", first.TextureHash).Scan(&refs, &state, &queued); err != nil {
		t.Fatal(err)
	}
	if refs != 0 || state != "deleted" || queued != 1 {
		t.Fatalf("last reference did not retire shared file refs/state/queued=%d/%s/%d", refs, state, queued)
	}
	before := f.facts(t)
	f.require(t, "", http.MethodGet, second.TextureURL, nil, 404)
	f.unchanged(t, before)
	fresh := f.asset(t, f.editor, firstFile, "shared reincarnation", "public")
	f.review(t, fresh.PublicID, "approved")
	var newKey string
	var newFileID int64
	if err := f.db.QueryRow(f.ctx, "select object_key,oss_file_id,active_reference_count from skin_texture_blobs where hash=$1", first.TextureHash).Scan(&newKey, &newFileID, &refs); err != nil {
		t.Fatal(err)
	}
	if newKey == oldKey || newFileID == oldFileID || refs != 1 {
		t.Fatal("reincarnation reused deleted physical object incarnation")
	}
	deletion := NewOSSDeletionWorker(f.server.cfg, f.db)
	deletion.drain(f.ctx)
	f.store.mu.Lock()
	_, oldExists := f.store.objects[oldKey]
	_, newExists := f.store.objects[newKey]
	f.store.mu.Unlock()
	if oldExists || !newExists {
		t.Fatal("actual old DELETE destroyed new shared incarnation or leaked retired bytes")
	}
	assertTEST039TextureBytes(t, f.require(t, "", http.MethodGet, fresh.TextureURL, nil, 200), fresh.TextureHash)
}

func TestTEST039ActualDerivedUploadRollbackUsesDurableCompensationAndFreshRetryFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "compensation.png", test039PNG(t, 64, 64, 6))
	if _, err := f.db.Exec(f.ctx, "alter table skin_assets add constraint test039_fail_after_derived_put check(display_name <> 'compensation')"); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	request := skinAssetCreateRequest{FileID: file, Kind: "skin", Name: "compensation", Visibility: "public"}
	f.require(t, f.editor, http.MethodPost, "/api/v1/skins", request, 500)
	after := f.facts(t)
	delete(before, "oss_object_deletion_outbox")
	delete(after, "oss_object_deletion_outbox")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed skin insert committed business/derived file facts")
	}
	var abandonedKey string
	var lineage *int64
	if err := f.db.QueryRow(f.ctx, "select object_key,oss_file_id from oss_object_deletion_outbox where reason='minecraft-texture-transaction-aborted'").Scan(&abandonedKey, &lineage); err != nil {
		t.Fatal(err)
	}
	if lineage != nil || abandonedKey == "" {
		t.Fatal("actual failed derived PUT lacked unregistered durable cleanup")
	}
	f.store.mu.Lock()
	_, exists := f.store.objects[abandonedKey]
	f.store.mu.Unlock()
	if !exists {
		t.Fatal("fixture did not exercise actual external PUT before rollback")
	}
	if _, err := f.db.Exec(f.ctx, "alter table skin_assets drop constraint test039_fail_after_derived_put"); err != nil {
		t.Fatal(err)
	}
	worker := NewOSSDeletionWorker(f.server.cfg, f.db)
	worker.drain(f.ctx)
	f.store.mu.Lock()
	_, exists = f.store.objects[abandonedKey]
	f.store.mu.Unlock()
	if exists {
		t.Fatal("production cleanup failed to delete unregistered external bytes")
	}
	asset := f.asset(t, f.editor, file, "compensation", "public")
	var liveKey string
	if err := f.db.QueryRow(f.ctx, "select object_key from skin_texture_blobs where hash=$1", asset.TextureHash).Scan(&liveKey); err != nil {
		t.Fatal(err)
	}
	if liveKey == abandonedKey {
		t.Fatal("actual retry reused abandoned object incarnation")
	}
	f.review(t, asset.PublicID, "approved")
	assertTEST039TextureBytes(t, f.require(t, "", http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
}

func TestTEST039ConcurrentFullHTTPLibraryAndProfileLimitsSerializeExactlyIntegration(t *testing.T) {
	for _, kind := range []string{"library", "profile"} {
		t.Run(kind, func(t *testing.T) {
			f := newTEST039Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "skin."+kind+".limit.1")
			var operations []test038Operation
			if kind == "library" {
				file := f.upload(t, f.editor, "limit.png", test039PNG(t, 64, 64, 7))
				for index := range 2 {
					operations = append(operations, test038Operation{http.MethodPost, "/api/v1/skins", skinAssetCreateRequest{FileID: file, Kind: "skin", Name: fmt.Sprintf("skin limit %d", index), Visibility: "public"}})
				}
			} else {
				for index := range 2 {
					operations = append(operations, test038Operation{http.MethodPost, "/api/v1/users/me/player-profiles", playerProfileCreateRequest{Name: fmt.Sprintf("Test039Limit%d", index), Visibility: "private"}})
				}
			}
			statuses := map[int]int{}
			for _, row := range f.parallel(t, f.editor, operations) {
				statuses[row.status]++
				if row.status != 201 && row.status != 403 {
					t.Fatalf("concurrent %s returned %d body=%s", kind, row.status, row.raw)
				}
			}
			if statuses[201] != 1 || statuses[403] != 1 {
				t.Fatalf("concurrent %s limit outcomes=%v", kind, statuses)
			}
			var count int
			if kind == "library" {
				var references, wardrobe int
				if err := f.db.QueryRow(f.ctx, "select (select count(*) from skin_assets where owner_id=$1 and status='active'),(select sum(active_reference_count) from skin_texture_blobs),(select count(*) from skin_wardrobe where user_id=$1)", f.userIDs[f.editor]).Scan(&count, &references, &wardrobe); err != nil {
					t.Fatal(err)
				}
				if count != 1 || references != 1 || wardrobe != 1 {
					t.Fatalf("serialized library count/references/wardrobe=%d/%d/%d", count, references, wardrobe)
				}
			} else {
				var defaults, accounts int
				if err := f.db.QueryRow(f.ctx, "select (select count(*) from player_profiles where user_id=$1 and status='active'),(select count(*) from player_profiles where user_id=$1 and status='active' and is_default),(select count(*) from yggdrasil_accounts where user_id=$1)", f.userIDs[f.editor]).Scan(&count, &defaults, &accounts); err != nil {
					t.Fatal(err)
				}
				if count != 1 || defaults != 1 || accounts != 1 {
					t.Fatalf("serialized profiles count/default/accounts=%d/%d/%d", count, defaults, accounts)
				}
			}
		})
	}
}

func TestTEST039FullHTTPInputPermissionAndDailyCreateBudgetsDoNotMutateFactsIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "valid.png", test039PNG(t, 64, 64, 8))
	invalid := f.upload(t, f.editor, "noncanonical.png", test039PNG(t, 65, 64, 9))
	before := f.facts(t)
	request := skinAssetCreateRequest{FileID: file, Kind: "skin", Name: "bounded skin", Visibility: "public"}
	f.require(t, "", http.MethodPost, "/api/v1/skins", request, 401)
	f.require(t, f.denied, http.MethodPost, "/api/v1/skins", request, 403)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/skins", request, 400)
	bad := request
	bad.FileID = invalid
	f.require(t, f.editor, http.MethodPost, "/api/v1/skins", bad, 400)
	bad = request
	bad.Kind = "unknown"
	f.require(t, f.editor, http.MethodPost, "/api/v1/skins", bad, 400)
	f.require(t, f.editor, http.MethodPost, "/api/v1/skins", map[string]any{"fileId": file, "kind": "skin", "name": "bad", "unknownField": true}, 400)
	f.unchanged(t, before)
	asset := f.asset(t, f.editor, file, "daily baseline", "public")
	// Historical deleted entries are legitimate daily-create facts; every FK and trigger remains enabled.
	if _, err := f.db.Exec(f.ctx, `insert into skin_assets(public_id,owner_id,kind,model,blob_hash,display_name,visibility,review_status,status)
		select 's'||lpad(value::text,8,'0'),$1,'skin','default',$2,'daily history','private','rejected','deleted' from generate_series(1,99) value`, f.userIDs[f.editor], asset.TextureHash); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/skins", request, 429)
	f.unchanged(t, before)
}

func TestTEST039ConcurrentReviewAndMissedCleanupRecoverUsingProductionWorkersFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "review-gc.png", test039PNG(t, 64, 64, 10))
	asset := f.asset(t, f.editor, file, "review gc", "public")
	revision := f.pendingRevision(t, asset.PublicID)
	statuses := map[int]int{}
	for _, row := range f.parallel(t, f.reviewer, []test038Operation{
		{http.MethodPatch, "/api/v1/content-revisions/" + revision, map[string]any{"status": "approved", "note": "racing reviewer"}},
		{http.MethodPatch, "/api/v1/content-revisions/" + revision, map[string]any{"status": "rejected", "note": "racing reviewer"}},
	}) {
		statuses[row.status]++
		if row.status != 200 && row.status != 409 {
			t.Fatalf("concurrent review status=%d body=%s", row.status, row.raw)
		}
	}
	if statuses[200] != 1 || statuses[409] != 1 {
		t.Fatalf("concurrent review outcomes=%v", statuses)
	}
	var review string
	var intents, catalog int
	if err := f.db.QueryRow(f.ctx, `select review_status,(select count(*) from nats_outbox where payload->'data'->>'skinId'=$1),
		(select count(*) from skin_public_catalog where asset_id=asset.id) from skin_assets asset where public_id=$1`, asset.PublicID).Scan(&review, &intents, &catalog); err != nil {
		t.Fatal(err)
	}
	if intents != 1 || (review == "approved" && catalog != 1) || (review == "rejected" && catalog != 0) {
		t.Fatalf("review/catalog/intent=%s/%d/%d", review, catalog, intents)
	}
	// An owned historical missed request-time cleanup state, not a claim that this uses DELETE HTTP.
	if _, err := f.db.Exec(f.ctx, "update skin_assets set status='deleted' where public_id=$1", asset.PublicID); err != nil {
		t.Fatal(err)
	}
	maintenance := NewMaintenanceWorker(f.db, f.server.cache)
	maintenance.pruneSkinTextureBlobs(f.ctx)
	var key, status string
	var refs, queued int
	if err := f.db.QueryRow(f.ctx, `select blob.object_key,blob.active_reference_count,file.status,
		(select count(*) from oss_object_deletion_outbox where oss_file_id=file.id) from skin_texture_blobs blob join oss_files file on file.id=blob.oss_file_id where blob.hash=$1`, asset.TextureHash).Scan(&key, &refs, &status, &queued); err != nil {
		t.Fatal(err)
	}
	if refs != 0 || status != "deleted" || queued != 1 {
		t.Fatalf("production GC ref/status/intents=%d/%s/%d", refs, status, queued)
	}
	f.require(t, "", http.MethodGet, asset.TextureURL, nil, 404)
	NewOSSDeletionWorker(f.server.cfg, f.db).drain(f.ctx)
	f.store.mu.Lock()
	_, exists := f.store.objects[key]
	f.store.mu.Unlock()
	if exists {
		t.Fatal("production GC/DELETE left actual retired bytes")
	}
	before := f.facts(t)
	maintenance.pruneSkinTextureBlobs(f.ctx)
	NewOSSDeletionWorker(f.server.cfg, f.db).drain(f.ctx)
	f.unchanged(t, before)
}

func TestTEST039ActualPGTextureLineageReadFailureIsObservableAndRecoversFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "lineage.png", test039PNG(t, 64, 64, 11))
	asset := f.asset(t, f.editor, file, "lineage read", "public")
	f.review(t, asset.PublicID, "approved")
	before := f.facts(t)
	if _, err := f.db.Exec(f.ctx, "alter table skin_texture_blobs rename column size_bytes to test039_actual_size"); err != nil {
		t.Fatal(err)
	}
	// The launcher protocol's existing internal-failure contract is structured 503.
	f.require(t, "", http.MethodGet, asset.TextureURL, nil, 503)
	f.conditionalTexture(t, asset.TextureURL, asset.TextureHash, 503)
	if _, err := f.db.Exec(f.ctx, "alter table skin_texture_blobs rename column test039_actual_size to size_bytes"); err != nil {
		t.Fatal(err)
	}
	assertTEST039TextureBytes(t, f.require(t, "", http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
	f.conditionalTexture(t, asset.TextureURL, asset.TextureHash, 304)
	f.unchanged(t, before)
}
