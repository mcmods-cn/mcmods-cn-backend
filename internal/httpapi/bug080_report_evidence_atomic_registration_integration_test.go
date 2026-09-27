package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestReportEvidenceFileAndMetadataCommitTogetherAndRetryIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify report evidence registration")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	evidenceBody := []byte("BUG-080 transient registration evidence\n")
	digest := sha256.Sum256(evidenceBody)
	sha := hex.EncodeToString(digest[:])
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.Header().Set("Content-Length", fmt.Sprint(len(evidenceBody)))
		response.Header().Set("x-oss-meta-sha256", sha)
		response.Header().Set("ETag", `"bug080"`)
		response.WriteHeader(http.StatusOK)
		if request.Method != http.MethodHead {
			_, _ = response.Write(evidenceBody)
		}
	}))
	defer provider.Close()

	if _, err = pool.Exec(ctx, `
		create temporary table system_settings(key text primary key,value jsonb not null);
		create temporary sequence bug080_file_public_id;
		create temporary sequence bug080_evidence_public_id;
		create temporary sequence bug080_reject_once;
		create temporary table oss_files(
			id bigserial primary key,
			public_id text not null unique default ('f'||lpad(nextval('pg_temp.bug080_file_public_id')::text,8,'0')),
			bucket text not null,endpoint text not null,region text not null,object_key text not null unique,
			category text not null,source text not null,original_name text not null,source_original_name text not null,
			content_type text not null,size_bytes bigint not null,source_size_bytes bigint not null,sha256 text not null,
			uploader_id bigint not null,status text not null,scan_status text not null,
			created_at timestamptz not null default now(),updated_at timestamptz not null default now()
		);
		create temporary table report_evidence(
			id bigserial primary key,
			public_id text not null unique default ('e'||lpad(nextval('pg_temp.bug080_evidence_public_id')::text,8,'0')),
			uploader_id bigint not null,object_key text not null unique,original_name text not null,
			content_type text not null,byte_size bigint not null,sha256 text not null,scan_status text not null,
			status text not null default 'temporary'
		);
		create temporary table oss_upload_logs(
			file_id bigint,uploader_id bigint,object_key text,original_name text,size_bytes bigint,
			ip text,user_agent text,result text,message text
		);
		create temporary table oss_scan_logs(file_id bigint,object_key text,engine text,result text,message text);
		create function pg_temp.reject_first_bug080_evidence() returns trigger language plpgsql as $$
		begin
			if nextval('pg_temp.bug080_reject_once')=1 then
				raise exception 'BUG-080 transient evidence registration failure';
			end if;
			return new;
		end $$;
		create trigger reject_first_bug080_evidence before insert on report_evidence
			for each row execute function pg_temp.reject_first_bug080_evidence();
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Load()}
	ossConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: provider.URL, PublicEndpoint: "https://public.example.test",
		Bucket: "bug080-bucket", AccessKeyID: "bug080-key", AccessKeySecret: "bug080-secret", UseCName: true, Prefix: "mcmods",
	}
	sealed, err := server.sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}

	objectKey := "mcmods/moderation/report-evidence/800/bug080-evidence.txt"
	body := map[string]any{
		"objectKey": objectKey, "originalName": "bug080-evidence.txt", "contentType": "text/plain",
		"sizeBytes": len(evidenceBody), "sha256": sha,
	}
	first := invokeBUG080ReportEvidenceComplete(t, ctx, server, body)
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first completion status = %d body=%s", first.Code, first.Body.String())
	}
	var filesAfterFailure, evidenceAfterFailure int
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_files),(select count(*) from report_evidence)`).
		Scan(&filesAfterFailure, &evidenceAfterFailure); err != nil {
		t.Fatal(err)
	}
	if filesAfterFailure != 0 || evidenceAfterFailure != 0 {
		t.Fatalf("failed atomic registration left files/evidence = %d/%d", filesAfterFailure, evidenceAfterFailure)
	}

	second := invokeBUG080ReportEvidenceComplete(t, ctx, server, body)
	if second.Code != http.StatusCreated {
		t.Fatalf("retry completion status = %d body=%s", second.Code, second.Body.String())
	}
	var response struct {
		Data struct {
			ID         string `json:"id"`
			EvidenceID string `json:"evidenceId"`
		} `json:"data"`
	}
	if err = json.Unmarshal(second.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var filesAfterRetry, evidenceAfterRetry int
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_files),(select count(*) from report_evidence)`).
		Scan(&filesAfterRetry, &evidenceAfterRetry); err != nil {
		t.Fatal(err)
	}
	if filesAfterRetry != 1 || evidenceAfterRetry != 1 || response.Data.EvidenceID == "" || response.Data.ID != response.Data.EvidenceID {
		t.Fatalf("retry registration = files/evidence %d/%d response=%+v body=%s", filesAfterRetry, evidenceAfterRetry, response, second.Body.String())
	}

	third := invokeBUG080ReportEvidenceComplete(t, ctx, server, body)
	if third.Code != http.StatusOK || !strings.Contains(third.Body.String(), `"idempotent":true`) || !strings.Contains(third.Body.String(), response.Data.EvidenceID) {
		t.Fatalf("idempotent completion status = %d body=%s", third.Code, third.Body.String())
	}
	mismatchedRetry := make(map[string]any, len(body))
	for key, value := range body {
		mismatchedRetry[key] = value
	}
	mismatchedRetry["sha256"] = strings.Repeat("f", 64)
	mismatchResponse := invokeBUG080ReportEvidenceComplete(t, ctx, server, mismatchedRetry)
	if mismatchResponse.Code != http.StatusBadRequest || strings.Contains(mismatchResponse.Body.String(), `"idempotent":true`) {
		t.Fatalf("mismatched idempotency status = %d body=%s", mismatchResponse.Code, mismatchResponse.Body.String())
	}

	legacyObjectKey := "mcmods/moderation/report-evidence/800/bug080-legacy-orphan.txt"
	if _, err = pool.Exec(ctx, `insert into oss_files(
		bucket,endpoint,region,object_key,category,source,original_name,source_original_name,content_type,
		size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values($1,$2,$3,$4,$5,'report_evidence',$6,$6,$7,$8,$8,$9,800,'active','pending')`,
		ossConfig.Bucket, ossConfig.Endpoint, ossConfig.Region, legacyObjectKey, "moderation/report-evidence/800",
		"bug080-legacy-orphan.txt", "text/plain; charset=utf-8", len(evidenceBody), sha); err != nil {
		t.Fatal(err)
	}
	legacyBody := map[string]any{
		"objectKey": legacyObjectKey, "originalName": "bug080-legacy-orphan.txt", "contentType": "text/plain",
		"sizeBytes": len(evidenceBody), "sha256": sha,
	}
	legacyResponse := invokeBUG080ReportEvidenceComplete(t, ctx, server, legacyBody)
	if legacyResponse.Code != http.StatusOK || !strings.Contains(legacyResponse.Body.String(), `"idempotent":true`) {
		t.Fatalf("legacy orphan recovery status = %d body=%s", legacyResponse.Code, legacyResponse.Body.String())
	}
	var legacyEvidence int
	if err = pool.QueryRow(ctx, `select count(*) from report_evidence where object_key=$1 and uploader_id=800
		and sha256=$2 and byte_size=$3 and status='temporary'`, legacyObjectKey, sha, len(evidenceBody)).Scan(&legacyEvidence); err != nil {
		t.Fatal(err)
	}
	if legacyEvidence != 1 {
		t.Fatalf("legacy orphan evidence rows = %d", legacyEvidence)
	}
}

func invokeBUG080ReportEvidenceComplete(t *testing.T, ctx context.Context, server *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/reports/evidence/uploads/complete", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 800}))
	response := httptest.NewRecorder()
	server.completeOSSDirectUploadWithScope(response, request, ossReportEvidenceScope)
	return response
}
