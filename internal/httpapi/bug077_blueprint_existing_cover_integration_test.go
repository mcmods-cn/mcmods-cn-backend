package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBlueprintExistingCoverIsAuthorizedByBindingNotEditorIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify blueprint cover binding authorization")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temporary table oss_files (like public.oss_files including all);
		create temporary table blueprints (like public.blueprints including all);
		insert into oss_files(id,public_id,object_key,original_name,content_type,size_bytes,uploader_id,status,scan_status) values
			(770,'cover077a','blueprints/owner/cover-a.png','cover-a.png','image/png',128,700,'active','clean'),
			(771,'cover077b','blueprints/owner/cover-b.png','cover-b.png','image/png',128,700,'active','clean');
		insert into blueprints(id,public_id,owner_id,title,description_markdown,source_format,status,review_status,cover_file_id,cover_object_key)
			values(770,'bluep077a',700,'Original title','Original body','nbt','ready','approved',770,'blueprints/owner/cover-a.png')`); err != nil {
		t.Fatal(err)
	}

	apply := func(actorID int64, snapshot blueprintContentSnapshot) error {
		tx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			return beginErr
		}
		applyErr := applyBlueprintContentSnapshotTx(ctx, tx, 770, 900, actorID, snapshot)
		if applyErr != nil {
			_ = tx.Rollback(ctx)
			return applyErr
		}
		return tx.Commit(ctx)
	}

	if err = apply(701, blueprintContentSnapshot{
		PublicID: "bluep077a", Title: "Administrator metadata edit", Description: "Updated body",
		CoverFileID: "cover077a", CoverKey: "client/must/not/decide.png",
	}); err != nil {
		t.Fatalf("administrator metadata edit with the existing owner cover failed: %v", err)
	}
	assertBlueprintCover077(t, ctx, pool, "Administrator metadata edit", 770, "blueprints/owner/cover-a.png")

	if err = apply(701, blueprintContentSnapshot{
		PublicID: "bluep077a", Title: "Unauthorized replacement", Description: "Updated body",
		CoverFileID: "cover077b",
	}); err == nil {
		t.Fatal("a different uploader's unbound cover was accepted for the editor")
	}
	assertBlueprintCover077(t, ctx, pool, "Administrator metadata edit", 770, "blueprints/owner/cover-a.png")

	if err = apply(700, blueprintContentSnapshot{
		PublicID: "bluep077a", Title: "Owner replacement", Description: "Updated body",
		CoverFileID: "cover077b",
	}); err != nil {
		t.Fatalf("owner could not bind another trusted self-uploaded cover: %v", err)
	}
	assertBlueprintCover077(t, ctx, pool, "Owner replacement", 771, "blueprints/owner/cover-b.png")
}

func assertBlueprintCover077(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wantTitle string, wantFileID int64, wantKey string) {
	t.Helper()
	var title, objectKey string
	var fileID int64
	if err := pool.QueryRow(ctx, `select title,cover_file_id,cover_object_key from blueprints where id=770`).Scan(&title, &fileID, &objectKey); err != nil {
		t.Fatal(err)
	}
	if title != wantTitle || fileID != wantFileID || objectKey != wantKey {
		t.Fatalf("blueprint cover state = %q/%d/%q, want %q/%d/%q", title, fileID, objectKey, wantTitle, wantFileID, wantKey)
	}
}
