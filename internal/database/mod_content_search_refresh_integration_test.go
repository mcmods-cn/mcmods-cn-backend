package database

import (
	"context"
	"os"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestResourceSearchRefreshWritesOnlyAffectedPlacementsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires PostgreSQL to observe writes to the search projection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture, err := NewIsolatedTestSchema(ctx, config.Load().DB.ConnString(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fixture.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if _, err = fixture.Pool.Exec(ctx, `
		insert into mods(id,project_code,slug,primary_name,review_status) values(71,'searchdoc','search-doc','Search document','approved');
		insert into mod_content_versions(id,mod_id,label) values(72,71,'first'),(73,71,'second');
		insert into mod_content_sections(id,mod_id,version_id,template_id,display_mode)
			select v.id,71,v.id,t.id,'compact' from mod_content_versions v cross join mod_content_templates t
			where t.builtin and t.code='item_block';
		insert into catalog_entities(id,identity_key,entity_type) values(74,'search:sample','resource');
		insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path)
			values(74,'minecraft.item','search:example','search','example');
		insert into mod_resource_bindings(resource_id,mod_id) values(74,71);
		insert into mod_resource_version_details(resource_id,version_id) values(74,72),(74,73);
		insert into mod_resource_version_detail_localizations(resource_id,version_id,locale,name) values(74,72,'en-US','before');
		insert into mod_content_section_resources(section_id,version_id,resource_id) values(72,72,74),(73,73,74)
	`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, update, word string
		version, writes    int
	}{
		{"rename", `update mod_resource_version_detail_localizations set name='after' where resource_id=74`, "after", 72, 1},
		{"move", `update mod_resource_version_detail_localizations set version_id=73 where resource_id=74`, "before", 73, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := fixture.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, tc.update); err != nil {
				t.Fatal(err)
			}
			var writes, matched, version int
			if err := tx.QueryRow(ctx, `select n_tup_upd from pg_stat_xact_user_tables
				where relid='mod_content_section_resources'::regclass`).Scan(&writes); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(ctx, `select count(*),coalesce(min(version_id),0)
				from mod_content_section_resources where search_document @@ plainto_tsquery('simple',$1)`, tc.word).
				Scan(&matched, &version); err != nil {
				t.Fatal(err)
			}
			if writes != tc.writes || matched != 1 || version != tc.version {
				t.Fatalf("projection writes=%d, matching versions=%d, first=%d; want %d,1,%d", writes, matched, version, tc.writes, tc.version)
			}
		})
	}
}
