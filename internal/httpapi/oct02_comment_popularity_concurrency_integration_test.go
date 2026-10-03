package httpapi

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise installed comment/popularity triggers on owned synthetic rows. No
// production target or replacement counter implementation is used.
func TestOCT02ConcurrentFirstCommentsCountOneEffectiveAuthorIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MCMODS_TEST_DATABASE_URL required")
	}
	target, err := url.Parse(dsn)
	if err != nil || (target.Hostname() != "127.0.0.1" && target.Hostname() != "localhost") {
		t.Fatal("comment fixture requires loopback isolated test database")
	}
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pc.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, tc := range []struct {
		name            string
		delete          bool
		differentAuthor bool
		differentTarget bool
	}{
		{name: "first_same_author_target"},
		{name: "last_same_author_target", delete: true},
		{name: "first_different_authors", differentAuthor: true},
		{name: "first_different_targets", differentTarget: true},
		{name: "first_different_authors_and_targets", differentAuthor: true, differentTarget: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			suffix := randomHex(8)
			var authors, routeIDs, targets []int64
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cleanupCancel()
				for _, targetID := range targets {
					if _, e := pool.Exec(cleanupCtx, `delete from comments where target_type='modpack' and target_id=$1 and author_id=any($2)`, targetID, authors); e != nil {
						t.Errorf("fixture comment cleanup: %v", e)
					}
				}
				if _, e := pool.Exec(cleanupCtx, `delete from public_routes where id=any($1) and internal_id=any($2)`, routeIDs, targets); e != nil {
					t.Errorf("fixture route cleanup: %v", e)
				}
				if _, e := pool.Exec(cleanupCtx, `delete from users where id=any($1) and username like $2`, authors, "oct02_comment_"+suffix+"_%"); e != nil {
					t.Errorf("fixture user cleanup: %v", e)
				}
			}()
			for _, label := range []string{"a", "b"} {
				var author, route int64
				if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,status,security_score) values($1,$2,'fixture-only','active',100) returning id`, "oct02_comment_"+suffix+"_"+label, suffix+label+"@example.test").Scan(&author); err != nil {
					t.Fatal(err)
				}
				authors = append(authors, author)
				targetID := -author // No real project is referenced.
				if err := pool.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id) values(new_public_id(),'modpack',$1) returning id`, targetID).Scan(&route); err != nil {
					t.Fatal(err)
				}
				targets, routeIDs = append(targets, targetID), append(routeIDs, route)
			}
			secondAuthor, secondTarget := authors[0], targets[0]
			if tc.differentAuthor {
				secondAuthor = authors[1]
			}
			if tc.differentTarget {
				secondTarget = targets[1]
			}
			var deletedIDs []int64
			if tc.delete {
				for _, label := range []string{"first", "second"} {
					var id int64
					if err := pool.QueryRow(ctx, `insert into comments(target_type,target_id,author_id,body,idempotency_key) values('modpack',$1,$2,$3,$4) returning id`, targets[0], authors[0], label, label+suffix).Scan(&id); err != nil {
						t.Fatal(err)
					}
					deletedIDs = append(deletedIDs, id)
				}
			}
			first, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer first.Rollback(context.Background())
			if tc.delete {
				_, err = first.Exec(ctx, `delete from comments where id=$1`, deletedIDs[0])
			} else {
				_, err = first.Exec(ctx, `insert into comments(target_type,target_id,author_id,body,idempotency_key) values('modpack',$1,$2,'first',$3)`, targets[0], authors[0], "first-"+suffix)
			}
			if err != nil {
				t.Fatal(err)
			}
			second, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Rollback(context.Background())
			var secondPID int
			if err := second.QueryRow(ctx, `select pg_backend_pid()`).Scan(&secondPID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				var e error
				if tc.delete {
					_, e = second.Exec(ctx, `delete from comments where id=$1`, deletedIDs[1])
				} else {
					_, e = second.Exec(ctx, `insert into comments(target_type,target_id,author_id,body,idempotency_key) values('modpack',$1,$2,'second',$3)`, secondTarget, secondAuthor, "second-"+suffix)
				}
				done <- e
			}()
			if !tc.differentTarget {
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
				for {
					var waits bool
					if err := pool.QueryRow(ctx, `select coalesce(wait_event_type='Lock',false) from pg_stat_activity where pid=$1`, secondPID).Scan(&waits); err != nil {
						t.Fatal(err)
					}
					if waits {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("second mutation never reached held transaction lock")
					case <-ticker.C:
					}
				}
			}
			if err := first.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if err := second.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			for i, route := range routeIDs {
				var count, effective int64
				if err := pool.QueryRow(ctx, `select coalesce((select comment_count from content_popularity_lifetime_facts where object_route_id=$1),0),coalesce((select effective_commenter_count from content_popularity_lifetime_facts where object_route_id=$1),0)`, route).Scan(&count, &effective); err != nil {
					t.Fatal(err)
				}
				wantCount, wantAuthors := int64(0), int64(0)
				if !tc.delete && (i == 0 || tc.differentTarget) {
					wantCount, wantAuthors = 2, 1
					if tc.differentTarget {
						wantCount = 1
					} else if tc.differentAuthor {
						wantAuthors = 2
					}
				}
				if count != wantCount || effective != wantAuthors {
					t.Fatalf("route %d comments/effective authors=%d/%d want %d/%d", i, count, effective, wantCount, wantAuthors)
				}
			}
		})
	}
}
