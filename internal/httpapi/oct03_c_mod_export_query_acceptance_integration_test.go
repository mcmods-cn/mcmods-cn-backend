package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
)

type oct03CExportQueryPage struct {
	Items []struct {
		ID       string `json:"id"`
		Registry string `json:"registry"`
		Path     string `json:"path"`
	} `json:"items"`
	Members []struct {
		ID string `json:"id"`
	} `json:"members"`
	Limit       int    `json:"limit"`
	MemberCount int    `json:"memberCount"`
	HasMore     bool   `json:"hasMore"`
	NextCursor  string `json:"nextCursor"`
}

// Capture the actual loader statement instead of maintaining another query
// whose execution plan could diverge from the production query.
type oct03CExportTagQueryCapture struct {
	sql  string
	args []any
}

func (c *oct03CExportTagQueryCapture) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "from catalog_tags tag") {
		c.sql, c.args = data.SQL, append([]any(nil), data.Args...)
	}
	return ctx
}

func (*oct03CExportTagQueryCapture) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestOCT03CPERF010ActualTagPrefixQueryUsesInstalledIndexAtScaleIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Prefix plan")
	revision := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	if _, err := f.db.Exec(f.ctx, `with entities as (
		insert into catalog_entities(identity_key,entity_type,status)
		select 'oct03-prefix-scale:'||i,'tag','active' from generate_series(0,99999) i
		returning id,identity_key
	)
	insert into catalog_tags(entity_id,registry,canonical_id)
	select id,'minecraft:item',case when split_part(identity_key,':',2)::integer between 50000 and 50010
		then 'oct03:rare' else 'oct03:common' end||lpad(split_part(identity_key,':',2),6,'0') from entities`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into tag_import_snapshots(id,tag_id,revision_id)
		select 'oct03-prefix-'||entity_id,entity_id,$1 from catalog_tags where canonical_id like 'oct03:%'`, revision); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"catalog_entities", "catalog_tags", "tag_import_snapshots"} {
		if _, err := f.db.Exec(f.ctx, "analyze "+table); err != nil {
			t.Fatal(err)
		}
	}
	capture := &oct03CExportTagQueryCapture{}
	cfg := f.db.Config().Copy()
	cfg.ConnConfig.Tracer = capture
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	items, hasMore, err := loadModExportTagPage(f.ctx, pool, revision, "minecraft:item", "OCT03:RARE", "", "", 200)
	if err != nil || len(items) != 11 || hasMore {
		t.Fatalf("scale prefix rows=%d hasMore=%v err=%v", len(items), hasMore, err)
	}
	if capture.sql == "" {
		t.Fatal("actual production tag query was not captured")
	}
	rows, err := f.db.Query(f.ctx, "explain (analyze,buffers,costs off) "+capture.sql, capture.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	state, err := database.InspectCatalogTagPrefixIndex(f.ctx, f.db)
	if err != nil || !state.Exists || !state.Matches || !state.Ready || !state.Valid {
		t.Fatalf("fresh schema prefix index=%+v err=%v", state, err)
	}
	if !strings.Contains(plan.String(), database.CatalogTagPrefixIndexName) {
		t.Fatalf("actual production prefix query did not use its installed index on 100000 representative tags:\n%s", plan.String())
	}
	t.Logf("100000 synthetic tags: captured production prefix SQL, default planner:\n%s", plan.String())
}

func oct03CReadPublicExportQueryPage(t *testing.T, f test013Fixture, path string) oct03CExportQueryPage {
	t.Helper()
	var response struct {
		Data oct03CExportQueryPage `json:"data"`
	}
	if err := json.Unmarshal(f.require(t, "", http.MethodGet, path, nil, http.StatusOK), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func TestOCT03CPERF010PublicExportTagAndAssetQueriesEnforceBoundedKeysetsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Bounded export queries")
	revision := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	var firstTag int64
	for i := 0; i < 207; i++ {
		registry, canonical := "minecraft:item", fmt.Sprintf("oct03:item%04d", i)
		if i == 205 {
			canonical = "other:oct03:item-containment-only"
		}
		if i == 206 {
			registry, canonical = "minecraft:block", "oct03:item0000"
		}
		var entity int64
		if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status)
			values($1,'tag','active') returning id`, "oct03-perf010:"+registry+":"+canonical).Scan(&entity); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(f.ctx, `insert into catalog_tags(entity_id,registry,canonical_id) values($1,$2,$3)`, entity, registry, canonical); err != nil {
			t.Fatal(err)
		}
		members := 0
		if i == 0 {
			firstTag, members = entity, 205
		}
		if _, err := f.db.Exec(f.ctx, `insert into tag_import_snapshots(id,tag_id,revision_id,member_count) values($1,$2,$3,$4)`, newExportID(), entity, revision, members); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(f.ctx, `insert into tag_import_members(tag_snapshot_id,raw_member_id,ordinal)
		select snapshot.id,'oct03:member'||lpad(i::text,4,'0'),205-i from tag_import_snapshots snapshot
		cross join generate_series(0,204) i where snapshot.revision_id=$1 and snapshot.tag_id=$2`, revision, firstTag); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length,text_content)
		select $1,'oct03/'||lpad(i::text,4,'0')||'.txt','text','text/plain',$2,1,'x' from generate_series(0,500) i`, revision, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/export-revisions/" + revision
	tagsPath := base + "/tags?registry=minecraft%3Aitem&q=OCT03%3AITEM&all=1&limit=999999"
	first := oct03CReadPublicExportQueryPage(t, f, tagsPath)
	if first.Limit != 200 || len(first.Items) != 200 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("public tag cap was not enforced: limit=%d items=%d hasMore=%v", first.Limit, len(first.Items), first.HasMore)
	}
	for i, item := range first.Items {
		if item.Registry != "minecraft:item" || item.ID != fmt.Sprintf("oct03:item%04d", i) {
			t.Fatalf("prefix/registry/keyset mismatch at %d: %v", i, item)
		}
	}
	// Removing a row before the cursor must not shift the next page's boundary.
	if _, err := f.db.Exec(f.ctx, `delete from tag_import_snapshots where revision_id=$1 and tag_id=$2`, revision, firstTag); err != nil {
		t.Fatal(err)
	}
	last := oct03CReadPublicExportQueryPage(t, f, tagsPath+"&cursor="+url.QueryEscape(first.NextCursor))
	if len(last.Items) != 5 || last.HasMore || last.NextCursor != "" {
		t.Fatalf("last tag page=%+v", last)
	}
	for i, item := range last.Items {
		if item.ID != fmt.Sprintf("oct03:item%04d", 200+i) {
			t.Fatalf("deletion changed cursor boundary: %v", last.Items)
		}
	}
	for _, suffix := range []string{
		"/tags?registry=minecraft%3Ablock&q=OCT03%3AITEM",
		"/tags?registry=minecraft%3Aitem&q=other",
	} {
		f.require(t, "", http.MethodGet, base+suffix+"&cursor="+url.QueryEscape(first.NextCursor), nil, http.StatusBadRequest)
	}
	// Restore the removed tag snapshot and its synthetic members for detail paging.
	snapshot := newExportID()
	if _, err := f.db.Exec(f.ctx, `insert into tag_import_snapshots(id,tag_id,revision_id,member_count) values($1,$2,$3,205)`, snapshot, firstTag, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into tag_import_members(tag_snapshot_id,raw_member_id,ordinal)
		select $1,'oct03:member'||lpad(i::text,4,'0'),205-i from generate_series(0,204) i`, snapshot); err != nil {
		t.Fatal(err)
	}
	memberPath := base + "/tag-detail?registry=minecraft%3Aitem&tagId=oct03%3Aitem0000&all=1&limit=999999"
	members := oct03CReadPublicExportQueryPage(t, f, memberPath)
	if members.Limit != 200 || members.MemberCount != 205 || len(members.Members) != 200 || !members.HasMore || members.NextCursor == "" {
		t.Fatalf("member page was not bounded: %+v", members)
	}
	for i, member := range members.Members {
		if member.ID != fmt.Sprintf("oct03:member%04d", i) {
			t.Fatalf("member keyset followed unstable ordinal: %v", member)
		}
	}
	lastMembers := oct03CReadPublicExportQueryPage(t, f, memberPath+"&cursor="+url.QueryEscape(members.NextCursor))
	if len(lastMembers.Members) != 5 || lastMembers.HasMore || lastMembers.NextCursor != "" || lastMembers.Members[0].ID != "oct03:member0200" {
		t.Fatalf("member continuation=%+v", lastMembers)
	}
	f.require(t, "", http.MethodGet, base+"/tag-detail?registry=minecraft%3Aitem&tagId=oct03%3Aitem0001&cursor="+url.QueryEscape(members.NextCursor), nil, http.StatusBadRequest)
	assetsPath := base + "/assets?all=1&limit=999999"
	assets := oct03CReadPublicExportQueryPage(t, f, assetsPath)
	if assets.Limit != 500 || len(assets.Items) != 500 || !assets.HasMore || assets.NextCursor == "" {
		t.Fatalf("asset page was not bounded: limit=%d count=%d hasMore=%v", assets.Limit, len(assets.Items), assets.HasMore)
	}
	for i, asset := range assets.Items {
		if asset.Path != fmt.Sprintf("oct03/%04d.txt", i) {
			t.Fatalf("asset keyset order changed: %v", asset)
		}
	}
	f.require(t, "", http.MethodGet, assetsPath+"&pathsOnly=1&cursor="+url.QueryEscape(assets.NextCursor), nil, http.StatusBadRequest)
	lastAssets := oct03CReadPublicExportQueryPage(t, f, assetsPath+"&cursor="+url.QueryEscape(assets.NextCursor))
	if len(lastAssets.Items) != 2 || lastAssets.HasMore || lastAssets.NextCursor != "" {
		t.Fatalf("asset continuation=%+v", lastAssets)
	}
	otherVersion := f.version(t, f.otherEditor, "test013-other", "Other export revision")
	other := f
	other.modID = f.otherModID
	otherRevision := oct02CInsertPublishedExportRevision(t, other, otherVersion.PublicID)
	f.require(t, "", http.MethodGet, "/api/v1/export-revisions/"+otherRevision+"/tags?registry=minecraft%3Aitem&q=OCT03%3AITEM&cursor="+url.QueryEscape(first.NextCursor), nil, http.StatusBadRequest)
}

func TestOCT03CPERF010TagSearchTreatsSQLWildcardsAsLiteralPrefixesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "Literal tag prefixes")
	revision := oct02CInsertPublishedExportRevision(t, f, version.PublicID)
	for _, canonical := range []string{"oct03:foo_bar", "oct03:foo_baz", "oct03:fooabar", "oct03:root"} {
		var entity int64
		if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status)
			values($1,'tag','active') returning id`, "oct03-literal:"+canonical).Scan(&entity); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(f.ctx, `insert into catalog_tags(entity_id,registry,canonical_id)
			values($1,'minecraft:item',$2)`, entity, canonical); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(f.ctx, `insert into tag_import_snapshots(id,tag_id,revision_id)
			values($1,$2,$3)`, newExportID(), entity, revision); err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the public handler so URL decoding and the caller's raw cursor
	// scope are covered together with PostgreSQL's actual LIKE semantics.
	for _, scenario := range []struct {
		query string
		limit int
		want  []string
	}{
		{"OCT03:FOO_", 200, []string{"oct03:foo_bar", "oct03:foo_baz"}},
		{"OCT03:FOO_BAR", 200, []string{"oct03:foo_bar"}},
		{"%", 2, nil},
		{`\`, 2, nil},
	} {
		path := "/api/v1/export-revisions/" + revision + "/tags?registry=minecraft%3Aitem&q=" + url.QueryEscape(scenario.query) + "&limit=" + fmt.Sprint(scenario.limit)
		page := oct03CReadPublicExportQueryPage(t, f, path)
		ids := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}
		t.Logf("literal q=%q: ids=%q limit=%d hasMore=%v", scenario.query, ids, page.Limit, page.HasMore)
		if strings.Join(ids, "\x00") != strings.Join(scenario.want, "\x00") || page.Limit != scenario.limit || page.HasMore || page.NextCursor != "" {
			t.Errorf("query %q expanded beyond its literal prefix: ids=%q want=%q limit=%d hasMore=%v", scenario.query, ids, scenario.want, page.Limit, page.HasMore)
		}
	}
	// Record the default planner for the same production statement and a '%'
	// query. Four rows show semantics, not a production performance benchmark.
	capture := &oct03CExportTagQueryCapture{}
	cfg := f.db.Config().Copy()
	cfg.ConnConfig.Tracer = capture
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, _, err = loadModExportTagPage(f.ctx, pool, revision, "minecraft:item", "%", "", "", 2); err != nil {
		t.Fatal(err)
	}
	if capture.sql == "" {
		t.Fatal("actual production tag query was not captured")
	}
	rows, err := f.db.Query(f.ctx, "explain (analyze,buffers,costs off) "+capture.sql, capture.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("captured production percent-query plan on four synthetic tags:\n%s", plan.String())
}
