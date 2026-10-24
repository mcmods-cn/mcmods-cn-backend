package httpapi

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func auditGR001ICCFixture(t *testing.T) ([]byte, []byte, *image.NRGBA) {
	t.Helper()
	profile, err := os.ReadFile("testdata/audit-20261024/icc-srgb-private-label.icc")
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(profile); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte(auditGR001PrivateMarker+"\x00\x00"), compressed.Bytes()...)
	chunk := make([]byte, len(payload)+12)
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
	copy(chunk[4:8], "iCCP")
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	pixels := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixels.SetNRGBA(0, 0, color.NRGBA{R: 137, G: 42, B: 78, A: 202})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	raw := encoded.Bytes()
	enriched := append(append(append([]byte(nil), raw[:33]...), chunk...), raw[33:]...)
	return profile, enriched, pixels
}

func TestAuditGR001ICCMetadataRemovedWhileColorTransformsRemain(t *testing.T) {
	profile, enriched, pixels := auditGR001ICCFixture(t)
	clean, err := minimizeExportPNG(enriched)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(clean, []byte(auditGR001PrivateMarker)) {
		t.Error("PNG ICC profile-name metadata remains public")
	}
	var sanitized []byte
	for offset := 8; offset < len(clean); {
		n := int(binary.BigEndian.Uint32(clean[offset : offset+4]))
		if string(clean[offset+4:offset+8]) == "iCCP" {
			p := clean[offset+8 : offset+8+n]
			start := bytes.IndexByte(p, 0) + 2
			r, err := zlib.NewReader(bytes.NewReader(p[start:]))
			if err != nil {
				t.Fatal(err)
			}
			sanitized, err = io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		offset += 12 + n
	}
	if len(sanitized) == 0 {
		t.Fatal("required ICC profile was discarded")
	}
	utf16Marker := []byte{}
	for _, b := range []byte(auditGR001PrivateMarker) {
		utf16Marker = append(utf16Marker, 0, b)
	}
	if bytes.Contains(sanitized, []byte(auditGR001PrivateMarker)) || bytes.Contains(sanitized, utf16Marker) {
		t.Error("ICC descriptive metadata remains public")
	}
	tags := func(p []byte) map[string][]byte {
		t.Helper()
		result := map[string][]byte{}
		count := int(binary.BigEndian.Uint32(p[128:132]))
		for i := 0; i < count; i++ {
			entry := 132 + 12*i
			off := int(binary.BigEndian.Uint32(p[entry+4 : entry+8]))
			size := int(binary.BigEndian.Uint32(p[entry+8 : entry+12]))
			result[string(p[entry:entry+4])] = p[off : off+size]
		}
		return result
	}
	originalTags, newTags := tags(profile), tags(sanitized)
	for _, tag := range []string{"wtpt", "chad", "rXYZ", "gXYZ", "bXYZ", "rTRC", "gTRC", "bTRC", "chrm"} {
		if len(originalTags[tag]) == 0 || !bytes.Equal(originalTags[tag], newTags[tag]) {
			t.Errorf("required color-transform tag changed/discarded: %s", tag)
		}
	}
	decoded, err := png.Decode(bytes.NewReader(clean))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.At(0, 0) != pixels.At(0, 0) {
		t.Error("ICC metadata removal changed pixel/alpha")
	}
	if directory := os.Getenv("MCMODS_AUDIT_ICC_EVIDENCE_DIR"); directory != "" {
		// The caller explicitly chooses a new evidence directory. Exclusive
		// creation establishes ownership without relying on cloud-only paths.
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"source.icc": profile, "sanitized.icc": sanitized, "sanitized.png": clean} {
			file, err := os.OpenFile(filepath.Join(directory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = file.Write(data)
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatal("write task ICC evidence failed")
			}
		}
	}
}

func TestAuditGR001ICCTagRejectsUnregisteredTrailingPayload(t *testing.T) {
	curve := []byte("para\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00")
	if !registeredExportICCColorTag("rTRC", curve) {
		t.Fatal("known parametric curve rejected")
	}
	if registeredExportICCColorTag("rTRC", append(curve, []byte(auditGR001PrivateMarker)...)) {
		t.Fatal("non-transform trailing metadata inherited ICC color permission")
	}
	_, enriched, _ := auditGR001ICCFixture(t)
	corrupt := append([]byte(nil), enriched...)
	corrupt[len(corrupt)-1] ^= 1
	if _, err := minimizeExportPNG(corrupt); err == nil || err.Error() != "invalid PNG chunk checksum" {
		t.Fatalf("expected exact invalid PNG checksum rejection, got %v", err)
	}
}

func TestAuditGR001ICCProjectionPersistsAndServesSanitizedAssetIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, enriched, pixels := auditGR001ICCFixture(t)
	assetPath := "assets/example/textures/item/icc.png"
	_, revision := f.imported(t, auditFixGRZip(t, map[string]string{assetPath: string(enriched)}))
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets/content?path="+url.QueryEscape(assetPath)+"&proxy=renderer", nil, 403)
	f.reviewGR(t, revision, "approved")
	served := f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets/content?path="+url.QueryEscape(assetPath)+"&proxy=renderer", nil, 200)
	clean, err := minimizeExportPNG(enriched)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(clean, served) {
		t.Error("actual public storage bytes differ from sanitized ICC projection")
	}
	repeated, err := minimizeExportPNG(served)
	if err != nil || !bytes.Equal(repeated, served) {
		t.Error("ICC projection is not idempotent")
	}
	decoded, err := png.Decode(bytes.NewReader(served))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.At(0, 0) != pixels.At(0, 0) {
		t.Error("served PNG pixel/alpha changed")
	}
	var digest string
	if err := f.db.QueryRow(f.ctx, `select file.sha256 from catalog_import_media media join oss_files file on file.id=media.oss_file_id where media.revision_id=$1 and media.asset_path=$2`, revision, assetPath).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if digest != sha256Hex(served) {
		t.Error("persisted digest did not describe actual clean ICC bytes")
	}
}
