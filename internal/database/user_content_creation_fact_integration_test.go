package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestUserContentCreationFactsFollowAuthoritativeActorLifecycleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify creation fact trigger behavior")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop ephemeral schema: %v", err)
		}
	}()

	var firstActorID, secondActorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('creation_fact_a','creation_fact_a@example.invalid','test-only',true) returning id`).Scan(&firstActorID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('creation_fact_b','creation_fact_b@example.invalid','test-only',true) returning id`).Scan(&secondActorID); err != nil {
		t.Fatal(err)
	}

	createdAt := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	testCases := []struct {
		name        string
		contentType string
		objectKey   string
		insertSQL   string
		updateSQL   string
		deleteSQL   string
		softDelete  string
		restore     string
	}{
		{
			name: "mod", contentType: "mod", objectKey: "modfact01",
			insertSQL: `insert into mods(project_code,slug,primary_name,review_status,submitted_by,created_at)
				values('modfact01','creation-fact-mod','Creation fact Mod','pending',$1,$2)`,
			updateSQL: `update mods set review_status='approved',submitted_by=$1 where project_code='modfact01'`,
			deleteSQL: `delete from mods where project_code='modfact01'`,
		},
		{
			name: "modpack", contentType: "modpack", objectKey: "packfact1",
			insertSQL: `insert into modpacks(public_id,slug,primary_name,review_status,submitted_by,created_at)
				values('packfact1','creation-fact-pack','Creation fact pack','pending',$1,$2)`,
			updateSQL: `update modpacks set review_status='approved',submitted_by=$1 where public_id='packfact1'`,
			deleteSQL: `delete from modpacks where public_id='packfact1'`,
		},
		{
			name: "simple_project", contentType: "plugin", objectKey: "plugfact1",
			insertSQL: `insert into simple_projects(public_id,project_type,slug,primary_name,review_status,submitted_by,created_at)
				values('plugfact1','plugin','creation-fact-plugin','Creation fact plugin','pending',$1,$2)`,
			updateSQL: `update simple_projects set review_status='approved',submitted_by=$1 where public_id='plugfact1'`,
			deleteSQL: `delete from simple_projects where public_id='plugfact1'`,
		},
		{
			name: "minecraft_server", contentType: "server", objectKey: "servfact1",
			insertSQL: `insert into minecraft_servers(public_id,slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,review_status,submitted_by,created_at)
				values('servfact1','creation-fact-server','play.example.invalid','play.example.invalid:25565','play.example.invalid','play.example.invalid',25565,'Creation fact server','survival','pending',$1,$2)`,
			updateSQL: `update minecraft_servers set review_status='approved',submitted_by=$1 where public_id='servfact1'`,
			deleteSQL: `delete from minecraft_servers where public_id='servfact1'`,
		},
		{
			name: "community_post", contentType: "discussion", objectKey: "postfact1",
			insertSQL: `insert into community_posts(public_id,kind,category,author_id,title,source_locale,review_status,created_at)
				values('postfact1','discussion','general',$1,'Creation fact post','zh-CN','pending',$2)`,
			updateSQL:  `update community_posts set review_status='approved',author_id=$1 where public_id='postfact1'`,
			softDelete: `update community_posts set status='deleted' where public_id='postfact1'`,
			restore:    `update community_posts set status='active' where public_id='postfact1'`,
			deleteSQL:  `delete from community_posts where public_id='postfact1'`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, testCase.insertSQL, firstActorID, createdAt); err != nil {
				t.Fatalf("insert source: %v", err)
			}
			assertUserContentCreationFact(t, ctx, pool, testCase.contentType, testCase.objectKey, firstActorID, "pending", true, false, createdAt)

			if _, err := pool.Exec(ctx, testCase.updateSQL, secondActorID); err != nil {
				t.Fatalf("update source: %v", err)
			}
			assertUserContentCreationFact(t, ctx, pool, testCase.contentType, testCase.objectKey, secondActorID, "approved", true, false, createdAt)

			if testCase.softDelete != "" {
				if _, err := pool.Exec(ctx, testCase.softDelete); err != nil {
					t.Fatalf("soft-delete source: %v", err)
				}
				assertUserContentCreationFact(t, ctx, pool, testCase.contentType, testCase.objectKey, secondActorID, "approved", false, true, createdAt)
				if _, err := pool.Exec(ctx, testCase.restore); err != nil {
					t.Fatalf("restore source: %v", err)
				}
				assertUserContentCreationFact(t, ctx, pool, testCase.contentType, testCase.objectKey, secondActorID, "approved", true, false, createdAt)
			}

			if _, err := pool.Exec(ctx, testCase.deleteSQL); err != nil {
				t.Fatalf("delete source: %v", err)
			}
			assertUserContentCreationFact(t, ctx, pool, testCase.contentType, testCase.objectKey, secondActorID, "approved", false, true, createdAt)
		})
	}
}

func assertUserContentCreationFact(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	contentType string,
	objectKey string,
	wantUserID int64,
	wantReviewStatus string,
	wantExists bool,
	wantDeleted bool,
	wantCreatedAt time.Time,
) {
	t.Helper()
	var userID int64
	var reviewStatus string
	var exists bool
	var createdAt time.Time
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx, `select user_id,review_status,current_exists,created_at,deleted_at
		from user_content_creation_facts where content_type=$1 and object_key=$2`, contentType, objectKey).
		Scan(&userID, &reviewStatus, &exists, &createdAt, &deletedAt); err != nil {
		t.Fatalf("read creation fact: %v", err)
	}
	if userID != wantUserID || reviewStatus != wantReviewStatus || exists != wantExists || (deletedAt != nil) != wantDeleted || !createdAt.Equal(wantCreatedAt) {
		t.Fatalf("creation fact = user %d status %q exists %t created %s deleted %v; want user %d status %q exists %t created %s deleted %t",
			userID, reviewStatus, exists, createdAt, deletedAt, wantUserID, wantReviewStatus, wantExists, wantCreatedAt, wantDeleted)
	}
}
