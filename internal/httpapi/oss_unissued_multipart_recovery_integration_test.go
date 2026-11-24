package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"mcmods-cn-backend/internal/security"
)

func TestUnissuedMultipartSigningFailureRetainsCleanupIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	actor := f.userIDs[f.editor]
	provider := credentials.CredentialsProviderFunc(func(context.Context) (credentials.Credentials, error) {
		f.store.mu.Lock()
		started := len(f.store.uploads) > 0
		f.store.mu.Unlock()
		if started {
			return credentials.Credentials{}, errors.New("synthetic signing credentials unavailable")
		}
		return credentials.Credentials{AccessKeyID: f.cfg.AccessKeyID, AccessKeySecret: f.cfg.AccessKeySecret}, nil
	})
	cfg := aliyunoss.LoadDefaultConfig().WithEndpoint(f.cfg.Endpoint).WithRegion(f.cfg.Region).
		WithUseCName(true).WithCredentialsProvider(provider).WithRetryMaxAttempts(1)
	req := ossDirectUploadRequest{OriginalName: "receipt.zip", SizeBytes: ossMultipartThreshold, SHA256: strings.Repeat("a", 64)}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/uploads/presign", nil)
	r = r.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: actor}))
	w := httptest.NewRecorder()
	f.server.issueOSSUploadTicket(w, r, aliyunoss.NewClient(cfg), f.cfg, req, "user", "imports", "test037", f.cfg.Prefix, "imports", "application/zip", time.Now().Add(time.Hour), time.Hour, false, ossUserQuotaLimits{}, false)
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "uploadId") {
		t.Fatalf("expected signing failure before ticket publication: %d %s", w.Code, w.Body.String())
	}
	var sessionID int64
	var uploadID string
	var reservations, capabilities int
	if err := f.db.QueryRow(f.ctx, `select id,upload_id,
		(select count(*) from oss_user_upload_quota_reservations where object_key=s.object_key),
		(select count(*) from oss_upload_capabilities where object_key=s.object_key)
		from oss_multipart_sessions s where status='cleanup_pending' and owner_id=$1`, actor).
		Scan(&sessionID, &uploadID, &reservations, &capabilities); err != nil || reservations != 0 || capabilities != 0 {
		t.Fatalf("cleanup intent missing or unissued upload counted: %d/%d err=%v", reservations, capabilities, err)
	}
	NewOSSMultipartCleanupWorker(f.server.cfg, f.db).drain(f.ctx)
	var status string
	if err := f.db.QueryRow(f.ctx, `select status from oss_multipart_sessions where id=$1`, sessionID).Scan(&status); err != nil || status != "aborted" {
		t.Fatalf("cleanup did not finish: %q %v", status, err)
	}
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	if len(f.store.uploads) != 0 || f.store.aborts[uploadID] != 1 {
		t.Fatal("new credentials did not settle the original provider session")
	}
}
