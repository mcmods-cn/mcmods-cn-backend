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

func TestAuditOSSMultipartActivityInstalledSchemaIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newTEST018IsolatedDatabase(t, ctx)
	config := db.Config()
	config.MaxConns, config.MinConns = 1, 1
	single, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(single.Close)
	db = single

	// The readiness probe reads the installed public table even when the
	// maintenance connection owns a similarly named temporary table.
	if _, err = db.Exec(ctx, `create temporary table oss_multipart_sessions(id bigint)`); err != nil {
		t.Fatal(err)
	}
	if err = database.RequireOSSMultipartActivitySchema(ctx, db); err != nil {
		t.Fatalf("fresh schema behind temporary table: %v", err)
	}
	if _, err = db.Exec(ctx, `drop table pg_temp.oss_multipart_sessions`); err != nil {
		t.Fatal(err)
	}

	var owner int64
	if err = db.QueryRow(ctx, `insert into users(username,email,password_hash) values('multipart-schema-owner','multipart-schema@example.invalid','synthetic') returning id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	var publicID string
	if err = db.QueryRow(ctx, `insert into oss_multipart_sessions(owner_id,bucket,endpoint,region,object_key,upload_id,original_name,content_type,size_bytes,sha256,category,source,status,expires_at)
		values($1,'owned','https://example.invalid','owned','owned-schema-source','owned-schema-upload','source.zip','application/zip',17,repeat('a',64),'owned','owned','aborted',now()) returning public_id`, owner).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`[]`, `{"version":1}`, `{"version":1,"kind":"oss_multipart_abort","event":{},"id":"` + strings.Repeat("a", 5000) + `","queued":false}`} {
		_, err = db.Exec(ctx, `update oss_multipart_sessions set abort_activity_intent=$2::jsonb where public_id=$1`, publicID, raw)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "oss_multipart_abort_activity_shape" {
			t.Fatalf("invalid deferred activity envelope returned unrelated error: %v", err)
		}
	}
	if err = database.Initialize(ctx, db); err != nil {
		t.Fatalf("recheck installed schema: %v", err)
	}
	var envelope, status string
	var size int64
	if err = db.QueryRow(ctx, `select abort_activity_intent::text,status,size_bytes from public.oss_multipart_sessions where public_id=$1`, publicID).
		Scan(&envelope, &status, &size); err != nil || envelope != "{}" || status != "aborted" || size != 17 {
		t.Fatalf("recheck changed the session: envelope=%q status=%s size=%d err=%v", envelope, status, size, err)
	}

	for _, drift := range []struct {
		name string
		ddl  string
	}{
		{"same name with a permissive check", `alter table oss_multipart_sessions drop constraint oss_multipart_abort_activity_shape;
			alter table oss_multipart_sessions add constraint oss_multipart_abort_activity_shape check(true)`},
		{"nullable intent", `alter table oss_multipart_sessions alter column abort_activity_intent drop not null`},
	} {
		t.Run(drift.name, func(t *testing.T) {
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, drift.ddl); err != nil {
				t.Fatal(err)
			}
			if err = database.RequireOSSMultipartActivitySchema(ctx, tx); err == nil || !strings.Contains(err.Error(), "incompatible OSS multipart activity") {
				t.Fatalf("readiness accepted incompatible multipart activity: %v", err)
			}
		})
	}
	if _, err = db.Exec(ctx, `alter table public.oss_multipart_sessions drop column abort_activity_intent`); err != nil {
		t.Fatal(err)
	}
	if err = database.Initialize(ctx, db); err == nil || !strings.Contains(err.Error(), "database catalog differs") {
		t.Fatalf("startup accepted the missing multipart column: %v", err)
	}
}
