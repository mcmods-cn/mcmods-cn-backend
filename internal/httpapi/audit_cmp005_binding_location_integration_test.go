package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestAuditCMP005ReceiptSeparatesProviderAndDisplayEndpointAndRejectsLocationDriftIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	cfg := f.cfg
	cfg.PublicEndpoint = "https://owned-display.example.invalid"
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealed); err != nil {
		t.Fatal(err)
	}
	raw := []byte("ordinary distinct provider/display endpoint source")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "location.zip", "application/zip", "test037-location", raw)
	f.put(t, ticket, raw)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	var providerEndpoint, displayEndpoint, fileEndpoint string
	if err = f.db.QueryRow(f.ctx, `select binding.endpoint,binding.file_endpoint,file.endpoint from oss_upload_byte_bindings binding join oss_files file on file.id=binding.file_id where binding.upload_object_key=$1 and binding.status='committed'`, ticket.ObjectKey).Scan(&providerEndpoint, &displayEndpoint, &fileEndpoint); err != nil || providerEndpoint != cfg.Endpoint || displayEndpoint != cfg.PublicEndpoint || fileEndpoint != cfg.PublicEndpoint {
		t.Fatalf("provider/display endpoint contracts conflated: %v", err)
	}
	if got := f.get(t, file["accessUrl"].(string), 200); string(got) != string(raw) {
		t.Fatal("distinct display endpoint broke the normal private provider read")
	}
	reused := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "same-location.zip", "application/zip", "test037-location", raw)
	if reused.UploadRequired {
		t.Fatal("distinct display endpoint lost normal verified hash reuse")
	}
	var req ossCompleteUploadRequest
	encoded, _ := json.Marshal(ticket.completion())
	if err = json.Unmarshal(encoded, &req); err != nil {
		t.Fatal(err)
	}
	client, actualCfg, err := f.server.ossClient(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"bucket", "endpoint"} {
		t.Run(column, func(t *testing.T) {
			// This explicitly models old/admin metadata relocation on an owned
			// copy. It is not a claim of reachable client mutation or exposure.
			if _, err := f.db.Exec(f.ctx, `update oss_files set `+column+`='owned-unmapped-location' where public_id=$1`, file["id"]); err != nil {
				t.Fatal(err)
			}
			before := f.facts(t)
			if key, err := f.server.resolvedOSSUploadObjectKey(f.ctx, actualCfg, ticket.ObjectKey, f.userIDs[f.editor]); err != nil || key != ticket.ObjectKey {
				t.Fatalf("location drift inherited old alias: %v", err)
			}
			resultReq := req
			resultReq.ObjectKey = file["objectKey"].(string)
			if err := f.server.canonicalOSSCompletedUploadAlias(f.ctx, actualCfg, f.userIDs[f.editor], &resultReq); !errors.Is(err, errOSSSealedReplayIdentity) {
				t.Fatalf("location drift inherited old sealed replay identity: %v", err)
			}
			if _, _, found, err := f.server.findCompletedOSSUpload(f.ctx, f.userIDs[f.editor], req); err != nil || found {
				t.Fatalf("location drift reused old completion proof: %v", err)
			}
			lookup := ossFileHashLookup{UploaderID: int64Pointer(f.userIDs[f.editor]), Category: ticket.Category, Source: ticket.Source, ScanStatuses: []string{"pending", "clean", "trusted_generated"}}
			if _, found, err := f.server.findExistingOSSFileByHashExact(f.ctx, ticket.SHA256, ticket.SizeBytes, lookup); err != nil || found {
				t.Fatalf("location drift reused old source hash proof: %v", err)
			}
			tx, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = completedOSSByteBindingForUploadTx(f.ctx, tx, actualCfg, ticket.ObjectKey, f.userIDs[f.editor], req, ticket.SizeBytes)
			_ = tx.Rollback(f.ctx)
			if !errors.Is(err, errOSSCompletedIdentityConflict) {
				t.Fatalf("location drift was accepted by final registration replay: %v", err)
			}
			f.unchanged(t, before)
			if _, err = f.db.Exec(f.ctx, `update oss_files set source_hash_verified=false where public_id=$1`, file["id"]); err != nil {
				t.Fatal(err)
			}
			if reverified, err := f.server.reverifyOSSCompletedByteBinding(f.ctx, client, actualCfg, f.userIDs[f.editor], req); err != nil || reverified {
				t.Fatalf("location drift was silently re-established: %v", err)
			}
			if _, err = f.db.Exec(f.ctx, `update oss_files set bucket=$2,endpoint=$3,source_hash_verified=true where public_id=$1`, file["id"], cfg.Bucket, cfg.PublicEndpoint); err != nil {
				t.Fatal(err)
			}
		})
	}
}
