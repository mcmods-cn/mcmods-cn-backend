package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestUserShowcaseProjectsAccessFirstFunctionAndScalePlanIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute showcase plans against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err = pool.Exec(ctx, `
		create temporary table showcase_scale_ids(id bigint primary key);
		create temporary table showcase_mod_fixtures(
			id bigint primary key,primary_name text,summary text,icon_url text,updated_at timestamptz,review_status text);
		create temporary view mods as
			select id,primary_name,summary,icon_url,updated_at,review_status from showcase_mod_fixtures
			union all
			select id,'scale mod','', '',to_timestamp(0),'approved' from showcase_scale_ids;
		create temporary table modpacks(
			id bigint primary key,primary_name text,summary text,icon_url text,updated_at timestamptz,review_status text);
		create temporary table simple_projects(
			id bigint primary key,project_type text,primary_name text,summary text,icon_url text,updated_at timestamptz,review_status text);
		create temporary table minecraft_servers(
			id bigint primary key,name text,body_markdown text,updated_at timestamptz,review_status text);
		create temporary table blueprints(
			id bigint primary key,title text,description_markdown text,cover_object_key text,updated_at timestamptz,status text,review_status text);
		create temporary table skin_assets(
			id bigint primary key,display_name text,description text,blob_hash text,updated_at timestamptz,status text,visibility text,review_status text);
		create temporary table skin_texture_blobs(hash text primary key,object_key text);
		create temporary table community_posts(
			id bigint primary key,title text,body_markdown text,updated_at timestamptz,status text,review_status text);
		create temporary table effective_project_access(
			user_id bigint,project_type text,project_id bigint,access_level text);
		create index effective_project_access_user_idx
			on effective_project_access(user_id,project_type,project_id,access_level);
		create temporary table showcase_route_fixtures(
			entity_type text,internal_id bigint,public_id text,canonical_path text,primary key(entity_type,internal_id));
		create temporary view public_routes as
			select entity_type,internal_id,public_id,canonical_path from showcase_route_fixtures
			union all
			select 'mod',id,'scale-'||id::text,'/mod/scale-'||id::text from showcase_scale_ids;

		insert into showcase_mod_fixtures values
			(1,'Mod one','summary','',now()-interval '1 day','approved'),
			(8,'Hidden mod','summary','',now(),'pending'),
			(9,'Other user mod','summary','',now(),'approved');
		insert into modpacks values (2,'Pack two','summary','',now()-interval '2 days','approved');
		insert into simple_projects values (3,'resourcepack','Resource three','summary','',now()-interval '3 days','approved');
		insert into minecraft_servers values (4,'Server four','summary',now()-interval '4 days','approved');
		insert into blueprints values (5,'Blueprint five','summary','',now()-interval '5 days','ready','approved');
		insert into skin_texture_blobs values ('skin-hash','');
		insert into skin_assets values (6,'Skin six','summary','skin-hash',now()-interval '6 days','active','public','approved');
		insert into community_posts values (7,'Post seven','summary',now()-interval '7 days','active','approved');
		insert into effective_project_access values
			(7,'mod',1,'developer'),(7,'mod',1,'editor'),(7,'modpack',2,'editor'),
			(7,'resourcepack',3,'developer'),(7,'minecraft_server',4,'developer'),
			(7,'blueprint',5,'editor'),(7,'skin',6,'developer'),(7,'community_post',7,'editor'),
			(7,'mod',8,'developer'),(8,'mod',9,'developer');
		insert into showcase_route_fixtures(entity_type,internal_id,public_id,canonical_path) values
			('mod',1,'mod-one','/mod/mod-one'),('mod',8,'hidden-mod','/mod/hidden-mod'),('mod',9,'other-mod','/mod/other-mod'),
			('modpack',2,'pack-two','/modpack/pack-two'),('resourcepack',3,'resource-three','/resourcepack/resource-three'),
			('minecraft_server',4,'server-four','/server/server-four'),('blueprint',5,'blueprint-five','/blueprint/blueprint-five'),
			('skin',6,'skin-six','/skin/skin-six'),('community_post',7,'post-seven','/community/post-seven');
		analyze effective_project_access; analyze showcase_mod_fixtures; analyze showcase_route_fixtures`); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, userShowcaseProjectsQuery, int64(7))
	if err != nil {
		t.Fatal(err)
	}
	roles := make(map[string][2]bool)
	for rows.Next() {
		var entityType, publicID, name, summary, iconURL, href string
		var developer, editor bool
		var updatedAt time.Time
		if err = rows.Scan(&entityType, &publicID, &name, &summary, &iconURL, &href, &developer, &editor, &updatedAt); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		roles[publicID] = [2]bool{developer, editor}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(roles) != 7 || roles["mod-one"] != [2]bool{true, true} || roles["pack-two"] != [2]bool{false, true} || roles["resource-three"] != [2]bool{true, false} {
		t.Fatalf("access-first showcase roles=%v, want seven visible fixtures with merged developer/editor roles", roles)
	}
	if _, exists := roles["hidden-mod"]; exists {
		t.Fatal("pending project leaked into showcase")
	}
	if _, exists := roles["other-mod"]; exists {
		t.Fatal("another user's project leaked into showcase")
	}

	var loaded int64
	for _, target := range []int64{100_000, 1_000_000, 10_000_000} {
		if _, err = pool.Exec(ctx, fmt.Sprintf(
			`insert into showcase_scale_ids(id) select 100000000+value from generate_series(%d,%d) value`, loaded+1, target,
		)); err != nil {
			t.Fatalf("populate %d project scale: %v", target, err)
		}
		loaded = target
		if _, err = pool.Exec(ctx, `analyze showcase_scale_ids`); err != nil {
			t.Fatal(err)
		}
		planRows, planErr := pool.Query(ctx, `explain (analyze,buffers,format text) `+userShowcaseProjectsQuery, int64(7))
		if planErr != nil {
			t.Fatalf("explain %d project scale: %v", target, planErr)
		}
		var plan strings.Builder
		for planRows.Next() {
			var line string
			if err = planRows.Scan(&line); err != nil {
				planRows.Close()
				t.Fatal(err)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		planRows.Close()
		if err = planRows.Err(); err != nil {
			t.Fatal(err)
		}
		planText := strings.ToLower(plan.String())
		if strings.Contains(planText, "seq scan on showcase_scale_ids") || !strings.Contains(planText, "showcase_scale_ids_pkey") {
			t.Fatalf("%d-project showcase plan was not bounded by project ID indexes:\n%s", target, plan.String())
		}
		t.Logf("%d-project access-first plan:\n%s", target, plan.String())
	}
}
