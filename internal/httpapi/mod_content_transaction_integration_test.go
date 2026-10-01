package httpapi

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModContentRevisionUsesItsTransactionWithOneConnectionIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var actorID, modID int64
	var slug, projectCode string
	slug = "synthetic-single-connection"
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('synthetic-content-actor','synthetic-content-actor@example.test','fixture') returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values(new_public_id(),$1,'Synthetic content transaction','approved',$2) returning id,project_code`, slug, actorID).Scan(&modID, &projectCode); err != nil {
		t.Fatal(err)
	}
	parsed := pool.Config().Copy()
	parsed.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	server := &Server{db: one, cfg: cfg}
	work, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	tx, err := one.Begin(work)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var versionPublicID string
	if err = tx.QueryRow(work, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status,created_by,updated_by) values($1,'1.21.1 / NeoForge',array['1.21.1'],array['neoforge'],'pending',$2,$2) returning public_id`, modID, actorID).Scan(&versionPublicID); err != nil {
		t.Fatal(err)
	}
	claims := security.Claims{Subject: actorID, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true}}}
	request := httptest.NewRequest(http.MethodPost, "/content-versions", nil).WithContext(context.WithValue(work, claimsContextKey, claims))
	identity := modIdentityRecord{ID: modID, UniqueID: projectCode, SiteID: slug, SubmittedByID: &actorID, ReviewStatus: "approved"}
	edit := modContentVersionEdit{Label: "1.21.1 / NeoForge", MinecraftVersions: []string{"1.21.1"}, Loaders: []string{"neoforge"}}
	result, err := server.createModContentRevisionTx(request, tx, identity, modContentSnapshot{Kind: "version", Operation: "create", PublicID: versionPublicID, Version: &edit}, nil)
	if err != nil {
		t.Fatalf("transaction attempted to acquire another connection: %v", err)
	}
	if result.ReviewStatus != "approved" || result.RevisionID == "" {
		t.Fatalf("revision not actually published: %#v", result)
	}
	if err = tx.Commit(work); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err = one.QueryRow(ctx, `select status='active' and published_revision_id is not null from mod_content_versions where public_id=$1`, versionPublicID).Scan(&active); err != nil || !active {
		t.Fatalf("one-connection revision did not persist: %v %v", active, err)
	}
}
