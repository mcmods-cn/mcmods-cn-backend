package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCatalogImportInfrastructureDiagnosticsStayOutOfUserRecords(t *testing.T) {

	const marker = "internal-diagnostic-marker"
	cases := []struct {
		name  string
		cause error
	}{
		{"object-service", &aliyunoss.ServiceError{StatusCode: http.StatusServiceUnavailable, Message: marker, RequestTarget: "https://storage.example.invalid/" + marker}},
		{"object-client", &aliyunoss.ClientError{Message: marker}},
		{"transport", &url.Error{Op: "Get", URL: "https://storage.example.invalid/" + marker, Err: errors.New("connection unavailable")}},
		{"local-io", &os.PathError{Op: "open", Path: "/test-data/" + marker, Err: os.ErrPermission}},
		{"database", &pgconn.PgError{Code: "23514", Message: marker}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			job := f.job(t, f.upload(t, auditFixGRZip(t, nil)))
			token, err := f.server.claimModExportJob(f.ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			cause := fmt.Errorf("import step: %w", tc.cause)
			if err = f.server.failModExportJob(job.ID, token, "import_failed", cause); err != nil {
				t.Fatal(err)
			}
			raw := f.require(t, f.editor, "GET", "/api/v1/mods/audit-gr-"+f.modCode+"/export-imports/"+job.ID, nil, http.StatusOK)
			job = decodeTEST022Data[modExportJobResponse](t, raw)
			var logMessage, eventMessage string
			if err = f.db.QueryRow(f.ctx, `select message from catalog_import_job_logs where job_id=$1 and stage='failed'`, job.ID).Scan(&logMessage); err != nil {
				t.Fatal(err)
			}
			if err = f.db.QueryRow(f.ctx, `select payload->'templateValues'->>'error' from nats_outbox where payload->'data'->>'jobId'=$1 and payload->>'templateKey'='mod_import_failure'`, job.ID).Scan(&eventMessage); err != nil {
				t.Fatal(err)
			}
			message, _ := job.ErrorDetail["message"].(string)
			if job.Status != "failed" || message == "" || logMessage != message || eventMessage != message || strings.Contains(message, marker) {
				t.Errorf("%s detail, job log and outgoing notification must agree on a usable, safe failure message", tc.name)
			}
		})
	}
}
