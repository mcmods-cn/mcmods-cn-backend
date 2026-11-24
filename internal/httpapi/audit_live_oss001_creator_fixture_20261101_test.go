package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

// Ordinary HTTP creates and an independent reviewer publish all creator facts.
// Only actor/RBAC/OSS config are isolated test prerequisites. The external
// browser must prove UI flow and persisted stable state; READY alone is no PASS.
func TestAuditLiveOSS001CreatorAvatar20261101Fixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_OSS001_CREATOR_20261101") != "1" {
		t.Skip("explicit owned real creator avatar browser fixture")
	}
	directory := os.Getenv("MCMODS_AUDIT_OSS001_DIR")
	authPath := os.Getenv("MCMODS_AUDIT_OSS001_PRIVATE_AUTH")
	frontend := os.Getenv("MCMODS_AUDIT_OSS001_FRONTEND_REPO")
	if !filepath.IsAbs(directory) || !filepath.IsAbs(authPath) || !filepath.IsAbs(frontend) || os.Getenv("SSL_CERT_FILE") == "" {
		t.Fatal("absolute owned paths and explicit inherited+task CA trust required")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(owner, "mcmods-oss001-creator-20261101 pid=%d\n", os.Getpid())
	owner.Close()
	raw, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	checkouts := auditLiveSourceCheckouts(strings.TrimSpace(string(raw)), frontend)
	pins := checkouts.revisions(t)
	revisions := map[string]any{"backend": pins["mcmods-cn-backend"], "frontend": pins["mcmods-cn-frontend"], "exporter": pins["mcmods_exporter"]}
	if revisions["backend"].(map[string]any)["commit"] != os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT") {
		t.Fatal("exact full backend source pin required")
	}
	const publicOrigin = "https://127.0.0.1:3431"
	t.Setenv("FRONTEND_ORIGIN", publicOrigin)
	unlockDDL := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlockDDL() })
	f := newAuditFixGRFixture(t, 45*time.Minute)
	unlockDDL()
	var replacement *httptest.Server
	defer func() {
		f.origin.CloseClientConnections()
		f.origin.Close()
		f.provider.CloseClientConnections()
		f.provider.Close()
		if replacement != nil {
			replacement.CloseClientConnections()
			replacement.Close()
		}
		unlockDDL = auditGlobalCatalogPGLock(t)
	}()
	cfg := f.server.ossConfigFromSettings(f.ctx)
	protocol := &auditOSS001PrivateProtocol{cfg: cfg}
	oldProvider := f.provider
	next := oldProvider.Config.Handler
	oldProvider.Close()
	replacement = httptest.NewServer(protocol.handler(next))
	f.provider = replacement
	cfg.Endpoint = publicOrigin
	cfg.PublicEndpoint = publicOrigin
	cfg.DownloadURLTTLMinutes = 1
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealed); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(directory, "runtime.json")
	tlsReady := filepath.Join(directory, "TLS_READY")
	stopPath := filepath.Join(directory, "STOP")
	var schema string
	if err = f.db.QueryRow(f.ctx, "select current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	runtime := map[string]any{"state": "BOOTSTRAP_WAIT_TLS_PROXY", "pid": os.Getpid(), "ownedSchema": schema, "apiOrigin": f.origin.URL, "internalOSSOrigin": replacement.URL, "frontendOrigin": publicOrigin, "ossBucketPathPrefix": "/" + cfg.Bucket + "/", "tlsReadyPath": tlsReady, "stopPath": stopPath, "repoRevisions": revisions, "antiAbuseEnabled": f.server.cfg.AntiAbuse.Enabled, "privateProtocol": "Actual SDK OSS V4 query/header signatures validated, unsigned/tampered/wall-clock expired requests rejected. Protocol fixture, not cloud OSS/CDN.", "downloadTTLMinutes": 1, "TTLPolicy": "Supported configured one-minute minimum, actual wall-clock expiry; no TTL/security bypass.", "privateAuthPath": authPath, "scope": "Actual ordinary creator HTTP create, independent review and upload/attachment. External native UI must verify create/update/stable refs/decoded private images/recovery. No seeded approved creators."}
	auditJourneyWriteJSON(t, runtimePath, runtime)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err = os.Stat(tlsReady); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case <-f.ctx.Done():
			t.Fatal("TLS bootstrap deadline")
		case <-ticker.C:
		}
	}
	probe, err := http.NewRequestWithContext(f.ctx, http.MethodGet, publicOrigin+"/api/v1/modpacks/oss001-owned-probe-missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(probe)
	if err != nil {
		t.Fatal("verified TLS gateway unavailable")
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.TLS == nil || len(response.TLS.VerifiedChains) == 0 || response.StatusCode != 404 {
		t.Fatal("verified actual TLS/API404 required")
	}
	digest := sha256.Sum256(response.TLS.PeerCertificates[0].Raw)
	if hex.EncodeToString(digest[:]) != os.Getenv("MCMODS_AUDIT_EXPECT_TLS_LEAF_SHA256") {
		t.Fatal("wrong actual TLS leaf")
	}
	password := "Audit-OSS001-Creator-20261101!"
	encoded, err := security.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var username, publicID string
	if err = f.db.QueryRow(f.ctx, `select username,public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&username, &publicID); err != nil {
		t.Fatal(err)
	}

	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "user.draft.retention_seconds.3600")
	runtime["actor"] = map[string]any{"publicId": publicID, "ordinary": true, "adminWildcardGranted": false, "creatorCreate": true, "draftRetentionSeconds": 3600, "editScopes": "only exact per-creator fixture grants"}
	profiles := []map[string]any{}
	for index, kind := range []string{"author", "team"} {
		// Use a legitimately shared, already validated file as the starting
		// legacy avatar; each creator is created with it in one ordinary request.
		// This avoids inventing a profile text edit or bypassing anti-abuse merely
		// to initialize the unrelated initial-detail/autosave regression.
		_, fileID := auditOSS001Upload(t, f, f.editor, "mod_resource:audit-gr-"+f.modCode+":item:icon", auditOSS001PNG(t, uint8(60+index*80)))
		var key string
		if err = f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, fileID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		stable := ossStoredObjectURL(cfg, key)
		id := auditOSS001CreateReviewedCreatorWithAvatar(t, f, kind, fileID, stable)
		var name, revision string
		if err = f.db.QueryRow(f.ctx, `select c.name,r.public_id from creators c join content_revisions r on r.id=c.published_revision_id where c.public_id=$1`, id).Scan(&name, &revision); err != nil {
			t.Fatal(err)
		}

		var detail struct {
			Creator struct {
				AvatarURL string `json:"avatarUrl"`
			} `json:"creator"`
			AvatarFileID string `json:"avatarFileId"`
			CanEdit      bool   `json:"canEditProfile"`
		}
		detail = decodeTEST022Data[struct {
			Creator struct {
				AvatarURL string `json:"avatarUrl"`
			} `json:"creator"`
			AvatarFileID string `json:"avatarFileId"`
			CanEdit      bool   `json:"canEditProfile"`
		}](t, f.require(t, f.editor, http.MethodGet, "/api/v1/creators/"+id, nil, 200))
		parsed, err := url.Parse(detail.Creator.AvatarURL)
		if err != nil || parsed.RawQuery == "" || detail.AvatarFileID != fileID || !detail.CanEdit {
			t.Fatal("actual creator API lacks signed preview/file/exact edit capability")
		}
		signedResponse, err := (&http.Client{Timeout: 10 * time.Second}).Get(detail.Creator.AvatarURL)
		if err != nil {
			t.Fatal("actual signed preview unavailable")
		}
		io.Copy(io.Discard, signedResponse.Body)
		signedResponse.Body.Close()
		if signedResponse.StatusCode != 200 {
			t.Fatal("valid actual SDK preview signature rejected", protocol.snapshot())
		}
		unsignedResponse, err := (&http.Client{Timeout: 10 * time.Second}).Get(stable)
		if err != nil {
			t.Fatal("unsigned owned image probe failed")
		}
		io.Copy(io.Discard, unsignedResponse.Body)
		unsignedResponse.Body.Close()
		if unsignedResponse.StatusCode != 403 {
			t.Fatal("private fixture must reject stable unsigned object")
		}
		profiles = append(profiles, map[string]any{"kind": kind, "publicId": id, "name": name, "filePublicId": fileID, "objectKey": key, "stableReference": stable, "publishedRevisionId": revision, "apiReturnedSignedPreview": true, "initialAvatarSource": "Legitimate same-uploader verified Mod resource raster; actual creator PUT and independent review; no seeded approved creator or scan override", "editPath": map[string]string{"author": "/authors/", "team": "/teams/"}[kind] + id + "/edit"})
	}
	// Password rotation invalidates the fixture bearer by design. Perform all
	// HTTP business prerequisites first, then let the native browser log in with
	// the new password/current auth version; never reuse the invalidated bearer.
	if _, err = f.db.Exec(f.ctx, `update users set password_hash=$2 where id=$1`, f.userIDs[f.editor], encoded); err != nil {
		t.Fatal(err)
	}
	authFile, err := os.OpenFile(authPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(authFile).Encode(map[string]any{"username": username, "password": password, "publicId": publicID}); err != nil {
		authFile.Close()
		t.Fatal(err)
	}
	authFile.Close()
	runtime["profiles"] = profiles
	runtime["state"] = "READY"
	runtime["privateProtocolObservations"] = protocol.snapshot()
	runtime["reviewIPC"] = map[string]any{"requestPattern": "review-request-%02d.json", "resultPattern": "review-result-%02d.json", "requestFields": []string{"creatorId", "revisionId", "fileId"}, "maxRequests": 12, "scope": "Existing independent synthetic reviewer performs actual HTTP approval of the exact ordinary native actor pending creator/avatar revision; no grant or seeded approval"}
	auditJourneyWriteJSON(t, runtimePath, runtime)
	t.Log("real ordinary author/team creation, independent review and private signed-avatar fixture READY; no credential/query values retained")
	reviews := []map[string]any{}
	nextReview := 1
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal("owned creator avatar fixture expired")
		case <-ticker.C:
			if nextReview <= 12 {
				if receipt, processed := auditOSS001ReviewNativeRequest(t, f, cfg, directory, nextReview); processed {
					reviews = append(reviews, receipt)
					nextReview++
					runtime["independentNativeReviewReceipts"] = reviews
					auditJourneyWriteJSON(t, runtimePath, runtime)
				}
			}
			if _, err = os.Stat(stopPath); err == nil {
				drafts := []map[string]any{}
				for _, profile := range profiles {
					observation, err := auditOSS001CurrentDraft(f.ctx, f.db, f.userIDs[f.editor], "creator:"+profile["publicId"].(string))
					if err != nil {
						t.Fatal("could not observe the current creator draft:", err)
					}
					observation["kind"] = profile["kind"]
					drafts = append(drafts, observation)
				}
				runtime["draftObservations"] = drafts
				runtime["finalOwnedDatabaseFacts"] = auditOSS001NativeFinalFacts(t, f)
				runtime["privateProtocolObservations"] = protocol.snapshot()
				runtime["state"] = "STOPPED"
				auditJourneyWriteJSON(t, runtimePath, runtime)
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}

func auditOSS001CurrentDraft(ctx context.Context, db *pgxpool.Pool, owner int64, key string) (map[string]any, error) {
	var stored string
	var fileID *string
	err := db.QueryRow(ctx, `select coalesce(payload->>'avatarUrl',''),payload->>'avatarFileId'
		from user_drafts where user_id=$1 and draft_key=$2 and submitted_at is null`, owner, key).Scan(&stored, &fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]any{"draftFound": false}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"draftFound": true, "avatarFileId": fileID, "avatarUrlNonempty": stored != "",
		"temporarySignaturePersisted": temporaryCreatorAvatarReference(stored),
		"avatarUrlSHA256":             fmt.Sprintf("%x", sha256.Sum256([]byte(stored))),
	}, nil
}

func TestAuditOSS001CurrentDraftReceiptDistinguishesAbsenceFromReadFailure(t *testing.T) {
	f := newAuditFixGRFixture(t, 90*time.Second)
	key := "creator:" + f.modCode
	owner := f.userIDs[f.editor]
	if got, err := auditOSS001CurrentDraft(f.ctx, f.db, owner, key); err != nil || got["draftFound"] != false {
		t.Fatalf("missing draft observation: %v, %v", got, err)
	}
	auditOSS001DraftToolSave(t, f, f.editor, key, "author", map[string]any{"name": "Private draft without an avatar"})
	if got, err := auditOSS001CurrentDraft(f.ctx, f.db, owner, key); err != nil || got["draftFound"] != true || got["avatarUrlNonempty"] != false {
		t.Fatalf("draft with optional avatar omitted: %v, %v", got, err)
	}
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	if got, err := auditOSS001CurrentDraft(cancelled, f.db, owner, key); err == nil || got != nil {
		t.Fatalf("cancelled query returned a usable observation: %v, %v", got, err)
	}
}
