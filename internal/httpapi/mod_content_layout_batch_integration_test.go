package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type modContentLayoutPublishQueryCounter struct {
	active atomic.Bool
	count  atomic.Int64
}

func (counter *modContentLayoutPublishQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if counter.active.Load() {
		counter.count.Add(1)
	}
	return ctx
}

func (*modContentLayoutPublishQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestPublishModContentLayoutUsesConstantQueriesAtCategoryScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify batched mod-content layout publication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	counter := &modContentLayoutPublishQueryCounter{}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	const categoryCount = maxModContentCategories
	const existingCategoryCount = categoryCount / 2
	var actorID, modID, versionID, templateID, rootID, revisionID int64
	var versionPublicID, rootPublicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('layout_batch_actor','layout-batch@example.invalid','test-only',true) returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('laybatch1','layout-batch','Layout batch','approved',$1) returning id`, actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status,created_by,updated_by)
		values($1,'Layout batch','active',$2,$2) returning id,public_id`, modID, actorID).Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from mod_content_templates where builtin and status='active' order by id limit 1`).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(
		mod_id,version_id,template_id,display_mode,ordinal,status,created_by,updated_by)
		values($1,$2,$3,'compact',0,'active',$4,$4) returning id,public_id`, modID, versionID, templateID, actorID).
		Scan(&rootID, &rootPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_sections(
		public_id,mod_id,version_id,template_id,parent_id,system_key,default_locale,display_mode,ordinal,status,created_by,updated_by)
		select 'lay'||lpad(value::text,6,'0'),$1,$2,$3,$4,'keep_'||value,'en-US','compact',value::int-1,'active',$5,$5
		from generate_series(1,$6) value`, modID, versionID, templateID, rootID, actorID, existingCategoryCount); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name,description)
		select section.id,locale.code,'Old '||section.public_id||' '||locale.code,''
		from mod_content_sections section cross join (values('en-US'),('zh-CN')) locale(code)
		where section.parent_id=$1`, rootID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into content_revisions(
		entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source)
		select route.entity_type,route.internal_id,'mod_content_section',$1,1,'{}'::jsonb,'layout-batch',$2,'test'
		from public_routes route where route.public_id=$1 and route.entity_type='mod_content_section' returning id`,
		rootPublicID, actorID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}

	layout := modContentLayoutEdit{
		VersionPublicID:     versionPublicID,
		RootSectionPublicID: rootPublicID,
		DisplayMode:         "compact",
		Categories:          make([]modContentLayoutCategoryEdit, 0, categoryCount),
	}
	for index := 1; index <= categoryCount; index++ {
		publicID := fmt.Sprintf("lay%06d", index)
		layout.Categories = append(layout.Categories, modContentLayoutCategoryEdit{
			PublicID:       publicID,
			ParentPublicID: rootPublicID,
			DefaultLocale:  "en-US",
			Ordinal:        index - 1,
			Localizations: []catalogLocalizationEdit{
				{Locale: "en-US", Name: "Category " + publicID, Summary: "English"},
				{Locale: "zh-CN", Name: "分类 " + publicID, Summary: "中文"},
			},
		})
	}
	if err = validateModContentCategoryTree(rootPublicID, layout.Categories); err != nil {
		t.Fatal(err)
	}
	prepareEdit := modContentLayoutEdit{
		VersionPublicID:     versionPublicID,
		RootSectionPublicID: rootPublicID,
		DisplayMode:         "compact",
		Categories:          make([]modContentLayoutCategoryEdit, maxModContentCategories),
	}
	for index := range prepareEdit.Categories {
		prepareEdit.Categories[index] = modContentLayoutCategoryEdit{
			PublicID:       fmt.Sprintf("temporary-category-%04d", index),
			ParentPublicID: rootPublicID,
			DefaultLocale:  "en-US",
			Ordinal:        index,
		}
	}
	counter.count.Store(0)
	counter.active.Store(true)
	err = (&Server{db: pool}).prepareModContentLayout(ctx, modID, versionID, &prepareEdit)
	counter.active.Store(false)
	if err != nil {
		t.Fatal(err)
	}
	if counter.count.Load() != 4 {
		t.Fatalf("preparing %d temporary categories used %d SQL statements; want 4", len(prepareEdit.Categories), counter.count.Load())
	}
	for index, category := range prepareEdit.Categories {
		if !modContentPublicIDPattern.MatchString(category.PublicID) || category.ParentPublicID != rootPublicID || category.Ordinal != index {
			t.Fatalf("prepared category %d id/parent/ordinal=%q/%q/%d", index, category.PublicID, category.ParentPublicID, category.Ordinal)
		}
	}
	counter.count.Store(0)
	counter.active.Store(true)
	generatedIDs, err := generateModContentPublicIDs(ctx, pool, maxModContentCategories+maxModContentResources/2)
	counter.active.Store(false)
	if err != nil {
		t.Fatal(err)
	}
	generatedSet := make(map[string]struct{}, len(generatedIDs))
	for _, publicID := range generatedIDs {
		if !modContentPublicIDPattern.MatchString(publicID) {
			t.Fatalf("generated invalid public ID %q", publicID)
		}
		generatedSet[publicID] = struct{}{}
	}
	if counter.count.Load() != 2 || len(generatedIDs) != 11000 || len(generatedSet) != len(generatedIDs) {
		t.Fatalf("maximum temporary IDs used queries=%d generated=%d unique=%d; want 2/11000/11000",
			counter.count.Load(), len(generatedIDs), len(generatedSet))
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counter.count.Store(0)
	counter.active.Store(true)
	publishStarted := time.Now()
	err = publishModContentLayoutTx(ctx, tx, revisionID, modContentSnapshot{
		Kind: "layout", Operation: "edit", ModID: modID, PublicID: rootPublicID, Layout: &layout,
	}, actorID)
	publishDuration := time.Since(publishStarted)
	counter.active.Store(false)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := counter.count.Load(); got > 15 {
		t.Fatalf("publishing %d categories used %d SQL statements; want at most 15", categoryCount, got)
	}
	t.Logf("published %d categories with %d SQL statements in %s", categoryCount, counter.count.Load(), publishDuration)

	var activeCategories, localizations, preservedSystemKeys, stagingKeys, ordinalMismatches int
	if err = pool.QueryRow(ctx, `select
		count(*) filter(where section.status='active'),
		count(*) filter(where section.system_key like 'keep_%'),
		count(*) filter(where section.system_key like '__layout_staging_%'),
		count(*) filter(where section.ordinal<>(substring(section.public_id from 4)::int-1))
		from mod_content_sections section where section.parent_id=$1`, rootID).
		Scan(&activeCategories, &preservedSystemKeys, &stagingKeys, &ordinalMismatches); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from mod_content_section_localizations localization
		join mod_content_sections section on section.id=localization.section_id where section.parent_id=$1`, rootID).Scan(&localizations); err != nil {
		t.Fatal(err)
	}
	if activeCategories != categoryCount || localizations != categoryCount*2 ||
		preservedSystemKeys != existingCategoryCount || stagingKeys != 0 || ordinalMismatches != 0 {
		t.Fatalf("active=%d localizations=%d systemKeys=%d staging=%d ordinalMismatches=%d",
			activeCategories, localizations, preservedSystemKeys, stagingKeys, ordinalMismatches)
	}
	var wrongLocalizationCount int
	if err = pool.QueryRow(ctx, `select count(*) from mod_content_section_localizations localization
		join mod_content_sections section on section.id=localization.section_id
		where section.parent_id=$1 and (localization.name like 'Old %' or localization.description not in ('English','中文'))`,
		rootID).Scan(&wrongLocalizationCount); err != nil {
		t.Fatal(err)
	}
	if wrongLocalizationCount != 0 {
		t.Fatalf("%d localization rows retained stale or incorrect values", wrongLocalizationCount)
	}
	if strings.TrimSpace(rootPublicID) == "" {
		t.Fatal("root public ID was empty")
	}
}
