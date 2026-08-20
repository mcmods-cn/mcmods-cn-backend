package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestApplyModSnapshotPublishesNonCatalogLocalizationsIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the mod localization integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	suffix := time.Now().UnixNano() % 1_000_000
	projectCode := fmt.Sprintf("mlt%06d", suffix)
	slug := fmt.Sprintf("localized-mod-%06d", suffix)
	var modID int64
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,$2,'Localized mod fixture','approved') returning id`, projectCode, slug).Scan(&modID); err != nil {
		t.Fatal(err)
	}

	snapshot := createModRequest{
		SiteID:           slug,
		PrimaryName:      "Localized mod fixture",
		DefaultLocale:    "zh-CN",
		Environment:      "bothRequired",
		PrimaryCategory:  "utility",
		OfficialStatus:   "development",
		SourceStatus:     "unknown",
		License:          "Custom",
		SubmissionMethod: "manual",
		Localizations: []catalogLocalizationEdit{{
			Locale:          "zh-CN",
			Name:            "本地化模组",
			Summary:         "回归测试简介",
			ContentMarkdown: "回归测试正文",
		}},
	}
	if err = normalizeAndValidateModRequest(&snapshot); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType:    "mod",
		EntityID:      modID,
		AggregateType: "mod",
		AggregateKey:  projectCode,
		Snapshot:      raw,
		Status:        "approved",
		Source:        "manual",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = applyModSnapshot(ctx, tx, modID, revision.RevisionID, 0, false, false, snapshot); err != nil {
		t.Fatalf("publish localized mod snapshot: %v", err)
	}

	var catalogEntityID *int64
	var name, summary, markdown string
	if err = tx.QueryRow(ctx, `select catalog_entity_id,name,summary,content_markdown
		from content_localizations where subject_type='mod' and subject_id=$1 and locale='zh-CN'`, modID).
		Scan(&catalogEntityID, &name, &summary, &markdown); err != nil {
		t.Fatal(err)
	}
	if catalogEntityID != nil {
		t.Fatalf("mod localization must not reference catalog_entities, got %d", *catalogEntityID)
	}
	if name != "本地化模组" || summary != "回归测试简介" || markdown != "回归测试正文" {
		t.Fatalf("unexpected published localization: name=%q summary=%q markdown=%q", name, summary, markdown)
	}
}
