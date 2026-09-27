package database

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestDerivedProjectAccessIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate current schema: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var nonce string
	if err = tx.QueryRow(ctx, `select new_public_id()`).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	var claimantID, otherUserID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test-only',true) returning id`, "claimant_"+nonce, nonce+"@claim.test").Scan(&claimantID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test-only',true) returning id`, "reviewer_"+nonce, nonce+"@review.test").Scan(&otherUserID); err != nil {
		t.Fatal(err)
	}

	var authorID, secondAuthorID, teamID, developerRoleID int64
	if err = tx.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status,created_by)
		values('author',$1,$2,'approved',$3) returning id`, "Author "+nonce, "author-"+nonce, otherUserID).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status,created_by)
		values('author',$1,$2,'approved',$3) returning id`, "Second "+nonce, "second-"+nonce, otherUserID).Scan(&secondAuthorID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status,created_by)
		values('team',$1,$2,'approved',$3) returning id`, "Team "+nonce, "team-"+nonce, otherUserID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select id from creator_role_definitions where code='developer'`).Scan(&developerRoleID); err != nil {
		t.Fatal(err)
	}

	var authBefore, permissionBefore int64
	if err = tx.QueryRow(ctx, `select auth_version,permission_version from users where id=$1`, claimantID).
		Scan(&authBefore, &permissionBefore); err != nil {
		t.Fatal(err)
	}
	// Administrators may retain an audited emergency path through direct RBAC.
	// It is independent from the derived authorship/editor relationships and,
	// like every ordinary authorization change, must not revoke the session.
	var manualPermissionID int64
	if err = tx.QueryRow(ctx, `insert into permissions(code,module,name,access_type)
		values($1,'project','Emergency project edit','administration') returning id`,
		"project.edit."+nonce).Scan(&manualPermissionID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into user_permissions(user_id,permission_id,allow)
		values($1,$2,true)`, claimantID, manualPermissionID); err != nil {
		t.Fatal(err)
	}
	permissionBefore++
	assertUserVersions(t, ctx, tx, claimantID, authBefore, permissionBefore)
	if _, err = tx.Exec(ctx, `delete from user_permissions where user_id=$1 and permission_id=$2`,
		claimantID, manualPermissionID); err != nil {
		t.Fatal(err)
	}
	permissionBefore++
	assertUserVersions(t, ctx, tx, claimantID, authBefore, permissionBefore)

	var claimID int64
	if err = tx.QueryRow(ctx, `insert into creator_claims(creator_id,user_id,proof_markdown)
		values($1,$2,'proof') returning id`, authorID, claimantID).Scan(&claimID); err != nil {
		t.Fatal(err)
	}
	assertUserVersions(t, ctx, tx, claimantID, authBefore, permissionBefore)
	if _, err = tx.Exec(ctx, `update creator_claims set status='approved',reviewed_by=$2,reviewed_at=now()
		where id=$1`, claimID, otherUserID); err != nil {
		t.Fatal(err)
	}
	permissionBefore++
	assertUserVersions(t, ctx, tx, claimantID, authBefore, permissionBefore)

	// A user may independently claim more than one personal author.
	if _, err = tx.Exec(ctx, `insert into creator_claims(creator_id,user_id,proof_markdown,status,reviewed_by,reviewed_at)
		values($1,$2,'second proof','approved',$3,now())`, secondAuthorID, claimantID, otherUserID); err != nil {
		t.Fatalf("one user must be able to claim multiple authors: %v", err)
	}
	permissionBefore++
	assertUserVersions(t, ctx, tx, claimantID, authBefore, permissionBefore)

	// A team is not a claimable identity, even if a caller bypasses the HTTP handler.
	if _, err = tx.Exec(ctx, `savepoint reject_team_claim`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into creator_claims(creator_id,user_id,proof_markdown)
		values($1,$2,'invalid')`, teamID, claimantID); err == nil {
		t.Fatal("database accepted a team claim")
	}
	if _, err = tx.Exec(ctx, `rollback to savepoint reject_team_claim`); err != nil {
		t.Fatal(err)
	}

	// The partial unique index permits competing pending evidence but only one
	// approved claimant for a personal author.
	var competingClaimID int64
	if err = tx.QueryRow(ctx, `insert into creator_claims(creator_id,user_id,proof_markdown)
		values($1,$2,'competing proof') returning id`, authorID, otherUserID).Scan(&competingClaimID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `savepoint reject_second_approval`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update creator_claims set status='approved',reviewed_at=now() where id=$1`, competingClaimID); err == nil {
		t.Fatal("database accepted two approved users for one author")
	}
	if _, err = tx.Exec(ctx, `rollback to savepoint reject_second_approval`); err != nil {
		t.Fatal(err)
	}

	projectOneID, projectOneRouteID := insertApprovedAccessTestMod(t, ctx, tx, nonce+"1", otherUserID)
	projectTwoID, _ := insertApprovedAccessTestMod(t, ctx, tx, nonce+"2", otherUserID)
	var projectACLBefore int64
	if err = tx.QueryRow(ctx, `select version from runtime_versions where name='project_acl'`).Scan(&projectACLBefore); err != nil {
		t.Fatal(err)
	}
	var directBindingID int64
	if err = tx.QueryRow(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,
		name_snapshot,role_snapshot,status,permission_granting,approved_by,approved_at)
		values('mod',$1,$2,$3,'Author','Developer','approved',true,$4,now()) returning id`,
		projectOneID, authorID, developerRoleID, otherUserID).Scan(&directBindingID); err != nil {
		t.Fatal(err)
	}
	assertProjectAccessCount(t, ctx, tx, claimantID, projectOneID, "developer", 1)
	var projectACLAfter int64
	if err = tx.QueryRow(ctx, `select version from runtime_versions where name='project_acl'`).Scan(&projectACLAfter); err != nil {
		t.Fatal(err)
	}
	if projectACLAfter <= projectACLBefore {
		t.Fatalf("project ACL version did not advance: before=%d after=%d", projectACLBefore, projectACLAfter)
	}
	assertUserVersions(t, ctx, tx, claimantID, authBefore, permissionBefore)

	if _, err = tx.Exec(ctx, `insert into creator_team_members(team_id,member_creator_id,role_id,title,status,
		created_by,approved_by,approved_at) values($1,$2,$3,'Developer','approved',$4,$4,now())`,
		teamID, authorID, developerRoleID, otherUserID); err != nil {
		t.Fatal(err)
	}
	var teamBindingOneID int64
	if err = tx.QueryRow(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,
		name_snapshot,role_snapshot,status,permission_granting,approved_by,approved_at)
		values('mod',$1,$2,$3,'Team','Developer','approved',true,$4,now()) returning id`,
		projectOneID, teamID, developerRoleID, otherUserID).Scan(&teamBindingOneID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,
		name_snapshot,role_snapshot,status,permission_granting,approved_by,approved_at)
		values('mod',$1,$2,$3,'Team','Developer','approved',true,$4,now())`,
		projectTwoID, teamID, developerRoleID, otherUserID); err != nil {
		t.Fatal(err)
	}
	assertProjectAccessCount(t, ctx, tx, claimantID, projectTwoID, "developer", 1)

	var applicationID int64
	if err = tx.QueryRow(ctx, `insert into project_editor_applications(target_route_id,user_id,proof_markdown,status,
		reviewed_by,reviewed_at) values($1,$2,'editor proof','approved',$3,now()) returning id`,
		projectOneRouteID, claimantID, otherUserID).Scan(&applicationID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into project_editor_assignments(target_route_id,user_id,application_id,granted_by)
		values($1,$2,$3,$4)`, projectOneRouteID, claimantID, applicationID, otherUserID); err != nil {
		t.Fatal(err)
	}
	permissionBefore++
	assertUserVersions(t, ctx, tx, claimantID, authBefore, permissionBefore)
	assertProjectAccessCount(t, ctx, tx, claimantID, projectOneID, "", 3)

	// Revoking one source leaves the other independent sources effective.
	if _, err = tx.Exec(ctx, `update content_creator_bindings set status='revoked' where id=$1`, directBindingID); err != nil {
		t.Fatal(err)
	}
	assertProjectAccessCount(t, ctx, tx, claimantID, projectOneID, "", 2)
	if _, err = tx.Exec(ctx, `update content_creator_bindings set status='revoked' where id=$1`, teamBindingOneID); err != nil {
		t.Fatal(err)
	}
	assertProjectAccessCount(t, ctx, tx, claimantID, projectOneID, "", 1)
	if _, err = tx.Exec(ctx, `update project_editor_assignments set status='revoked',revoked_at=now()
		where target_route_id=$1 and user_id=$2`, projectOneRouteID, claimantID); err != nil {
		t.Fatal(err)
	}
	permissionBefore++
	assertUserVersions(t, ctx, tx, claimantID, authBefore, permissionBefore)
	assertProjectAccessCount(t, ctx, tx, claimantID, projectOneID, "", 0)
}

func TestConcurrentAuthorClaimApprovalIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var nonce string
	if err = pool.QueryRow(ctx, `select new_public_id()`).Scan(&nonce); err != nil {
		t.Fatal(err)
	}
	userIDs := make([]int64, 2)
	for index := range userIDs {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
			values($1,$2,'test-only',true) returning id`, fmt.Sprintf("claim_race_%d_%s", index, nonce),
			fmt.Sprintf("%d_%s@claim-race.test", index, nonce)).Scan(&userIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	var authorID int64
	if err = pool.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status,created_by)
		values('author',$1,$2,'approved',$3) returning id`, "Race author "+nonce, "race-author-"+nonce,
		userIDs[0]).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `delete from creators where id=$1`, authorID)
		_, _ = pool.Exec(context.Background(), `delete from users where id=any($1)`, userIDs)
	}()
	claimIDs := make([]int64, 2)
	for index := range claimIDs {
		if err = pool.QueryRow(ctx, `insert into creator_claims(creator_id,user_id,proof_markdown)
			values($1,$2,'independent proof') returning id`, authorID, userIDs[index]).Scan(&claimIDs[index]); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, claimID := range claimIDs {
		go func(id int64) {
			tx, beginErr := pool.Begin(ctx)
			ready.Done()
			if beginErr != nil {
				results <- beginErr
				return
			}
			defer tx.Rollback(ctx)
			<-start
			if _, updateErr := tx.Exec(ctx, `update creator_claims set status='approved',reviewed_at=now() where id=$1`, id); updateErr != nil {
				results <- updateErr
				return
			}
			results <- tx.Commit(ctx)
		}(claimID)
	}
	ready.Wait()
	close(start)
	successes := 0
	for range claimIDs {
		if result := <-results; result == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent approvals succeeded %d times, want exactly 1", successes)
	}
	var approved int
	if err = pool.QueryRow(ctx, `select count(*) from creator_claims where creator_id=$1 and status='approved'`, authorID).Scan(&approved); err != nil {
		t.Fatal(err)
	}
	if approved != 1 {
		t.Fatalf("approved claims=%d want=1", approved)
	}
}

func insertApprovedAccessTestMod(t *testing.T, ctx context.Context, tx pgx.Tx, suffix string, submitterID int64) (int64, int64) {
	t.Helper()
	var code string
	if err := tx.QueryRow(ctx, `select new_public_id()`).Scan(&code); err != nil {
		t.Fatal(err)
	}
	var projectID int64
	slug := fmt.Sprintf("access-%s-%s", suffix, code)
	if err := tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,$3,'approved',$4) returning id`, code, slug, "Access project "+suffix, submitterID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	var routeID int64
	if err := tx.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	return projectID, routeID
}

func assertUserVersions(t *testing.T, ctx context.Context, tx pgx.Tx, userID, wantAuth, wantPermission int64) {
	t.Helper()
	var authVersion, permissionVersion int64
	if err := tx.QueryRow(ctx, `select auth_version,permission_version from users where id=$1`, userID).
		Scan(&authVersion, &permissionVersion); err != nil {
		t.Fatal(err)
	}
	if authVersion != wantAuth || permissionVersion != wantPermission {
		t.Fatalf("versions = auth %d permission %d, want auth %d permission %d",
			authVersion, permissionVersion, wantAuth, wantPermission)
	}
}

func assertProjectAccessCount(t *testing.T, ctx context.Context, tx pgx.Tx, userID, projectID int64, level string, want int) {
	t.Helper()
	var count int
	if err := tx.QueryRow(ctx, `select count(*) from effective_project_access
		where user_id=$1 and project_type='mod' and project_id=$2 and ($3='' or access_level=$3)`,
		userID, projectID, level).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("effective access count for project %d level %q = %d, want %d", projectID, level, count, want)
	}
}
