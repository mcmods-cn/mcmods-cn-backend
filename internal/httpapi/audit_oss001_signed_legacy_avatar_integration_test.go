package httpapi

import (
	"bytes"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These are explicitly old-data/fault fixtures, not evidence that a supported
// current or frozen creator API wrote a signature into a creator row. The
// actual business-reachable05 failure wrote signatures into user_drafts only.
func TestAuditOSS001BoundSignedLegacyReferenceHasSafeDTOAndCanBeRetainedByAnotherEditorHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	id, fileID, stable := auditOSS001RetainedAvatarSetup(t, f, "author")
	var key string
	if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, fileID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	cfg := f.server.ossConfigFromSettings(f.ctx)
	access, err := f.server.resolveOSSObjectAccessWithConfig(f.ctx, cfg, key, ossObjectAccessOptions{Expires: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(access.URL)
	if err != nil {
		t.Fatal("owned actual SDK URL malformed")
	}
	protocol := &auditOSS001PrivateProtocol{cfg: cfg}
	request, err := http.NewRequest(http.MethodGet, access.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if valid, _ := protocol.authorize(request); !valid {
		t.Fatal("must seed an actual SDK capability, not an arbitrary403 sample")
	}
	at, err := time.Parse("20060102T150405Z", parsed.Query().Get("x-oss-date"))
	if err != nil {
		t.Fatal(err)
	}
	seconds, err := strconv.Atoi(parsed.Query().Get("x-oss-expires"))
	if err != nil {
		t.Fatal(err)
	}
	if delay := time.Until(at.Add(time.Duration(seconds)*time.Second)) + 30*time.Millisecond; delay > 0 {
		time.Sleep(delay)
	}
	if valid, reason := protocol.authorize(request); valid || reason != "expired_query" {
		t.Fatal("legacy sample must have actual wall-clock expiration")
	}
	// Preserve current file FK, public creator and its actual byte/scan proof;
	// only the old stored URL is the controlled legacy corruption.
	if _, err = f.db.Exec(f.ctx, `update creators set avatar_url=$2 where public_id=$1`, id, access.URL); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "creator.edit."+id)
	t.Run("safe_current_DTO", func(t *testing.T) {
		detail := decodeTEST022Data[struct {
			Creator struct {
				AvatarURL string `json:"avatarUrl"`
			} `json:"creator"`
			Storage string `json:"avatarStorageUrl"`
			Needs   bool   `json:"avatarReferenceNeedsVerification"`
		}](t, f.require(t, f.otherEditor, http.MethodGet, "/api/v1/creators/"+id, nil, 200))
		if detail.Storage != stable || detail.Needs || strings.Contains(detail.Storage, "x-oss-signature") {
			t.Fatal("explicit stored DTO must be canonical only after the bound identity is proved")
		}
		fresh, err := http.NewRequest(http.MethodGet, detail.Creator.AvatarURL, nil)
		if err != nil {
			t.Fatal("fresh preview missing")
		}
		if valid, _ := protocol.authorize(fresh); !valid {
			t.Fatal("detail must produce a fresh actual SDK capability, not replay the expired reference")
		}
		var current string
		if err = f.db.QueryRow(f.ctx, `select avatar_url from creators where public_id=$1`, id).Scan(&current); err != nil || current != access.URL {
			t.Fatal("read-only DTO normalization must not silently migrate published data", err)
		}
	})
	t.Run("authorized_current_retention", func(t *testing.T) {
		changed := decodeTEST022Data[struct {
			Revision string `json:"revisionId"`
			Status   string `json:"reviewStatus"`
		}](t, f.require(t, f.otherEditor, http.MethodPut, "/api/v1/creators/"+id, auditOSS001ProfilePayload("author", id, fileID, stable), 200))
		if changed.Status != "pending" {
			t.Fatal("legacy normalization still requires independent review")
		}
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+changed.Revision, map[string]any{"status": "approved", "note": "Normalize only proven currently bound legacy image"}, 200)
		var stored, currentFile string
		if err = f.db.QueryRow(f.ctx, `select c.avatar_url,file.public_id from creators c join oss_files file on file.id=c.avatar_file_id where c.public_id=$1`, id).Scan(&stored, &currentFile); err != nil || stored != stable || currentFile != fileID {
			t.Fatal("current proven asset must survive another editor, without persisting the legacy capability", err)
		}
	})
}

func TestAuditOSS001UnboundSignedLegacyReferenceHoldsWithoutClearingPublishedOrCopyingCapabilitiesHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	id := auditOSS001CreateReviewedCreator(t, f, "author")
	legacy := "https://legacy.example.invalid/avatar.png?Signature=synthetic-legacy-capability"
	if _, err := f.db.Exec(f.ctx, `update creators set avatar_url=$2,avatar_file_id=null where public_id=$1`, id, legacy); err != nil {
		t.Fatal(err)
	}
	detail := decodeTEST022Data[struct {
		Storage string `json:"avatarStorageUrl"`
		Needs   bool   `json:"avatarReferenceNeedsVerification"`
	}](t, f.require(t, f.editor, http.MethodGet, "/api/v1/creators/"+id, nil, 200))
	if detail.Storage != "" || !detail.Needs {
		t.Error("unbound signed legacy reference must be a precise data HOLD, never labelled stable")
	}
	before := auditOSS001CreatorWriteFacts(t, f, id)
	f.require(t, f.editor, http.MethodPut, "/api/v1/creators/"+id, auditOSS001ProfilePayload("author", id, "", legacy), 400)
	if after := auditOSS001CreatorWriteFacts(t, f, id); !bytes.Equal(before, after) {
		t.Fatal("unverifiable legacy must not be deleted, rebound or copied into a new revision")
	}
}

func TestAuditOSS001ExternalStableLegacyQueryRemainsAnUnchangedReferenceHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	id := auditOSS001CreateReviewedCreator(t, f, "team")
	stable := "https://legacy-images.example.invalid/avatar.png?size=256"
	if _, err := f.db.Exec(f.ctx, `update creators set avatar_url=$2,avatar_file_id=null where public_id=$1`, id, stable); err != nil {
		t.Fatal(err)
	}
	changed := decodeTEST022Data[struct {
		Revision string `json:"revisionId"`
	}](t, f.require(t, f.editor, http.MethodPut, "/api/v1/creators/"+id, auditOSS001ProfilePayload("team", id, "", stable), 200))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+changed.Revision, map[string]any{"status": "approved", "note": "Retain the exact old external stable reference; no byte/ownership claim"}, 200)
	var stored string
	var file *int64
	if err := f.db.QueryRow(f.ctx, `select avatar_url,avatar_file_id from creators where public_id=$1`, id).Scan(&stored, &file); err != nil || stored != stable || file != nil {
		t.Fatal("legitimate old business query must not be stripped or silently rebound", err)
	}
}
