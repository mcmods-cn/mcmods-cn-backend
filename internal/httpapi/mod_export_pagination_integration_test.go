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
	f, revision := newModExportPaginationFixture(t, "Export pagination")
	db, ctx := f.db, f.ctx
	var firstTag int64
	if err := db.QueryRow(ctx, `with entities as (
			insert into catalog_entities(identity_key,entity_type)
			select 'pagination-tag:'||suffix,'tag' from unnest(array['a','b','c','d','e']) suffix
			returning id,identity_key
		), tags as (
			insert into catalog_tags(entity_id,registry,canonical_id)
			select id,'items','example:'||split_part(identity_key,':',2) from entities
			returning entity_id,canonical_id
		), snapshots as (
			insert into tag_import_snapshots(id,tag_id,revision_id,member_count)
			select 'tag-snapshot-'||split_part(canonical_id,':',2),entity_id,$1,5 from tags
			returning id,tag_id
		)
		select tag_id from snapshots where id='tag-snapshot-a'`, revision).Scan(&firstTag); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into tag_import_members(tag_snapshot_id,raw_member_id,ordinal)
		select 'tag-snapshot-a','example:member_'||lpad(value::text,2,'0'),value from generate_series(1,5) value`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `with text_assets as (
			insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length,json_content,text_content) values
				($1,'assets/a.json','json','application/json',repeat('a',64),2,'{}',null),
				($1,'assets/b.json','json','application/json',repeat('b',64),2,'{}',null),
				($1,'assets/shared.bin','text','text/plain',repeat('c',64),1,null,'x')
		), binary_assets as (
			insert into catalog_import_binary_assets(id,revision_id,asset_path,asset_kind,sha256,byte_length,data) values
				('binary-c',$1,'assets/c.bin','binary',repeat('d',64),1,'x'),
				('binary-shared',$1,'assets/shared.bin','binary',repeat('e',64),1,'x')
		), media_file as (
			insert into oss_files(object_key,bucket,endpoint,original_name,content_type,size_bytes,status,scan_status)
			values('pagination/derived.png','synthetic','https://storage.invalid','d.png','image/png',1,'active','trusted_generated')
			returning id
		)
		insert into catalog_import_media(revision_id,asset_path,media_kind,oss_file_id,content_type,sha256,byte_length)
		select $1,'assets/d.png','image',id,'image/png',repeat('f',64),1 from media_file`, revision); err != nil {
		t.Fatal(err)
	}

	var tags []string
	afterRegistry, afterID := "", ""
	for {
		page, more, err := loadModExportTagPage(ctx, db, revision, "items", "example:", afterRegistry, afterID, 2)
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
		page, more, err := loadModExportTagMemberPage(ctx, db, revision, firstTag, afterMember, 2)
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
	if _, err := db.Exec(ctx, `with entity as (
			insert into catalog_entities(identity_key,entity_type) values('pagination-member-03','resource') returning id
		), resource as (
			insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path)
			select id,'minecraft.item','example:member_03','example','member_03' from entity returning entity_id
		), snapshot as (
			insert into resource_import_snapshots(id,resource_id,revision_id,registry,names)
			select 'malformed-member',entity_id,$1,'items','[]'::jsonb from resource returning resource_id
		)
		update tag_import_members set resource_id=(select resource_id from snapshot)
		where tag_snapshot_id='tag-snapshot-a' and raw_member_id='example:member_03'`, revision); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadModExportTagMemberPage(ctx, db, revision, firstTag, "example:member_02", 1); err == nil || !strings.Contains(err.Error(), "example:member_03") {
		t.Fatalf("invalid persisted member names were silently accepted: %v", err)
	}

	fullAssets := collectModExportAssetPages(t, ctx, db, revision, false, 2)
	if len(fullAssets) != 6 || fullAssets[4].Path != "assets/shared.bin" || fullAssets[5].Path != "assets/shared.bin" || fullAssets[4].SourceOrder == fullAssets[5].SourceOrder {
		t.Fatalf("full asset cursor lost cross-table duplicate paths: %+v", fullAssets)
	}
	pathAssets := collectModExportAssetPages(t, ctx, db, revision, true, 2)
	if len(pathAssets) != 5 {
		t.Fatalf("paths-only cursor did not deduplicate paths: %+v", pathAssets)
	}
}

func TestModExportTagAndAssetScalePlansUseCursorIndexesIntegration(t *testing.T) {
	f, revision := newModExportPaginationFixture(t, "Export pagination scale")
	db := f.db
	ctx := context.Background()
	var tagID int64
	if err := db.QueryRow(ctx, `with entity as (
			insert into catalog_entities(identity_key,entity_type) values('pagination-scale-tag','tag') returning id
		), tag as (
			insert into catalog_tags(entity_id,registry,canonical_id) select id,'items','scale:tag' from entity returning entity_id
		)
		insert into tag_import_snapshots(id,tag_id,revision_id,member_count)
		select 'scale-snapshot',entity_id,$1,1000000 from tag returning tag_id`, revision).Scan(&tagID); err != nil {
		t.Fatal(err)
	}
	seedModExportPaginationScale(t, ctx, db, revision, 1, 100000)
	memberPlan100K := explainModExportPlan(t, ctx, db, `select raw_member_id from tag_import_members
		where tag_snapshot_id='scale-snapshot' and raw_member_id>'scale:member_0090000'
		order by raw_member_id limit 101`)
	if !strings.Contains(memberPlan100K, "tag_import_members_pkey") {
		t.Fatalf("100k tag cursor plan missed primary-key index:\n%s", memberPlan100K)
	}
	assetPlan100K := explainModExportPlan(t, ctx, db, `with assets as (
		select asset_path,0 source_order from catalog_import_text_assets where revision_id=$1
		union all select asset_path,1 from catalog_import_binary_assets where revision_id=$1
		union all select asset_path,2 from catalog_import_media where revision_id=$1
	) select asset_path,source_order from assets
	where (asset_path,source_order)>('assets/0090000.json',0) order by asset_path,source_order limit 101`, revision)
	if !strings.Contains(assetPlan100K, "catalog_import_text_assets_pkey") {
		t.Fatalf("100k asset cursor plan missed revision/path index:\n%s", assetPlan100K)
	}
	seedModExportPaginationScale(t, ctx, db, revision, 100001, 1000000)
	memberPlan1M := explainModExportPlan(t, ctx, db, `select raw_member_id from tag_import_members
		where tag_snapshot_id='scale-snapshot' and raw_member_id>'scale:member_0900000'
		order by raw_member_id limit 101`)
	if !strings.Contains(memberPlan1M, "tag_import_members_pkey") {
		t.Fatalf("million-row tag cursor plan missed primary-key index:\n%s", memberPlan1M)
	}
	assetPlan1M := explainModExportPlan(t, ctx, db, `with assets as (
		select asset_path,0 source_order from catalog_import_text_assets where revision_id=$1
		union all select asset_path,1 from catalog_import_binary_assets where revision_id=$1
		union all select asset_path,2 from catalog_import_media where revision_id=$1
	) select asset_path,source_order from assets
	where (asset_path,source_order)>('assets/0900000.json',0) order by asset_path,source_order limit 101`, revision)
	if !strings.Contains(assetPlan1M, "catalog_import_text_assets_pkey") {
		t.Fatalf("million-row asset cursor plan missed revision/path index:\n%s", assetPlan1M)
	}
	memberPage, more, err := loadModExportTagMemberPage(ctx, db, revision, tagID, "scale:member_0900000", 100)
	if err != nil || len(memberPage) != 100 || !more {
		t.Fatalf("million-row member page was not bounded: rows=%d more=%v err=%v", len(memberPage), more, err)
	}
	assetPage, more, err := loadModExportAssetPage(ctx, db, revision, false, "assets/0900000.json", 0, 100)
	if err != nil || len(assetPage) != 100 || !more {
		t.Fatalf("million-row asset page was not bounded: rows=%d more=%v err=%v", len(assetPage), more, err)
	}
}

func collectModExportAssetPages(t *testing.T, ctx context.Context, db catalogResourceResolverQuery, revision string, pathsOnly bool, limit int) []modExportAssetPageRow {
	t.Helper()
	result := make([]modExportAssetPageRow, 0)
	afterPath, afterSource := "", 0
	for {
		page, more, err := loadModExportAssetPage(ctx, db, revision, pathsOnly, afterPath, afterSource, limit)
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

func newModExportPaginationFixture(t *testing.T, label string) (test013Fixture, string) {
	t.Helper()
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", label)
	revision := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	if _, err := f.db.Exec(f.ctx, `delete from catalog_import_text_assets where revision_id=$1 and asset_path='private.txt'`, revision); err != nil {
		t.Fatal(err)
	}
	return f, revision
}

func seedModExportPaginationScale(t *testing.T, ctx context.Context, db *pgxpool.Pool, revision string, first, last int) {
	t.Helper()
	if _, err := db.Exec(ctx, `with members as (
			insert into tag_import_members(tag_snapshot_id,raw_member_id,ordinal)
			select 'scale-snapshot','scale:member_'||lpad(value::text,7,'0'),value from generate_series($2::int,$3::int) value
		)
		insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length,json_content)
		select $1,'assets/'||lpad(value::text,7,'0')||'.json','json','application/json',lpad(value::text,64,'0'),2,'{}'::jsonb
		from generate_series($2::int,$3::int) value`, revision, first, last); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `analyze tag_import_members; analyze catalog_import_text_assets`); err != nil {
		t.Fatal(err)
	}
}

func explainModExportPlan(t *testing.T, ctx context.Context, db *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	rows, err := db.Query(ctx, "explain (analyze,buffers,costs off) "+query, args...)
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
