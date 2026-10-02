package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestModExportUploadRecoverySourcesCarryAuthenticatedOwner(t *testing.T) {
	base := ossModImportCategory("example-project", "mcmods-exporter", "packages")
	first := ossOwnerObjectCategory(base, 41)
	second := ossOwnerObjectCategory(base, 42)
	if first == second || !strings.HasSuffix(first, "/owners/41") || !strings.HasSuffix(second, "/owners/42") {
		t.Fatalf("owner-scoped object categories are not isolated: first=%q second=%q", first, second)
	}

	ossSource, err := os.ReadFile("oss_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	handlerSource, err := os.ReadFile("mod_export_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"create": string(ossSource),
		"resume": string(handlerSource),
	} {
		if !strings.Contains(source, "ossOwnerObjectCategory") {
			t.Fatalf("%s upload path does not enforce the authenticated owner category", name)
		}
	}
	if !strings.Contains(string(handlerSource), "job.created_by=$3") ||
		!strings.Contains(string(handlerSource), "modExportJobByIDForCreator") {
		t.Fatal("job recovery does not enforce the authenticated creator")
	}
}

func TestModExportRecoveryIsIsolatedAcrossTwoAccountsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify dual-account Mod export recovery")
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
	var firstUserID, secondUserID int64
	for index, target := range []*int64{&firstUserID, &secondUserID} {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
			values($1,$2,'test-only',true) returning id`,
			fmt.Sprintf("export_recovery_%d_%s", index, nonce),
			fmt.Sprintf("%d-%s@export-recovery.invalid", index, nonce)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	var modID, versionID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,'Export recovery isolation','approved',$3) returning id`,
		randomCatalogPublicID(), "export-recovery-"+nonce, firstUserID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	var versionPublicID string
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by)
		values($1,'Recovery','active',$2,$2) returning id,public_id`, modID, firstUserID).
		Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	newJob := func(ownerID int64, sequence int) string {
		t.Helper()
		packageID, jobID := newExportID(), newExportID()
		digest := fmt.Sprintf("%064x", sequence)
		var fileID int64
		if insertErr := pool.QueryRow(ctx, `insert into oss_files(object_key,original_name,sha256,uploader_id,source,category)
			values($1,'recovery.zip',$2,$3,'mcmods_exporter',$4) returning id`,
			ossOwnerObjectCategory("tests/export-recovery", ownerID)+"/recovery.zip", digest, ownerID, "tests/export-recovery").Scan(&fileID); insertErr != nil {
			t.Fatal(insertErr)
		}
		if _, insertErr := pool.Exec(ctx, `insert into catalog_import_packages(
			id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,profile,uploaded_by)
			values($1,$2,$3,'recovery.zip','mcmods-export/v1','test','1.21.1','unknown','{}','all',$4)`, packageID, digest, fileID, ownerID); insertErr != nil {
			t.Fatal(insertErr)
		}
		if _, insertErr := pool.Exec(ctx, `insert into catalog_import_jobs(
			id,mod_id,package_id,target_version_id,importer_version,status,current_stage,created_by)
			values($1,$2,$3,$4,$5,'queued','recovery',$6)`,
			jobID, modID, packageID, versionID, modExportImporterVersion, ownerID); insertErr != nil {
			t.Fatal(insertErr)
		}
		return jobID
	}

	firstJobID := newJob(firstUserID, 1)
	if _, err = server.modExportJobByIDForCreator(ctx, firstJobID, modID, firstUserID); err != nil {
		t.Fatalf("owner could not recover their job: %v", err)
	}
	if _, err = server.modExportJobByIDForCreator(ctx, firstJobID, modID, secondUserID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second account recovered the first account's job: %v", err)
	}
	if activeID, activeErr := server.activeModExportJobID(ctx, modID, versionPublicID, firstUserID); activeErr != nil || activeID != firstJobID {
		t.Fatalf("first account active recovery = %q, %v; want %q", activeID, activeErr, firstJobID)
	}
	if activeID, activeErr := server.activeModExportJobID(ctx, modID, versionPublicID, secondUserID); !errors.Is(activeErr, pgx.ErrNoRows) || activeID != "" {
		t.Fatalf("second account discovered the first account's active job: %q, %v", activeID, activeErr)
	}

	secondJobID := newJob(secondUserID, 2)
	if activeID, activeErr := server.activeModExportJobID(ctx, modID, versionPublicID, secondUserID); activeErr != nil || activeID != secondJobID {
		t.Fatalf("second account did not recover its own job: %q, %v; want %q", activeID, activeErr, secondJobID)
	}
}
