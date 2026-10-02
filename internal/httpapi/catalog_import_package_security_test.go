package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCatalogImportCreationDoesNotOverwritePackagesByDeclaredHash(t *testing.T) {
	for _, name := range []string{"mod_export_handlers.go", "mod_embedded_icon_import.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(source))
		if strings.Contains(text, "on conflict (sha256) do update") || strings.Contains(text, "on conflict(sha256) do update") {
			t.Fatalf("%s still mutates a global package on a client-declared hash conflict", name)
		}
		if !strings.Contains(text, "insertcatalogimportpackage") {
			t.Fatalf("%s does not use the immutable package source path", name)
		}
		if !strings.Contains(text, "for key share") {
			t.Fatalf("%s does not lock and revalidate the source file in its creation transaction", name)
		}
		verifiedIndex := strings.Index(text, "markcatalogimportpackagecontentverified")
		if verifiedIndex < 0 {
			t.Fatalf("%s never records successful content verification", name)
		}
		switch name {
		case "mod_export_handlers.go":
			hashCheckIndex := strings.Index(text, "hex.encodetostring(hasher.sum(nil)) != expectedhash")
			if hashCheckIndex < 0 || verifiedIndex <= hashCheckIndex {
				t.Fatalf("%s can mark a package verified before comparing the streamed object hash", name)
			}
		case "mod_embedded_icon_import.go":
			downloadIndex := strings.Index(text, "raw, err := downloadembeddediconcatalog")
			hashCheckIndex := strings.Index(text, "hex.encodetostring(hasher.sum(nil)) != expectedhash")
			if downloadIndex < 0 || hashCheckIndex < 0 || verifiedIndex <= downloadIndex || hashCheckIndex <= verifiedIndex {
				t.Fatalf("%s does not keep verification behind its hash-validating download boundary", name)
			}
		}
	}
	deletionSource, err := os.ReadFile("user_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(deletionSource), "catalogImportSourceHasActiveJob") {
		t.Fatal("user file deletion does not protect active catalog import sources")
	}
}

func TestCatalogImportSameSHAKeepsDistinctUserSources(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `create temp table catalog_import_packages(
		id text primary key,sha256 text not null,archive_file_id bigint not null unique,archive_name text not null,
		schema_version text not null,exporter_version text not null,minecraft_version text not null,loader text not null,
		manifest jsonb not null,namespaces text[] not null,profile text not null,uploaded_by bigint not null,
		content_verified_at timestamptz,uploaded_at timestamptz not null default now(),imported_at timestamptz) on commit drop;
		create temp table catalog_import_jobs(
		id text primary key,package_id text not null,status text not null) on commit drop`); err != nil {
		t.Fatal(err)
	}
	sharedHash := strings.Repeat("a", 64)
	first := catalogImportPackageInput{
		ID: "package-1", SHA256: sharedHash, ArchiveFileID: 101, ArchiveName: "first.zip",
		SchemaVersion: "", ExporterVersion: "", Loader: "unknown", Manifest: json.RawMessage(`{}`),
		Profile: "all", UploadedBy: 1,
	}
	firstID, err := insertCatalogImportPackage(ctx, tx, first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID, second.ArchiveFileID, second.ArchiveName, second.UploadedBy = "package-2", 202, "second.zip", 2
	secondID, err := insertCatalogImportPackage(ctx, tx, second)
	if err != nil {
		t.Fatal(err)
	}
	if firstID == secondID {
		t.Fatalf("same SHA collapsed two user sources into package %q", firstID)
	}
	retry := first
	retry.ID = "package-retry"
	retryID, err := insertCatalogImportPackage(ctx, tx, retry)
	if err != nil {
		t.Fatal(err)
	}
	if retryID != firstID {
		t.Fatalf("same immutable file was not idempotent: got %q want %q", retryID, firstID)
	}
	var count int
	var firstFile, secondFile int64
	if err = tx.QueryRow(ctx, `select count(*),min(archive_file_id),max(archive_file_id) from catalog_import_packages`).
		Scan(&count, &firstFile, &secondFile); err != nil {
		t.Fatal(err)
	}
	if count != 2 || firstFile != 101 || secondFile != 202 {
		t.Fatalf("package sources changed: count=%d files=%d/%d", count, firstFile, secondFile)
	}
	if err = markCatalogImportPackageContentVerified(ctx, tx, firstID, 101, sharedHash); err != nil {
		t.Fatal(err)
	}
	if err = markCatalogImportPackageContentVerified(ctx, tx, firstID, 202, sharedHash); !errors.Is(err, errCatalogImportPackageSourceChanged) {
		t.Fatalf("mismatched verified source error = %v", err)
	}
	if _, err = tx.Exec(ctx, `insert into catalog_import_jobs(id,package_id,status) values('job-1',$1,'queued')`, firstID); err != nil {
		t.Fatal(err)
	}
	locked, err := catalogImportSourceHasActiveJob(ctx, tx, 101)
	if err != nil || !locked {
		t.Fatalf("queued source lock = %v/%v", locked, err)
	}
	if _, err = tx.Exec(ctx, `update catalog_import_jobs set status='ready' where id='job-1'`); err != nil {
		t.Fatal(err)
	}
	locked, err = catalogImportSourceHasActiveJob(ctx, tx, 101)
	if err != nil || locked {
		t.Fatalf("terminal source lock = %v/%v", locked, err)
	}
}
