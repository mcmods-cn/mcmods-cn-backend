package httpapi

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

type relationshipAuditIdentity struct {
	ID         int64
	PublicID   string
	Status     string
	CreatedBy  *int64
	ApprovedBy *int64
	ApprovedAt *time.Time
	CreatedAt  time.Time
}

func TestRelationshipSyncPreservesAuditIdentityAndHiddenStatesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify differential relationship sync against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	if _, err = pool.Exec(ctx, `
		create temporary table creators (
			id bigint primary key, public_id text not null unique, kind text not null, name text not null
		);
		create temporary table creator_role_definitions (
			id bigint primary key, public_id text not null unique, name text not null, permission_granting boolean not null
		);
		create temporary table content_creator_bindings (
			id bigserial primary key, public_id text not null unique default new_public_id(),
			subject_type text not null, subject_id bigint not null, creator_id bigint not null, role_id bigint,
			name_snapshot text not null default '', role_snapshot text not null default '',
			status text not null, permission_granting boolean not null default false,
			approved_by bigint, approved_at timestamptz, display_order integer not null default 0,
			created_at timestamptz not null default now()
		);
		create unique index uq_content_creator_bindings_stable_key on content_creator_bindings(
			subject_type,subject_id,creator_id,(coalesce(role_id,0))
		);
		create temporary table creator_team_members (
			team_id bigint not null, member_creator_id bigint not null, role_id bigint not null,
			title text not null default '', status text not null, created_by bigint, approved_by bigint,
			approved_at timestamptz, display_order integer not null default 0,
			created_at timestamptz not null default now(), updated_at timestamptz not null default now(),
			primary key(team_id,member_creator_id,role_id)
		);
		insert into creators(id,public_id,kind,name) values
			(10,'team00001','team','Audit Team'),
			(11,'author001','author','Approved Author'),
			(12,'author002','author','Pending Author'),
			(13,'author003','author','Rejected Author'),
			(14,'author004','author','New Author');
		insert into creator_role_definitions(id,public_id,name,permission_granting)
			values(21,'role00001','Developer',true);
		insert into content_creator_bindings(id,public_id,subject_type,subject_id,creator_id,role_id,
			name_snapshot,role_snapshot,status,permission_granting,approved_by,approved_at,display_order,created_at) values
			(31,'binding01','mod',99,11,21,'Approved snapshot','Developer','approved',true,501,
				timestamptz '2025-01-02 00:00:00+00',0,timestamptz '2025-01-01 00:00:00+00'),
			(32,'binding02','mod',99,12,21,'Pending snapshot','Developer','pending',true,null,null,1,
				timestamptz '2025-02-01 00:00:00+00'),
			(33,'binding03','mod',99,10,21,'Rejected snapshot','Developer','rejected',true,null,null,2,
				timestamptz '2025-03-01 00:00:00+00');
		insert into creator_team_members(team_id,member_creator_id,role_id,title,status,created_by,approved_by,
			approved_at,display_order,created_at,updated_at) values
			(10,11,21,'Approved title','approved',401,501,timestamptz '2025-01-02 00:00:00+00',0,
				timestamptz '2025-01-01 00:00:00+00',timestamptz '2025-01-02 00:00:00+00'),
			(10,12,21,'Pending title','pending',402,null,null,1,
				timestamptz '2025-02-01 00:00:00+00',timestamptz '2025-02-01 00:00:00+00'),
			(10,13,21,'Rejected title','rejected',403,null,null,2,
				timestamptz '2025-03-01 00:00:00+00',timestamptz '2025-03-02 00:00:00+00')`); err != nil {
		t.Fatal(err)
	}

	projectBefore := loadProjectRelationshipAuditIdentities(t, ctx, pool)
	teamBefore := loadTeamRelationshipAuditIdentities(t, ctx, pool)
	roleID := "role00001"
	request := httptest.NewRequest("PUT", "/api/v1/mods/example", nil)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = syncProjectCreatorBindingsTx(ctx, tx, "mod", 99, []modAuthorPayload{{
		CreatorID: "author001", RoleID: &roleID,
	}}, 700, true, false, false, request); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = replaceCreatorTeamMembersTx(ctx, tx, 10, []creatorMemberPayload{{
		CreatorID: "author001", RoleID: roleID, Title: "Approved title",
	}}, "approved", false, 700); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	projectAfterUnprivileged := loadProjectRelationshipAuditIdentities(t, ctx, pool)
	teamAfterUnprivileged := loadTeamRelationshipAuditIdentities(t, ctx, pool)
	assertRelationshipAuditIdentity(t, projectBefore, projectAfterUnprivileged, map[int64]string{
		11: "approved", 12: "pending", 10: "rejected",
	})
	assertRelationshipAuditIdentity(t, teamBefore, teamAfterUnprivileged, map[int64]string{
		11: "approved", 12: "pending", 13: "rejected",
	})

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = syncProjectCreatorBindingsTx(ctx, tx, "mod", 99, []modAuthorPayload{
		{CreatorID: "author001", RoleID: &roleID},
		{CreatorID: "author004", RoleID: &roleID},
	}, 701, true, true, true, request); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = replaceCreatorTeamMembersTx(ctx, tx, 10, []creatorMemberPayload{{
		CreatorID: "author004", RoleID: roleID, Title: "New lead",
	}}, "approved", true, 701); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	projectAfterManager := loadProjectRelationshipAuditIdentities(t, ctx, pool)
	teamAfterManager := loadTeamRelationshipAuditIdentities(t, ctx, pool)
	assertRelationshipAuditIdentity(t, projectBefore, projectAfterManager, map[int64]string{
		11: "approved", 12: "revoked", 10: "revoked",
	})
	assertRelationshipAuditIdentity(t, teamBefore, teamAfterManager, map[int64]string{
		11: "revoked", 12: "revoked", 13: "revoked",
	})
	if added := projectAfterManager[14]; added.Status != "approved" || added.ID == 0 || added.PublicID == "" || added.ApprovedBy == nil || *added.ApprovedBy != 701 {
		t.Fatalf("new project relationship was not inserted as a distinct approved audit row: %#v", added)
	}
	if added := teamAfterManager[14]; added.Status != "approved" || added.CreatedBy == nil || *added.CreatedBy != 701 || added.ApprovedBy == nil || *added.ApprovedBy != 701 {
		t.Fatalf("new team membership was not inserted as a distinct approved audit row: %#v", added)
	}
}

func loadProjectRelationshipAuditIdentities(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[int64]relationshipAuditIdentity {
	t.Helper()
	rows, err := pool.Query(ctx, `select creator_id,id,public_id,status,null::bigint,approved_by,approved_at,created_at
		from content_creator_bindings where subject_type='mod' and subject_id=99 order by creator_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := map[int64]relationshipAuditIdentity{}
	for rows.Next() {
		var creatorID int64
		var item relationshipAuditIdentity
		if err = rows.Scan(&creatorID, &item.ID, &item.PublicID, &item.Status, &item.CreatedBy,
			&item.ApprovedBy, &item.ApprovedAt, &item.CreatedAt); err != nil {
			t.Fatal(err)
		}
		result[creatorID] = item
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func loadTeamRelationshipAuditIdentities(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[int64]relationshipAuditIdentity {
	t.Helper()
	rows, err := pool.Query(ctx, `select member_creator_id,0::bigint,''::text,status,created_by,approved_by,approved_at,created_at
		from creator_team_members where team_id=10 order by member_creator_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := map[int64]relationshipAuditIdentity{}
	for rows.Next() {
		var memberID int64
		var item relationshipAuditIdentity
		if err = rows.Scan(&memberID, &item.ID, &item.PublicID, &item.Status, &item.CreatedBy,
			&item.ApprovedBy, &item.ApprovedAt, &item.CreatedAt); err != nil {
			t.Fatal(err)
		}
		result[memberID] = item
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertRelationshipAuditIdentity(t *testing.T, before, after map[int64]relationshipAuditIdentity, wantStatus map[int64]string) {
	t.Helper()
	for creatorID, status := range wantStatus {
		old, oldFound := before[creatorID]
		current, currentFound := after[creatorID]
		if !oldFound || !currentFound {
			t.Fatalf("relationship %d disappeared: before=%t after=%t", creatorID, oldFound, currentFound)
		}
		if old.ID != current.ID || old.PublicID != current.PublicID || !old.CreatedAt.Equal(current.CreatedAt) ||
			!optionalInt64Equal(old.CreatedBy, current.CreatedBy) || !optionalInt64Equal(old.ApprovedBy, current.ApprovedBy) ||
			!optionalTimeEqual(old.ApprovedAt, current.ApprovedAt) {
			t.Fatalf("relationship %d audit identity changed: before=%#v after=%#v", creatorID, old, current)
		}
		if current.Status != status {
			t.Fatalf("relationship %d status=%q, want %q", creatorID, current.Status, status)
		}
	}
}

func optionalInt64Equal(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func optionalTimeEqual(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}
