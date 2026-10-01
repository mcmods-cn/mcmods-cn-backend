package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestBlueprintUploadOutboxAndDeletedDownloadIntegration(t *testing.T) {
	url := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires dedicated migrated PostgreSQL via MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// Clone the project's actual table definitions into an allocated schema.
	// This isolates queued jobs from the real test-service dispatcher; it does
	// not substitute for complete-schema FK/migration validation.
	schema := pgx.Identifier{"apia_blueprint_" + randomHex(8)}.Sanitize()
	if _, err = admin.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := admin.Exec(cleanup, "drop schema "+schema+" cascade"); err != nil {
			t.Errorf("allocated blueprint fixture cleanup: %v", err)
		}
	}()
	for _, table := range []string{"blueprints", "blueprint_variants", "blueprint_jobs", "nats_outbox"} {
		id := pgx.Identifier{table}.Sanitize()
		if _, err = admin.Exec(ctx, "create table "+schema+"."+id+" (like public."+id+" including all)"); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	server := &Server{db: pool, cfg: config.Config{NATS: config.NATSConfig{OutboxEnabled: true}}}
	makeBlueprint := func() (int64, string, string) {
		t.Helper()
		var id int64
		var publicID string
		objectKey := "fixture/" + randomHex(8) + ".nbt"
		if err = pool.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format,original_object_key,status) values(1,'Fixture','nbt',$1,'uploading') returning id,public_id`, objectKey).Scan(&id, &publicID); err != nil {
			t.Fatal(err)
		}
		return id, publicID, objectKey
	}
	id, publicID, objectKey := makeBlueprint()
	reply, err := server.completeBlueprintUpload(ctx, 1, 1, objectKey, "fixture.nbt", "application/octet-stream", 1, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	var events int
	if err = pool.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_id=$1 and event_type='blueprint.conversion.requested' and subject='blueprint_convert' and status='pending'`, reply["jobId"]).Scan(&events); err != nil || events != 1 {
		t.Fatalf("committed upload lost its durable normalize event: events=%d err=%v", events, err)
	}
	// A completed upload is idempotent and creates neither a second job nor an event.
	if _, err = server.completeBlueprintUpload(ctx, 1, 1, objectKey, "fixture.nbt", "application/octet-stream", 1, "fixture"); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err = pool.QueryRow(ctx, `select count(*) from blueprint_jobs where blueprint_id=$1`, id).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("duplicate upload jobs=%d err=%v", jobs, err)
	}
	var variantID string
	if err = pool.QueryRow(ctx, `select public_id from blueprint_variants where blueprint_id=$1`, id).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	var storedObject string
	var owner int64
	if err = pool.QueryRow(ctx, blueprintVariantDownloadSQL, publicID, variantID, int64(1), false).Scan(&storedObject, &owner); err != nil || storedObject != objectKey {
		t.Fatalf("active owner's original variant is not available: key=%q err=%v", storedObject, err)
	}
	if _, err = pool.Exec(ctx, `update blueprints set status='deleted' where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/blueprints/variant", nil)
	req.SetPathValue("publicId", publicID)
	req.SetPathValue("variantId", variantID)
	res := httptest.NewRecorder()
	server.downloadBlueprintVariant(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("deleted variant reached URL signing: %d %s", res.Code, res.Body.String())
	}
	// A fault confined to the allocated outbox must roll back the upload,
	// original variant and job, rather than leaving a committed orphan job.
	if _, err = pool.Exec(ctx, `alter table nats_outbox add constraint fixture_reject_event check(event_type<>'blueprint.conversion.requested') not valid`); err != nil {
		t.Fatal(err)
	}
	failedID, _, failedKey := makeBlueprint()
	if _, err = server.completeBlueprintUpload(ctx, 2, 1, failedKey, "fixture.nbt", "application/octet-stream", 1, "fixture"); err == nil {
		t.Fatal("outbox insert failure was ignored")
	}
	var status string
	if err = pool.QueryRow(ctx, `select status from blueprints where id=$1`, failedID).Scan(&status); err != nil || status != "uploading" {
		t.Fatalf("failed outbox changed upload status=%q err=%v", status, err)
	}
	if err = pool.QueryRow(ctx, `select (select count(*) from blueprint_variants where blueprint_id=$1)+(select count(*) from blueprint_jobs where blueprint_id=$1)`, failedID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("outbox failure left committed jobs/variants=%d err=%v", jobs, err)
	}
}
