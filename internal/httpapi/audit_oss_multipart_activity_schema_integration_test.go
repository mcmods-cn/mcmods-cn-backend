package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/database"
)

func TestAuditOSSMultipartActivityFreshExistingAndIdempotentMigrationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newTEST018IsolatedDatabase(t, ctx)
	// Keep one owned connection so a deliberately shadowing pg_temp table
	// persists across the explicit upgrade calls; production DDL must target public.
	singleConfig := db.Config()
	singleConfig.MaxConns, singleConfig.MinConns = 1, 1
	single, err := pgxpool.NewWithConfig(ctx, singleConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(single.Close)
	db = single
	if err := database.RequireOSSMultipartActivitySchema(ctx, db); err != nil {
		t.Fatalf("fresh extension: %v", err)
	}
	var owner int64
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash) values('multipart-upgrade-owner','multipart-upgrade@example.invalid','synthetic') returning id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	var publicID string
	if err := db.QueryRow(ctx, `insert into oss_multipart_sessions(owner_id,bucket,endpoint,region,object_key,upload_id,original_name,content_type,size_bytes,sha256,category,source,status,expires_at) values($1,'owned','https://example.invalid','owned','owned-upgrade-source','owned-upgrade-upload','source.zip','application/zip',17,repeat('a',64),'owned','owned','aborted',now()) returning public_id`, owner).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var raw string
		if err := db.QueryRow(ctx, `select jsonb_build_array(public_id,owner_id,object_key,upload_id,status,size_bytes,sha256)::text from public.oss_multipart_sessions where public_id=$1`, publicID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	if _, err := db.Exec(ctx, `alter table oss_multipart_sessions drop column abort_activity_intent`); err != nil {
		t.Fatal(err)
	}
	if err := database.Initialize(ctx, db); err == nil || !strings.Contains(err.Error(), "OSS multipart activity extension required") {
		t.Fatalf("old startup failed at wrong boundary: %v", err)
	}
	state, err := database.InspectOSSMultipartActivity(ctx, db)
	if err != nil || state.Present || state.LegacyAbortedSessions != 1 {
		t.Fatalf("read-only state=%+v err=%v", state, err)
	}
	if _, err = database.ApplyOSSMultipartActivityUpgrade(ctx, db, state.Database+"_wrong", "owned-backup"); err == nil || !strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("wrong target accepted or unrelated error: %v", err)
	}
	if _, err = database.ApplyOSSMultipartActivityUpgrade(ctx, db, state.Database, ""); err == nil || !strings.Contains(err.Error(), "backup reference") {
		t.Fatalf("missing backup accepted: %v", err)
	}
	for range 2 {
		if _, err = db.Exec(ctx, `create temporary table oss_multipart_sessions(id bigint)`); err != nil {
			t.Fatal(err)
		}
		after, applyErr := database.ApplyOSSMultipartActivityUpgrade(ctx, db, state.Database, "owned synthetic backup reference")
		if applyErr != nil || !after.Present || after.LegacyAbortedSessions != 1 || after.PendingIntents != 0 || after.QueuedIntents != 0 {
			t.Fatalf("upgrade state=%+v err=%v", after, applyErr)
		}
		var shadowAltered bool
		if err = db.QueryRow(ctx, `select exists(select 1 from pg_attribute where attrelid='pg_temp.oss_multipart_sessions'::regclass and attname='abort_activity_intent' and not attisdropped)`).Scan(&shadowAltered); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `drop table pg_temp.oss_multipart_sessions`); err != nil {
			t.Fatal(err)
		}
		if shadowAltered {
			t.Fatal("explicit public upgrade altered a shadowing temporary table")
		}
		if snapshot() != before {
			t.Fatal("upgrade rewrote a legacy session")
		}
	}
	if err = database.Initialize(ctx, db); err != nil {
		t.Fatalf("upgraded startup: %v", err)
	}
	for _, raw := range []string{`[]`, `{"version":1}`, `{"version":1,"kind":"oss_multipart_abort","event":{},"id":"` + strings.Repeat("a", 5000) + `","queued":false}`} {
		if _, err = db.Exec(ctx, `update oss_multipart_sessions set abort_activity_intent=$2::jsonb where public_id=$1`, publicID, raw); err == nil {
			t.Fatal("invalid deferred activity envelope accepted")
		} else {
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "oss_multipart_abort_activity_shape" {
				t.Fatalf("invalid envelope hit unrelated failure: %v", err)
			}
		}
	}
	var envelope string
	if err = db.QueryRow(ctx, `select abort_activity_intent::text from oss_multipart_sessions where public_id=$1`, publicID).Scan(&envelope); err != nil || envelope != "{}" {
		t.Fatalf("legacy session acquired guessed activity=%q err=%v", envelope, err)
	}
	if _, err = db.Exec(ctx, `alter table oss_multipart_sessions drop constraint oss_multipart_abort_activity_shape; alter table oss_multipart_sessions add constraint oss_multipart_abort_activity_shape check(true)`); err != nil {
		t.Fatal(err)
	}
	if err = database.RequireOSSMultipartActivitySchema(ctx, db); err == nil || !strings.Contains(err.Error(), "incompatible OSS multipart activity") {
		t.Fatalf("same-name CHECK(true) passed schema compatibility: %v", err)
	}
	if _, err = db.Exec(ctx, `alter table oss_multipart_sessions drop constraint oss_multipart_abort_activity_shape`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range database.OSSMultipartActivitySchemaStatements() {
		if _, err = db.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(ctx, `alter table oss_multipart_sessions alter column abort_activity_intent drop not null`); err != nil {
		t.Fatal(err)
	}
	if err = database.RequireOSSMultipartActivitySchema(ctx, db); err == nil || !strings.Contains(err.Error(), "incompatible OSS multipart activity") {
		t.Fatalf("nullable extension accepted or wrong error: %v", err)
	}
}
