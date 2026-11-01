package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This deliberately waits through the ordinary ten-minute writable capability.
// The explicit gate keeps the repository's ten-minute DB batch timeout honest;
// a skipped run is not physical-cleanup evidence. No TTL or timestamp changes.
func TestAuditGR001DefaultTicketRealExpiryRemovesOriginalPrivateBytesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_RAW_EXPIRY_INTEGRATION") != "1" {
		t.Skip("requires a declared 15-minute real default-signature expiry window")
	}
	unlock := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlock() })
	base := newAuditFixGRFixture(t, 15*time.Minute)
	f := newTEST037FixtureWithBase(t, base.test013Fixture)
	unlock()
	t.Cleanup(func() { unlock = auditGlobalCatalogPGLock(t) })
	var mu sync.Mutex
	deletions := map[string]int{}
	provider := f.provider.Config.Handler
	f.provider.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Query().Get("uploadId") == "" {
			key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), f.cfg.Bucket+"/")
			mu.Lock()
			deletions[key]++
			mu.Unlock()
		}
		provider.ServeHTTP(w, r)
	})
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	entry, err := zw.CreateHeader(&zip.FileHeader{Name: "synthetic-config.json", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte(`{"path":"/home/owned-fixture","private_marker":"SYNTHETIC_OWNED_PRIVATE_PLAYER_AND_MACHINE_PATH"}`)); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := archive.Bytes()
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "private-config.zip", "application/zip", "test037-retention", raw)
	expiry := auditRawRequireCapability(t, f, ticket, 600, 1)
	f.put(t, ticket, raw)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	var binding, sourceKey, storedKey string
	var fileID int64
	if err = f.db.QueryRow(f.ctx, `select binding.id::text,binding.file_id,binding.sealed_source_key,binding.stored_object_key from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id where file.public_id=$1 and binding.status='committed' and binding.upload_object_key=$2`, file["id"], ticket.ObjectKey).Scan(&binding, &fileID, &sourceKey, &storedKey); err != nil {
		t.Fatal(err)
	}
	auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	// Workers share the real database with a maximum pool of two, while all
	// streaming reads release their transaction connection before provider IO.
	poolCfg := f.db.Config().Copy()
	poolCfg.MaxConns, poolCfg.MinConns = 2, 1
	workerPool, err := pgxpool.NewWithConfig(f.ctx, poolCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(workerPool.Close)
	workerCtx, cancel := context.WithCancel(f.ctx)
	worker := NewOSSDeletionWorker(f.server.cfg, workerPool)
	other := NewOSSDeletionWorker(f.server.cfg, workerPool)
	var wg sync.WaitGroup
	for _, w := range []*OSSDeletionWorker{worker, other} {
		wg.Add(1)
		go func(w *OSSDeletionWorker) { defer wg.Done(); w.run(workerCtx) }(w)
	}
	t.Cleanup(func() { cancel(); wg.Wait() })
	client, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	absent := func(key string) bool {
		response, e := client.GetObject(f.ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key)})
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return isOSSRawObjectAbsent(e)
	}
	core := time.NewTimer(30 * time.Second)
	t.Cleanup(func() { core.Stop() })
	poll := time.NewTicker(100 * time.Millisecond)
	t.Cleanup(poll.Stop)
	for !absent(storedKey) {
		select {
		case <-poll.C:
		case <-core.C:
			t.Fatal("normal deletion worker did not finish the sealed object within the declared 30-second core budget")
		case <-f.ctx.Done():
			t.Fatal("owned long fixture context ended")
		}
	}
	var status string
	var attempts int
	var next, dbClock time.Time
	if err = f.db.QueryRow(f.ctx, `select status,attempts,next_attempt_at,clock_timestamp() from oss_object_deletion_outbox where raw_upload_binding_id=$1::uuid and oss_file_id=$2 and object_key=$3`, binding, fileID, ticket.ObjectKey).Scan(&status, &attempts, &next, &dbClock); err != nil || status != "pending" || attempts != 0 || next.Before(expiry.Add(time.Second)) || !dbClock.Before(next) {
		t.Fatal("live capability lost its exact typed deferred intent or consumed failure attempts")
	}
	retained, err := client.GetObject(f.ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)})
	if err != nil {
		t.Fatal("still-live signature was followed by early original deletion")
	}
	actual, err := io.ReadAll(io.LimitReader(retained.Body, int64(len(raw)+1)))
	_ = retained.Body.Close()
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("single original owned input changed before its real expiry")
	}
	t.Logf("early core: poolMax=%d binding=%s file=%d capExpiry=%s notBefore=%s rawSHA256=%x rawStillPresent=true storedAbsent=true attempts=0", workerPool.Config().MaxConns, binding, fileID, expiry.UTC().Format(time.RFC3339Nano), next.UTC().Format(time.RFC3339Nano), sha256.Sum256(actual))
	// Poll the actual durable state and SDK bytes through wall-clock expiry.
	// Normal workers keep their unchanged 15-second poll interval throughout.
	physicalDeadline := time.NewTimer(time.Until(next.Add(45 * time.Second)))
	t.Cleanup(func() { physicalDeadline.Stop() })
	for {
		if err = f.db.QueryRow(f.ctx, `select status,attempts,clock_timestamp() from oss_object_deletion_outbox where raw_upload_binding_id=$1::uuid`, binding).Scan(&status, &attempts, &dbClock); err != nil {
			t.Fatal(err)
		}
		if status == "completed" {
			break
		}
		if status == "dead" {
			t.Fatal("original raw cleanup entered a dead state")
		}
		select {
		case <-poll.C:
		case <-physicalDeadline.C:
			t.Fatalf("default-signature physical cleanup exceeded actual expiry+45s: status=%s attempts=%d", status, attempts)
		case <-f.ctx.Done():
			t.Fatal("declared 15-minute fixture expired")
		}
	}
	if dbClock.Before(next) || attempts != 1 || !absent(ticket.ObjectKey) || !absent(sourceKey) || !absent(storedKey) {
		t.Fatal("completed raw intent lacks actual elapsed expiry and precise provider NoSuchKey for every original/sealed/stored object")
	}
	mu.Lock()
	rawDeletes := deletions[ticket.ObjectKey]
	mu.Unlock()
	if rawDeletes != 1 {
		t.Fatalf("two actual workers performed %d raw DELETEs, expected one", rawDeletes)
	}
	var terminal bool
	if err = f.db.QueryRow(f.ctx, `select exists(select 1 from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id where binding.id=$1::uuid and binding.status='committed' and file.status='deleted')`, binding).Scan(&terminal); err != nil || !terminal {
		t.Fatal("physical cleanup modified or dropped audit/file identity")
	}
	t.Log("real default600s expiry + unchanged15s workers + pool2: original/sealed/stored exact404NoSuchKey, one raw DELETE, terminal audit identity retained; no resume/overwrite/provider metadata mutation/timestamp aging/TTL change")
}
