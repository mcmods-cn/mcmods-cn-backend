package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestPublicContributionActivityRevalidatesCurrentTargetVisibilityIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to execute contribution visibility against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		create temporary table public_routes(
			id bigint primary key,public_id text,entity_type text,internal_id bigint,canonical_path text);
		create temporary table change_requests(
			id bigint primary key,public_id text,entity_type text,entity_id bigint,base_revision_id bigint,
			submitted_by bigint,status text,resolved_at timestamptz,submitted_at timestamptz);
		create temporary table mods(id bigint primary key,primary_name text,review_status text,submitted_by bigint);
		create temporary table modpacks(id bigint primary key,primary_name text,review_status text,submitted_by bigint);
		create temporary table simple_projects(id bigint primary key,project_type text,primary_name text,review_status text,submitted_by bigint);
		create temporary table minecraft_servers(id bigint primary key,name text,review_status text,submitted_by bigint);
		create temporary table community_posts(id bigint primary key,title text,status text,review_status text,author_id bigint);
		create temporary table blueprints(id bigint primary key,title text,status text,review_status text,owner_id bigint);
		create temporary table skin_assets(id bigint primary key,display_name text,status text,visibility text,review_status text,owner_id bigint);

		insert into public_routes values
			(1,'publicmod','mod',10,'/mods/publicmod'),
			(2,'hiddenmod','mod',11,'/mods/hiddenmod'),
			(3,'deletedbp','blueprint',20,'/blueprints/deletedbp'),
			(4,'privskin1','skin',21,'/skins/privskin1'),
			(5,'publicskn','skin',22,'/skins/publicskn'),
			(6,'author001','author',30,'/authors/author001'),
			(7,'unlistskn','skin',23,'/skins/unlistskn');
		insert into mods values
			(10,'Current public Mod','approved',8),
			(11,'Governance-hidden Mod','pending',7);
		insert into blueprints values (20,'Deleted blueprint','deleted','approved',7);
		insert into skin_assets values
			(21,'Private skin','active','private','approved',7),
			(22,'Current public skin','active','public','approved',8),
			(23,'Unlisted skin','active','unlisted','approved',7);
		insert into change_requests values
			(101,'change001','mod',10,null,7,'approved',now()-interval '1 minute',now()-interval '2 minutes'),
			(102,'change002','mod',11,1,7,'approved',now()-interval '2 minutes',now()-interval '3 minutes'),
			(103,'change003','blueprint',20,null,7,'approved',now()-interval '3 minutes',now()-interval '4 minutes'),
			(104,'change004','skin',21,null,7,'approved',now()-interval '4 minutes',now()-interval '5 minutes'),
			(105,'change005','skin',22,1,7,'approved',now()-interval '5 minutes',now()-interval '6 minutes'),
			(106,'change006','author',30,null,7,'approved',now()-interval '6 minutes',now()-interval '7 minutes'),
			(107,'change007','mod',999,null,7,'approved',now()-interval '7 minutes',now()-interval '8 minutes'),
			(108,'change008','skin',23,null,7,'approved',now()-interval '8 minutes',now()-interval '9 minutes')`); err != nil {
		t.Fatal(err)
	}

	activity := loadPublicContributionActivityFixture(t, ctx, pool, 7)
	if len(activity) != 2 || activity[0].Name != "Current public Mod" || activity[1].Name != "Current public skin" {
		t.Fatalf("initial public activity=%+v, want only current public Mod and skin", activity)
	}
	for _, item := range activity {
		if item.Name == "Governance-hidden Mod" || item.Name == "Deleted blueprint" || item.Name == "Private skin" || item.EntityType == "author" {
			t.Fatalf("hidden or unsupported target leaked: %+v", item)
		}
	}

	if _, err = pool.Exec(ctx, `update mods set review_status='pending' where id=10; update mods set review_status='approved' where id=11`); err != nil {
		t.Fatal(err)
	}
	activity = loadPublicContributionActivityFixture(t, ctx, pool, 7)
	if len(activity) != 2 || activity[0].Name != "Governance-hidden Mod" || activity[1].Name != "Current public skin" {
		t.Fatalf("activity after visibility change=%+v, want newly public Mod and skin", activity)
	}
}

func loadPublicContributionActivityFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID int64,
) []userContributionActivity {
	t.Helper()
	rows, err := pool.Query(ctx, userContributionRecentActivityQuery(), userID, time.Now().UTC().AddDate(0, -1, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	items := make([]userContributionActivity, 0)
	for rows.Next() {
		var item userContributionActivity
		if err = rows.Scan(&item.ID, &item.EntityType, &item.Action, &item.Name, &item.Href, &item.OccurredAt); err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return items
}
