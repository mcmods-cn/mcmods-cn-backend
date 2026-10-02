package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSectionPublicationCannotMoveAcrossVersionsOrReplayLegacyEditIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify section publication authority")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `
		create temporary table mod_content_versions(
			id bigint primary key,public_id text not null,mod_id bigint not null,status text not null
		) on commit drop;
		create temporary table mod_content_templates(
			id bigint primary key,public_id text not null,status text not null,builtin boolean not null,
			owner_mod_id bigint,code text not null
		) on commit drop;
		create temporary table mod_content_sections(
			id bigint primary key,public_id text not null,mod_id bigint not null,version_id bigint not null,
			template_id bigint not null,parent_id bigint,default_locale text not null,display_mode text not null,
			ordinal integer not null,status text not null,published_revision_id bigint,updated_by bigint,updated_at timestamptz
		) on commit drop;
		create temporary table mod_content_section_localizations(
			section_id bigint,locale text,name text,description text
		) on commit drop;
		insert into mod_content_versions values(1,'version01',7,'active'),(2,'version02',7,'active');
		insert into mod_content_templates values(3,'template01','active',true,null,'custom');
		insert into mod_content_sections values
			(10,'legacy-edit',7,1,3,null,'en-US','compact',0,'active',10,5,now()),
			(11,'mismatched-create',7,1,3,null,'en-US','compact',0,'pending',null,5,now()),
			(12,'healthy-create',7,1,3,null,'en-US','compact',0,'pending',null,5,now())
	`); err != nil {
		t.Fatal(err)
	}

	legacyEdit := modContentSnapshot{Kind: "section", Operation: "edit", ModID: 7, PublicID: "legacy-edit", Section: &modContentSectionEdit{
		VersionPublicID: "version02", TemplatePublicID: "template01", DefaultLocale: "en-US", DisplayMode: "compact",
	}}
	if err = publishModContentSnapshotTx(ctx, tx, 20, legacyEdit, 5); err == nil {
		t.Fatal("legacy section edit snapshot was published")
	}
	mismatchedCreate := modContentSnapshot{Kind: "section", Operation: "create", ModID: 7, PublicID: "mismatched-create", Section: &modContentSectionEdit{
		VersionPublicID: "version02", TemplatePublicID: "template01", DefaultLocale: "en-US", DisplayMode: "compact",
	}}
	if err = publishModContentSnapshotTx(ctx, tx, 21, mismatchedCreate, 5); err == nil {
		t.Fatal("reserved root was moved to another version during publication")
	}

	for _, publicID := range []string{"legacy-edit", "mismatched-create"} {
		var versionID int64
		if err = tx.QueryRow(ctx, `select version_id from mod_content_sections where public_id=$1`, publicID).Scan(&versionID); err != nil {
			t.Fatal(err)
		}
		if versionID != 1 {
			t.Fatalf("%s moved to version %d", publicID, versionID)
		}
	}

	healthyCreate := modContentSnapshot{Kind: "section", Operation: "create", ModID: 7, PublicID: "healthy-create", Section: &modContentSectionEdit{
		VersionPublicID: "version01", TemplatePublicID: "template01", DefaultLocale: "en-US", DisplayMode: "compact",
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Healthy root", Summary: "Created empty"}},
	}}
	if err = publishModContentSnapshotTx(ctx, tx, 22, healthyCreate, 5); err != nil {
		t.Fatalf("healthy empty root publication failed: %v", err)
	}
	var versionID int64
	var status string
	var localizationCount int
	if err = tx.QueryRow(ctx, `select version_id,status,(select count(*)::int from mod_content_section_localizations where section_id=12)
		from mod_content_sections where id=12`).Scan(&versionID, &status, &localizationCount); err != nil {
		t.Fatal(err)
	}
	if versionID != 1 || status != "active" || localizationCount != 1 {
		t.Fatalf("healthy root state: version=%d status=%s localizations=%d", versionID, status, localizationCount)
	}
}
