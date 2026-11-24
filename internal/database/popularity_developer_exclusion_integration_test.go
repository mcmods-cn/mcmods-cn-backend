package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestPopularityExcludesEveryEffectiveDeveloperIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify multi-developer popularity exclusion")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	if _, err = pool.Exec(ctx, `
		insert into users(id,public_id,username,email,password_hash,status,security_score,created_at) values
			(957001,'ubug057a1','bug057_dev_one','bug057-dev-one@example.test','test','active',100,'2025-01-01'),
			(957002,'ubug057a2','bug057_dev_two','bug057-dev-two@example.test','test','active',100,'2025-01-01'),
			(957003,'ubug057a3','bug057_outside','bug057-outside@example.test','test','active',100,'2025-01-01'),
			(957004,'ubug057a4','bug057_review','bug057-review@example.test','test','active',100,'2025-01-01');
		insert into mods(id,project_code,slug,primary_name,review_status,submitted_by)
			values(957010,'pbug057a1','bug-057-project','BUG 057 project','approved',957004);
		insert into creators(id,public_id,kind,name,normalized_name,created_by,review_status) values
			(957020,'abug057a1','author','BUG 057 author one','bug 057 author one',957004,'approved'),
			(957021,'abug057a2','author','BUG 057 author two','bug 057 author two',957004,'approved');
		insert into creator_claims(id,public_id,creator_id,user_id,status,reviewed_by,reviewed_at) values
			(957030,'qbug057a1',957020,957001,'approved',957004,now()),
			(957031,'qbug057a2',957021,957002,'approved',957004,now());
		insert into content_creator_bindings(id,public_id,subject_type,subject_id,creator_id,
			name_snapshot,role_snapshot,status,permission_granting,approved_by,approved_at) values
			(957040,'bbug057a1','mod',957010,957020,'Author one','Developer','approved',true,957004,now()),
			(957041,'bbug057a2','mod',957010,957021,'Author two','Developer','approved',true,957004,now());
	`); err != nil {
		t.Fatalf("install multi-developer fixture: %v", err)
	}
	var routeID int64
	var developerCount int
	if err = pool.QueryRow(ctx, `select route.id,
		(select count(distinct access.user_id) from effective_project_access access
		 where access.project_type='mod' and access.project_id=957010 and access.access_level='developer')
		from public_routes route where route.entity_type='mod' and route.internal_id=957010`).
		Scan(&routeID, &developerCount); err != nil {
		t.Fatal(err)
	}
	if developerCount != 2 {
		t.Fatalf("effective developer fixture count=%d want 2", developerCount)
	}

	if _, err = pool.Exec(ctx, `
		insert into favorite_collections(id,public_id,user_id,name) values
			(957050,'fbug057a1',957001,'Developer one'),
			(957051,'fbug057a2',957002,'Developer two'),
			(957052,'fbug057a3',957003,'Outsider');
		insert into favorite_collection_items(id,collection_id,entity_type,entity_id) values
			(957060,957050,'mod',957010),(957061,957051,'mod',957010),(957062,957052,'mod',957010);
		insert into comments(id,public_id,target_type,target_id,author_id,body) values
			(957070,'cbug057a1','mod',957010,957001,'developer one'),
			(957071,'cbug057a2','mod',957010,957002,'developer two'),
			(957072,'cbug057a3','mod',957010,957003,'outsider')
	`); err != nil {
		t.Fatalf("record favorite/comment interactions: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		insert into content_ratings(id,public_id,object_route_id,author_id,overall_score) values
			(957080,'rbug057a1',$1,957001,3),(957081,'rbug057a2',$1,957002,4),(957082,'rbug057a3',$1,957003,5)
	`, routeID); err != nil {
		t.Fatalf("record rating interactions: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		insert into content_rating_scores(rating_id,dimension_code,score) values
			(957080,'quality',3),(957081,'quality',4),(957082,'quality',5)
	`); err != nil {
		t.Fatalf("record dimension interactions: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		insert into content_unique_views(object_route_id,viewer_hash,viewer_user_id) values
			($1,decode(md5('bug057-dev-one'),'hex'),957001),
			($1,decode(md5('bug057-dev-two'),'hex'),957002),
			($1,decode(md5('bug057-outsider'),'hex'),957003)
	`, routeID); err != nil {
		t.Fatalf("record unique-view interactions: %v", err)
	}

	assertMultiDeveloperPopularityFacts(t, ctx, pool, routeID, "incremental")
	if _, err = pool.Exec(ctx, `
		update content_popularity_lifetime_facts set unique_view_count=77,favorite_count=77,
			comment_count=77,effective_commenter_count=77,rating_count=77,rating_sum=77
		where object_route_id=$1
	`, routeID); err != nil {
		t.Fatalf("corrupt route popularity facts: %v", err)
	}
	if _, err = pool.Exec(ctx, `update content_rating_global_stats set rating_count=77,rating_sum=77,average_rating=1
		where entity_type='mod'`); err != nil {
		t.Fatalf("corrupt global popularity facts: %v", err)
	}
	if _, err = pool.Exec(ctx, `select pg_temp.rebuild_content_popularity_lifetime_facts($1::bigint)`, routeID); err != nil {
		t.Fatalf("rebuild route popularity facts: %v", err)
	}
	if _, err = pool.Exec(ctx, `select pg_temp.rebuild_content_rating_global_stats('mod')`); err != nil {
		t.Fatalf("rebuild global popularity facts: %v", err)
	}
	assertMultiDeveloperPopularityFacts(t, ctx, pool, routeID, "rebuild")
	t.Run("returning viewer after a project role changes", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		check := func(want int) {
			t.Helper()
			var incremental, actual int
			err := tx.QueryRow(ctx, `select facts.unique_view_count,(select count(*) from content_unique_views v
				where v.object_route_id=facts.object_route_id and content_popularity_unique_view_eligible(v.object_route_id,v.viewer_user_id))
				from content_popularity_lifetime_facts facts where facts.object_route_id=$1`, routeID).Scan(&incremental, &actual)
			if err != nil || incremental != want || actual != want {
				t.Fatalf("project unique views=%d source=%d, want %d: %v", incremental, actual, want, err)
			}
		}
		if _, err = tx.Exec(ctx, `update content_creator_bindings set permission_granting=false where id=957040`); err != nil {
			t.Fatal(err)
		}
		check(2)
		// This is the returning authenticated visitor upsert in recordContentRouteView.
		if _, err = tx.Exec(ctx, `insert into content_unique_views(object_route_id,viewer_hash,viewer_user_id)
			values ($1,decode(md5('bug057-dev-one'),'hex'),957001) on conflict(object_route_id,viewer_hash)
			do update set last_seen_at=now(),viewer_user_id=coalesce(content_unique_views.viewer_user_id,excluded.viewer_user_id)`, routeID); err != nil {
			t.Fatal(err)
		}
		check(2)
		if _, err = tx.Exec(ctx, `update content_creator_bindings set permission_granting=true where id=957040`); err != nil {
			t.Fatal(err)
		}
		check(1)
		if _, err = tx.Exec(ctx, `update creator_claims set status='revoked' where id=957030`); err != nil {
			t.Fatal(err)
		}
		check(2)
		if _, err = tx.Exec(ctx, `update users set security_score=30 where id=957003`); err != nil {
			t.Fatal(err)
		}
		check(2)
	})
	t.Run("existing ratings follow current eligibility", func(t *testing.T) {
		exec := func(query string) {
			t.Helper()
			if _, err := pool.Exec(ctx, query); err != nil {
				t.Fatal(err)
			}
		}
		assertTotals := func(count, total, dimensions int64) {
			t.Helper()
			var n, score, gn, gs, dn, ds int64
			err := pool.QueryRow(ctx, `select facts.rating_count,facts.rating_sum,global.rating_count,
				global.rating_sum,coalesce(dim.rating_count,0),coalesce(dim.score_sum,0)
				from content_popularity_lifetime_facts facts
				join content_rating_global_stats global on global.entity_type='mod'
				left join content_popularity_rating_dimension_facts dim on dim.object_route_id=facts.object_route_id
				  and dim.dimension_code='quality' where facts.object_route_id=$1`, routeID).
				Scan(&n, &score, &gn, &gs, &dn, &ds)
			if err != nil || n != count || gn != count || dn != count || score != total || gs != total || ds != dimensions {
				t.Fatalf("route=%d/%d global=%d/%d quality=%d/%d, want %d/%d quality %d: %v",
					n, score, gn, gs, dn, ds, count, total, dimensions, err)
			}
		}
		exec(`update users set security_score=30 where id=957003`)
		assertTotals(0, 0, 0)
		exec(`update content_rating_scores set score=2 where rating_id=957082 and dimension_code='quality'`)
		exec(`update users set security_score=80 where id=957003`)
		assertTotals(1, 5, 2)
		exec(`update content_creator_bindings set permission_granting=false where id=957040`)
		assertTotals(2, 8, 5)
		exec(`update content_creator_bindings set permission_granting=true where id=957040`)
		assertTotals(1, 5, 2)
		exec(`update creator_claims set status='revoked' where id=957030`)
		assertTotals(2, 8, 5)
		exec(`update creator_claims set status='approved' where id=957030`)
		assertTotals(1, 5, 2)
		exec(`update mods set review_status='pending' where id=957010`)
		assertTotals(3, 12, 9)
		exec(`update mods set review_status='approved' where id=957010`)
		assertTotals(1, 5, 2)
		exec(`update content_creator_bindings set permission_granting=false where id=957040;
			insert into creators(id,kind,name,normalized_name,created_by,review_status)
				values(957090,'team','Rating contributors','rating contributors',957004,'approved');
			insert into creator_team_members(team_id,member_creator_id,role_id,status)
				values(957090,957020,2,'pending');
			insert into content_creator_bindings(id,subject_type,subject_id,creator_id,
				name_snapshot,role_snapshot,status,permission_granting,approved_by,approved_at)
				values(957091,'mod',957010,957090,'Rating contributors','Developer','approved',true,957004,now())`)
		assertTotals(2, 8, 5)
		exec(`update creator_team_members set status='approved' where team_id=957090`)
		assertTotals(1, 5, 2)
		exec(`update creators set review_status='rejected' where id=957090`)
		assertTotals(2, 8, 5)
		exec(`update creators set review_status='approved' where id=957090`)
		assertTotals(1, 5, 2)
		exec(`insert into content_ratings(object_route_id,author_id,overall_score)
			select object_route_id,author_id,2 from content_ratings where id=957082
			on conflict(object_route_id,author_id) do nothing`)
		assertTotals(1, 5, 2)
		exec(`update content_ratings set status='hidden' where id=957082`)
		assertTotals(0, 0, 0)
		exec(`update content_ratings set status='published' where id=957082`)
		assertTotals(1, 5, 2)
		exec(`update users set security_score=30 where id=957003`)
		exec(`delete from content_ratings where id=957082`)
		assertTotals(0, 0, 0)
		var remainingWeight float64
		if err := pool.QueryRow(ctx, `select sum(rating_value)::double precision
			from content_popularity_events_daily where object_route_id=$1`, routeID).Scan(&remainingWeight); err != nil || remainingWeight != 0 {
			t.Fatalf("removed rating trend has residual weight %v: %v", remainingWeight, err)
		}
	})
}

func assertMultiDeveloperPopularityFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID int64, stage string) {
	t.Helper()
	var uniqueViews, favorites, comments, commenters, ratings, ratingSum int64
	var globalRatings, globalRatingSum int64
	var favoriteTrend, commentTrend, ratingTrend float64
	if err := pool.QueryRow(ctx, `select facts.unique_view_count,facts.favorite_count,facts.comment_count,
		facts.effective_commenter_count,facts.rating_count,facts.rating_sum,
		global.rating_count,global.rating_sum,
		trend.favorite_value::double precision,trend.comment_value::double precision,trend.rating_value::double precision
		from content_popularity_lifetime_facts facts
		join content_rating_global_stats global on global.entity_type='mod'
		join content_popularity_events_daily trend on trend.object_route_id=facts.object_route_id
		where facts.object_route_id=$1`, routeID).Scan(
		&uniqueViews, &favorites, &comments, &commenters, &ratings, &ratingSum,
		&globalRatings, &globalRatingSum, &favoriteTrend, &commentTrend, &ratingTrend,
	); err != nil {
		t.Fatalf("%s read popularity facts: %v", stage, err)
	}
	if uniqueViews != 1 || favorites != 1 || comments != 1 || commenters != 1 || ratings != 1 || ratingSum != 5 ||
		globalRatings != 1 || globalRatingSum != 5 || favoriteTrend != 4 || commentTrend != 3 || ratingTrend != 2 {
		t.Fatalf("%s facts unique=%d favorites=%d comments=%d commenters=%d ratings=%d/%d global=%d/%d trend=%v/%v/%v; want every developer excluded",
			stage, uniqueViews, favorites, comments, commenters, ratings, ratingSum, globalRatings, globalRatingSum,
			favoriteTrend, commentTrend, ratingTrend)
	}
}
