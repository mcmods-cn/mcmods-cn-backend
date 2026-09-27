package httpapi

import (
	"context"
	"encoding/json"
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

func TestFileLogShareBatchReturnsEveryItemAndReusesCompletedSourcesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute the log-share batch contract against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `
		create temporary table oss_files (
			id bigint primary key,public_id text not null,uploader_id bigint not null,status text not null,
			scan_status text not null,source text not null,object_key text not null,
			source_original_name text not null,original_name text not null,content_type text not null,
			source_size_bytes bigint not null,sha256 text not null
		);
		create temporary table log_shares (
			source_file_id bigint,redaction_version integer,status text,deleted_at timestamptz,
			expires_at timestamptz,public_code text
		);
		insert into oss_files values
			(1,'filegood01',7001,'active','clean','log_share','logs/one.log','one.log','one.log','text/plain',10,''),
			(2,'filegood02',7001,'active','clean','log_share','logs/two.log','two.log','two.log','text/plain',10,'');
		insert into log_shares values
			(1,1,'ready',null,now()+interval '1 day','sharegood01'),
			(2,1,'ready',null,now()+interval '1 day','sharegood02')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}

	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/log-shares/files", strings.NewReader(
			`{"fileIds":["filegood01","missing001","filegood02"],"retentionDays":30}`,
		))
		request.Header.Set("Content-Type", "application/json")
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 7001}))
		response := httptest.NewRecorder()
		server.createFileLogShares(response, request)
		if response.Code != http.StatusMultiStatus {
			t.Fatalf("attempt %d returned %d: %s", attempt, response.Code, response.Body.String())
		}
		var envelope struct {
			Data struct {
				Items []struct {
					FileID     string `json:"fileId"`
					PublicCode string `json:"publicCode"`
					Status     string `json:"status"`
					Error      string `json:"error"`
				} `json:"items"`
			} `json:"data"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		items := envelope.Data.Items
		if len(items) != 3 || items[0].FileID != "filegood01" || items[0].PublicCode != "sharegood01" || items[0].Status != "ready" ||
			items[1].FileID != "missing001" || items[1].Status != "failed" || items[1].Error == "" ||
			items[2].FileID != "filegood02" || items[2].PublicCode != "sharegood02" || items[2].Status != "ready" {
			t.Fatalf("attempt %d items=%+v", attempt, items)
		}
	}
}
