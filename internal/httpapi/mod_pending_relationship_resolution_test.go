package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestModPublicationResolvesOnlyMatchingRelationshipsInConstantQueriesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	cfg := base.Config().ConnConfig
	counter := &modContentReferenceSyncQueryCounter{}
	cfg.Tracer = counter
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var source, target, other int64
	for _, entry := range []struct {
		name string
		id   *int64
	}{{"source", &source}, {"target", &target}, {"other", &other}} {
		if err := tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
			values(new_public_id(),$1,$1,'approved') returning id`, entry.name).Scan(entry.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(ctx, "insert into mod_identifiers(mod_id,identifier) values($1,'Future_Mod')", target); err != nil {
		t.Fatal(err)
	}
	resolve := func() int64 {
		t.Helper()
		counter.count.Store(0)
		counter.active.Store(true)
		err := resolvePendingModReferencesTx(ctx, tx, target)
		counter.active.Store(false)
		if err != nil {
			t.Fatal(err)
		}
		return counter.count.Load()
	}
	emptyQueries := resolve()
	const fresh = 128
	if _, err = tx.Exec(ctx, `with inserted as (
		insert into mod_relationships(mod_id,relation_type,related_mod_id,related_mod_identifier,display_order)
		select case when i=$4::int+3 then $2::bigint else $1::bigint end,'dependency',
			case i when $4::int+1 then $2::bigint when $4::int+2 then $3::bigint else null end,'Future_Mod',i
		from generate_series(1,$4::int+3) i returning id
	)
	insert into unresolved_references(source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier)
	select 'mod_relationship',id,'relatedMod','mod','Future_Mod','future_mod' from inserted`, source, target, other, fresh); err != nil {
		t.Fatal(err)
	}
	loadedQueries := resolve()
	var linked, preserved, selfUnresolved int
	if err = tx.QueryRow(ctx, `select
		count(*) filter(where r.related_mod_id=$1 and u.status='resolved' and u.resolved_id=r.related_mod_id),
		count(*) filter(where r.related_mod_id=$2 and u.status='pending' and u.resolved_id is null),
		count(*) filter(where r.mod_id=$1 and r.related_mod_id is null and u.status='pending')
		from mod_relationships r join unresolved_references u on u.source_type='mod_relationship' and u.source_id=r.id`,
		target, other).Scan(&linked, &preserved, &selfUnresolved); err != nil {
		t.Fatal(err)
	}
	t.Logf("empty=%d statements, %d candidate records=%d statements", emptyQueries, fresh+3, loadedQueries)
	if linked != fresh+1 || preserved != 1 || selfUnresolved != 1 || loadedQueries != emptyQueries {
		t.Fatalf("linked=%d, preserved independent binding=%d, preserved self reference=%d; statements empty=%d loaded=%d",
			linked, preserved, selfUnresolved, emptyQueries, loadedQueries)
	}
}
