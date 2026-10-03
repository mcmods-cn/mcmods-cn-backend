package database

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestOCT02CommentPopularityStatementDeltas(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || os.Getenv("MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET") == "" {
		t.Skip("requires an explicitly confirmed owned function-repair test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := Connect(ctx, config.Load())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	original, _, err := InspectProjectionFunctions(ctx, pool)
	if err != nil || original.Database != os.Getenv("MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET") {
		t.Fatal("test database identity does not match the explicitly owned target")
	}
	if err := ChangeProjectionFunctions(ctx, pool, original.Database, nil, func(ProjectionFunctionBackup) error { return nil }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := ChangeProjectionFunctions(cleanupCtx, pool, original.Database, &original, func(ProjectionFunctionBackup) error { return nil }); err != nil {
			t.Errorf("restore owned function definitions: %v", err)
		}
	})
	var author, other, route, otherRoute int64
	for index, destination := range []*int64{&author, &other} {
		if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,security_score,created_at)
			values('statement_'||new_public_id(),new_public_id()||'@example.invalid','fixture',100,'2025-01-01') returning id`).Scan(destination); err != nil {
			t.Fatalf("create synthetic author %d: %v", index, err)
		}
	}
	for index, destination := range []*int64{&route, &otherRoute} {
		if err := pool.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id)
			values(new_public_id(),'modpack',$1) returning id`, -[]int64{author, other}[index]).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, operation := range []struct {
			sql      string
			argument []int64
		}{
			{`delete from comments where target_type='modpack' and target_id=any($1)`, []int64{-author, -other}},
			{`delete from public_routes where id=any($1)`, []int64{route, otherRoute}},
			{`delete from users where id=any($1)`, []int64{author, other}},
		} {
			if _, err := pool.Exec(cleanupCtx, operation.sql, operation.argument); err != nil {
				t.Errorf("remove exclusively owned fixture rows: %v", err)
			}
		}
	})
	execute := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	assert := func(target int64, count, authors int64) {
		t.Helper()
		var actualCount, actualAuthors int64
		if err := pool.QueryRow(ctx, `select comment_count,effective_commenter_count from content_popularity_lifetime_facts
			where object_route_id=$1`, target).Scan(&actualCount, &actualAuthors); err != nil {
			t.Fatal(err)
		}
		if actualCount != count || actualAuthors != authors {
			t.Fatalf("route %d comments/authors=%d/%d, want %d/%d", target, actualCount, actualAuthors, count, authors)
		}
	}
	t.Run("batch first contribution and harmless body updates", func(t *testing.T) {
		execute(`insert into comments(target_type,target_id,author_id,body) values('modpack',$1,$2,'one'),('modpack',$1,$2,'two')`, -author, author)
		assert(route, 2, 1)
		execute(`update comments set body=body||' edited' where target_type='modpack' and target_id=$1`, -author)
		assert(route, 2, 1)
	})
	t.Run("different author and partial deletion", func(t *testing.T) {
		execute(`insert into comments(target_type,target_id,author_id,body) values('modpack',$1,$2,'other')`, -author, other)
		assert(route, 3, 2)
		execute(`delete from comments where id=(select min(id) from comments where target_type='modpack' and target_id=$1 and author_id=$2)`, -author, author)
		assert(route, 2, 2)
	})
	t.Run("batch hide repeat restore and delete", func(t *testing.T) {
		execute(`update comments set status='hidden' where target_type='modpack' and target_id=$1`, -author)
		assert(route, 0, 0)
		execute(`update comments set status='hidden' where target_type='modpack' and target_id=$1`, -author)
		assert(route, 0, 0)
		execute(`update comments set status='published' where target_type='modpack' and target_id=$1`, -author)
		assert(route, 2, 2)
		execute(`delete from comments where target_type='modpack' and target_id=$1`, -author)
		assert(route, 0, 0)
	})
	t.Run("different target and single statement final author removal", func(t *testing.T) {
		execute(`insert into comments(target_type,target_id,author_id,body) values('modpack',$1,$2,'one'),('modpack',$1,$2,'two')`, -other, author)
		assert(route, 0, 0)
		assert(otherRoute, 2, 1)
		execute(`delete from comments where target_type='modpack' and target_id=$1`, -other)
		assert(otherRoute, 0, 0)
	})
	t.Run("moving target and author maintains both route facts", func(t *testing.T) {
		execute(`insert into comments(target_type,target_id,author_id,body) values('modpack',$1,$2,'move')`, -author, author)
		assert(route, 1, 1)
		execute(`update comments set target_id=$2,author_id=$3 where target_type='modpack' and target_id=$1`, -author, -other, other)
		assert(route, 0, 0)
		assert(otherRoute, 1, 1)
		execute(`delete from comments where target_type='modpack' and target_id=$1`, -other)
		assert(otherRoute, 0, 0)
	})
	for _, target := range []int64{route, otherRoute} {
		var trend float64
		if err := pool.QueryRow(ctx, `select coalesce(sum(comment_value),0)::double precision from content_popularity_events_daily where object_route_id=$1`, target).Scan(&trend); err != nil {
			t.Fatal(err)
		}
		if math.Abs(trend) > 0.000001 {
			t.Fatal(fmt.Sprintf("first/last contribution events did not balance: %v", trend))
		}
	}
}
