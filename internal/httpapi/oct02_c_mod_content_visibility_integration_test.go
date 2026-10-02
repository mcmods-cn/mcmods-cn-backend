package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02CModContentReadsHonorProjectPublicationIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Private project content")
	section := f.section(t, version.PublicID, f.builtin(t, "advancement"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:private_advancement", "minecraft.advancement", "advancement", map[string]any{})
	f.review(t, resource, f.editor, "approved")
	revisionID := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	exportPaths := []string{
		"/api/v1/export-revisions/" + revisionID + "/registries/items",
		"/api/v1/export-revisions/" + revisionID + "/registry-entries?registries=items&summary=1",
		"/api/v1/export-revisions/" + revisionID + "/document-entries?kind=advancements&summary=1",
		"/api/v1/export-revisions/" + revisionID + "/tags",
		"/api/v1/export-revisions/" + revisionID + "/assets",
		"/api/v1/export-revisions/" + revisionID + "/assets/content?path=private.txt",
		"/api/v1/export-revisions/" + revisionID + "/structures",
	}
	for _, path := range exportPaths {
		f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
	}
	paths := []string{
		"/api/v1/mods/test013-mod/content-versions",
		"/api/v1/mods/test013-mod/content-templates",
		"/api/v1/mods/test013-mod/content-sections",
		"/api/v1/mods/test013-mod/content-sections/" + section.PublicID + "/resources",
		"/api/v1/mods/test013-mod/content-sections/" + section.PublicID + "/resource-graph",
		"/api/v1/mods/test013-mod/content-resources",
		"/api/v1/mods/test013-mod/content-resources/" + resource.PublicID,
		"/api/v1/mods/test013-mod/content-resources/" + resource.PublicID + "/similar?version=" + version.PublicID,
		"/api/v1/mods/test013-mod/content-resources/" + resource.PublicID + "/history?version=" + version.PublicID,
		"/api/v1/mods/test013-mod/export-data",
	}
	for _, path := range paths {
		f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
	}
	// A parent project's visibility is independent of the already-published
	// child content. Only this test's disposable database is changed.
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending',submitted_by=$2 where id=$1`, f.modID, f.userIDs[f.denied]); err != nil {
		t.Fatal(err)
	}
	carrier := map[string]any{"publicId": resource.PublicID}
	server := &Server{db: f.db}
	if err := server.decorateResourceVersionRows(f.ctx, []map[string]any{carrier}, "en-US", "zh-CN"); err != nil {
		t.Fatal(err)
	}
	if versions := carrier["versions"].([]map[string]any); len(versions) != 0 {
		t.Errorf("global resource decoration exposed unpublished project versions: %v", versions)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
	for _, path := range exportPaths {
		t.Run("export "+path, func(t *testing.T) {
			f.require(t, "", http.MethodGet, path, nil, http.StatusForbidden)
			f.require(t, f.denied, http.MethodGet, path, nil, http.StatusForbidden)
			for _, token := range []string{f.editor, f.reviewer} {
				raw := f.require(t, token, http.MethodGet, path, nil, http.StatusOK)
				if strings.HasSuffix(path, "private.txt") && !bytes.Equal(raw, []byte("Private export content")) {
					t.Fatalf("private export content was lost: %s", raw)
				}
			}
		})
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			f.require(t, "", http.MethodGet, path, nil, http.StatusNotFound)
			// Submitting a project does not grant content management or preview.
			f.require(t, f.denied, http.MethodGet, path, nil, http.StatusNotFound)
			f.require(t, f.editor, http.MethodGet, path, nil, http.StatusOK)
		})
	}
	for _, path := range paths[:len(paths)-1] {
		f.require(t, f.reviewer, http.MethodGet, path, nil, http.StatusOK)
	}
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
	}
	for _, path := range exportPaths {
		f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
	}
}

func oct02CInsertPublishedExportRevision(t *testing.T, f test013Fixture, versionPublicID string) string {
	t.Helper()
	packageID, jobID, revisionID := newExportID(), newExportID(), newExportID()
	var versionID, fileID int64
	if err := f.db.QueryRow(f.ctx, `select id from mod_content_versions where public_id=$1`, versionPublicID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(object_key,bucket,endpoint,original_name,content_type,size_bytes,uploader_id,status,scan_status)
		values($1,'synthetic','https://storage.invalid','export.zip','application/zip',16,$2,'active','clean') returning id`, "oct02-c-export-"+packageID+".zip", f.userIDs[f.editor]).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into catalog_import_packages(id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,uploaded_by)
		values($1,$2,$3,'export.zip','mcmods-export/v1','test','1.21.1','neoforge','{}',$4)`, packageID, fmt.Sprintf("%x", sha256.Sum256([]byte(packageID))), fileID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status,created_by)
		values($1,$2,$3,$4,'test','ready',$5)`, jobID, f.modID, packageID, versionID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active)
		values($1,$2,$3,$4,$5,1,'ready','1.21.1','neoforge','test','test013',true)`, revisionID, f.modID, packageID, jobID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length,text_content)
		values($1,'private.txt','text','text/plain',$2,22,'Private export content')`, revisionID, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	return revisionID
}

func TestOCT02CResourceReferenceResolutionOmitsUnpublishedOtherProjectsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Referenced project")
	section := f.section(t, version.PublicID, f.builtin(t, "advancement"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:reference", "minecraft.advancement", "advancement", map[string]any{})
	f.review(t, resource, f.editor, "approved")
	sourceRevision := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	otherVersion := f.version(t, f.otherEditor, "test013-other", "Public referencing project")
	otherFixture := f
	otherFixture.modID = f.otherModID
	preferredRevision := oct02CInsertPublishedExportRevision(t, otherFixture, otherVersion.PublicID)
	server := &Server{db: f.db}
	key := exportResourceKey{RevisionID: preferredRevision, ResourceID: "test013:reference"}
	manualKey := exportResourceKey{ResourceID: key.ResourceID}
	for _, status := range []string{"approved", "pending", "approved"} {
		if _, err := f.db.Exec(f.ctx, `update mods set review_status=$2 where id=$1`, f.modID, status); err != nil {
			t.Fatal(err)
		}
		resolved, err := server.resolveManualModContentResources(f.ctx, []exportResourceKey{manualKey, key})
		if err != nil {
			t.Fatal(err)
		}
		want := status == "approved"
		for _, requested := range []exportResourceKey{manualKey, key} {
			if _, exists := resolved[requested]; exists != want {
				t.Errorf("manual reference status=%s resolved=%v want=%v", status, exists, want)
			}
		}
	}
	if _, err := f.db.Exec(f.ctx, `insert into resource_import_snapshots(id,resource_id,revision_id,registry,names)
		select $1,entity.id,$2,'advancements','{"en-US":"Private referenced name"}'::jsonb
		from catalog_entities entity where entity.public_id=$3`, newExportID(), sourceRevision, resource.PublicID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"approved", "pending", "approved"} {
		if _, err := f.db.Exec(f.ctx, `update mods set review_status=$2 where id=$1`, f.modID, status); err != nil {
			t.Fatal(err)
		}
		ownKey := exportResourceKey{RevisionID: sourceRevision, ResourceID: key.ResourceID}
		resolved, err := server.resolveExportResources(f.ctx, []exportResourceKey{key, ownKey})
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := resolved[key]; exists != (status == "approved") {
			t.Errorf("imported cross-project reference status=%s resolved=%v", status, exists)
		}
		if _, exists := resolved[ownKey]; exists != (status == "approved") {
			t.Errorf("unauthorized preferred revision status=%s resolved=%v", status, exists)
		}
		editorContext := context.WithValue(f.ctx, claimsContextKey, security.Claims{
			Subject: f.userIDs[f.editor], PermissionRules: []security.PermissionRule{{Code: "project.edit." + f.modCode, Allow: true}},
		})
		resolved, err = server.resolveExportResources(editorContext, []exportResourceKey{ownKey, key})
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := resolved[ownKey]; !exists {
			t.Errorf("scoped editor's own current-revision preview lost at status=%s", status)
		}
		if _, exists := resolved[key]; exists != (status == "approved") {
			t.Errorf("editor's unrelated public reference exposed pending target at status=%s", status)
		}
	}
}

func TestOCT02CExportJobDoesNotAcknowledgeMalformedCommittedReadIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Import acknowledgement")
	var fileID string
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(object_key,bucket,endpoint,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status,source,category)
		values('oct02-c-job.zip','synthetic','https://storage.invalid','export.zip','application/zip',16,$1,$2,'active','clean','mcmods_exporter',$3)
		returning public_id`, strings.Repeat("d", 64), f.userIDs[f.editor], ossModImportCategory(f.modCode, "mcmods-exporter", "packages")).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	// This test's isolated DB injects an unreadable response shape after a
	// successful task creation; it performs no archive fetch or object upload.
	if _, err := f.db.Exec(f.ctx, `create function oct02_c_break_job_read() returns trigger language plpgsql as $$
		begin new.error_detail='[]'::jsonb; return new; end $$;
		create trigger oct02_c_break_job_read before insert on catalog_import_jobs
		for each row execute function oct02_c_break_job_read()`); err != nil {
		t.Fatal(err)
	}
	request := createModExportJobRequest{OSSFileID: fileID, TargetVersionPublicID: version.PublicID}
	f.require(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/export-imports", request, http.StatusInternalServerError)
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from catalog_import_jobs where mod_id=$1`, f.modID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed import was lost: count=%d err=%v", count, err)
	}
	if _, err := f.db.Exec(f.ctx, `drop trigger oct02_c_break_job_read on catalog_import_jobs;
		drop function oct02_c_break_job_read(); update catalog_import_jobs set error_detail='{}'::jsonb`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/mods/test013-mod/export-imports", request, http.StatusAccepted)
	if err := f.db.QueryRow(f.ctx, `select count(*) from catalog_import_jobs where mod_id=$1`, f.modID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry duplicated committed import: count=%d err=%v", count, err)
	}
}

func TestOCT02CResourceDecorationsDiscardUnresolvedImportedPresentationIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Trusted reference decoration")
	section := f.section(t, version.PublicID, f.builtin(t, "advancement"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:reference", "minecraft.advancement", "advancement", map[string]any{})
	f.review(t, resource, f.editor, "approved")
	revisionID := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	server := &Server{db: f.db}
	const missing = "missing:unresolved"
	const forgedURL = "javascript:untrusted-import"
	for _, kind := range []string{"canonical", "loot", "enchantment"} {
		t.Run(kind, func(t *testing.T) {
			data := map[string]any{
				"parentId": missing, "possible_item_ids": []any{missing}, "compatible_enchantments": []any{missing},
				"resourceSources": map[string]any{missing: map[string]any{"detailUrl": forgedURL, "names": map[string]any{"en-US": "forged imported name"}}},
			}
			items := []map[string]any{{"data": data}}
			var err error
			switch kind {
			case "canonical":
				err = server.decorateCanonicalDefinitionReferences(f.ctx, revisionID, items)
			case "loot":
				err = server.decorateLootTableResources(f.ctx, revisionID, items)
			case "enchantment":
				err = server.decorateCompatibleEnchantmentResources(f.ctx, revisionID, items)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(forgedURL)) || bytes.Contains(raw, []byte("forged imported name")) {
				t.Fatalf("unresolved imported presentation survived %s decoration: %s", kind, raw)
			}
			if data["parentId"] != missing {
				t.Fatal("unresolved canonical identity was removed")
			}
		})
	}
	// Several decorators run sequentially on the same export response. Results
	// genuinely resolved during this request must survive later decoration.
	data := map[string]any{"parentId": "test013:reference", "loot_table": missing}
	items := []map[string]any{{"data": data}}
	if err := server.decorateCanonicalDefinitionReferences(f.ctx, revisionID, items); err != nil {
		t.Fatal(err)
	}
	if err := server.decorateLootTableReferences(f.ctx, revisionID, items); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	wantURL := "/mods/test013-mod/resources/" + resource.PublicID + "?version=" + version.PublicID
	if !bytes.Contains(raw, []byte(wantURL)) {
		t.Fatalf("later decoration discarded resolved reference: %s", raw)
	}
}

func TestOCT02CManualReferenceCannotSelectAnotherProjectsVersionIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Published binding")
	section := f.section(t, version.PublicID, f.builtin(t, "advancement"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:shared_reference", "minecraft.advancement", "advancement", map[string]any{})
	f.review(t, resource, f.editor, "approved")
	otherVersion := f.version(t, f.otherEditor, "test013-other", "Private observation")
	// One canonical resource can have observations in multiple imported Mod
	// versions while its manual binding still identifies the original owner.
	if _, err := f.db.Exec(f.ctx, `insert into mod_resource_version_details(resource_id,version_id,definition,status)
		select entity.id,version.id,'{}'::jsonb,'active' from catalog_entities entity,mod_content_versions version
		where entity.public_id=$1 and version.public_id=$2`, resource.PublicID, otherVersion.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into mod_resource_version_detail_localizations(resource_id,version_id,locale,name)
		select entity.id,version.id,'en-US','Private cross-project observation' from catalog_entities entity,mod_content_versions version
		where entity.public_id=$1 and version.public_id=$2`, resource.PublicID, otherVersion.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.otherModID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update mod_content_versions set updated_at=now()+interval '1 hour' where public_id=$1`, otherVersion.PublicID); err != nil {
		t.Fatal(err)
	}
	key := exportResourceKey{ResourceID: "test013:shared_reference"}
	resolved, err := (&Server{db: f.db}).resolveManualModContentResources(f.ctx, []exportResourceKey{key})
	if err != nil {
		t.Fatal(err)
	}
	source, exists := resolved[key]
	if !exists || source.VersionPublicID != version.PublicID || source.Names["en-US"] == "Private cross-project observation" {
		t.Fatalf("manual reference crossed its owner binding: source=%+v exists=%t", source, exists)
	}
}
