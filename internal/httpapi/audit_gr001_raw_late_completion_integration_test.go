package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

func TestAuditGR001DeletedOriginalFencesAlreadyCopiedAndNewHTTPCompletionIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("one ordinary owned input; concurrent private copies never rewrite this raw object")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "two-tabs.zip", "application/zip", "test037-retention", raw)
	f.put(t, ticket, raw)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var copies atomic.Int64
	original := f.provider.Config.Handler
	f.provider.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.Header.Get("x-oss-copy-source") != "" {
			switch copies.Add(1) {
			case 1:
				select {
				case <-entered:
				case <-r.Context().Done():
					return
				}
			case 2:
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
		}
		original.ServeHTTP(w, r)
	})
	type result struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan result, 2)
	requestBytes, err := json.Marshal(ticket.completion())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			r, e := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+"/api/v1/users/me/oss/uploads/complete", bytes.NewReader(requestBytes))
			if e != nil {
				done <- result{err: e}
				return
			}
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+f.editor)
			response, e := f.origin.Client().Do(r)
			if e != nil {
				done <- result{err: e}
				return
			}
			body, e := io.ReadAll(io.LimitReader(response.Body, 4<<20))
			_ = response.Body.Close()
			done <- result{response.StatusCode, body, e}
		}()
	}
	close(start)
	var first result
	select {
	case first = <-done:
	case <-f.ctx.Done():
		t.Fatal("two-copy deterministic provider barrier did not complete")
	}
	if first.err != nil || first.status != 201 || copies.Load() != 2 {
		t.Fatalf("first exact copy/completion barrier: status%d copies%d transportError=%v", first.status, copies.Load(), first.err != nil)
	}
	file := decodeTEST022Data[map[string]any](t, first.body)
	if file["id"] == nil {
		t.Fatal("successful first completion lost public file identity")
	}
	auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	before := f.facts(t)
	unblock()
	var late result
	select {
	case late = <-done:
	case <-f.ctx.Done():
		t.Fatal("late copied completion did not return")
	}
	// The already-committed logical upload identity is examined before final
	// adoption. Its now-deleted file reaches this precise existing conflict.
	if late.err != nil || late.status != 409 || !bytes.Contains(late.body, []byte("此上传已使用不同文件信息完成")) {
		t.Fatalf("late actual HTTP completion did not reach exact terminal-identity409: status%d transportError=%v", late.status, late.err != nil)
	}
	auditOSSAssertFailureOnlyCompensatesOwnCandidates(t, f, before)
	var files, active, rawIntents, abandoned int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from oss_files),(select count(*) from oss_files where status<>'deleted'),(select count(*) from oss_object_deletion_outbox where object_key=$1 and raw_upload_binding_id is not null),(select count(*) from oss_upload_byte_bindings where upload_object_key=$1 and status='cleanup_pending' and file_id is null)`, ticket.ObjectKey).Scan(&files, &active, &rawIntents, &abandoned); err != nil || files != 1 || active != 0 || rawIntents != 1 || abandoned != 1 {
		t.Fatalf("late adoption changed file/proof/retirement facts: files%d active%d raw%d unadopted%d err=%v", files, active, rawIntents, abandoned, err)
	}
	// A new request after retirement is rejected before reserving another
	// receipt/file/quota/activity or any extra deletion intent.
	before = f.facts(t)
	response := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 410)
	if !strings.Contains(string(response), "原上传已按保留规则清理") {
		t.Fatal("new completion failed at an unrelated boundary")
	}
	f.unchanged(t, before)
	var receipts int
	if err = f.db.QueryRow(f.ctx, `select count(*) from oss_upload_byte_bindings where upload_object_key=$1`, ticket.ObjectKey).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatal("new retired completion reserved another attempt")
	}
	t.Log("Two real completion requests crossed deterministic private-copy barrier; first201 then ordinaryDELETE; precise late identity409 preserved all business rows and only compensated its own unadopted UUID key; subsequent prepare410 zero new effects. One initial raw PUT, no overwrite/aging.")
}

func TestAuditGR001FinalAdoptChecksRetirementAfterEarlierVerifiedCopyIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("one owned source for explicit final-adoption transaction guard")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "final-guard.zip", "application/zip", "test037-retention", raw)
	f.put(t, ticket, raw)
	client, cfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	head, err := client.HeadObject(f.ctx, &aliyunoss.HeadObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(ticket.ObjectKey)})
	if err != nil {
		t.Fatal(err)
	}
	var request ossCompleteUploadRequest
	encoded, err := json.Marshal(ticket.completion())
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(encoded, &request); err != nil {
		t.Fatal(err)
	}
	candidate, err := f.server.sealOSSUploadedSource(f.ctx, client, cfg, request, f.userIDs[f.editor], head)
	if err != nil {
		t.Fatal(err)
	}
	candidate.stop()
	candidate.ctx = f.ctx
	defer f.server.discardOSSByteBinding(candidate)
	// Component-level final registration with the exact real sealed bytes.
	// Both trials roll back the provisional file/quota changes. This separate
	// oracle does not relabel the HTTP race's earlier identity-conflict branch.
	adopt := func() error {
		tx, e := f.db.Begin(f.ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(f.ctx)
		if e = lockOSSOriginalUploadKeyTx(f.ctx, tx, cfg, ticket.ObjectKey); e != nil {
			return e
		}
		var fileID int64
		e = tx.QueryRow(f.ctx, `insert into oss_files(uploader_id,bucket,endpoint,region,object_key,category,source,original_name,source_original_name,content_type,sha256,source_size_bytes,stored_sha256,size_bytes,source_hash_verified,status) values($1,$2,$3,$4,$5,$6,$7,$8,$8,$9,$10,$11,$10,$11,true,'active') returning id`, f.userIDs[f.editor], cfg.Bucket, cfg.displayEndpoint(), cfg.Region, candidate.StoredKey, request.Category, request.Source, request.OriginalName, request.ContentType, request.SHA256, int64(len(raw))).Scan(&fileID)
		if e != nil {
			return e
		}
		return commitOSSByteBindingTx(f.ctx, tx, candidate, fileID, request.SHA256, int64(len(raw)), request.SHA256, int64(len(raw)))
	}
	before := f.facts(t)
	if err = adopt(); err != nil {
		t.Fatalf("matching unretired private byte proof failed positive final-adoption control: %v", err)
	}
	f.unchanged(t, before)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
	auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	before = f.facts(t)
	if err = adopt(); !errors.Is(err, errOSSRawUploadRetired) {
		t.Fatalf("final-adoption retirement guard reached unrelated error: %v", err)
	}
	f.unchanged(t, before)
	var preparing bool
	if err = f.db.QueryRow(f.ctx, `select status='preparing' and file_id is null from oss_upload_byte_bindings where id=$1::uuid`, candidate.ID).Scan(&preparing); err != nil || !preparing {
		t.Fatal("late rejected final adoption changed its original candidate")
	}
}
