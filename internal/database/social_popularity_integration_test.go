package database

import "testing"

func TestSocialPopularityFollowsEligibilityAndReversesActualInteractions(t *testing.T) {
	ctx, pool := newOCT02ProjectionPool(t)
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`insert into users(id,username,email,password_hash,security_score) values(912341,'social-facts','social@example.invalid','unused',30);
		insert into mods(id,project_code,slug,primary_name,review_status) values(912341,'socfact01','social-facts','Social facts','approved');
		insert into favorite_collections(id,user_id,name) values(912342,912341,'first'),(912343,912341,'second');
		insert into favorite_collection_items(collection_id,entity_type,entity_id) values(912342,'mod',912341),(912343,'mod',912341);
		insert into comments(id,target_type,target_id,author_id,body) values(912344,'mod',912341,912341,'One'),(912345,'mod',912341,912341,'Two')`)
	assert := func(comments, commenters, favorites int) {
		t.Helper()
		var c, u, f int
		err := pool.QueryRow(ctx, `select coalesce(facts.comment_count,0),coalesce(facts.effective_commenter_count,0),coalesce(facts.favorite_count,0)
			from public_routes route left join content_popularity_lifetime_facts facts on facts.object_route_id=route.id
			where route.entity_type='mod' and route.internal_id=912341`).Scan(&c, &u, &f)
		if err != nil || c != comments || u != commenters || f != favorites {
			t.Fatalf("comments/commenters/favorites=%d/%d/%d, want %d/%d/%d, err=%v", c, u, f, comments, commenters, favorites, err)
		}
	}
	assert(0, 0, 0)
	exec(`update users set security_score=80 where id=912341`)
	assert(2, 1, 1)
	exec(`select pg_temp.rebuild_content_popularity_lifetime_facts(id) from public_routes where entity_type='mod' and internal_id=912341`)
	assert(2, 1, 1)
	exec(`update comments set status='hidden' where id=912344; delete from favorite_collections where id=912342`)
	assert(1, 1, 1)
	exec(`update users set status='banned' where id=912341`)
	assert(0, 0, 0)
	exec(`update users set status='active' where id=912341`)
	assert(1, 1, 1)
	exec(`delete from comments where id in (912344,912345); delete from favorite_collection_items where collection_id=912343`)
	assert(0, 0, 0)
	var balanced bool
	if err := pool.QueryRow(ctx, `select coalesce(sum(comment_value),0)=0 and coalesce(sum(favorite_value),0)=0
		from content_popularity_events_daily where object_route_id=(select id from public_routes where entity_type='mod' and internal_id=912341)`).Scan(&balanced); err != nil || !balanced {
		t.Fatalf("comment/favorite trend did not reverse the original weights: balanced=%v err=%v", balanced, err)
	}
}

func TestSocialEligibilityChangesDuringInteractionIntegration(t *testing.T) {
	ctx, pool := newPopularityConcurrencyPool(t)
	if _, err := pool.Exec(ctx, `insert into users(id,username,email,password_hash,security_score)
		values(61,'social_concurrency','social-concurrency@example.invalid','fixture',30);
		insert into mods(id,project_code,slug,primary_name,review_status) values(62,'socialcon','concurrent-social','Social','approved');
		insert into favorite_collections(id,user_id,name) values(63,61,'Collection')`); err != nil {
		t.Fatal(err)
	}

	assert := func(counted bool, comments, favorites int) {
		t.Helper()
		var applied bool
		var commentCount, commenters, favoriteCount int
		err := pool.QueryRow(ctx, `select receipt.counted, lifetime.comment_count,lifetime.effective_commenter_count,lifetime.favorite_count
			from content_social_contributions receipt join content_popularity_lifetime_facts lifetime using(object_route_id)
			where receipt.actor_id=61`).Scan(&applied, &commentCount, &commenters, &favoriteCount)
		if err != nil || applied != counted || commentCount != comments || commenters != comments || favoriteCount != favorites {
			t.Fatalf("applied=%v comments/commenters/favorites=%d/%d/%d, want %v %d/%d/%d: %v", applied, commentCount, commenters, favoriteCount, counted, comments, comments, favorites, err)
		}
	}
	t.Run("first membership waits for current account", func(t *testing.T) {
		runPopularityLockOrder(t, ctx, pool, `update users set security_score=80 where id=61`,
			`insert into favorite_collection_items(collection_id,entity_type,entity_id) values(63,'mod',62)`, `select 1`)
		assert(true, 0, 1)
	})
	t.Run("account change waits for published comment", func(t *testing.T) {
		runPopularityLockOrder(t, ctx, pool, `insert into comments(target_type,target_id,author_id,body) values('mod',62,61,'One')`,
			`update users set security_score=30 where id=61`, `select 1`)
		assert(false, 0, 0)
	})
	if _, err := pool.Exec(ctx, `update users set security_score=80 where id=61`); err != nil {
		t.Fatal(err)
	}
	assert(true, 1, 1)
}

func TestCommentPopularityTransfersTheCorrectAuthorContributionIntegration(t *testing.T) {
	ctx, pool := newPopularityConcurrencyPool(t)
	if _, err := pool.Exec(ctx, `insert into users(id,username,email,password_hash,security_score) values
		(61,'first_author','first@example.invalid','fixture',80),(62,'second_author','second@example.invalid','fixture',80);
		insert into mods(id,project_code,slug,primary_name,review_status) values(63,'authorcnt','author-count','Authors','approved');
		insert into comments(id,target_type,target_id,author_id,body) values
		(64,'mod',63,61,'First'),(65,'mod',63,61,'Second'),(66,'mod',63,62,'Third');
		update comments set author_id=62 where id=64`); err != nil {
		t.Fatal(err)
	}
	var first, second, total, authors int
	if err := pool.QueryRow(ctx, `select first.published_comments,second.published_comments,facts.comment_count,facts.effective_commenter_count
		from public_routes route join content_popularity_lifetime_facts facts on facts.object_route_id=route.id
		join content_social_contributions first on first.object_route_id=route.id and first.actor_id=61
		join content_social_contributions second on second.object_route_id=route.id and second.actor_id=62
		where route.entity_type='mod' and route.internal_id=63`).Scan(&first, &second, &total, &authors); err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 2 || total != 3 || authors != 2 {
		t.Fatalf("stored author contributions=%d/%d, total/distinct=%d/%d; want 1/2 and 3/2", first, second, total, authors)
	}
}
