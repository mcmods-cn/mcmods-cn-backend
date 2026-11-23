package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestCatalogImportStagingFollowsCurrentAttemptIntegration(t *testing.T) {
	for _, source := range []string{"mcmods_exporter", iconRendererImportSource} {
		t.Run(source, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			var job modExportJobResponse
			if source == "mcmods_exporter" {
				job = f.job(t, f.upload(t, auditFixGRZip(t, nil)))
			} else {
				job = auditImportEmbeddedJob(t, f)
			}

			observer := &Server{db: f.db}
			oldToken, err := observer.claimModExportJob(f.ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			metadata := catalogImportMetadata{
				SchemaVersion: "previous-attempt", ExporterVersion: "previous-attempt",
				MinecraftVersion: "1.20.1", Loader: "forge",
				Manifest: json.RawMessage(`{}`), Namespaces: []string{"example"}, Profile: "all",
			}
			if err = observer.startCatalogImportStaging(f.ctx, job.ID, oldToken, metadata, func(tx pgx.Tx) error {
				_, err := tx.Exec(f.ctx, `insert into catalog_import_revisions(
					id,mod_id,package_id,job_id,target_version_id,revision_no,minecraft_version,loader,
					exporter_version,source_namespace,source_kind,import_run_token)
					select $2,mod_id,package_id,id,target_version_id,1,'1.20.1','forge',
						'previous-attempt','example',$3,$4 from catalog_import_jobs where id=$1`,
					job.ID, newExportID(), source, oldToken)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, `update catalog_import_jobs
				set heartbeat_at=now()-interval '10 minutes' where id=$1 and run_token=$2`, job.ID, oldToken); err != nil {
				t.Fatal(err)
			}
			if err = observer.recoverStaleModExportJobs(f.ctx); err != nil {
				t.Fatal(err)
			}
			assertModExportAttemptState(t, f.ctx, f.db, job.ID, "queued", "mod.catalog_import.recovered", 1)
			if count := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1`, job.ID); count != 0 {
				t.Fatalf("recovered attempt left %d staging revisions", count)
			}

			barrier, _ := auditImportInstallBarrier(t, f, job.ID, 2, 25)
			done := make(chan struct{})
			var importErr error
			go func() {
				defer close(done)
				importErr = f.server.importModExportJob(context.WithValue(f.ctx, auditImportBarrierContextKey{}, job.ID), job.ID)
			}()
			var released bool
			defer func() {
				if !released {
					close(barrier.release)
				}
				<-done
			}()
			select {
			case <-barrier.ready:
			case <-done:
				t.Fatalf("replacement did not reach its staging writes: %v", importErr)
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			var currentToken, currentSchema string
			if err = f.db.QueryRow(f.ctx, `select job.run_token,package.schema_version from catalog_import_jobs job
				join catalog_import_packages package on package.id=job.package_id where job.id=$1`, job.ID).
				Scan(&currentToken, &currentSchema); err != nil {
				t.Fatal(err)
			}
			if currentToken == "" || currentToken == oldToken || currentSchema == metadata.SchemaVersion {
				t.Fatalf("replacement did not stage its own manifest under a new lease")
			}
			rejectOldWrite := func(pgx.Tx) error {
				t.Error("a stale attempt reached the staging write")
				return nil
			}
			if err = observer.startCatalogImportStaging(f.ctx, job.ID, oldToken, metadata, rejectOldWrite); !errors.Is(err, errModExportLeaseLost) {
				t.Errorf("late staging initialization error=%v", err)
			}
			if err = observer.runCatalogImportStagingTransaction(f.ctx, job.ID, oldToken, rejectOldWrite); !errors.Is(err, errModExportLeaseLost) {
				t.Errorf("late staging write error=%v", err)
			}
			if err = observer.cleanupModExportStaging(f.ctx, job.ID, oldToken); err != nil {
				t.Fatal(err)
			}
			if count := f.number(t, `select count(*) from catalog_import_revisions
				where job_id=$1 and import_run_token=$2 and status='staging'`, job.ID, currentToken); count != 1 {
				t.Fatalf("late cleanup left %d replacement staging revisions", count)
			}
			if count := f.number(t, `select count(*) from catalog_import_packages package
				join catalog_import_jobs job on job.package_id=package.id
				where job.id=$1 and package.schema_version=$2`, job.ID, currentSchema); count != 1 {
				t.Fatalf("late attempt changed the replacement manifest")
			}

			close(barrier.release)
			released = true
			<-done
			if importErr != nil {
				t.Fatal(importErr)
			}
			if count := f.number(t, `select count(*) from catalog_import_revisions revision
				join catalog_import_jobs job on job.id=revision.job_id where job.id=$1
				and revision.import_run_token=$2 and revision.status in ('ready','partial') and not revision.is_active
				and job.current_stage='review' and job.run_token=''`, job.ID, currentToken); count != 1 {
				t.Fatalf("replacement did not finish in independent review: %d", count)
			}
		})
	}
}
