package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogPublicationWritesOrderedMembersInOneBatch(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	request := map[string]any{"canonicalId": "batch:source", "defaultLocale": "en-US",
		"localizations": []catalogLocalizationEdit{{Locale: "en-US", Name: "Batch source"}}}
	source := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, "/api/v1/recipe-types", request, http.StatusAccepted))
	var revisionID int64
	if err := f.db.QueryRow(f.ctx, `select id from content_revisions where public_id=$1`, source.RevisionID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	config := f.db.Config()
	counter := &integrationQueryCounter{}
	config.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `insert into catalog_entities(identity_key,entity_type,status)
		select 'relation-batch:'||i,'resource','active' from generate_series(1,500) i;
		insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		select id,'minecraft.item','batch:'||id,'batch',id::text,true from catalog_entities where identity_key like 'relation-batch:%'`); err != nil {
		t.Fatal(err)
	}
	var publicIDs []string
	if err = tx.QueryRow(f.ctx, `select array_agg(public_id order by id desc) from catalog_entities where identity_key like 'relation-batch:%'`).Scan(&publicIDs); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tag", "recipe_type"} {
		var id int64
		if err = tx.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status) values($1,$2,'active') returning id`, "relation-parent:"+kind, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		table, column := "catalog_tag_members", "tag_id"
		if kind == "recipe_type" {
			table, column = "recipe_type_catalysts", "recipe_type_id"
		}
		for _, n := range []int{1, 64, 500, 0} {
			snapshot := catalogEditorSnapshot{EntityID: id}
			started := time.Now()
			counter.queries.Store(0)
			if kind == "tag" {
				snapshot.Tag = &catalogTagEdit{Registry: "minecraft:item", CanonicalID: "batch:tag", MemberResourcePublicIDs: publicIDs[:n]}
				err = publishCatalogTagTx(f.ctx, tx, snapshot, revisionID)
			} else {
				snapshot.RecipeType = &catalogRecipeTypeEdit{CanonicalID: "batch:type", CatalystResourcePublicIDs: publicIDs[:n]}
				err = publishCatalogRecipeTypeTx(f.ctx, tx, snapshot, revisionID, f.userIDs[f.editor])
			}
			if err != nil {
				t.Fatal(err)
			}
			queries := counter.queries.Load()
			t.Logf("kind=%s members=%d queries=%d duration=%s", kind, n, queries, time.Since(started))
			var stored []string
			if err = tx.QueryRow(f.ctx, fmt.Sprintf(`select coalesce(array_agg(entity.public_id order by member.ordinal),'{}'::text[])
				from %s member join catalog_entities entity on entity.id=member.resource_id
				where member.%s=$1 and member.published_revision_id=$2`, table, column), id, revisionID).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(stored, publicIDs[:n]) {
				t.Fatalf("%s lost the replacement order or revision for %d members", kind, n)
			}
			if n > 0 && queries > 5 {
				t.Errorf("%s with %d members used %d queries; budget 5", kind, n, queries)
			}
		}
	}
}
