package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestPublicLogShareResponsesStayBoundedForSingleAndZipScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to run the log share scale integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	started := time.Now()
	if _, err := pool.Exec(ctx, `with owner as (insert into users(username,email,password_hash) values('scale-owner','scale-owner@example.invalid','disabled') returning id)
		insert into log_shares(id,public_code,owner_user_id,source_type,title,original_name,status,redaction_version,redaction_applied_version,expires_at) values
		(1,'single-scale-code',(select id from owner),'file','Single scale','latest.log','ready',$1,$1,now()+interval '1 day'),
		(2,'zip-scale-code',(select id from owner),'file','ZIP scale','logs.zip','ready',$1,$1,now()+interval '1 day')`, logRedactionVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into log_share_entries(log_share_id,entry_index,safe_display_name,content_type,sanitized_text,byte_size,line_count,checksum,status)
		select 1,0,'latest.log','text/plain',repeat('<',$1),$1,500000,'single-checksum','ready'`, 20<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into log_share_entries(log_share_id,entry_index,safe_display_name,content_type,sanitized_text,byte_size,line_count,checksum,status)
		select 2,value-1,'logs/entry-'||lpad(value::text,3,'0')||'.log','text/plain',repeat('<',$1),$1,10000,'zip-checksum-'||value,'ready'
		from generate_series(1,$2) value`, 512<<10, maxLogArchiveFiles); err != nil {
		t.Fatal(err)
	}
	fixtureElapsed := time.Since(started)
	server := &Server{db: pool, logShareBodyReads: make(chan struct{}, 8)}

	singleMetadata, singleElapsed := loadLogShareScaleResponse(t, server.publicLogShare, "/api/v1/log-shares/s/single-scale-code", "single-scale-code", "", "")
	assertLogShareMetadataBudget(t, singleMetadata, 1)
	zipMetadata, zipElapsed := loadLogShareScaleResponse(t, server.publicLogShare, "/api/v1/log-shares/s/zip-scale-code", "zip-scale-code", "", "")
	assertLogShareMetadataBudget(t, zipMetadata, maxLogArchiveFiles)

	firstChunk, firstChunkElapsed := loadLogShareScaleResponse(t, server.publicLogShareEntryContent,
		"/api/v1/log-shares/s/single-scale-code/entries/0/content", "single-scale-code", "0", "")
	assertLogShareChunkBudget(t, firstChunk, true)
	deepCursor := encodeLogShareChunkCursor(logShareChunkCursorScope("single-scale-code", 0), (20<<20)-maxLogShareChunkRunes)
	deepChunk, deepChunkElapsed := loadLogShareScaleResponse(t, server.publicLogShareEntryContent,
		"/api/v1/log-shares/s/single-scale-code/entries/0/content?cursor="+deepCursor, "single-scale-code", "0", deepCursor)
	assertLogShareChunkBudget(t, deepChunk, false)
	zipChunk, zipChunkElapsed := loadLogShareScaleResponse(t, server.publicLogShareEntryContent,
		"/api/v1/log-shares/s/zip-scale-code/entries/199/content", "zip-scale-code", "199", "")
	assertLogShareChunkBudget(t, zipChunk, true)
	for label, elapsed := range map[string]time.Duration{
		"single metadata": singleElapsed, "zip metadata": zipElapsed, "first chunk": firstChunkElapsed,
		"deep chunk": deepChunkElapsed, "zip chunk": zipChunkElapsed,
	} {
		if elapsed > 3*time.Second {
			t.Fatalf("%s exceeded 3 seconds: %s", label, elapsed)
		}
	}
	t.Logf("fixture=%s single-metadata=%s/%dB zip200-metadata=%s/%dB first=%s/%dB deep=%s/%dB zip-entry=%s/%dB",
		fixtureElapsed, singleElapsed, singleMetadata.Body.Len(), zipElapsed, zipMetadata.Body.Len(),
		firstChunkElapsed, firstChunk.Body.Len(), deepChunkElapsed, deepChunk.Body.Len(), zipChunkElapsed, zipChunk.Body.Len())
}

func loadLogShareScaleResponse(
	t *testing.T,
	handler http.HandlerFunc,
	path, code, entryIndex, cursor string,
) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("code", code)
	if entryIndex != "" {
		request.SetPathValue("entryIndex", entryIndex)
	}
	if cursor != "" {
		query := request.URL.Query()
		query.Set("cursor", cursor)
		request.URL.RawQuery = query.Encode()
	}
	response := httptest.NewRecorder()
	started := time.Now()
	handler(response, request)
	return response, time.Since(started)
}

func assertLogShareMetadataBudget(t *testing.T, response *httptest.ResponseRecorder, entries int) {
	t.Helper()
	if response.Code != http.StatusOK || response.Body.Len() > maxLogShareMetadataResponseBytes || bytes.Contains(response.Body.Bytes(), []byte(`"text":`)) {
		t.Fatalf("unbounded metadata response: status=%d bytes=%d body-prefix=%q", response.Code, response.Body.Len(), response.Body.Bytes()[:min(256, response.Body.Len())])
	}
	var envelope struct {
		Data publicLogShareResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || len(envelope.Data.Entries) != entries {
		t.Fatalf("metadata decode/count: entries=%d err=%v", len(envelope.Data.Entries), err)
	}
}

func assertLogShareChunkBudget(t *testing.T, response *httptest.ResponseRecorder, hasMore bool) {
	t.Helper()
	if response.Code != http.StatusOK || response.Body.Len() > maxLogShareChunkResponseBytes {
		t.Fatalf("unbounded chunk response: status=%d bytes=%d", response.Code, response.Body.Len())
	}
	var envelope struct {
		Data logShareChunkPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.HasMore != hasMore || len([]rune(envelope.Data.Text)) > maxLogShareChunkRunes {
		t.Fatalf("chunk decode/bounds: hasMore=%v runes=%d err=%v", envelope.Data.HasMore, len([]rune(envelope.Data.Text)), err)
	}
}
