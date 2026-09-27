package httpapi

import (
	"context"
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

func TestTemporaryStickerUploadsAreExplicitlyDiscardedOrExpiredIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run sticker upload lifecycle tests")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`create temporary table oss_files(
			id bigint primary key,public_id text not null unique,uploader_id bigint not null,source text not null,
			bucket text not null,endpoint text not null,region text not null,object_key text not null,
			status text not null,created_at timestamptz not null,updated_at timestamptz not null default now()
		)`,
		`create temporary table stickers(id bigint primary key,image_file_id bigint not null)`,
		`create temporary table log_shares(source_file_id bigint,status text not null,deleted_at timestamptz)`,
		`create temporary table oss_object_deletion_outbox(
			oss_file_id bigint,bucket text not null,endpoint text not null,region text not null,use_cname boolean not null,
			object_key text not null,reason text not null,status text not null default 'pending',attempts integer not null default 0,max_attempts integer not null default 12,
			next_attempt_at timestamptz not null default now(),locked_at timestamptz,locked_by text not null default '',last_error text not null default '',failure_class text not null default '',
			updated_at timestamptz not null default now(),deleted_at timestamptz,dead_at timestamptz,replay_count integer not null default 0,
			last_replayed_at timestamptz,last_replayed_by bigint,unique(bucket,endpoint,object_key)
		)`,
		`insert into oss_files(id,public_id,uploader_id,source,bucket,endpoint,region,object_key,status,created_at) values
			(10,'f00000010',7001,'sticker-upload','bucket','https://oss-cn-test.aliyuncs.com','cn-test','temporary/expired-legacy.png','active',now()-interval '2 hours'),
			(11,'f00000011',7001,'sticker-upload:new','bucket','https://oss-cn-test.aliyuncs.com','cn-test','temporary/new.png','active',now()),
			(12,'f00000012',7001,'other','bucket','https://oss-cn-test.aliyuncs.com','cn-test','temporary/other.png','active',now()-interval '2 hours'),
			(13,'f00000013',7001,'sticker-upload:referenced','bucket','https://oss-cn-test.aliyuncs.com','cn-test','temporary/referenced.png','active',now()-interval '2 hours'),
			(14,'f00000014',7001,'sticker-upload:already','bucket','https://oss-cn-test.aliyuncs.com','cn-test','temporary/already.png','deleted',now()-interval '2 hours'),
			(15,'f00000015',7001,'sticker-upload:discard','bucket','https://oss-cn-test.aliyuncs.com','cn-test','temporary/discard.png','active',now()),
			(16,'f00000016',7002,'sticker-upload:foreign','bucket','https://oss-cn-test.aliyuncs.com','cn-test','temporary/foreign.png','active',now())`,
		`insert into stickers values(20,13)`,
	} {
		if _, err = connection.Exec(ctx, statement); err != nil {
			connection.Release()
			t.Fatal(err)
		}
	}
	connection.Release()

	worker := NewMaintenanceWorker(db, nil)
	worker.pruneStickerUploads(ctx)
	assertTemporaryStickerFileStatus(t, ctx, db, 10, "deleted")
	for _, fileID := range []int64{11, 12, 13, 16} {
		assertTemporaryStickerFileStatus(t, ctx, db, fileID, "active")
	}
	assertTemporaryStickerFileStatus(t, ctx, db, 14, "deleted")
	var expiredJobs int
	if err = db.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where oss_file_id=10 and reason='sticker_upload_expired'`).Scan(&expiredJobs); err != nil || expiredJobs != 1 {
		t.Fatalf("expired cleanup jobs=%d err=%v", expiredJobs, err)
	}

	server := &Server{db: db}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/sticker-upload-files/f00000015", nil)
	request.SetPathValue("fileId", "f00000015")
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 7001}))
	response := httptest.NewRecorder()
	server.discardStickerUpload(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("discard status=%d body=%s", response.Code, response.Body.String())
	}
	assertTemporaryStickerFileStatus(t, ctx, db, 15, "deleted")
	var discardedJobs int
	if err = db.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where oss_file_id=15 and reason='sticker_upload_discarded'`).Scan(&discardedJobs); err != nil || discardedJobs != 1 {
		t.Fatalf("discard cleanup jobs=%d err=%v", discardedJobs, err)
	}

	foreignRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/sticker-upload-files/f00000016", nil)
	foreignRequest.SetPathValue("fileId", "f00000016")
	foreignRequest = foreignRequest.WithContext(context.WithValue(foreignRequest.Context(), claimsContextKey, security.Claims{Subject: 7001}))
	foreignResponse := httptest.NewRecorder()
	server.discardStickerUpload(foreignResponse, foreignRequest)
	if foreignResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-owner discard status=%d body=%s", foreignResponse.Code, foreignResponse.Body.String())
	}
	assertTemporaryStickerFileStatus(t, ctx, db, 16, "active")
}

func assertTemporaryStickerFileStatus(t *testing.T, ctx context.Context, db *pgxpool.Pool, fileID int64, want string) {
	t.Helper()
	var status string
	if err := db.QueryRow(ctx, `select status from oss_files where id=$1`, fileID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("file %d status=%s want=%s", fileID, status, want)
	}
}
