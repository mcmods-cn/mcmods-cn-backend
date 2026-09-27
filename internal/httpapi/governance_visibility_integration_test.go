package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

func TestReportTargetVisibilityReusesProjectAndCommentAuthorization(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []string{
		`create temp table public_routes(
			id bigint primary key,internal_id bigint not null,public_id text not null,entity_type text not null,canonical_path text not null
		) on commit drop`,
		`create temp table mods(
			id bigint primary key,project_code text not null,primary_name text not null,review_status text not null,
			submitted_by bigint not null,updated_at timestamptz not null default now()
		) on commit drop`,
		`create temp table comments(
			public_id text primary key,status text not null,target_type text not null,target_id bigint not null,target_version_id bigint
		) on commit drop`,
		`insert into public_routes values(10,20,'abc123def','mod','/mods/private-mod')`,
		`insert into mods(id,project_code,primary_name,review_status,submitted_by) values(20,'abc123def','Private mod','pending',1)`,
		`insert into comments values('comment01','published','mod',20,null),('comment02','pending','mod',20,null)`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	server := &Server{db: db}
	viewer := security.Claims{Subject: 2}
	owner := security.Claims{Subject: 1}
	for _, target := range []struct{ targetType, publicID string }{{"mod", "abc123def"}, {"comment", "comment01"}} {
		if _, err = server.validateReportTargetVisibility(ctx, tx, target.targetType, target.publicID, viewer); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("viewer could report hidden %s: %v", target.targetType, err)
		}
		if _, err = server.validateReportTargetVisibility(ctx, tx, target.targetType, target.publicID, owner); err != nil {
			t.Fatalf("owner could not report own %s: %v", target.targetType, err)
		}
	}
	if _, err = server.validateReportTargetVisibility(ctx, tx, "comment", "comment02", owner); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("non-public comment was reportable: %v", err)
	}
	if _, err = tx.Exec(ctx, `update mods set review_status='approved' where id=20`); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct{ targetType, publicID string }{{"mod", "abc123def"}, {"comment", "comment01"}} {
		if _, err = server.validateReportTargetVisibility(ctx, tx, target.targetType, target.publicID, viewer); err != nil {
			t.Fatalf("viewer could not report public %s: %v", target.targetType, err)
		}
	}
}
