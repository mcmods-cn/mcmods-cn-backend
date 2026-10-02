package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestMODIDConfirmationProtocolIsBoundSingleUseAndTransactionalIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the MODID confirmation protocol")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
	var actorID, otherActorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test-only',true) returning id`, "modid_actor_"+nonce, nonce+"@modid-confirm.invalid").Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test-only',true) returning id`, "modid_other_"+nonce, nonce+"-other@modid-confirm.invalid").Scan(&otherActorID); err != nil {
		t.Fatal(err)
	}

	var modID, otherModID, versionID int64
	modSiteID := "modid-confirm-" + nonce
	otherModSiteID := "modid-other-" + nonce
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,'MODID confirmation','approved',$3) returning id`,
		randomCatalogPublicID(), modSiteID, actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,'Other MODID confirmation','approved',$3) returning id`,
		randomCatalogPublicID(), otherModSiteID, actorID).Scan(&otherModID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_identifiers(mod_id,identifier,is_primary)
		values($1,'configuredmod',true),($2,'othermod',true)`, modID, otherModID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by)
		values($1,'MODID protocol','active',$2,$2) returning id`, modID, actorID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}

	jobSequence := 0
	newJob := func(status, runToken string, createdBy int64) string {
		t.Helper()
		jobSequence++
		packageID, jobID := newExportID(), newExportID()
		archiveHash := fmt.Sprintf("%064x", jobSequence)
		var archiveFileID int64
		if insertErr := pool.QueryRow(ctx, `insert into oss_files(object_key,original_name,sha256,uploader_id)
			values($1,'modid.zip',$2,$3) returning id`,
			"tests/modid-confirmation/"+packageID, archiveHash, createdBy).Scan(&archiveFileID); insertErr != nil {
			t.Fatal(insertErr)
		}
		if _, insertErr := pool.Exec(ctx, `insert into catalog_import_packages(
			id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,uploaded_by)
			values($1,$2,$3,'modid.zip','mcmods-export/v1','test','1.21.1','neoforge','{}',$4)`,
			packageID, archiveHash, archiveFileID, createdBy); insertErr != nil {
			t.Fatal(insertErr)
		}
		if _, insertErr := pool.Exec(ctx, `insert into catalog_import_jobs(
			id,mod_id,package_id,target_version_id,importer_version,status,progress,current_stage,created_by,run_token)
			values($1,$2,$3,$4,$5,$6,10,'analysis',$7,$8)`,
			jobID, modID, packageID, versionID, modExportImporterVersion, status, createdBy, runToken); insertErr != nil {
			t.Fatal(insertErr)
		}
		return jobID
	}

	server := &Server{db: pool}
	counts := map[string]int{"detectedmod": 10}
	sourceHash := strings.Repeat("a", 64)

	wrongModJob := newJob("importing", "wrong-mod-token", actorID)
	if paused, pauseErr := server.pauseCatalogImportForMODIDConfirmation(
		ctx, wrongModJob, "wrong-mod-token", otherModID, sourceHash, counts,
	); paused || !errors.Is(pauseErr, pgx.ErrNoRows) {
		t.Fatalf("worker analysis accepted a different Mod: paused=%t err=%v", paused, pauseErr)
	}
	assertMODIDJobState(t, ctx, pool, wrongModJob, "importing", "wrong-mod-token", "", false)
	if paused, pauseErr := server.pauseCatalogImportForMODIDConfirmation(
		ctx, wrongModJob, "different-run-token", modID, sourceHash, counts,
	); paused || !errors.Is(pauseErr, pgx.ErrNoRows) {
		t.Fatalf("worker analysis accepted a different run token: paused=%t err=%v", paused, pauseErr)
	}

	happyJob := newJob("importing", "happy-token", actorID)
	paused, err := server.pauseCatalogImportForMODIDConfirmation(ctx, happyJob, "happy-token", modID, sourceHash, counts)
	if err != nil || !paused {
		t.Fatalf("mismatched MODID did not pause: paused=%t err=%v", paused, err)
	}
	var analysisHash string
	if err = pool.QueryRow(ctx, `select modid_analysis_hash from catalog_import_jobs where id=$1`, happyJob).Scan(&analysisHash); err != nil {
		t.Fatal(err)
	}
	if len(analysisHash) != 64 {
		t.Fatalf("analysis hash length=%d", len(analysisHash))
	}
	assertMODIDJobState(t, ctx, pool, happyJob, "confirmation_required", "", analysisHash, true)

	claims := security.Claims{Subject: actorID, PermissionRules: []security.PermissionRule{{Code: "project.edit", Allow: true, Priority: 100}}}
	otherClaims := claims
	otherClaims.Subject = otherActorID
	if response := invokeMODIDConfirmation(t, ctx, server, modSiteID, happyJob, analysisHash, otherClaims); response.Code != http.StatusConflict {
		t.Fatalf("different creator confirmation status=%d body=%s", response.Code, response.Body.String())
	}
	if response := invokeMODIDConfirmation(t, ctx, server, otherModSiteID, happyJob, analysisHash, claims); response.Code != http.StatusConflict {
		t.Fatalf("different Mod confirmation status=%d body=%s", response.Code, response.Body.String())
	}
	if response := invokeMODIDConfirmation(t, ctx, server, modSiteID, happyJob, strings.Repeat("b", 64), claims); response.Code != http.StatusConflict {
		t.Fatalf("different analysis confirmation status=%d body=%s", response.Code, response.Body.String())
	}
	assertMODIDJobState(t, ctx, pool, happyJob, "confirmation_required", "", analysisHash, true)

	expiredJob := newJob("importing", "expired-token", actorID)
	if paused, err = server.pauseCatalogImportForMODIDConfirmation(ctx, expiredJob, "expired-token", modID, sourceHash, counts); err != nil || !paused {
		t.Fatalf("prepare expired confirmation: paused=%t err=%v", paused, err)
	}
	var expiredHash string
	if err = pool.QueryRow(ctx, `update catalog_import_jobs set updated_at=now()-interval '25 hours'
		where id=$1 returning modid_analysis_hash`, expiredJob).Scan(&expiredHash); err != nil {
		t.Fatal(err)
	}
	if response := invokeMODIDConfirmation(t, ctx, server, modSiteID, expiredJob, expiredHash, claims); response.Code != http.StatusConflict {
		t.Fatalf("expired confirmation status=%d body=%s", response.Code, response.Body.String())
	}
	assertMODIDJobState(t, ctx, pool, expiredJob, "confirmation_required", "", expiredHash, true)

	response := invokeMODIDConfirmation(t, ctx, server, modSiteID, happyJob, strings.ToUpper(analysisHash), claims)
	if response.Code != http.StatusAccepted {
		_, readErr := server.modExportJobByID(ctx, happyJob, modID)
		t.Fatalf("valid confirmation status=%d body=%s readback=%v", response.Code, response.Body.String(), readErr)
	}
	var confirmedBy *int64
	var outboxCount int
	var eventType string
	if err = pool.QueryRow(ctx, `select modid_confirmed_by from catalog_import_jobs where id=$1`, happyJob).Scan(&confirmedBy); err != nil {
		t.Fatal(err)
	}
	if confirmedBy == nil || *confirmedBy != actorID {
		t.Fatalf("confirmation actor=%v want=%d", confirmedBy, actorID)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int,coalesce(max(event_type),'')
		from nats_outbox where aggregate_type='mod_export_job' and aggregate_id=$1`, happyJob).Scan(&outboxCount, &eventType); err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 || eventType != "mod.catalog_import.confirmed" {
		t.Fatalf("confirmation outbox count=%d event=%q", outboxCount, eventType)
	}
	if replay := invokeMODIDConfirmation(t, ctx, server, modSiteID, happyJob, analysisHash, claims); replay.Code != http.StatusConflict {
		t.Fatalf("replayed confirmation status=%d body=%s", replay.Code, replay.Body.String())
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from nats_outbox
		where aggregate_type='mod_export_job' and aggregate_id=$1`, happyJob).Scan(&outboxCount); err != nil || outboxCount != 1 {
		t.Fatalf("replay duplicated outbox count=%d err=%v", outboxCount, err)
	}

	if _, err = pool.Exec(ctx, `update catalog_import_jobs set status='importing',run_token='resumed-token' where id=$1`, happyJob); err != nil {
		t.Fatal(err)
	}
	if paused, err = server.pauseCatalogImportForMODIDConfirmation(ctx, happyJob, "resumed-token", modID, sourceHash, counts); err != nil || paused {
		t.Fatalf("confirmed unchanged analysis paused again: paused=%t err=%v", paused, err)
	}
	assertMODIDJobState(t, ctx, pool, happyJob, "importing", "resumed-token", analysisHash, false)
	changedSourceHash := strings.Repeat("c", 64)
	if paused, err = server.pauseCatalogImportForMODIDConfirmation(ctx, happyJob, "resumed-token", modID, changedSourceHash, counts); err != nil || !paused {
		t.Fatalf("changed source hash did not require a new confirmation: paused=%t err=%v", paused, err)
	}
	var changedAnalysisHash string
	if err = pool.QueryRow(ctx, `select modid_analysis_hash from catalog_import_jobs where id=$1`, happyJob).Scan(&changedAnalysisHash); err != nil {
		t.Fatal(err)
	}
	if changedAnalysisHash == analysisHash {
		t.Fatal("source hash change did not change the bound analysis hash")
	}
	assertMODIDJobState(t, ctx, pool, happyJob, "confirmation_required", "", changedAnalysisHash, true)

	atomicJob := newJob("importing", "atomic-token", actorID)
	if paused, err = server.pauseCatalogImportForMODIDConfirmation(ctx, atomicJob, "atomic-token", modID, sourceHash, counts); err != nil || !paused {
		t.Fatalf("prepare atomic confirmation: paused=%t err=%v", paused, err)
	}
	var atomicHash string
	if err = pool.QueryRow(ctx, `select modid_analysis_hash from catalog_import_jobs where id=$1`, atomicJob).Scan(&atomicHash); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from nats_outbox`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table nats_outbox add constraint reject_modid_confirmation_for_test
		check(event_type<>'mod.catalog_import.confirmed')`); err != nil {
		t.Fatal(err)
	}
	if failed := invokeMODIDConfirmation(t, ctx, server, modSiteID, atomicJob, atomicHash, claims); failed.Code != http.StatusInternalServerError {
		t.Fatalf("outbox failure confirmation status=%d body=%s", failed.Code, failed.Body.String())
	}
	assertMODIDJobState(t, ctx, pool, atomicJob, "confirmation_required", "", atomicHash, true)
	if err = pool.QueryRow(ctx, `select count(*)::int from nats_outbox where aggregate_id=$1`, atomicJob).Scan(&outboxCount); err != nil || outboxCount != 0 {
		t.Fatalf("failed confirmation left outbox count=%d err=%v", outboxCount, err)
	}
}

func invokeMODIDConfirmation(t *testing.T, ctx context.Context, server *Server, siteID, jobID, analysisHash string, claims security.Claims) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(confirmMODIDMismatchRequest{Confirm: true, AnalysisHash: analysisHash})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mods/"+siteID+"/export-imports/"+jobID+"/confirm-modid", bytes.NewReader(raw))
	request.SetPathValue("siteId", siteID)
	request.SetPathValue("jobId", jobID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.confirmModExportMODIDMismatch(response, request)
	return response
}

func assertMODIDJobState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID, wantStatus, wantRunToken, wantHash string, wantRequired bool) {
	t.Helper()
	var status, runToken, analysisHash string
	var required bool
	if err := pool.QueryRow(ctx, `select status,run_token,modid_analysis_hash,modid_confirmation_required
		from catalog_import_jobs where id=$1`, jobID).Scan(&status, &runToken, &analysisHash, &required); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || runToken != wantRunToken || analysisHash != wantHash || required != wantRequired {
		t.Fatalf("job %s state status=%q runToken=%q hash=%q required=%t want status=%q runToken=%q hash=%q required=%t",
			jobID, status, runToken, analysisHash, required, wantStatus, wantRunToken, wantHash, wantRequired)
	}
}
