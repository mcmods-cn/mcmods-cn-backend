package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestAuditOSS001AvatarPreviewRequiresCurrentPurposeOwnerAndMatchingPublicIdentityHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	id := auditOSS001CreateReviewedCreator(t, f, "author")
	_, fileID := auditOSS001Upload(t, f, f.editor, "creator_avatar:author:"+id, auditOSS001PNG(t, 90))
	_, otherFile := auditOSS001Upload(t, f, f.editor, "mod_resource:audit-gr-"+f.modCode+":item:icon", auditOSS001PNG(t, 170))
	var key, otherKey string
	if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, fileID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, otherFile).Scan(&otherKey); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/users/me/files/presign"
	auditOSS001PreviewWithinRateWindow(t, f, f.editor, path, map[string]any{"fileId": fileID}, 200)
	auditOSS001PreviewWithinRateWindow(t, f, f.editor, path, map[string]any{"objectKey": key}, 200)
	var beforeStats, beforeOutbox int64
	if err := f.db.QueryRow(f.ctx, `select coalesce((select downloads from oss_download_stats where object_key=$1),0),(select count(*) from activity_event_outbox)`, key).Scan(&beforeStats, &beforeOutbox); err != nil {
		t.Fatal(err)
	}
	mismatch := auditOSS001PreviewWithinRateWindow(t, f, f.editor, path, map[string]any{"fileId": fileID, "objectKey": otherKey}, 400)
	var problem struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(mismatch, &problem); err != nil || problem.Error != "文件 ID 与 ObjectKey 不一致" {
		t.Fatal("must reject the exact mismatched public identity", err)
	}
	auditOSS001PreviewWithinRateWindow(t, f, f.otherEditor, path, map[string]any{"fileId": fileID}, 403)
	auditOSS001PreviewWithinRateWindow(t, f, f.reviewer, path, map[string]any{"fileId": fileID}, 403)
	auditOSS001PreviewWithinRateWindow(t, f, f.editor, path, map[string]any{"fileId": otherFile}, 400)
	if _, err := f.db.Exec(f.ctx, `update user_permissions own set allow=false from permissions definition where own.permission_id=definition.id and own.user_id=$1 and definition.code=$2`, f.userIDs[f.editor], "creator.edit."+id); err != nil {
		t.Fatal(err)
	}
	auditOSS001PreviewWithinRateWindow(t, f, f.editor, path, map[string]any{"fileId": fileID}, 400)
	var afterStats, afterOutbox int64
	if err := f.db.QueryRow(f.ctx, `select coalesce((select downloads from oss_download_stats where object_key=$1),0),(select count(*) from activity_event_outbox)`, key).Scan(&afterStats, &afterOutbox); err != nil || afterStats != beforeStats || afterOutbox != beforeOutbox {
		t.Fatal("denied preview must not disclose a ticket or add activity/download counts", err)
	}
}

func TestAuditOSS001CreatorStagingRejectsUnsupportedKindPermissionAndInvalidRasterHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	body := auditOSS001PresignBody(auditOSS001PNG(t, 80), "creator_avatar:author:new")
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", body, 403)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.create")
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", auditOSS001PresignBody(auditOSS001PNG(t, 80), "creator_avatar:other:new"), 403)
	data := auditOSS001PNG(t, 80)
	data = data[:len(data)/2]
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", auditOSS001PresignBody(data, "creator_avatar:author:new"), 200))
	req, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range ticket.Headers {
		req.Header.Set(key, value)
	}
	response, err := f.provider.Client().Do(req)
	if err != nil {
		t.Fatal("owned invalid raster PUT failed")
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("owned transport did not store actual invalid input")
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 422)
	var files, creators int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from oss_files where uploader_id=$1 and status='active'),(select count(*) from creators where created_by=$1)`, f.userIDs[f.editor]).Scan(&files, &creators); err != nil || files != 0 || creators != 0 {
		t.Fatal("invalid raster must not become a bindable/public file or fabricated creator", err, files, creators)
	}
}

// These independent purpose/identity assertions share an actor with two real
// uploads. Keep the default5/60 policy, recording and honoring its actual
// Retry-After instead of widening quota or mistaking429 for the target error.
func auditOSS001PreviewWithinRateWindow(t *testing.T, f auditFixGRFixture, actor, path string, body any, want int) []byte {
	t.Helper()
	if path != "/api/v1/users/me/files/presign" {
		t.Fatal("bounded preview-only retry helper")
	}
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+actor)
		response, err := f.origin.Client().Do(req)
		if err != nil {
			t.Fatal("owned preview request failed")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		var problem struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(data, &problem)
		if response.StatusCode == 429 && attempt == 0 {
			seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
			if err != nil || seconds <= 0 || seconds > 60 || problem.Code != "rate_limited" {
				t.Fatal("expected exact default short-window budget and retry contract")
			}
			t.Logf("default upload quota observed; respecting Retry-After=%ds without policy or identity changes", seconds)
			timer := time.NewTimer(time.Duration(seconds) * time.Second)
			select {
			case <-f.ctx.Done():
				timer.Stop()
				t.Fatal("existing fixture deadline exceeded waiting original quota window")
			case <-timer.C:
			}
			continue
		}
		if response.StatusCode != want {
			t.Fatalf("preview status=%d want=%d code=%s error=%s", response.StatusCode, want, problem.Code, problem.Error)
		}
		return data
	}
	t.Fatal("bounded original-rate recovery did not reach target assertion")
	return nil
}
