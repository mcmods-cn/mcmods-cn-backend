package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The vanilla archival package contains no key mappings. Separately import
// every real package that does, checking exact identities/default keys and
// full-schema section recreation instead of fabricating vanilla entries.
func TestRealExporterKeyMappingsRoundTripIntegration(t *testing.T) {
	directory := strings.TrimSpace(os.Getenv("MCMODS_EXPORT_TEST_DIR"))
	if directory == "" {
		t.Skip("set MCMODS_EXPORT_TEST_DIR for actual exporter key mappings")
	}
	paths, err := filepath.Glob(filepath.Join(directory, "*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	pool := openModExportConcurrentTestDB(t)
	packages, totalEntries := 0, 0
	for _, archivePath := range paths {
		t.Run(filepath.Base(archivePath), func(t *testing.T) {
			archive, err := zip.OpenReader(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			files, err := validateExportZIP(archive.File)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := readExportZIPFile(files["registries/key_mappings.json"], maxExportJSONSize)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err = json.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			entries := exportObjectArray(document["entries"])
			expectedDefaultKeys := make(map[string]string, len(entries))
			for _, entry := range entries {
				expectedDefaultKeys[exportString(entry["id"])] = exportString(entry["default_key"])
			}
			if len(entries) == 0 {
				t.Log("actual package has zero key mappings; vanilla test requires zero projected key mappings")
				return
			}
			manifestRaw, err := readExportZIPFile(files["manifest.json"], maxExportJSONSize)
			if err != nil {
				t.Fatal(err)
			}
			var manifest modExportManifest
			if err = json.Unmarshal(manifestRaw, &manifest); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			revisionID, versionID := createRealKeyMappingImportFacts(t, ctx, tx)
			revisions := make(map[string]string)
			for _, namespace := range manifest.Configuration.Namespaces {
				revisions[namespace] = revisionID
			}
			rows, err := prepareExportRegistryResources(nil, revisions, "registries/key_mappings.json", raw)
			if err != nil || len(rows) != len(entries) {
				t.Fatalf("actual registry input=%d prepared=%d err=%v", len(entries), len(rows), err)
			}
			batch := newModExportWriteBatch()
			for _, row := range rows {
				queueCatalogResource(batch, row)
			}
			if err = batch.flush(ctx, tx); err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 2; pass++ {
				if pass == 1 {
					if _, err = tx.Exec(ctx, `update mod_content_sections set status='archived' where version_id=$1 and parent_id is null`, versionID); err != nil {
						t.Fatal(err)
					}
				}
				if err = syncImportedResourcesToContentVersionTx(ctx, tx, []string{revisionID}, versionID, pass == 1, 0); err != nil {
					t.Fatal(err)
				}
				var placements int
				if err = tx.QueryRow(ctx, `select count(*)::int from mod_content_section_resources placement join mod_content_sections section on section.id=placement.section_id join mod_content_templates template on template.id=section.template_id where placement.version_id=$1 and section.status='active' and section.parent_id is null and template.code='key_mapping'`, versionID).Scan(&placements); err != nil || placements != len(entries) {
					t.Fatalf("pass=%d placements=%d source=%d err=%v", pass, placements, len(entries), err)
				}
				for _, row := range rows {
					var defaultKey string
					if err = tx.QueryRow(ctx, `select detail.definition->>'defaultKey' from mod_resource_version_details detail join game_resources resource on resource.entity_id=detail.resource_id where detail.version_id=$1 and resource.kind_code='minecraft.key_mapping' and resource.canonical_id=$2 and detail.status='active'`, versionID, row.CanonicalID).Scan(&defaultKey); err != nil || defaultKey != expectedDefaultKeys[row.RawID] {
						t.Fatalf("pass=%d resource=%s default=%q source=%q err=%v", pass, row.CanonicalID, defaultKey, expectedDefaultKeys[row.RawID], err)
					}
				}
			}
			packages++
			totalEntries += len(entries)
			t.Logf("actual input=%d initial/rebuilt placements and default keys match", len(entries))
		})
	}
	if packages == 0 || totalEntries == 0 {
		t.Fatal("no real nonempty key-mapping input was tested")
	}
	t.Logf("nonempty_real_packages=%d source_key_mappings=%d", packages, totalEntries)
}

func createRealKeyMappingImportFacts(t *testing.T, ctx context.Context, tx pgx.Tx) (string, int64) {
	t.Helper()
	var userID, modID, versionID, fileID int64
	for _, step := range []struct {
		sql    string
		args   []any
		result *int64
	}{
		{`insert into users(username,email,password_hash) values('real_key_mapping_test','real_key_mapping_test@example.invalid','test') returning id`, nil, &userID},
		{`insert into mods(project_code,slug,primary_name,review_status) values('keytest01','real-key-mapping-test','Real key mapping test','approved') returning id`, nil, &modID},
	} {
		if err := tx.QueryRow(ctx, step.sql, step.args...).Scan(step.result); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status) values($1,'real key mappings',array['1.20.1'],array['forge'],'active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `insert into oss_files(object_key,original_name,sha256,uploader_id) values('tests/real-key-mapping/package.zip','test.zip',$1,$2) returning id`, strings.Repeat("b", 64), userID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	packageID, jobID, revisionID := "00000000-0000-4000-8000-000000100001", "00000000-0000-4000-8000-000000100002", "00000000-0000-4000-8000-000000100003"
	if _, err := tx.Exec(ctx, `insert into catalog_import_packages(id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,uploaded_by) values($1,$2,$3,'test.zip','mcmods-export/v1','0.7.0','1.20.1','forge','{}',$4)`, packageID, strings.Repeat("b", 64), fileID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status) values($1,$2,$3,$4,$5,'ready')`, jobID, modID, packageID, versionID, modExportImporterVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active) values($1,$2,$3,$4,$5,1,'ready','1.20.1','forge','0.7.0','key-test',true)`, revisionID, modID, packageID, jobID, versionID); err != nil {
		t.Fatal(err)
	}
	return revisionID, versionID
}
