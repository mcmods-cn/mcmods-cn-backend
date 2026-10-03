package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOCT02LegacyLogShareUpgradesAtomicallyBeforePublicReadIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	cfg := base.Config()
	cfg.MaxConns = 1
	cfg.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var owner, fileID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('legacy-log-owner','legacy@example.invalid','test') returning id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,object_key,category,source,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status) values('test','https://storage.invalid','logs/legacy.log','log','log_share','legacy.log','text/plain',100,repeat('a',64),$1,'active','clean') returning id`, owner).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	legacyText := strings.Repeat("a", maxLogShareChunkRunes-12) + `{"password":"synthetic-legacy-secret"}` + "\nnats://synthetic-legacy-token@localhost:4222\nprevious=❄"
	create := func(code string, version int, text string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `insert into log_shares(public_code,owner_user_id,source_type,source_file_id,title,original_name,status,redaction_version,redaction_applied_version,redaction_counts,expires_at) values($1,$2,'file',$3,'Legacy','legacy.log','ready',$4,$4,'{"email":1}',now()+interval '1 day') returning id`, code, owner, fileID, version).Scan(&id); err != nil {
			t.Fatal(err)
		}
		entry := makeLogShareEntry("legacy.log", "text/plain", text)
		if _, err := pool.Exec(ctx, `insert into log_share_entries(log_share_id,entry_index,original_name,safe_display_name,content_type,sanitized_text,byte_size,line_count,checksum,status) values($1,0,'legacy.log','legacy.log','text/plain',$2,$3,$4,$5,'ready')`, id, entry.Text, entry.Size, entry.Lines, entry.Checksum); err != nil {
			t.Fatal(err)
		}
		return id
	}
	legacyID := create("oct02-legacy-safe-code", 1, legacyText)
	nativeID := create("oct02-native-safe-code", 2, "already sanitized")
	server := &Server{db: pool, logShareBodyReads: make(chan struct{}, 8)}
	invoke := func(code string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/log-shares/s/"+code+"/entries/0/content", nil).WithContext(ctx)
		req.SetPathValue("code", code)
		req.SetPathValue("entryIndex", "0")
		response := httptest.NewRecorder()
		server.publicLogShareEntryContent(response, req)
		return response
	}
	response := invoke("oct02-legacy-safe-code")
	if response.Code != http.StatusOK {
		t.Fatalf("safe legacy read status=%d body=%s", response.Code, response.Body.String())
	}
	var creation, applied int
	var persisted string
	var bytes, lines int64
	var checksum string
	if err = pool.QueryRow(ctx, `select redaction_version,redaction_applied_version,sanitized_text,byte_size,line_count,checksum from log_shares join log_share_entries on log_share_id=log_shares.id where log_shares.id=$1`, legacyID).Scan(&creation, &applied, &persisted, &bytes, &lines, &checksum); err != nil {
		t.Fatal(err)
	}
	if creation != 1 || applied != 2 || strings.Contains(persisted, "synthetic-") || !strings.Contains(persisted, "previous=❄") {
		t.Fatal("legacy upgrade did not preserve identity and remove current secret forms")
	}
	expected := makeLogShareEntry("legacy.log", "text/plain", persisted)
	if bytes != expected.Size || lines != expected.Lines || checksum != expected.Checksum {
		t.Fatal("derived metadata not recomputed atomically")
	}
	var existing int
	if err = pool.QueryRow(ctx, `select count(*) from log_shares where source_file_id=$1 and id=any($2::bigint[])`, fileID, []int64{legacyID, nativeID}).Scan(&existing); err != nil || existing != 2 {
		t.Fatal("same-source native v2 or old public code was lost")
	}
	if _, err = pool.Exec(ctx, `update log_shares set status='expired' where id=$1`, legacyID); err != nil {
		t.Fatal(err)
	}
	faultID := create("oct02-legacy-fault-code", 1, `{"password":"synthetic-fault-secret"}`)
	if _, err = pool.Exec(ctx, `alter table log_share_entries add constraint oct02_upgrade_fault check(sanitized_text not like '%❄%') not valid`); err != nil {
		t.Fatal(err)
	}
	response = invoke("oct02-legacy-fault-code")
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "synthetic-") {
		t.Fatal("failed upgrade served old unsafe public content")
	}
	if err = pool.QueryRow(ctx, `select redaction_applied_version from log_shares where id=$1`, faultID).Scan(&applied); err != nil || applied != 1 {
		t.Fatal("failed upgrade partially advanced watermark")
	}
	if _, err = pool.Exec(ctx, `alter table log_share_entries drop constraint oct02_upgrade_fault`); err != nil {
		t.Fatal(err)
	}
	response = invoke("oct02-legacy-fault-code")
	if response.Code != http.StatusOK {
		t.Fatal("legacy upgrade did not recover")
	}
	var page struct{ Data logShareChunkPage }
	if err = json.Unmarshal(response.Body.Bytes(), &page); err != nil || strings.Contains(page.Data.Text, "synthetic-") {
		t.Fatal("recovered chunk contained credential")
	}
}
