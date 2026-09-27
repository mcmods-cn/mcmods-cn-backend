package httpapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

var errCatalogImportPackageSourceChanged = errors.New("catalog import package source changed")

type catalogImportPackageInput struct {
	ID               string
	SHA256           string
	ArchiveFileID    int64
	ArchiveName      string
	SchemaVersion    string
	ExporterVersion  string
	MinecraftVersion string
	Loader           string
	Manifest         json.RawMessage
	Namespaces       []string
	Profile          string
	UploadedBy       int64
}

type catalogImportPackageQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// insertCatalogImportPackage is idempotent only for the exact OSS file row.
// Equal client-declared hashes from different files deliberately create
// independent package sources until a worker verifies each object's bytes.
func insertCatalogImportPackage(ctx context.Context, queryer catalogImportPackageQueryer, input catalogImportPackageInput) (string, error) {
	manifest := input.Manifest
	if len(manifest) == 0 {
		manifest = json.RawMessage(`{}`)
	}
	if input.Namespaces == nil {
		input.Namespaces = []string{}
	}
	var packageID string
	err := queryer.QueryRow(ctx, `insert into catalog_import_packages(
		id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,
		manifest,namespaces,profile,uploaded_by)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12)
		on conflict(archive_file_id) do update set archive_file_id=excluded.archive_file_id
		where catalog_import_packages.sha256=excluded.sha256
		  and catalog_import_packages.archive_name=excluded.archive_name
		  and catalog_import_packages.uploaded_by is not distinct from excluded.uploaded_by
		returning id`,
		input.ID, input.SHA256, input.ArchiveFileID, input.ArchiveName, input.SchemaVersion,
		input.ExporterVersion, input.MinecraftVersion, input.Loader, manifest, input.Namespaces,
		input.Profile, input.UploadedBy).Scan(&packageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errCatalogImportPackageSourceChanged
	}
	return packageID, err
}

func markCatalogImportPackageContentVerified(ctx context.Context, queryer catalogImportPackageQueryer, packageID string, archiveFileID int64, sha256 string) error {
	var verifiedID string
	err := queryer.QueryRow(ctx, `update catalog_import_packages
		set content_verified_at=coalesce(content_verified_at,now())
		where id=$1 and archive_file_id=$2 and sha256=$3
		returning id`, packageID, archiveFileID, sha256).Scan(&verifiedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errCatalogImportPackageSourceChanged
	}
	return err
}

func catalogImportSourceHasActiveJob(ctx context.Context, queryer catalogImportPackageQueryer, archiveFileID int64) (bool, error) {
	var active bool
	err := queryer.QueryRow(ctx, `select exists(
		select 1 from catalog_import_packages package
		join catalog_import_jobs job on job.package_id=package.id
		where package.archive_file_id=$1
		  and job.status in ('queued','validating','confirmation_required','importing'))`, archiveFileID).Scan(&active)
	return active, err
}
