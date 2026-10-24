package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"testing"
)

// Only read-only TP007 ancestors are bootstrapped here. The PNG itself uses the
// real ordinary-user presign/PUT/completion flow. Direct ancestor publication
// is declared explicitly and cannot establish project creation/review UI PASS.
func auditGlobalCatalogTP007Ancestors(t *testing.T, f auditFixGRFixture, directory string) map[string]any {
	t.Helper()
	cfg := f.server.ossConfigFromSettings(f.ctx)
	if publicOrigin := os.Getenv("MCMODS_AUDIT_PUBLIC_OSS_ORIGIN"); publicOrigin != "" {
		parsed, err := url.Parse(publicOrigin)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			t.Fatal("explicit HTTPS public OSS origin required")
		}
		cfg.PublicEndpoint = publicOrigin
		sealed, err := f.server.sealSystemSetting(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealed); err != nil {
			t.Fatal(err)
		}
	}
	picture := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			picture.SetNRGBA(x, y, color.NRGBA{uint8(x * 4), uint8(y * 4), 120, uint8(128 + (x+y)%128)})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	source := encoded.Bytes()
	sourceDigest := sha256.Sum256(source)
	sourceSHA := hex.EncodeToString(sourceDigest[:])
	sourcePath := filepath.Join(directory, "tp007-source.png")
	if err := os.WriteFile(sourcePath, source, 0600); err != nil {
		t.Fatal(err)
	}
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", map[string]any{"originalName": "tp007-icon.png", "contentType": "image/png", "source": "tp007_icon", "category": "mod_icon", "sizeBytes": len(source), "sha256": sourceSHA}, 200))
	if !ticket.UploadRequired || ticket.Method != http.MethodPut || ticket.Multipart.UploadID != "" || ticket.SHA256 != sourceSHA {
		t.Fatal("expected ordinary small-file upload ticket")
	}
	request, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range ticket.Headers {
		request.Header.Set(key, value)
	}
	response, err := f.provider.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("controlled OSS PUT failed", response.StatusCode)
	}
	type completed struct {
		ID          string `json:"id"`
		StorageURL  string `json:"storageUrl"`
		ContentType string `json:"contentType"`
		SourceSHA   string `json:"sourceSha256"`
		StoredSHA   string `json:"storedSha256"`
		ScanStatus  string `json:"scanStatus"`
		SizeBytes   int64  `json:"sizeBytes"`
	}
	result := decodeTEST022Data[completed](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	if result.ID == "" || result.StorageURL == "" || result.ContentType != "image/png" || result.SourceSHA != sourceSHA || result.ScanStatus != "clean" {
		t.Fatal("ordinary icon completion lacks validated scanned PNG facts")
	}
	f.objectMu.Lock()
	stored, exists := f.objects[ticket.ObjectKey]
	actual := append([]byte(nil), stored.data...)
	f.objectMu.Unlock()
	actualDigest := sha256.Sum256(actual)
	actualSHA := hex.EncodeToString(actualDigest[:])
	decoded, err := png.Decode(bytes.NewReader(actual))
	if err != nil || !exists || decoded.Bounds() != picture.Bounds() || actualSHA != result.StoredSHA || int64(len(actual)) != result.SizeBytes {
		t.Fatal("actual stored icon bytes do not match completed file")
	}
	var fileOwner int64
	var fileStatus, fileScan, fileSHA string
	if err = f.db.QueryRow(f.ctx, `select uploader_id,status,scan_status,stored_sha256 from oss_files where public_id=$1`, result.ID).Scan(&fileOwner, &fileStatus, &fileScan, &fileSHA); err != nil {
		t.Fatal(err)
	}
	if fileOwner != f.userIDs[f.editor] || fileStatus != "active" || fileScan != "clean" || fileSHA != actualSHA {
		t.Fatal("actual public file relation mismatch")
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	mods := make([]map[string]any, 0, 2)
	var modIDs []int64
	for index, icon := range []string{result.StorageURL, ""} {
		publicID := randomCatalogPublicID()
		slug := "audit-tp007-" + publicID
		var internalID, revisionID int64
		if err = tx.QueryRow(f.ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by,icon_url) values($1,$2,$3,'approved',$4,$5) returning id`, publicID, slug, "TP007 recorded Mod "+publicID, f.userIDs[f.editor], icon).Scan(&internalID); err != nil {
			t.Fatal(err)
		}
		snapshot, _ := json.Marshal(map[string]any{"iconUrl": icon, "primaryName": "TP007 recorded Mod " + publicID, "fixtureAncestor": true})
		digest := sha256.Sum256(snapshot)
		if err = tx.QueryRow(f.ctx, `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by) values('mod',$1,'mod',$2,1,$3::jsonb,$4,$5) returning id`, internalID, publicID, snapshot, hex.EncodeToString(digest[:]), f.userIDs[f.editor]).Scan(&revisionID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(f.ctx, `insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,submitted_by,resolved_at) values('mod',$1,'mod',$2,$3,'approved',$4,now())`, internalID, publicID, revisionID, f.userIDs[f.editor]); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(f.ctx, `update mods set published_revision_id=$1,published_at=now() where id=$2`, revisionID, internalID); err != nil {
			t.Fatal(err)
		}
		modIDs = append(modIDs, internalID)
		mods = append(mods, map[string]any{"publicId": publicID, "siteId": slug, "hasIcon": index == 0})
	}
	var packID int64
	var packPublicID string
	packSlug := "audit-tp007-pack-" + randomCatalogPublicID()
	if err = tx.QueryRow(f.ctx, `insert into modpacks(slug,primary_name,default_locale,review_status,submitted_by,published_at,packaging_method) values($1,'TP007 stored icon and empty icon','en-US','approved',$2,now(),'manual') returning id,public_id`, packSlug, f.userIDs[f.editor]).Scan(&packID, &packPublicID); err != nil {
		t.Fatal(err)
	}
	for index, id := range modIDs {
		if _, err = tx.Exec(f.ctx, `insert into modpack_mods(modpack_id,mod_id,provider,display_order) values($1,$2,'manual',$3)`, packID, id, index); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual anonymous API without retaining signed access URLs.
	pack := decodeTEST022Data[modpackResponse](t, f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+packSlug, nil, 200))
	if len(pack.Mods) != 2 || pack.Mods[0].IconURL == "" || pack.Mods[1].IconURL != "" || !pack.Mods[0].Resolved || !pack.Mods[1].Resolved {
		t.Fatal("anonymous modpack did not preserve scanned icon and empty fallback")
	}
	return map[string]any{"modpackPublicId": packPublicID, "modpackSiteId": packSlug, "mods": mods, "filePublicId": result.ID, "sourcePNGPath": sourcePath, "sourceSHA256": sourceSHA, "storedSHA256": actualSHA, "sourceBytes": len(source), "storedBytes": len(actual), "width": 64, "height": 64, "internalOSSOrigin": f.provider.URL, "bucket": cfg.Bucket, "publicOSSOrigin": cfg.PublicEndpoint, "exactObjectPath": "/" + ticket.ObjectKey, "objectPathPrefix": "/" + path.Dir(ticket.ObjectKey) + "/", "setupLayer": "Real ordinary presign+PUT+completion; explicit synthetic approved ancestor Mod/revision/request/pointer and modpack rows, not creation/review UI or vendor import proof", "downloadURLContract": "Current SDK signs cfg.Endpoint; PublicEndpoint is the stable reference only. No JSON/signedURL/header rewrite."}
}
