package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func catalogReadFenceFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (int64, int64, string, string, int64) {
	t.Helper()
	var mod, version, user int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('catalog-fence-'||gen_random_uuid()::text,gen_random_uuid()::text||'@example.invalid','synthetic') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values(new_public_id(),'catalog-fence-'||gen_random_uuid()::text,'Synthetic pending import','pending',$1) returning id`, user).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id) values($1) returning id`, mod).Scan(&version); err != nil {
		t.Fatal(err)
	}
	packageID, job, revision := newExportID(), newExportID(), newExportID()
	if _, err := pool.Exec(ctx, `insert into catalog_import_packages(id,sha256,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest) values($1,$2,'synthetic.zip','1','synthetic','1.20.1','forge','{}')`, packageID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into catalog_import_jobs(id,mod_id,package_id,target_version_id,importer_version,status,run_token,heartbeat_at) values($1,$2,$3,$4,'synthetic','importing','current',now())`, job, mod, packageID, version); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,status,minecraft_version,loader,exporter_version,source_namespace,is_active,import_run_token) values($1,$2,$3,$4,$5,1,'ready','1.20.1','forge','synthetic','synthetic',true,'current')`, revision, mod, packageID, job, version); err != nil {
		t.Fatal(err)
	}
	return mod, version, job, revision, user
}

func TestCatalogPublicRevisionRequiresApprovalAndOwnerPreviewIsPrivateIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	mod, _, _, revision, user := catalogReadFenceFixture(t, ctx, pool)
	server := &Server{db: pool, cfg: cfg}
	request := httptest.NewRequest(http.MethodGet, "/revision", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	if server.canReadModExportRevision(response, request, revision) || response.Code != 403 {
		t.Fatalf("public pending allowed: %d", response.Code)
	}
	owner := request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: user, PermissionRules: []security.PermissionRule{{Code: "project.edit", Allow: true}}}))
	response = httptest.NewRecorder()
	if !server.canReadModExportRevision(response, owner, revision) || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("owner preview inaccessible or public-cacheable: %d %s", response.Code, response.Header().Get("Cache-Control"))
	}
	if _, err := pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, mod); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	if !server.canReadModExportRevision(response, request, revision) || response.Header().Get("Cache-Control") != "" {
		t.Fatal("approved revision unavailable")
	}
	if _, err := pool.Exec(ctx, `update catalog_import_revisions set status='rejected' where id=$1`, revision); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	if server.canReadModExportRevision(response, request, revision) {
		t.Fatal("rejected active flag was public")
	}
}

func TestModDetailHandlesAuthorWithoutRoleIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	mod, _, _, _, user := catalogReadFenceFixture(t, ctx, pool)
	var creator int64
	if err := pool.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status) values('author','Synthetic no-role author','synthetic no-role author','approved') returning id`).Scan(&creator); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_snapshot,status) values('mod',$1,$2,'Contributor','approved')`, mod, creator); err != nil {
		t.Fatal(err)
	}
	result, err := (&Server{db: pool, cfg: cfg}).modByID(ctx, mod, user)
	if err != nil || len(result.Authors) != 1 || result.Authors[0].RoleID != nil || result.Authors[0].Role != "Contributor" {
		t.Fatalf("nullable role detail=%+v err=%v", result.Authors, err)
	}
}

func TestCatalogLateImporterCannotDeleteCurrentStagingIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	mod, version, job, _, _ := catalogReadFenceFixture(t, ctx, pool)
	var packageID string
	if err := pool.QueryRow(ctx, `select package_id from catalog_import_jobs where id=$1`, job).Scan(&packageID); err != nil {
		t.Fatal(err)
	}
	staged := newExportID()
	if _, err := pool.Exec(ctx, `insert into catalog_import_revisions(id,mod_id,package_id,job_id,target_version_id,revision_no,minecraft_version,loader,exporter_version,source_namespace,import_run_token) values($1,$2,$3,$4,$5,2,'1.20.1','forge','synthetic','synthetic','current')`, staged, mod, packageID, job, version); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: cfg}
	err := server.runModExportTransaction(withModExportLease(ctx, job, "expired-old-token"), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `delete from catalog_import_revisions where job_id=$1 and status='staging'`, job)
		return err
	})
	if !errors.Is(err, errModExportLeaseLost) {
		t.Fatalf("old attempt entered write transaction: %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `select count(*) from catalog_import_revisions where id=$1`, staged).Scan(&count); err != nil || count != 1 {
		t.Fatalf("new staging lost: %d %v", count, err)
	}
	if err = server.runModExportTransaction(withModExportLease(ctx, job, "current"), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update catalog_import_packages set manifest='{"newAttempt":true}' where id=$1`, packageID)
		return err
	}); err != nil {
		t.Fatalf("current fenced write: %v", err)
	}
	if _, err = pool.Exec(ctx, `update catalog_import_jobs set status='cancelled' where id=$1`, job); err != nil {
		t.Fatal(err)
	}
	err = server.runModExportTransaction(withModExportLease(ctx, job, "current"), func(tx pgx.Tx) error { t.Fatal("cancelled importer reached action"); return nil })
	if !errors.Is(err, errModExportLeaseLost) {
		t.Fatalf("cancelled write: %v", err)
	}
}

func TestProjectIconDatabaseFailureIsNotReportedAsMissing(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	// A separate closed pool makes a deterministic read failure without touching
	// the shared test service or dropping any schema objects.
	closed, err := pgxpool.New(ctx, pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	request := httptest.NewRequest(http.MethodGet, "/icon", nil).WithContext(ctx)
	request.SetPathValue("siteId", "example")
	response := httptest.NewRecorder()
	(&Server{db: closed, cfg: cfg}).publicProjectIcon(response, request, "mod")
	if response.Code != 500 {
		t.Fatalf("DB error hidden as missing: %d", response.Code)
	}
}
