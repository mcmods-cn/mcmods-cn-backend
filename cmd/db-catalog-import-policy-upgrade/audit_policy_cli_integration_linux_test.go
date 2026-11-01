package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

const auditPolicyCLIArguments = "MCMODS_AUDIT_POLICY_CLI_CHILD_ARGUMENTS"

// Only this test executable dispatches this private child mode. The real CLI
// main/flags/configuration/connection and exit status run in a separate process.
func TestAuditPolicyCLIChild(t *testing.T) {
	raw := os.Getenv(auditPolicyCLIArguments)
	if raw == "" {
		t.Skip("private owned CLI subprocess only")
	}
	var arguments []string
	if json.Unmarshal([]byte(raw), &arguments) != nil {
		t.Fatal("invalid private test arguments")
	}
	flag.CommandLine = flag.NewFlagSet("owned-policy-upgrade", flag.ExitOnError)
	os.Args = append([]string{os.Args[0]}, arguments...)
	main()
	os.Exit(0)
}

func auditPolicyCLILock(t *testing.T, ctx context.Context) func() {
	t.Helper()
	path := os.Getenv("MCMODS_AUDIT_PG_LOCK")
	// The project DB-suite runner already holds its own batch lock. Standalone
	// task runs can request short setup/drop coordination without nesting it.
	if path == "" {
		return func() {}
	}
	if !filepath.IsAbs(path) {
		t.Fatal("cooperation lock must be absolute")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			file.Close()
			t.Fatal("owned database setup/drop lock exceeded its original context")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func auditPolicyCLIEnvironment(t *testing.T) (context.Context, *pgxpool.Pool, string, string) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires explicitly configured owned PostgreSQL integration database")
	}
	connection := config.Load().DB.ConnString()
	if os.Getenv("APP_ENV") != "test" || os.Getenv("MCMODS_TEST_DATABASE_URL") != connection {
		t.Fatal("requires APP_ENV=test and identical explicit test connection")
	}
	parsed, err := url.Parse(connection)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1" && parsed.Hostname() != "localhost") {
		t.Fatal("refusing non-loopback or invalid test connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	owner, err := pgxpool.New(ctx, connection)
	if err != nil {
		t.Fatal("owned test connection failed")
	}
	t.Cleanup(owner.Close)
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "audit_policy_cli_" + hex.EncodeToString(nonce[:])
	marker := "mcmods-audit-policy-cli:" + hex.EncodeToString(nonce[:])
	unlock := auditPolicyCLILock(t, ctx)
	if _, err = owner.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()+" template template0"); err != nil {
		unlock()
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, "comment on database "+pgx.Identifier{name}.Sanitize()+" is '"+marker+"'"); err != nil {
		unlock()
		t.Fatal(err)
	}
	parsed.Path, parsed.RawPath = "/"+name, ""
	targetURL := parsed.String()
	db, err := pgxpool.New(ctx, targetURL)
	if err != nil {
		unlock()
		t.Fatal("owned target connection failed")
	}
	t.Cleanup(func() {
		db.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		release := auditPolicyCLILock(t, cleanup)
		defer release()
		var ours bool
		if err := owner.QueryRow(cleanup, `select datdba=(select oid from pg_roles where rolname=current_user) and shobj_description(oid,'pg_database')=$2 from pg_database where datname=$1`, name, marker).Scan(&ours); err != nil || !ours {
			t.Error("refusing cleanup: exact database owner/nonce mismatch")
			return
		}
		// No FORCE, backend termination or cleanup of another test's database.
		if _, err := owner.Exec(cleanup, "drop database "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Error(err)
		}
	})
	err = database.Migrate(ctx, db)
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	return ctx, db, name, targetURL
}

func auditRunPolicyCLI(t *testing.T, ctx context.Context, targetURL string, arguments ...string) ([]byte, string, int) {
	t.Helper()
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAuditPolicyCLIChild$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "DATABASE_URL=") && !strings.HasPrefix(entry, auditPolicyCLIArguments+"=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "DATABASE_URL="+targetURL, auditPolicyCLIArguments+"="+string(raw))
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	if err == nil {
		return stdout.Bytes(), stderr.String(), 0
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		t.Fatal("owned CLI subprocess failed to execute")
	}
	return stdout.Bytes(), stderr.String(), exited.ExitCode()
}

func auditPolicyCLIShadow(t *testing.T, ctx context.Context, db *pgxpool.Pool, targetURL string) string {
	t.Helper()
	if _, err := db.Exec(ctx, `create schema audit_shadow; create table audit_shadow.catalog_import_jobs(id text,status text)`); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(targetURL)
	if err != nil {
		t.Fatal("invalid owned target")
	}
	query := parsed.Query()
	query.Set("search_path", "audit_shadow,public")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func auditPolicyCLIShadowUntouched(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var columns int
	if err := db.QueryRow(ctx, `select count(*) from information_schema.columns where table_schema='audit_shadow' and table_name='catalog_import_jobs'`).Scan(&columns); err != nil || columns != 2 {
		t.Fatalf("CLI changed the caller's unrelated shadow table: columns=%d err=%v", columns, err)
	}
}

func TestAuditPolicyCLIReadyPublicSchemaNeverChangesCallerShadowIntegration(t *testing.T) {
	ctx, db, name, targetURL := auditPolicyCLIEnvironment(t)
	targetURL = auditPolicyCLIShadow(t, ctx, db, targetURL)
	stdout, stderr, code := auditRunPolicyCLI(t, ctx, targetURL, "--apply", "--database", name, "--backup-reference", "synthetic-owned-copy")
	if code != 0 || stderr != "" {
		t.Fatalf("ready-public owned CLI failed: exit%d %s", code, stderr)
	}
	var report struct{ Applied bool }
	if json.Unmarshal(stdout, &report) != nil || !report.Applied {
		t.Fatal("CLI did not report actual completed apply")
	}
	auditPolicyCLIShadowUntouched(t, ctx, db)
	if err := database.RequireCatalogImportPolicySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestAuditPolicyCLIOldDatabaseReadOnlyGuardsAndIdempotentUpgradeIntegration(t *testing.T) {
	ctx, db, name, targetURL := auditPolicyCLIEnvironment(t)
	var user, mod, version, file int64
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash) values('owned-cli-policy','owned-cli-policy@example.invalid','synthetic-only') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by) values('pcli00001','owned-cli-policy','Owned policy tool',$1) returning id`, user).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by) values($1,'1.20.1','active',$2,$2) returning id`, mod, user).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,sha256,uploader_id) values('fixture','https://example.invalid','fixture','owned-policy-source',repeat('a',64),$1) returning id`, user).Scan(&file); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into catalog_import_packages(id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,uploaded_by) values('owned-policy-package',repeat('a',64),$1,'synthetic.zip','mcmods-export/v1','test','1.20.1','forge','{}',$2)`, file, user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `alter table public.catalog_import_jobs drop column requested_publication_mode`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,created_by) values('owned-legacy-policy-job',$1,'owned-policy-package',$2,'fixture/v1',$3)`, mod, version, user); err != nil {
		t.Fatal(err)
	}
	targetURL = auditPolicyCLIShadow(t, ctx, db, targetURL)
	readonly, _ := url.Parse(targetURL)
	parameters := readonly.Query()
	parameters.Set("default_transaction_read_only", "on")
	readonly.RawQuery = parameters.Encode()
	stdout, stderr, code := auditRunPolicyCLI(t, ctx, readonly.String())
	if code != 0 || stderr != "" {
		t.Fatalf("actual readonly CLI failed: exit%d %s", code, stderr)
	}
	var report struct {
		Applied             bool
		ColumnPresentBefore bool     `json:"column_present_before"`
		JobStatesModified   bool     `json:"job_states_modified"`
		LegacyActiveJobs    int64    `json:"legacy_active_jobs"`
		CandidateIDs        []string `json:"candidate_ids"`
	}
	if json.Unmarshal(stdout, &report) != nil || report.Applied || report.ColumnPresentBefore || report.JobStatesModified || report.LegacyActiveJobs != 1 || len(report.CandidateIDs) != 1 || report.CandidateIDs[0] != "owned-legacy-policy-job" {
		t.Fatal("dry-run falsely reported a migration or state mutation")
	}
	for _, args := range [][]string{{"--apply", "--database", "unrelated_database", "--backup-reference", "synthetic-owned-copy"}, {"--apply", "--database", name}} {
		_, stderr, code = auditRunPolicyCLI(t, ctx, targetURL, args...)
		if code != 1 || strings.TrimSpace(stderr) != "apply requires exact --database and --backup-reference" {
			t.Fatal("guard did not return its precise target/backup refusal")
		}
	}
	if err := database.RequireCatalogImportPolicySchema(ctx, db); err == nil || !strings.Contains(err.Error(), "catalog import policy extension required") {
		t.Fatal("readonly/rejected apply changed the public extension")
	}
	stdout, stderr, code = auditRunPolicyCLI(t, ctx, targetURL, "--apply", "--database", name, "--backup-reference", "synthetic-owned-copy")
	if code != 0 || stderr != "" {
		t.Fatalf("actual old-public CLI upgrade failed: exit%d %s", code, stderr)
	}
	if json.Unmarshal(stdout, &report) != nil || !report.Applied || report.ColumnPresentBefore || report.JobStatesModified {
		t.Fatal("old-public apply report does not describe its actual operation")
	}
	if err := database.RequireCatalogImportPolicySchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var mode, status string
	var creator, preservedFile int64
	if err := db.QueryRow(ctx, `select job.requested_publication_mode,job.status,job.created_by,package.archive_file_id from catalog_import_jobs job join catalog_import_packages package on package.id=job.package_id where job.id='owned-legacy-policy-job'`).Scan(&mode, &status, &creator, &preservedFile); err != nil || mode != "legacy_unknown" || status != "queued" || creator != user || preservedFile != file {
		t.Fatal("actual CLI changed the legacy state, mode provenance, creator or source")
	}
	if _, err := db.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,created_by,overwrite_existing) values('owned-new-policy-job',$1,'owned-policy-package',$2,'fixture/v1',$3,true)`, mod, version, user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `select requested_publication_mode from catalog_import_jobs where id='owned-new-policy-job'`).Scan(&mode); err != nil || mode != "review" {
		t.Fatal("new row did not receive the actual review default")
	}
	if _, err := db.Exec(ctx, `update catalog_import_jobs set requested_publication_mode='automatic' where id='owned-new-policy-job'`); err != nil {
		t.Fatal(err)
	}
	auditPolicyCLIShadowUntouched(t, ctx, db)
	_, stderr, code = auditRunPolicyCLI(t, ctx, targetURL, "--apply", "--database", name, "--backup-reference", "synthetic-owned-copy")
	if code != 0 || stderr != "" {
		t.Fatal("actual repeat upgrade failed")
	}
	if err := db.QueryRow(ctx, `select requested_publication_mode from catalog_import_jobs where id='owned-new-policy-job'`).Scan(&mode); err != nil || mode != "automatic" {
		t.Fatal("repeat CLI changed an explicitly recorded publication mode")
	}
	auditPolicyCLIShadowUntouched(t, ctx, db)
	t.Log("Actual process CLI: readonly PostgreSQL diagnosis, exact wrong-target/missing-backup zero-DDL guards, old-public upgrade and repeat, unrelated shadow unchanged; synthetic owned databases only")
}
