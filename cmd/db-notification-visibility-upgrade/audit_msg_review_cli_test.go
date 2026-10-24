package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func newAuditMsgReviewDatabase(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 for an owned local migration database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "::1" {
		t.Fatal("refusing a non-loopback database fixture")
	}
	owner, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "audit_msg_review_" + hex.EncodeToString(nonce[:])
	marker := "mcmods-audit-owned:" + hex.EncodeToString(nonce[:])
	if _, err = owner.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, "comment on database "+pgx.Identifier{name}.Sanitize()+" is '"+marker+"'"); err != nil {
		t.Fatal(err)
	}
	targetCfg := cfg.Copy()
	targetCfg.ConnConfig.Database = name
	targetCfg.MaxConns = 1
	db, err := pgxpool.NewWithConfig(ctx, targetCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		var ours bool
		if err := owner.QueryRow(cleanup, `select datdba=(select oid from pg_roles where rolname=current_user) and shobj_description(oid,'pg_database')=$2 from pg_database where datname=$1`, name, marker).Scan(&ours); err != nil || !ours {
			t.Errorf("refusing cleanup: database owner/marker mismatch (%v)", err)
			return
		}
		if _, err := owner.Exec(cleanup, "drop database "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Error(err)
		}
	})
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	return ctx, db, name
}

func TestAuditMsgReviewMigrationPinsPublicAndVerifiesActualState(t *testing.T) {
	ctx, db, name := newAuditMsgReviewDatabase(t)
	// A caller-controlled search_path must not redirect any migration DDL.
	if _, err := db.Exec(ctx, `create schema audit_shadow;
 create table audit_shadow.notifications(id bigint);
 drop trigger trg_notifications_revision on public.notifications;
 drop function public.stamp_notification_revision();
 alter table public.notifications drop column revision_xid;
 alter table public.notification_receipts drop column read_revision_xid;
 alter table public.notification_read_watermarks drop column visibility_snapshot;
 drop table public.realtime_connection_leases;
 set search_path=audit_shadow,public`); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runVisibilityMigration(ctx, db, migrationOptions{}, &output); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Before, After visibilityInspection
		Applied       bool
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Before.ColumnsPresent != 0 || report.After.Ready || report.Applied {
		t.Fatalf("dry-run guessed completion: %s", output.Bytes())
	}
	output.Reset()
	if _, err := db.Exec(ctx, `set search_path=audit_shadow,public`); err != nil {
		t.Fatal(err)
	}
	var callerPath string
	if err := db.QueryRow(ctx, `show search_path`).Scan(&callerPath); err != nil || !strings.HasPrefix(callerPath, "audit_shadow") {
		t.Fatalf("wrong caller-path precondition: %s/%v", callerPath, err)
	}
	if err := runVisibilityMigration(ctx, db, migrationOptions{Apply: true, ConfirmedDatabase: name, BackupReference: "synthetic-owned-copy"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.After.Ready || !report.Applied || report.After.ColumnsCorrect != 3 {
		t.Fatalf("apply postconditions wrong: %s", output.Bytes())
	}
	var shadowColumns int
	if err := db.QueryRow(ctx, `select count(*) from information_schema.columns where table_schema='audit_shadow' and table_name='notifications' and column_name='revision_xid'`).Scan(&shadowColumns); err != nil || shadowColumns != 0 {
		t.Fatalf("shadow schema was changed: count=%d err=%v", shadowColumns, err)
	}
	output.Reset()
	if err := runVisibilityMigration(ctx, db, migrationOptions{Apply: true, ConfirmedDatabase: name, BackupReference: "synthetic-owned-copy"}, &output); err != nil {
		t.Fatalf("idempotent apply: %v", err)
	}
}

func TestAuditMsgReviewMigrationRejectsWrongTypesAndWrongTarget(t *testing.T) {
	ctx, db, name := newAuditMsgReviewDatabase(t)
	if _, err := db.Exec(ctx, `drop trigger trg_notifications_revision on public.notifications;
 alter table public.notification_receipts drop column read_revision_xid;
 alter table public.notification_receipts add column read_revision_xid text;
 drop table public.realtime_connection_leases`); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"unrelated_database", name} {
		var output bytes.Buffer
		err := runVisibilityMigration(ctx, db, migrationOptions{Apply: true, ConfirmedDatabase: target, BackupReference: "synthetic-owned-copy"}, &output)
		if err == nil {
			t.Fatalf("unsafe target/types accepted: target=%s output=%s", target, output.Bytes())
		}
		if target != name && !strings.Contains(err.Error(), "apply requires exact") {
			t.Fatalf("wrong target rejection reason: %v", err)
		}
		if target == name && !strings.Contains(err.Error(), "incompatible types") {
			t.Fatalf("wrong rejection reason: %v", err)
		}
		var leases bool
		if err := db.QueryRow(ctx, `select to_regclass('public.realtime_connection_leases') is not null`).Scan(&leases); err != nil || leases {
			t.Fatalf("rejection left DDL side effect: leases=%v error=%v", leases, err)
		}
	}
}

func TestAuditMsgReviewInspectionRejectsWrongTriggerBindingAndBody(t *testing.T) {
	ctx, db, _ := newAuditMsgReviewDatabase(t)
	for _, ddl := range []string{
		`create or replace function public.stamp_notification_revision() returns trigger language plpgsql as $$ begin return new; end $$`,
		`drop trigger trg_notifications_revision on public.notifications; create trigger trg_notifications_revision after insert on public.notifications for each row execute function public.stamp_notification_revision()`,
	} {
		if _, err := db.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
		state, err := inspectVisibilitySchema(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if state.Ready || state.TriggerCorrect {
			t.Fatalf("bad trigger reported ready: %+v", state)
		}
		if err := database.Migrate(ctx, db); err == nil || !strings.Contains(err.Error(), "notification visibility extension") {
			t.Fatalf("startup accepted bad trigger or wrong reason: %v", err)
		}
	}
}

func TestAuditMsgReviewMigrationRequiresExactSnapshotIndex(t *testing.T) {
	ctx, db, name := newAuditMsgReviewDatabase(t)
	if _, err := db.Exec(ctx, `drop index public.idx_notifications_broadcast_revision`); err != nil {
		t.Fatal(err)
	}
	state, err := inspectVisibilitySchema(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if state.Ready || state.BroadcastRevisionIndexCorrect {
		t.Fatalf("missing index was ready: %+v", state)
	}
	if err := database.Migrate(ctx, db); err == nil || !strings.Contains(err.Error(), "notification visibility extension") {
		t.Fatalf("startup accepted missing index or wrong reason: %v", err)
	}
	var output bytes.Buffer
	if err := runVisibilityMigration(ctx, db, migrationOptions{Apply: true, ConfirmedDatabase: name, BackupReference: "synthetic-owned-copy"}, &output); err != nil {
		t.Fatal(err)
	}
	state, err = inspectVisibilitySchema(ctx, db)
	if err != nil || !state.Ready {
		t.Fatalf("explicit index upgrade=%+v/%v", state, err)
	}
	if _, err := db.Exec(ctx, `drop index public.idx_notifications_broadcast_revision;create index idx_notifications_broadcast_revision on public.notifications(id) where recipient_id is null`); err != nil {
		t.Fatal(err)
	}
	state, err = inspectVisibilitySchema(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if state.Ready || state.BroadcastRevisionIndexCorrect {
		t.Fatalf("same name with wrong columns was ready: %+v", state)
	}
	output.Reset()
	if err := runVisibilityMigration(ctx, db, migrationOptions{Apply: true, ConfirmedDatabase: name, BackupReference: "synthetic-owned-copy"}, &output); err == nil || !strings.Contains(err.Error(), "final schema postconditions") {
		t.Fatalf("wrong index rejection=%v output=%s", err, output.Bytes())
	}
}
