package httpapi

import (
	"context"
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
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestAdminDashboardContractIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the administration dashboard against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := config.Load()
	pool, err := pgxpool.New(ctx, cfg.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cache := querycache.New(cfg.Redis)
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil).WithContext(ctx)
	response := httptest.NewRecorder()

	server.loadAdminDashboard(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Overview adminDashboardOverview `json:"overview"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	stats := envelope.Data.Overview.OSS
	if stats.ActiveFiles < 0 || stats.StoredBytes < 0 || stats.SourceBytes < 0 || stats.PendingScans < 0 || stats.QuarantinedFiles < 0 || stats.UploadsToday < 0 {
		t.Fatalf("dashboard returned invalid OSS statistics: %+v", stats)
	}
	if len(envelope.Data.Overview.Trend) != 30 {
		t.Fatalf("dashboard returned %d trend points, want 30", len(envelope.Data.Overview.Trend))
	}
	for _, point := range envelope.Data.Overview.Trend {
		if _, parseErr := time.Parse(time.DateOnly, point.Date); parseErr != nil {
			t.Fatalf("dashboard returned invalid trend date %q: %v", point.Date, parseErr)
		}
	}
}

func TestStickerMarkdownUsageQueryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify sticker references against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	server := &Server{db: pool}
	used, err := server.stickerMarkdownIsUsed(ctx, "[sticker:integration:missing]")
	if err != nil {
		t.Fatalf("query sticker references: %v", err)
	}
	if used {
		t.Fatal("nonexistent integration sticker unexpectedly has references")
	}
}

func TestFavoriteExportExhaustedLeaseRecoveryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify export lease recovery against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := config.Load()
	pool, err := pgxpool.New(ctx, cfg.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var userID, collectionID int64
	if err = pool.QueryRow(ctx, `select id from users where status='active' order by id limit 1`).Scan(&userID); err != nil {
		t.Fatalf("load integration user: %v", err)
	}
	name := fmt.Sprintf("export-lease-integration-%d", time.Now().UnixNano())
	if err = pool.QueryRow(ctx, `insert into favorite_collections(user_id,name) values($1,$2) returning id`, userID, name).Scan(&collectionID); err != nil {
		t.Fatalf("create integration collection: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `delete from favorite_collections where id=$1`, collectionID)
	}()
	var taskID string
	if err = pool.QueryRow(ctx, `insert into favorite_modpack_export_tasks(owner_user_id,collection_id,pack_name,pack_version_id,
		minecraft_version,loader_type,loader_version,status,attempt_count,lease_token,lease_expires_at)
		values($1,$2,$3,'1.0.0','1.21.1','fabric','0.16.14','processing',$4,'expired-test',now()-interval '1 minute')
		returning public_id`, userID, collectionID, name, favoriteExportMaxAttempts(cfg.FavoriteExport.MaxBuildAttempts)).Scan(&taskID); err != nil {
		t.Fatalf("create expired export task: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `delete from notifications where data->>'taskId'=$1`, taskID)
	}()

	worker := NewFavoriteModpackExportWorker(cfg, pool, nil)
	worker.failExhaustedLeases(ctx)
	var status, code, detail string
	if err = pool.QueryRow(ctx, `select status,error_code,error_detail from favorite_modpack_export_tasks where public_id=$1`, taskID).Scan(&status, &code, &detail); err != nil {
		t.Fatalf("read recovered task: %v", err)
	}
	if status != "failed" || code != "WORKER_LEASE_EXHAUSTED" || detail != "export processing failed" {
		t.Fatalf("unexpected recovered task state: status=%q code=%q detail=%q", status, code, detail)
	}
}

func TestSystemNotificationTranslationIsRejectedIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the system-notification translation boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var userID int64
	if err = pool.QueryRow(ctx, `select id from users where status='active' order by id limit 1`).Scan(&userID); err != nil {
		t.Fatalf("load integration user: %v", err)
	}
	var notificationID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale)
		values($1,'system','系统标题','系统正文','zh-CN') returning id,public_id`, userID).Scan(&notificationID, &publicID); err != nil {
		t.Fatalf("create system notification: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `delete from notifications where id=$1`, notificationID)
	}()

	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+publicID+"/translate", strings.NewReader(`{"targetLocale":"en-US"}`)).WithContext(
		context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}),
	)
	request.SetPathValue("id", publicID)
	response := httptest.NewRecorder()
	server.translateNotification(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "SYSTEM_NOTIFICATION_TRANSLATION_DISABLED") {
		t.Fatalf("translate system notification status=%d body=%s", response.Code, response.Body.String())
	}
}
