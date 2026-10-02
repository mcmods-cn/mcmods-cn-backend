package httpapi

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

type templatePromotionQueryCounter struct {
	promotionReads atomic.Int64
	slotReads      atomic.Int64
}

func (counter *templatePromotionQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	query := strings.ToLower(data.SQL)
	if strings.Contains(query, "from recipe_template_import_snapshots") {
		counter.promotionReads.Add(1)
	}
	if strings.Contains(query, "recipe_template_import_slots") {
		counter.slotReads.Add(1)
	}
	return ctx
}

func (*templatePromotionQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestImportedRecipeTemplatePromotionReadsStayConstantAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify imported recipe template promotion query count")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	counter := &templatePromotionQueryCounter{}
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
	if _, err = pool.Exec(ctx, `create temp table catalog_import_revisions(id text primary key,is_active boolean not null,status text not null);
		create temp table recipe_types(entity_id bigint primary key,canonical_id text not null);
		create temp table recipe_template_import_snapshots(
			id text primary key,recipe_type_snapshot_id text not null,revision_id text not null,recipe_type_id bigint not null,
			source_template_id text not null,schema_version text not null,template_collection_path text not null,
			background_path text not null,background_contains_ingredients boolean not null,coordinate_space text not null,
			image_scale integer not null,canvas jsonb not null,image_pixels jsonb not null,content_rect jsonb not null);
		create temp table recipe_template_import_slots(
			id text primary key,template_id text not null,source_slot_id text not null,role text not null,jei_role text not null,
			output_index integer,ordinal integer not null,coordinates_available boolean not null,rect jsonb not null,visual_rect jsonb not null);
		insert into recipe_types values(1,'minecraft:crafting')`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `insert into catalog_import_revisions values('promotion-empty',true,'ready');
		insert into recipe_template_import_snapshots(
			id,recipe_type_snapshot_id,revision_id,recipe_type_id,source_template_id,schema_version,template_collection_path,
			background_path,background_contains_ingredients,coordinate_space,image_scale,canvas,image_pixels,content_rect)
		values('promotion-empty-snapshot','promotion-empty-type','promotion-empty',1,'empty-template',
			'mcmods-jei-layout-template/v2','recipes/jei/templates/empty.json','recipes/jei/backgrounds/empty.png',false,
			'logical_pixels',1,'{}','{}','{}')`); err != nil {
		t.Fatal(err)
	}
	counter.promotionReads.Store(0)
	counter.slotReads.Store(0)
	emptyPromotions, err := loadImportedRecipeTemplatePromotions(ctx, tx, "promotion-empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyPromotions) != 1 || len(emptyPromotions[0].Slots) != 0 ||
		counter.promotionReads.Load() != 1 || counter.slotReads.Load() != 1 {
		t.Fatalf("zero-slot template was not preserved by one joined read: promotions=%#v reads=%d/%d",
			emptyPromotions, counter.promotionReads.Load(), counter.slotReads.Load())
	}

	for _, templates := range []int{1, 64, 512} {
		revisionID := "promotion-scale-" + time.Now().UTC().Format("150405.000000000") + "-" + strconv.Itoa(templates)
		if _, err = tx.Exec(ctx, `insert into catalog_import_revisions values($1,true,'ready')`, revisionID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `insert into recipe_template_import_snapshots(
			id,recipe_type_snapshot_id,revision_id,recipe_type_id,source_template_id,schema_version,template_collection_path,
			background_path,background_contains_ingredients,coordinate_space,image_scale,canvas,image_pixels,content_rect)
			select $1 || '-snapshot-' || lpad(value::text,4,'0'),$1 || '-type',$1,1,
				'template-' || lpad(value::text,4,'0'),'mcmods-jei-layout-template/v2',
				'recipes/jei/templates/' || value || '.json','recipes/jei/backgrounds/' || value || '.png',false,
				'logical_pixels',1,'{"x":0,"y":0,"width":176,"height":88}',
				'{"width":176,"height":88}','{"x":0,"y":0,"width":176,"height":88}'
			from generate_series(1,$2) value`, revisionID, templates); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `insert into recipe_template_import_slots(
			id,template_id,source_slot_id,role,jei_role,output_index,ordinal,coordinates_available,rect,visual_rect)
			select snapshot.id || '-slot-0',snapshot.id,'slot-0','input','',null,0,true,
				'{"x":0,"y":0,"width":16,"height":16}','{"x":0,"y":0,"width":16,"height":16}'
			from recipe_template_import_snapshots snapshot where snapshot.revision_id=$1`, revisionID); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `insert into recipe_template_import_slots(
			id,template_id,source_slot_id,role,jei_role,output_index,ordinal,coordinates_available,rect,visual_rect)
			select snapshot.id || '-slot-1',snapshot.id,'slot-1','output','',0,1,true,
				'{"x":20,"y":0,"width":16,"height":16}','{"x":20,"y":0,"width":16,"height":16}'
			from recipe_template_import_snapshots snapshot where snapshot.revision_id=$1
			order by snapshot.source_template_id desc limit 1`, revisionID); err != nil {
			t.Fatal(err)
		}

		counter.promotionReads.Store(0)
		counter.slotReads.Store(0)
		promotions, loadErr := loadImportedRecipeTemplatePromotions(ctx, tx, revisionID)
		if loadErr != nil {
			t.Fatalf("load %d templates: %v", templates, loadErr)
		}
		if len(promotions) != templates {
			t.Fatalf("revision with %d templates returned %d", templates, len(promotions))
		}
		for index := range promotions {
			wantSlots := 1
			if index == len(promotions)-1 {
				wantSlots = 2
			}
			if len(promotions[index].Slots) != wantSlots {
				t.Fatalf("template %d/%d slots=%d want=%d", index+1, templates, len(promotions[index].Slots), wantSlots)
			}
		}
		if promotions[len(promotions)-1].Slots[1].SlotID != "slot-1" {
			t.Fatalf("template/slot ordering changed at %d templates", templates)
		}
		if outputIndex := promotions[len(promotions)-1].Slots[1].OutputIndex; outputIndex == nil || *outputIndex != 0 {
			t.Fatalf("template output index changed at %d templates: %v", templates, outputIndex)
		}
		if counter.promotionReads.Load() != 1 || counter.slotReads.Load() != 1 {
			t.Fatalf("%d templates used promotion_reads=%d slot_reads=%d, want one joined read",
				templates, counter.promotionReads.Load(), counter.slotReads.Load())
		}
	}
}
