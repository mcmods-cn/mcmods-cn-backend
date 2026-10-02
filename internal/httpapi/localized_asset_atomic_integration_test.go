package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestLocalizedAssetSnapshotsCommitOrRollbackAsOneUnitIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify atomic localized asset updates against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `
		create temporary table skin_assets (like public.skin_assets including all);
		create temporary table skin_wardrobe (like public.skin_wardrobe including all);
		create temporary table player_profiles (like public.player_profiles including all);
		create temporary table player_profile_textures (like public.player_profile_textures including all);
		create temporary table blueprints (like public.blueprints including all);
		create temporary table content_subjects (like public.content_subjects including all);
		create temporary table content_localizations (like public.content_localizations including all);
		insert into skin_assets(id,public_id,owner_id,blob_hash,kind,model,display_name,description,tags,visibility,review_status,status)
			values(11,'skin00001',99,repeat('a',64),'skin','default','Old skin','Old skin description','{}','public','approved','active');
		insert into blueprints(id,public_id,owner_id,title,description_markdown,source_format,status,review_status)
			values(22,'bluep0001',99,'Old blueprint','Old blueprint description','litematic','ready','approved');
		insert into content_subjects(subject_type,subject_id,default_locale) values('skin',11,'en-US'),('blueprint',22,'en-US');
		insert into content_localizations(subject_type,subject_id,locale,name,summary,content_markdown,updated_by)
			values('skin',11,'en-US','Old skin','Old skin description','',99),
			('blueprint',22,'en-US','Old blueprint','','Old blueprint description',99)`); err != nil {
		t.Fatal(err)
	}

	commitLocalizedSkinSnapshot(t, ctx, pool, skinAssetContentSnapshot{
		PublicID: "skin00001", Model: "default", Name: "Unified skin", Description: "Unified description",
		Tags: []string{"atomic"}, Visibility: "public", DefaultLocale: "zh-CN", ReplaceLocalizations: true,
		Localizations: []catalogLocalizationEdit{{Locale: "zh-CN", Name: "统一皮肤", Summary: "统一介绍"}, {Locale: "en-US", Name: "Unified skin", Summary: "Unified description"}},
	}, false)
	commitLocalizedBlueprintSnapshot(t, ctx, pool, blueprintContentSnapshot{
		PublicID: "bluep0001", Title: "Unified blueprint", Description: "Unified body",
		DefaultLocale: "zh-CN", ReplaceLocalizations: true,
		Localizations: []catalogLocalizationEdit{{Locale: "zh-CN", Name: "统一蓝图", ContentMarkdown: "统一正文"}, {Locale: "en-US", Name: "Unified blueprint", ContentMarkdown: "Unified body"}},
	}, false)
	assertLocalizedAssetIntegrationState(t, ctx, pool, "skin", 11, "Unified skin", "zh-CN", 2)
	assertLocalizedAssetIntegrationState(t, ctx, pool, "blueprint", 22, "Unified blueprint", "zh-CN", 2)

	if _, err = pool.Exec(ctx, `alter table content_localizations add constraint force_localized_asset_failure
		check(name <> 'force failure') not valid`); err != nil {
		t.Fatal(err)
	}
	commitLocalizedSkinSnapshot(t, ctx, pool, skinAssetContentSnapshot{
		PublicID: "skin00001", Model: "slim", Name: "Must roll back", Description: "Must roll back",
		Tags: []string{"broken"}, Visibility: "private", DefaultLocale: "en-US", ReplaceLocalizations: true,
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "force failure"}},
	}, true)
	commitLocalizedBlueprintSnapshot(t, ctx, pool, blueprintContentSnapshot{
		PublicID: "bluep0001", Title: "Must roll back", Description: "Must roll back",
		DefaultLocale: "en-US", ReplaceLocalizations: true,
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "force failure"}},
	}, true)
	assertLocalizedAssetIntegrationState(t, ctx, pool, "skin", 11, "Unified skin", "zh-CN", 2)
	assertLocalizedAssetIntegrationState(t, ctx, pool, "blueprint", 22, "Unified blueprint", "zh-CN", 2)
}

func commitLocalizedSkinSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, snapshot skinAssetContentSnapshot, wantFailure bool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err == nil {
		err = applySkinAssetSnapshotTx(ctx, tx, 11, 99, 101, 99, snapshot)
	}
	if err == nil {
		err = tx.Commit(ctx)
	} else if tx != nil {
		_ = tx.Rollback(ctx)
	}
	if wantFailure == (err == nil) {
		t.Fatalf("skin snapshot failure=%v err=%v", wantFailure, err)
	}
}

func commitLocalizedBlueprintSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, snapshot blueprintContentSnapshot, wantFailure bool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err == nil {
		err = applyBlueprintContentSnapshotTx(ctx, tx, 22, 201, 99, snapshot)
	}
	if err == nil {
		err = tx.Commit(ctx)
	} else if tx != nil {
		_ = tx.Rollback(ctx)
	}
	if wantFailure == (err == nil) {
		t.Fatalf("blueprint snapshot failure=%v err=%v", wantFailure, err)
	}
}

func assertLocalizedAssetIntegrationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, kind string, id int64, wantName, wantLocale string, wantLocalizations int) {
	t.Helper()
	var name, locale string
	var count int
	if err := pool.QueryRow(ctx, `select case when $1='skin' then
		(select display_name from skin_assets where id=$2) else
		(select title from blueprints where id=$2) end`, kind, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select default_locale from content_subjects where subject_type=$1 and subject_id=$2`, kind, id).Scan(&locale); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from content_localizations where subject_type=$1 and subject_id=$2`, kind, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if name != wantName || locale != wantLocale || count != wantLocalizations {
		t.Fatalf("%s state=(%q,%q,%d), want=(%q,%q,%d)", kind, name, locale, count, wantName, wantLocale, wantLocalizations)
	}
}
