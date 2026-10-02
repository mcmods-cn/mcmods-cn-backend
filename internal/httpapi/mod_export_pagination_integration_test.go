package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestModExportPageCursorIsFilterBoundAndStrict(t *testing.T) {
	want := modExportPageCursor{RevisionID: "revision-a", Scope: "tags\x00items\x00gear", First: "items", Second: "example:gear"}
	raw := encodeModExportPageCursor(want)
	got, err := decodeModExportPageCursor(raw, want.RevisionID, want.Scope)
	if err != nil || got != want {
		t.Fatalf("cursor round trip=%+v err=%v, want %+v", got, err, want)
	}
	if _, err = decodeModExportPageCursor(raw, "revision-b", want.Scope); err == nil {
		t.Fatal("cursor was reusable across revisions")
	}
	if _, err = decodeModExportPageCursor(raw, want.RevisionID, "different-filter"); err == nil {
		t.Fatal("cursor was reusable across filter scopes")
	}
	if _, err = decodeModExportPageCursor("not-base64", want.RevisionID, want.Scope); err == nil {
		t.Fatal("malformed cursor was accepted")
	}
	if _, err = decodeModExportPageCursor(strings.Repeat("a", maxModExportCursorLength+1), want.RevisionID, want.Scope); err == nil {
		t.Fatal("oversized cursor was accepted")
	}
}

func TestModExportJSONDecodingRejectsMalformedOrWrongShapes(t *testing.T) {
	if _, err := decodeModExportJSONObjectValue([]byte(`{"ok":true}`), "fixture"); err != nil {
		t.Fatalf("valid object rejected: %v", err)
	}
	for _, raw := range []string{`{`, `null`, `[]`, `"scalar"`} {
		if _, err := decodeModExportJSONObjectValue([]byte(raw), "fixture"); err == nil {
			t.Fatalf("invalid object shape %q was accepted", raw)
		}
	}
	var values []map[string]any
	if err := decodeModExportJSON([]byte(`{}`), &values, "array fixture"); err == nil {
		t.Fatal("object decoded into an array without error")
	}
}

func TestModExportTagAndAssetPagesAreBoundedAndStableIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	createModExportPaginationTestTables(t, ctx, db)
	if _, err := db.Exec(ctx, `insert into catalog_entities(id,public_id) select value,'tag-'||value from generate_series(1,5) value;
		insert into catalog_tags(entity_id,registry,canonical_id) values
			(1,'items','example:a'),(2,'items','example:b'),(3,'items','example:c'),(4,'items','example:d'),(5,'items','example:e');
		insert into tag_import_snapshots(id,tag_id,revision_id,member_count)
			select 'tag-snapshot-'||value,value,'revision-page',5 from generate_series(1,5) value;
		insert into tag_import_members(tag_snapshot_id,raw_member_id,ordinal)
			select 'tag-snapshot-1','example:member_'||lpad(value::text,2,'0'),value from generate_series(1,5) value;
		insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length) values
			('revision-page','assets/a.json','json','application/json','a',1),
			('revision-page','assets/b.json','json','application/json','b',1),
			('revision-page','assets/shared.bin','text','text/plain','s1',1);
		insert into catalog_import_binary_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length) values
			('revision-page','assets/c.bin','binary','application/octet-stream','c',1),
			('revision-page','assets/shared.bin','binary','application/octet-stream','s2',1);
		insert into catalog_import_media(revision_id,asset_path,media_kind,content_type,sha256,byte_length) values
			('revision-page','assets/d.png','image','image/png','d',1)`); err != nil {
		t.Fatal(err)
	}

	var tags []string
	afterRegistry, afterID := "", ""
	for {
		page, more, err := loadModExportTagPage(ctx, db, "revision-page", "items", "example:", afterRegistry, afterID, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) > 2 {
			t.Fatalf("tag page exceeded limit: %d", len(page))
		}
		for _, item := range page {
			tags = append(tags, item.ID)
		}
		if !more {
			break
		}
		last := page[len(page)-1]
		afterRegistry, afterID = last.Registry, last.ID
	}
	if strings.Join(tags, ",") != "example:a,example:b,example:c,example:d,example:e" {
		t.Fatalf("tag cursor pages skipped or duplicated rows: %v", tags)
	}

	var members []string
	afterMember := ""
	for {
		page, more, err := loadModExportTagMemberPage(ctx, db, "revision-page", 1, afterMember, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page {
			members = append(members, item.ID)
		}
		if !more {
			break
		}
		afterMember = page[len(page)-1].ID
	}
	if len(members) != 5 || members[0] != "example:member_01" || members[4] != "example:member_05" {
		t.Fatalf("tag member cursor pages skipped or duplicated rows: %v", members)
	}
	if _, err := db.Exec(ctx, `insert into catalog_entities(id,public_id) values(100,'resource-corrupt');
		insert into game_resources(entity_id) values(100);
		insert into resource_import_snapshots(resource_id,revision_id,registry,names) values(100,'revision-page','items','[]'::jsonb);
		update tag_import_members set resource_id=100 where tag_snapshot_id='tag-snapshot-1' and raw_member_id='example:member_03'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadModExportTagMemberPage(ctx, db, "revision-page", 1, "example:member_02", 1); err == nil || !strings.Contains(err.Error(), "example:member_03") {
		t.Fatalf("invalid persisted member names were silently accepted: %v", err)
	}

	fullAssets := collectModExportAssetPages(t, ctx, db, false, 2)
	if len(fullAssets) != 6 || fullAssets[4].Path != "assets/shared.bin" || fullAssets[5].Path != "assets/shared.bin" || fullAssets[4].SourceOrder == fullAssets[5].SourceOrder {
		t.Fatalf("full asset cursor lost cross-table duplicate paths: %+v", fullAssets)
	}
	pathAssets := collectModExportAssetPages(t, ctx, db, true, 2)
	if len(pathAssets) != 5 {
		t.Fatalf("paths-only cursor did not deduplicate paths: %+v", pathAssets)
	}
}

func TestModExportTagAndAssetScalePlansUseCursorIndexesIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	createModExportPaginationTestTables(t, ctx, db)
	if _, err := db.Exec(ctx, `insert into catalog_entities(id,public_id) values(1,'scale-tag');
		insert into catalog_tags(entity_id,registry,canonical_id) values(1,'items','scale:tag');
		insert into tag_import_snapshots(id,tag_id,revision_id,member_count) values('scale-snapshot',1,'revision-scale',1000000);
		insert into tag_import_members(tag_snapshot_id,raw_member_id,ordinal)
			select 'scale-snapshot','scale:member_'||lpad(value::text,7,'0'),value from generate_series(1,100000) value;
		insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length)
			select 'revision-scale','assets/'||lpad(value::text,7,'0')||'.json','json','application/json',lpad(value::text,64,'0'),64
			from generate_series(1,100000) value;
		analyze tag_import_members; analyze catalog_import_text_assets`); err != nil {
		t.Fatal(err)
	}
	memberPlan100K := explainModExportPlan(t, ctx, db, `select raw_member_id from tag_import_members
		where tag_snapshot_id='scale-snapshot' and raw_member_id>'scale:member_0090000'
		order by raw_member_id limit 101`)
	if !strings.Contains(memberPlan100K, "tag_import_members_pkey") {
		t.Fatalf("100k tag cursor plan missed primary-key index:\n%s", memberPlan100K)
	}
	assetPlan100K := explainModExportPlan(t, ctx, db, `with assets as (
		select asset_path,0 source_order from catalog_import_text_assets where revision_id='revision-scale'
		union all select asset_path,1 from catalog_import_binary_assets where revision_id='revision-scale'
		union all select asset_path,2 from catalog_import_media where revision_id='revision-scale'
	) select asset_path,source_order from assets
	where (asset_path,source_order)>('assets/0090000.json',0) order by asset_path,source_order limit 101`)
	if !strings.Contains(assetPlan100K, "catalog_import_text_assets_pkey") {
		t.Fatalf("100k asset cursor plan missed revision/path index:\n%s", assetPlan100K)
	}
	if _, err := db.Exec(ctx, `insert into tag_import_members(tag_snapshot_id,raw_member_id,ordinal)
			select 'scale-snapshot','scale:member_'||lpad(value::text,7,'0'),value from generate_series(100001,1000000) value;
		insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length)
			select 'revision-scale','assets/'||lpad(value::text,7,'0')||'.json','json','application/json',lpad(value::text,64,'0'),64
			from generate_series(100001,1000000) value;
		analyze tag_import_members; analyze catalog_import_text_assets`); err != nil {
		t.Fatal(err)
	}
	memberPlan1M := explainModExportPlan(t, ctx, db, `select raw_member_id from tag_import_members
		where tag_snapshot_id='scale-snapshot' and raw_member_id>'scale:member_0900000'
		order by raw_member_id limit 101`)
	if !strings.Contains(memberPlan1M, "tag_import_members_pkey") {
		t.Fatalf("million-row tag cursor plan missed primary-key index:\n%s", memberPlan1M)
	}
	assetPlan1M := explainModExportPlan(t, ctx, db, `with assets as (
		select asset_path,0 source_order from catalog_import_text_assets where revision_id='revision-scale'
		union all select asset_path,1 from catalog_import_binary_assets where revision_id='revision-scale'
		union all select asset_path,2 from catalog_import_media where revision_id='revision-scale'
	) select asset_path,source_order from assets
	where (asset_path,source_order)>('assets/0900000.json',0) order by asset_path,source_order limit 101`)
	if !strings.Contains(assetPlan1M, "catalog_import_text_assets_pkey") {
		t.Fatalf("million-row asset cursor plan missed revision/path index:\n%s", assetPlan1M)
	}
	memberPage, more, err := loadModExportTagMemberPage(ctx, db, "revision-scale", 1, "scale:member_0900000", 100)
	if err != nil || len(memberPage) != 100 || !more {
		t.Fatalf("million-row member page was not bounded: rows=%d more=%v err=%v", len(memberPage), more, err)
	}
	assetPage, more, err := loadModExportAssetPage(ctx, db, "revision-scale", false, "assets/0900000.json", 0, 100)
	if err != nil || len(assetPage) != 100 || !more {
		t.Fatalf("million-row asset page was not bounded: rows=%d more=%v err=%v", len(assetPage), more, err)
	}
}

func collectModExportAssetPages(t *testing.T, ctx context.Context, db catalogResourceResolverQuery, pathsOnly bool, limit int) []modExportAssetPageRow {
	t.Helper()
	result := make([]modExportAssetPageRow, 0)
	afterPath, afterSource := "", 0
	for {
		page, more, err := loadModExportAssetPage(ctx, db, "revision-page", pathsOnly, afterPath, afterSource, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) > limit {
			t.Fatalf("asset page exceeded limit: %d", len(page))
		}
		result = append(result, page...)
		if !more {
			return result
		}
		last := page[len(page)-1]
		afterPath, afterSource = last.Path, last.SourceOrder
	}
}

func createModExportPaginationTestTables(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `create temp table catalog_entities(id bigint primary key,public_id text not null);
		create temp table catalog_tags(entity_id bigint primary key,registry text not null,canonical_id text not null,unique(registry,canonical_id));
		create temp table tag_import_snapshots(id text primary key,tag_id bigint not null,revision_id text not null,member_count integer not null,unique(tag_id,revision_id));
		create temp table tag_import_members(tag_snapshot_id text not null,resource_id bigint,raw_member_id text not null,ordinal integer not null,primary key(tag_snapshot_id,raw_member_id));
		create temp table game_resources(entity_id bigint primary key);
		create temp table resource_import_snapshots(resource_id bigint not null,revision_id text not null,registry text not null default '',translation_key text not null default '',names jsonb not null default '{}'::jsonb,icon_path text not null default '',created_at timestamptz not null default now());
		create temp table catalog_import_text_assets(revision_id text not null,asset_path text not null,asset_kind text not null,content_type text not null,sha256 text not null,byte_length bigint not null,primary key(revision_id,asset_path));
		create temp table catalog_import_binary_assets(revision_id text not null,asset_path text not null,asset_kind text not null,content_type text not null,sha256 text not null,byte_length bigint not null,primary key(revision_id,asset_path));
		create temp table catalog_import_media(revision_id text not null,asset_path text not null,media_kind text not null,content_type text not null,sha256 text not null,byte_length bigint not null,primary key(revision_id,asset_path))`); err != nil {
		t.Fatal(err)
	}
}

func explainModExportPlan(t *testing.T, ctx context.Context, db *pgxpool.Pool, query string) string {
	t.Helper()
	rows, err := db.Query(ctx, "explain (analyze,buffers,costs off) "+query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}
