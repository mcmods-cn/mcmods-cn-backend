package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"sync"
	"time"
)

type auditTP008ManualScan struct {
	mu       sync.Mutex
	events   []map[string]any
	attempts map[string]bool
}

// A declared, minimally privileged operator inspects the owned PNG bytes and
// uses the existing authorized scan-status HTTP API. This is not a virus engine
// or a product scan Worker; no SQL writes mark a file clean.
func (scan *auditTP008ManualScan) run(ctx context.Context, s *Server, apiOrigin, token string, owners []int64, allowed map[string]string) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rows, err := s.db.Query(ctx, `select file.public_id,file.object_key,file.size_bytes,file.stored_sha256,file.source_hash_verified and exists(select 1 from oss_upload_byte_bindings binding where binding.file_id=file.id and binding.status='committed' and `+ossByteBindingCurrentFileProofSQL+`) from oss_files file where file.uploader_id=any($1::bigint[]) and file.source like 'simple_project_gallery:%' and file.status='active' and file.scan_status='pending'`, owners)
		if err != nil {
			scan.record(map[string]any{"state": "DATABASE_QUERY_ERROR"})
			continue
		}
		type file struct {
			id, key, sha string
			size         int64
			proof        bool
		}
		var files []file
		for rows.Next() {
			var f file
			if err = rows.Scan(&f.id, &f.key, &f.size, &f.sha, &f.proof); err != nil {
				break
			}
			files = append(files, f)
		}
		if rows.Err() != nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			scan.record(map[string]any{"state": "DATABASE_SCAN_ERROR"})
			continue
		}
		for _, f := range files {
			scan.mu.Lock()
			attempted := scan.attempts[f.id]
			scan.attempts[f.id] = true
			scan.mu.Unlock()
			if attempted {
				continue
			}
			event := map[string]any{"fileId": f.id, "startedAt": time.Now().UTC().Format(time.RFC3339Nano), "state": "IN_PROGRESS", "authority": "oss.write only", "layer": "scripted manual-scan precondition via existing HTTP API, not antivirus engine"}
			client, cfg, err := s.ossClient(ctx)
			if err != nil || !f.proof {
				event["state"] = "NO_CURRENT_BYTE_PROOF"
				scan.record(event)
				continue
			}
			raw, err := readOSSUploadedRaster(ctx, client, cfg, f.key, f.size, f.sha)
			sample := allowed[sha256Hex(raw)]
			picture, format, decodeErr := image.Decode(bytes.NewReader(raw))
			if err != nil || sample == "" || decodeErr != nil || format != "png" || picture.Bounds().Dx() != 8 || picture.Bounds().Dy() != 8 {
				event["state"] = "INSPECTION_REJECTED"
				scan.record(event)
				continue
			}
			event["sampleId"], event["sha256"], event["bytes"], event["decodedFormat"], event["dimensions"] = sample, sha256Hex(raw), len(raw), format, []int{8, 8}
			payload, _ := json.Marshal(map[string]string{"status": "clean", "note": "TP008 owned declared PNG bytes/hash inspected; isolated manual-scan precondition, not antivirus engine"})
			request, err := http.NewRequestWithContext(ctx, http.MethodPatch, apiOrigin+"/api/v1/admin/oss/files/"+f.id+"/scan", bytes.NewReader(payload))
			if err != nil {
				event["state"] = "REQUEST_ERROR"
				scan.record(event)
				continue
			}
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "http://127.0.0.1:3551")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				event["state"] = "HTTP_ERROR"
				scan.record(event)
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			event["httpStatus"] = response.StatusCode
			event["responseSha256"] = sha256Hex(body)
			if response.StatusCode != 200 {
				event["state"] = "HTTP_REJECTED"
				scan.record(event)
				continue
			}
			var state string
			var audits int
			err = s.db.QueryRow(ctx, `select file.scan_status,(select count(*) from audit_events where aggregate_type='oss_file' and aggregate_key=file.public_id and action='scan_status_update') from oss_files file where file.public_id=$1`, f.id).Scan(&state, &audits)
			if err != nil || state != "clean" || audits != 1 {
				event["state"] = "POSTCONDITION_FAILED"
				event["scanAuditCount"] = audits
				scan.record(event)
				continue
			}
			event["state"], event["scanAuditCount"], event["finishedAt"] = "CLEAN_VIA_AUTHORIZED_API", audits, time.Now().UTC().Format(time.RFC3339Nano)
			scan.record(event)
		}
	}
}

func (scan *auditTP008ManualScan) record(event map[string]any) {
	scan.mu.Lock()
	scan.events = append(scan.events, event)
	scan.mu.Unlock()
}
func (scan *auditTP008ManualScan) snapshot() []map[string]any {
	scan.mu.Lock()
	defer scan.mu.Unlock()
	return append([]map[string]any{}, scan.events...)
}

func auditTP008HTTP(ctx context.Context, origin, token, method, path string, payload any) (int, http.Header, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:3551")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, nil, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return response.StatusCode, response.Header, nil, fmt.Errorf("owned HTTP body unavailable")
	}
	return response.StatusCode, response.Header.Clone(), body, nil
}
