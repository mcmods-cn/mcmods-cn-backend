package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Only an explicitly supplied isolated test database is used. No application
// configuration or deployed credentials are read by these tests.
func TestCommunityPostTranslationLifecycleIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to an isolated migrated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	applicationName := "apia-community-translation-" + randomHex(8)
	poolConfig.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	worker := &AIWorker{db: pool}
	for _, test := range []struct {
		name, taskStatus, postStatus, reviewStatus, sourceBody, translatedBody, locale string
		concurrentChange                                                               string
		changedSource, missingBody, wantError                                          bool
	}{
		{name: "published", taskStatus: "running", postStatus: "active", reviewStatus: "approved", sourceBody: "Source body", translatedBody: "Translated body", locale: "en-US"},
		{name: "cancelled", taskStatus: "cancelled", postStatus: "active", reviewStatus: "approved", sourceBody: "Source body", translatedBody: "Translated body", locale: "en-US", wantError: true},
		{name: "completed", taskStatus: "completed", postStatus: "active", reviewStatus: "approved", sourceBody: "Source body", translatedBody: "Translated body", locale: "en-US", wantError: true},
		{name: "source-changed", taskStatus: "running", postStatus: "active", reviewStatus: "approved", sourceBody: "Source body", translatedBody: "Translated body", locale: "en-US", changedSource: true, wantError: true},
		{name: "source-changes-during-persistence", taskStatus: "running", postStatus: "active", reviewStatus: "approved", sourceBody: "Source body", translatedBody: "Translated body", locale: "en-US", concurrentChange: "source", wantError: true},
		{name: "cancelled-during-persistence", taskStatus: "running", postStatus: "active", reviewStatus: "approved", sourceBody: "Source body", translatedBody: "Translated body", locale: "en-US", concurrentChange: "task", wantError: true},
		{name: "deleted", taskStatus: "running", postStatus: "deleted", reviewStatus: "approved", sourceBody: "Source body", translatedBody: "Translated body", locale: "en-US", wantError: true},
		{name: "unapproved", taskStatus: "running", postStatus: "active", reviewStatus: "rejected", sourceBody: "Source body", translatedBody: "Translated body", locale: "en-US", wantError: true},
		{name: "title-only", taskStatus: "running", postStatus: "active", reviewStatus: "approved", locale: "en-US"},
		{name: "empty-translation", taskStatus: "running", postStatus: "active", reviewStatus: "approved", sourceBody: "Source body", locale: "en-US", wantError: true},
		{name: "missing-body", taskStatus: "running", postStatus: "active", reviewStatus: "approved", locale: "en-US", missingBody: true, wantError: true},
		{name: "invalid-locale", taskStatus: "running", postStatus: "active", reviewStatus: "approved", sourceBody: "Source body", translatedBody: "Translated body", locale: "$invalid", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			unique := "apia_translation_" + randomHex(8)
			var userID, revisionID, currentRevision, postID, taskID int64
			if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'fixture') returning id`, unique, unique+"@example.test").Scan(&userID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cleanupCancel()
				// Every target is a primary key allocated by this test. Immutable
				// content revisions intentionally remain in the disposable database:
				// its append-only trigger also applies to synthetic test records.
				for _, deletion := range []struct {
					sql string
					id  int64
				}{
					{`delete from community_posts where id=$1`, postID},
					{`delete from ai_tasks where id=$1`, taskID},
					{`delete from users where id=$1`, userID},
				} {
					if deletion.id > 0 {
						if _, err := pool.Exec(cleanupContext, deletion.sql, deletion.id); err != nil {
							t.Errorf("fixture cleanup failed: %v", err)
						}
					}
				}
			})
			if err := pool.QueryRow(ctx, `insert into content_revisions(aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash)
				values('community_post',$1,1,'{}','fixture') returning id`, unique).Scan(&revisionID); err != nil {
				t.Fatal(err)
			}
			currentRevision = revisionID
			if test.changedSource || test.concurrentChange == "source" {
				if err := pool.QueryRow(ctx, `insert into content_revisions(aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash)
					values('community_post',$1,2,'{}','fixture') returning id`, unique).Scan(&currentRevision); err != nil {
					t.Fatal(err)
				}
			}
			initialRevision := currentRevision
			if test.concurrentChange == "source" {
				initialRevision = revisionID
			}
			if err := pool.QueryRow(ctx, `insert into community_posts(kind,category,author_id,title,source_locale,body_markdown,
				status,review_status,published_revision_id) values('discussion','help',$1,'Source title','zh-CN',$2,$3,$4,$5) returning id`,
				userID, test.sourceBody, test.postStatus, test.reviewStatus, initialRevision).Scan(&postID); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type,status,created_by) values($1,$2,$3,$4) returning id`,
				unique, aiTaskContentTranslation, test.taskStatus, userID).Scan(&taskID); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(map[string]any{"postInternalId": postID, "sourceRevisionId": revisionID, "targetLocale": test.locale})
			if err != nil {
				t.Fatal(err)
			}
			items := []any{map[string]any{"key": "title", "text": "Translated title"}}
			if !test.missingBody {
				items = append(items, map[string]any{"key": "bodyMarkdown", "text": test.translatedBody})
			}
			result := map[string]any{"items": items}
			if test.concurrentChange == "" {
				err = worker.persistCommunityPostTranslation(ctx, taskID, payload, result)
			} else {
				change, beginErr := pool.Begin(ctx)
				if beginErr != nil {
					t.Fatal(beginErr)
				}
				defer change.Rollback(ctx)
				if test.concurrentChange == "source" {
					_, err = change.Exec(ctx, `update community_posts set published_revision_id=$2 where id=$1`, postID, currentRevision)
				} else {
					_, err = change.Exec(ctx, `update ai_tasks set status='cancelled' where id=$1`, taskID)
				}
				if err != nil {
					t.Fatal(err)
				}
				finished := make(chan error, 1)
				go func() { finished <- worker.persistCommunityPostTranslation(ctx, taskID, payload, result) }()
				// Observe PostgreSQL's actual lock wait before committing the
				// concurrent change; elapsed time is not proof of serialization.
				deadline := time.Now().Add(3 * time.Second)
				for {
					select {
					case early := <-finished:
						t.Fatalf("persistence completed before the concurrent row lock was released: %v", early)
					default:
					}
					var waiting bool
					if queryErr := pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity
						where application_name=$1 and pid<>pg_backend_pid() and wait_event_type='Lock')`, applicationName).Scan(&waiting); queryErr != nil {
						t.Fatal(queryErr)
					}
					if waiting {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("persistence did not wait for the concurrent row lock")
					}
					time.Sleep(5 * time.Millisecond)
				}
				if err = change.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case err = <-finished:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if (err != nil) != test.wantError {
				t.Fatalf("persistence error=%v, wantError=%v", err, test.wantError)
			}
			var count int
			if err := pool.QueryRow(ctx, `select count(*) from community_post_translations where post_id=$1`, postID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if test.wantError {
				if count != 0 {
					t.Fatalf("rejected translation persisted %d rows", count)
				}
				return
			}
			if count != 1 {
				t.Fatalf("valid translation persisted %d rows", count)
			}
			var finalStatus string
			if err := pool.QueryRow(ctx, `select status from ai_tasks where id=$1`, taskID).Scan(&finalStatus); err != nil || finalStatus != "completed" {
				t.Fatalf("publication and completion were not committed together: status=%q err=%v", finalStatus, err)
			}
			if err := worker.persistCommunityPostTranslation(ctx, taskID, payload, result); err == nil {
				t.Fatal("completed task accepted a duplicate late publication")
			}
			var title, body string
			if err := pool.QueryRow(ctx, `select title,body_markdown from community_post_translations where post_id=$1 and locale=$2`, postID, test.locale).Scan(&title, &body); err != nil {
				t.Fatal(err)
			}
			if title != "Translated title" || body != test.translatedBody || strings.TrimSpace(title) == "" {
				t.Fatalf("unexpected persisted title=%q body=%q", title, body)
			}
		})
	}
}
