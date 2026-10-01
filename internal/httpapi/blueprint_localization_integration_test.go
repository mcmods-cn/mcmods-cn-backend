package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBlueprintLocalizationPreservesHumanWorkIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to an isolated migrated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	unique := "apia_blueprint_" + randomHex(8)
	var ownerID, blueprintID int64
	var publicID string
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'fixture') returning id`, unique, unique+"@example.test").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format) values($1,'Original','nbt') returning id,public_id`, ownerID).Scan(&blueprintID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,editable,review_status,revision_no)
		values('blueprint',$1,'en-US','AI original','ai',false,'approved',4),('blueprint',$1,'ja-JP','Human original','human',true,'approved',3)`, blueprintID); err != nil {
		t.Fatal(err)
	}
	snapshot := blueprintContentSnapshot{PublicID: publicID, Title: "Edited", Description: "Edited body", DefaultLocale: "en-US", ReplaceLocalizations: true,
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Edited", ContentMarkdown: "Edited body"}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{EntityType: "blueprint", EntityID: blueprintID,
		AggregateType: "blueprint", AggregateKey: publicID, Snapshot: raw, ActorID: ownerID, Source: "blueprint_metadata", Status: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if err = applyBlueprintContentSnapshotTx(ctx, tx, blueprintID, created.RevisionID, ownerID, snapshot); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(ctx, `select count(*) from content_localizations where subject_type='blueprint' and subject_id=$1`, blueprintID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("language edits deleted an omitted human localization: count=%d", count)
	}
	var provenance string
	var revision int64
	if err = tx.QueryRow(ctx, `select provenance,revision_no from content_localizations where subject_type='blueprint' and subject_id=$1 and locale='en-US'`, blueprintID).Scan(&provenance, &revision); err != nil {
		t.Fatal(err)
	}
	if provenance != "human_corrected" || revision != 5 {
		t.Fatalf("manual correction reset provenance=%s revision=%d", provenance, revision)
	}
}
