package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestDraftPagesUseCategoryScopedKeysetAndAuthoritativeStatuses(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	installDraftListingTempTables(t, ctx, tx)

	statusAt := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	if _, err = tx.Exec(ctx, `insert into change_requests(id,public_id,submitted_by,status,resolved_at) values
		(10,'change001',7,'pending',null),(11,'change002',7,'approved',$1)`, statusAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into user_drafts(
		id,public_id,user_id,draft_key,project_key,project_title,kind,title,edit_url,target_url,payload,
		change_request_id,review_target_type,review_target_id,submitted_status,submitted_at,expires_at,created_at,updated_at)
		values
		(1,'draft0001',7,'a','project','Project','mod','A','/a','','{}',null,'',null,'',null,$2,$1,$1),
		(2,'draft0002',7,'b','project','Project','mod','B','/b','','{}',null,'',null,'',null,$2,$1,$1),
		(3,'draft0003',7,'c','project','Project','mod','C','/c','','{}',null,'',null,'',null,$2,$1,$1),
		(4,'draft0004',7,'d','project','Project','mod','D','/d','/d','{}',10,'',null,'pending',$1,$2,$1,$1),
		(5,'draft0005',7,'e','project','Project','mod','E','/e','/e','{}',11,'',null,'approved',$1,$2,$1,$1),
		(6,'draft0006',8,'foreign','project','Foreign','mod','F','/f','','{}',null,'',null,'',null,$2,$1,$1)`,
		statusAt, statusAt.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	first, next, err := loadUserDraftPage(ctx, tx, 7, userDraftListRequest{Category: userDraftCategoryActive, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].internalID != 3 || first[1].internalID != 2 || next == "" {
		t.Fatalf("unexpected first active page: items=%#v next=%q", first, next)
	}
	cursor, err := decodeUserDraftCursor(next)
	if err != nil {
		t.Fatal(err)
	}
	second, next, err := loadUserDraftPage(ctx, tx, 7, userDraftListRequest{
		Category: userDraftCategoryActive, Limit: 2, Cursor: &cursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].internalID != 1 || next != "" {
		t.Fatalf("unexpected second active page: items=%#v next=%q", second, next)
	}

	completed, next, err := loadUserDraftPage(ctx, tx, 7, userDraftListRequest{Category: userDraftCategoryCompleted, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 2 || completed[0].internalID != 5 || completed[0].Status != "approved" ||
		completed[1].internalID != 4 || completed[1].Status != "reviewing" || next != "" {
		t.Fatalf("unexpected completed page: items=%#v next=%q", completed, next)
	}
}

func TestDraftCompletionAuthorityComesOnlyFromOwnedDatabaseRows(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	installDraftListingTempTables(t, ctx, tx)
	if _, err = tx.Exec(ctx, `insert into change_requests(id,public_id,submitted_by,status) values
		(10,'change001',7,'approved'),(11,'change002',8,'pending');
		insert into minecraft_servers(id,public_id,submitted_by,review_status) values
		(20,'server001',7,'rejected'),(21,'server002',8,'approved')`); err != nil {
		t.Fatal(err)
	}

	changeAuthority, err := resolveUserDraftAuthority(ctx, tx, 7, completeUserDraftRequest{ChangeRequestID: "change001"})
	if err != nil {
		t.Fatal(err)
	}
	if changeAuthority.ChangeRequestID != int64(10) || changeAuthority.ReviewTargetID != nil || changeAuthority.Status != "approved" {
		t.Fatalf("unexpected change authority: %#v", changeAuthority)
	}
	serverAuthority, err := resolveUserDraftAuthority(ctx, tx, 7, completeUserDraftRequest{
		ReviewTargetType: userDraftReviewTargetServer, ReviewTargetPublicID: "server001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if serverAuthority.ChangeRequestID != nil || serverAuthority.ReviewTargetID != int64(20) ||
		serverAuthority.ReviewTargetType != userDraftReviewTargetServer || serverAuthority.Status != "rejected" {
		t.Fatalf("unexpected server authority: %#v", serverAuthority)
	}
	if _, err = resolveUserDraftAuthority(ctx, tx, 7, completeUserDraftRequest{ChangeRequestID: "change002"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-user change request error = %v", err)
	}
	if _, err = resolveUserDraftAuthority(ctx, tx, 7, completeUserDraftRequest{
		ReviewTargetType: userDraftReviewTargetServer, ReviewTargetPublicID: "server002",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-user server error = %v", err)
	}
}

func installDraftListingTempTables(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	_, err := tx.Exec(ctx, `create temp table change_requests(
		id bigint primary key,public_id text not null,submitted_by bigint,status text not null,resolved_at timestamptz) on commit drop;
		create temp table minecraft_servers(
		id bigint primary key,public_id text not null,submitted_by bigint,review_status text not null) on commit drop;
		create temp table user_drafts(
		id bigint primary key,public_id text not null,user_id bigint not null,draft_key text not null,
		project_key text not null,project_title text not null,kind text not null,title text not null,
		edit_url text not null,target_url text not null,payload jsonb not null,change_request_id bigint,
		review_target_type text not null,review_target_id bigint,submitted_status text not null,
		submitted_at timestamptz,expires_at timestamptz not null,created_at timestamptz not null,updated_at timestamptz not null) on commit drop`)
	if err != nil {
		t.Fatal(err)
	}
}
