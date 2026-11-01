package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func newOSSByteBindingSchemaTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires explicitly enabled owned loopback PostgreSQL")
	}
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "::1" {
		t.Fatal("refusing non-loopback schema test")
	}
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "audit_byte_binding_" + hex.EncodeToString(nonce[:8])
	marker := "mcmods-byte-binding-test:" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "create database "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "comment on database "+quoted+" is '"+marker+"'"); err != nil {
		t.Fatal(err)
	}
	scoped := cfg.Copy()
	scoped.ConnConfig.Database = name
	scoped.MaxConns, scoped.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, scoped)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		defer admin.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var owned bool
		if err := admin.QueryRow(cleanup, `select datdba=(select oid from pg_roles where rolname=current_user) and shobj_description(oid,'pg_database')=$2 from pg_database where datname=$1`, name, marker).Scan(&owned); err != nil || !owned {
			t.Errorf("refuse schema-test database cleanup: ownership not verified (%v)", err)
			return
		}
		if _, err := admin.Exec(cleanup, "drop database "+quoted); err != nil {
			t.Error(err)
		}
	})
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestOSSByteBindingFreshLegacyUpgradePreservesIdentityAndRejectsSchemaDriftIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newOSSByteBindingSchemaTestDatabase(t, ctx)
	if err := RequireOSSUploadByteBindingSchema(ctx, db); err != nil {
		t.Fatalf("fresh schema missing byte-binding receipt: %v", err)
	}
	var fileID int64
	var publicID string
	if err := db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,source_hash_verified,sha256,stored_sha256,status) values('owned','https://example.invalid','owned','owned-legacy-client-key',true,repeat('a',64),repeat('a',64),'active') returning id,public_id`).Scan(&fileID, &publicID); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var raw string
		if err := db.QueryRow(ctx, `select to_jsonb(file)::text from oss_files file where id=$1`, fileID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	if _, err := db.Exec(ctx, `drop table oss_upload_maintenance_actions; drop function oss_upload_maintenance_shape(text,text,bigint,text,text,text); drop table oss_upload_byte_bindings; drop function oss_upload_byte_binding_shape(text,bigint,text,bigint,text,text,text,text,bigint,text,bigint,bigint)`); err != nil {
		t.Fatal(err)
	}
	if err := RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "OSS upload byte binding expansion required") {
		t.Fatalf("legacy schema hit wrong boundary: %v", err)
	}
	legacy, err := InspectOSSUploadByteBindings(ctx, db)
	if err != nil || legacy.Present || legacy.LegacyActiveFiles != 1 {
		t.Fatalf("legacy diagnosis=%+v err=%v", legacy, err)
	}
	if _, err = ApplyOSSUploadByteBindingUpgrade(ctx, db, legacy.Database+"_wrong", "owned schema-only backup"); err == nil || !strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("wrong target accepted: %v", err)
	}
	if _, err = ApplyOSSUploadByteBindingUpgrade(ctx, db, legacy.Database, ""); err == nil || !strings.Contains(err.Error(), "backup reference") {
		t.Fatalf("missing backup accepted: %v", err)
	}
	for range 2 {
		if _, err = db.Exec(ctx, `create temporary table oss_upload_byte_bindings(id bigint)`); err != nil {
			t.Fatal(err)
		}
		after, applyErr := ApplyOSSUploadByteBindingUpgrade(ctx, db, legacy.Database, "owned schema-only backup")
		if applyErr != nil || !after.Present || after.Committed != 0 || after.Preparing != 0 || after.LegacyActiveFiles != 1 {
			t.Fatalf("upgrade state=%+v err=%v", after, applyErr)
		}
		var shadowChanged bool
		if err = db.QueryRow(ctx, `select exists(select 1 from pg_attribute where attrelid='pg_temp.oss_upload_byte_bindings'::regclass and attname='declared_sha256' and not attisdropped)`).Scan(&shadowChanged); err != nil || shadowChanged {
			t.Fatalf("public upgrade touched shadow table=%v err=%v", shadowChanged, err)
		}
		if _, err = db.Exec(ctx, `drop table pg_temp.oss_upload_byte_bindings`); err != nil {
			t.Fatal(err)
		}
		if snapshot() != before {
			t.Fatal("upgrade changed old publicID, source marker or object reference")
		}
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `drop index oss_upload_byte_bindings_reconcile`); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "expansion required") {
		t.Fatalf("missing reconciliation expansion was silently accepted: %v", err)
	}
	beforeReconcile, e := InspectOSSUploadByteBindings(ctx, db)
	if e != nil || beforeReconcile.ReconciliationReady {
		t.Fatalf("partial old binding schema inspection=%+v err=%v", beforeReconcile, e)
	}
	if _, err = ApplyOSSUploadByteBindingUpgrade(ctx, db, legacy.Database, "owned schema-only backup"); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	// The provider API endpoint and the file display/CDN endpoint have separate
	// semantics. An older typed table may lack the latter snapshot; expansion
	// adds an unknown blank value without inventing a proof for historical rows.
	if _, err = db.Exec(ctx, `alter table oss_upload_byte_bindings drop column file_endpoint`); err != nil {
		t.Fatal(err)
	}
	oldEndpointMapping, e := InspectOSSUploadByteBindings(ctx, db)
	if e != nil || oldEndpointMapping.EndpointMappingReady {
		t.Fatalf("missing endpoint mapping was not an inspectable old expansion: %+v %v", oldEndpointMapping, e)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "expansion required") {
		t.Fatalf("writer accepted missing endpoint mapping: %v", err)
	}
	mapped, e := ApplyOSSUploadByteBindingUpgrade(ctx, db, legacy.Database, "owned schema-only backup")
	if e != nil || !mapped.EndpointMappingReady {
		t.Fatalf("explicit endpoint expansion=%+v err=%v", mapped, e)
	}
	if snapshot() != before {
		t.Fatal("endpoint expansion changed a legacy file")
	}
	// A preparing receipt is not an asset, deduplication authority or old-data
	// backfill. The committed shape cannot be forged merely by toggling status.
	if _, err = db.Exec(ctx, `insert into oss_upload_byte_bindings(id,owner_id,bucket,endpoint,region,upload_object_key,logical_category,original_name,declared_sha256,declared_size_bytes,sealed_source_key,run_token,lease_expires_at) values('11111111-1111-4111-8111-111111111111',2147483648,'owned','https://example.invalid','owned','uploads/source.zip','owned','source.zip',repeat('a',64),17,'uploads/_mcmods_sealed_v1/11111111-1111-4111-8111-111111111111/source.zip','owned-token',now()+interval '2 minutes')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `update oss_upload_byte_bindings set status='committed'`); err == nil {
		t.Fatal("unverified receipt became committed")
	} else {
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "oss_upload_byte_bindings_shape" {
			t.Fatalf("unverified receipt hit unrelated error: %v", err)
		}
	}
	if _, err = db.Exec(ctx, `alter table oss_upload_byte_bindings drop constraint oss_upload_byte_bindings_shape; alter table oss_upload_byte_bindings add constraint oss_upload_byte_bindings_shape check(true)`); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "proof function or constraint") {
		t.Fatalf("same-name CHECK(true) accepted: %v", err)
	}
	if _, err = db.Exec(ctx, `alter table oss_upload_byte_bindings drop constraint oss_upload_byte_bindings_shape; alter table oss_upload_byte_bindings add constraint oss_upload_byte_bindings_shape check(`+ossUploadByteBindingProofExpression+`)`); err != nil {
		t.Fatal(err)
	}
	function := OSSUploadByteBindingSchemaStatements()[0]
	wrong := strings.Replace(function, ossUploadByteBindingProofBody, "select true", 1)
	if _, err = db.Exec(ctx, wrong); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "proof function or constraint") {
		t.Fatalf("same-name fake proof function accepted: %v", err)
	}
	if _, err = db.Exec(ctx, function); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `drop index oss_upload_byte_bindings_committed_upload; create index oss_upload_byte_bindings_committed_upload on oss_upload_byte_bindings(bucket,endpoint,upload_object_key) where status='committed'`); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "identity index") {
		t.Fatalf("nonunique fake identity index accepted: %v", err)
	}
	if _, err = db.Exec(ctx, `drop index oss_upload_byte_bindings_committed_upload`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, OSSUploadByteBindingSchemaStatements()[2]); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `alter table oss_upload_byte_bindings alter column declared_sha256 drop not null`); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "incompatible OSS byte binding column") {
		t.Fatalf("nullable proof identity accepted: %v", err)
	}
	if snapshot() != before {
		t.Fatal("proof tests rewrote a legacy file or falsely upgraded its trust")
	}
}

func TestOSSByteBindingOldTypedEndpointExpansionPreservesUnknownProofIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newOSSByteBindingSchemaTestDatabase(t, ctx)
	var owner, fileID int64
	var filePublicID string
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash) values('old-binding-map','old-binding-map@example.invalid','synthetic') returning id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	key := "uploads/_mcmods_sealed_v1/22222222-2222-4222-8222-222222222222/source.zip"
	if err := db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,size_bytes,source_size_bytes,sha256,stored_sha256,source_hash_verified,uploader_id,status) values('owned','https://owned-display.invalid','owned',$1,'owned','test','source.zip',17,17,repeat('a',64),repeat('a',64),true,$2,'active') returning id,public_id`, key, owner).Scan(&fileID, &filePublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into oss_upload_byte_bindings(id,owner_id,bucket,endpoint,region,file_endpoint,upload_object_key,logical_category,source,original_name,declared_sha256,declared_size_bytes,sealed_source_key,stored_object_key,source_verified_sha256,source_verified_size_bytes,stored_verified_sha256,stored_verified_size_bytes,file_id,status,run_token,lease_expires_at) values('22222222-2222-4222-8222-222222222222',$1,'owned','https://owned-provider.invalid','owned','https://owned-display.invalid','uploads/source.zip','owned','test','source.zip',repeat('a',64),17,$2,$2,repeat('a',64),17,repeat('a',64),17,$3,'committed','owned-old-proof',now())`, owner, key, fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `alter table oss_upload_byte_bindings drop column file_endpoint`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var raw string
		if err := db.QueryRow(ctx, `select jsonb_build_object('file',to_jsonb(file),'receipt',to_jsonb(binding)-'file_endpoint')::text from oss_files file join oss_upload_byte_bindings binding on binding.file_id=file.id where file.public_id=$1`, filePublicID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	old, err := InspectOSSUploadByteBindings(ctx, db)
	if err != nil || old.EndpointMappingReady || old.Committed != 1 || old.LegacyActiveFiles != 1 {
		t.Fatalf("old typed rows were silently given a location proof: %+v %v", old, err)
	}
	after, err := ApplyOSSUploadByteBindingUpgrade(ctx, db, old.Database, "owned schema-only backup")
	if err != nil || !after.EndpointMappingReady || after.Committed != 1 || after.LegacyActiveFiles != 1 || snapshot() != before {
		t.Fatalf("typed endpoint expansion changed identity/proof/data: %+v %v", after, err)
	}
	var endpoint string
	if err = db.QueryRow(ctx, `select file_endpoint from oss_upload_byte_bindings where file_id=$1`, fileID).Scan(&endpoint); err != nil || endpoint != "" {
		t.Fatal("expansion inferred historical endpoint from current file metadata")
	}
}
