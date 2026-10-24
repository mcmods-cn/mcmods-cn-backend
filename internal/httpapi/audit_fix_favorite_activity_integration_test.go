package httpapi

import (
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func auditFixFavoriteActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	rows, err := f.db.Query(f.ctx, `select tablename from pg_tables where schemaname=current_schema() and (tablename like 'favorite%' or tablename like 'content_popularity%') order by tablename`)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, table := range names {
		var raw string
		if err = f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func TestAuditFixCMP003FavoritesActivitySharesMembershipQuotaAndProjectionTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	recover := func(method, path string, body any, want int) []byte {
		t.Helper()
		auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
		before := auditFixFavoriteActivityFacts(t, f)
		raw := f.require(t, f.editor, method, path, body, 500)
		auditFixRequireSpecificActivityError(t, raw, "failed to persist favorite activity")
		auditFixRequireProjectFactsUnchanged(t, before, auditFixFavoriteActivityFacts(t, f))
		auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
		return f.require(t, f.editor, method, path, body, want)
	}
	collection := decodeTEST022Data[favoriteCollectionSummary](t, recover(http.MethodPost, "/api/v1/users/me/favorite-collections", map[string]any{"name": "Atomic favorites", "isPublic": false}, 201))
	path := "/api/v1/users/me/favorite-collections/" + collection.ID
	recover(http.MethodPut, path, map[string]any{"name": "Atomic renamed", "isPublic": true}, 200)
	body := map[string]any{"entityType": "mod", "entityPublicId": f.modCode, "collectionIds": []string{collection.ID}}
	recover(http.MethodPut, "/api/v1/users/me/favorites", body, 200)
	for _, operation := range []string{"remove", "add"} {
		delta := map[string]any{"entityType": "mod", "entityPublicId": f.modCode, "addCollectionIds": []string{}, "removeCollectionIds": []string{}}
		delta[operation+"CollectionIds"] = []string{collection.ID}
		recover(http.MethodPatch, "/api/v1/users/me/favorites", delta, 200)
	}
	recover(http.MethodDelete, path, nil, 204)
	var collections, items int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from favorite_collections where public_id=$1),(select count(*) from favorite_collection_items item join favorite_collections collection on collection.id=item.collection_id where collection.user_id=$2)`, collection.ID, f.userIDs[f.editor]).Scan(&collections, &items); err != nil || collections != 0 || items != 0 {
		t.Fatalf("deleted favorite collection/items=%d/%d err=%v", collections, items, err)
	}
	var creates, edits, deletes int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where action_id=$2),count(*) filter(where action_id=$3),count(*) filter(where action_id=$4) from activity_event_outbox where user_id=$1 and object_type_id=$5`, f.userIDs[f.editor], activity.ActionCreate, activity.ActionEdit, activity.ActionDelete, activity.ObjectUser).Scan(&creates, &edits, &deletes); err != nil || creates != 1 || edits != 4 || deletes != 1 {
		t.Fatalf("favorite activity create/edit/delete=%d/%d/%d want1/4/1 err=%v", creates, edits, deletes, err)
	}
}
