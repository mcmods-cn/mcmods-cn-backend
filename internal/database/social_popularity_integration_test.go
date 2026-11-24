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
