package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newOSSByteBindingSchemaTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool := newBaselineTestDatabase(t, ctx)
	if err := Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestOSSByteBindingSchemaProtectsProofAndIdentityIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newOSSByteBindingSchemaTestDatabase(t, ctx)
	if err := RequireOSSUploadByteBindingSchema(ctx, db); err != nil {
		t.Fatalf("fresh schema missing byte-binding receipt: %v", err)
	}
	var fileID int64
	if err := db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,source_hash_verified,sha256,stored_sha256,status)
		values('owned','https://example.invalid','owned','owned-unbound-client-key',true,repeat('a',64),repeat('a',64),'active') returning id`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var raw string
		if err := db.QueryRow(ctx, `select to_jsonb(file)::text from oss_files file where id=$1`, fileID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	if state, err := InspectOSSUploadByteBindings(ctx, db); err != nil || !state.Present || state.LegacyActiveFiles != 1 || state.Committed != 0 {
		t.Fatalf("unbound file metadata was treated as proof: %+v, %v", state, err)
	}
	if _, err := db.Exec(ctx, `insert into oss_upload_byte_bindings(id,owner_id,bucket,endpoint,region,upload_object_key,logical_category,original_name,declared_sha256,declared_size_bytes,sealed_source_key,run_token,lease_expires_at)
		values('11111111-1111-4111-8111-111111111111',2147483648,'owned','https://example.invalid','owned','uploads/source.zip','owned','source.zip',repeat('a',64),17,'uploads/_mcmods_sealed_v1/11111111-1111-4111-8111-111111111111/source.zip','owned-token',now()+interval '2 minutes')`); err != nil {
		t.Fatal(err)
	}
	var pgerr *pgconn.PgError
	if _, err := db.Exec(ctx, `update oss_upload_byte_bindings set status='committed'`); !errors.As(err, &pgerr) || pgerr.Code != "23514" || pgerr.ConstraintName != "oss_upload_byte_bindings_shape" {
		t.Fatalf("unverified receipt was accepted or hit an unrelated error: %v", err)
	}
	for _, test := range []struct {
		name string
		ddl  string
		want string
	}{
		{"missing receipt", `drop table oss_upload_byte_bindings cascade`, "shape is missing"},
		{"missing reconciliation index", `drop index oss_upload_byte_bindings_reconcile`, "shape is missing"},
		{"missing endpoint identity", `alter table oss_upload_byte_bindings drop column file_endpoint`, "shape is missing"},
		{"different check", `alter table oss_upload_byte_bindings drop constraint oss_upload_byte_bindings_shape; alter table oss_upload_byte_bindings add constraint oss_upload_byte_bindings_shape check(true)`, "proof function or constraint"},
		{"different function", strings.Replace(OSSUploadByteBindingSchemaStatements()[0], ossUploadByteBindingProofBody, "select true", 1), "proof function or constraint"},
		{"nonunique identity", `drop index oss_upload_byte_bindings_committed_upload; create index oss_upload_byte_bindings_committed_upload on oss_upload_byte_bindings(bucket,endpoint,upload_object_key) where status='committed'`, "identity index"},
		{"nullable hash", `alter table oss_upload_byte_bindings alter column declared_sha256 drop not null`, "incompatible OSS byte binding column"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, test.ddl); err != nil {
				t.Fatal(err)
			}
			if err = RequireOSSUploadByteBindingSchema(ctx, tx); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("guard for %s returned %v, want %q", test.name, err, test.want)
			}
		})
	}
	if snapshot() != before {
		t.Fatal("proof checks rewrote an existing file or falsely upgraded its trust")
	}
}

func TestOSSByteBindingUnknownEndpointIsNotLocationProofIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newOSSByteBindingSchemaTestDatabase(t, ctx)
	var owner, fileID int64
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash)
		values('binding-map','binding-map@example.invalid','synthetic') returning id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	key := "uploads/_mcmods_sealed_v1/22222222-2222-4222-8222-222222222222/source.zip"
	if err := db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,size_bytes,source_size_bytes,sha256,stored_sha256,source_hash_verified,uploader_id,status)
		values('owned','https://owned-display.invalid','owned',$1,'owned','test','source.zip',17,17,repeat('a',64),repeat('a',64),true,$2,'active') returning id`, key, owner).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `insert into oss_upload_byte_bindings(id,owner_id,bucket,endpoint,region,upload_object_key,logical_category,source,original_name,declared_sha256,declared_size_bytes,sealed_source_key,stored_object_key,source_verified_sha256,source_verified_size_bytes,stored_verified_sha256,stored_verified_size_bytes,file_id,status,run_token,lease_expires_at)
		values('22222222-2222-4222-8222-222222222222',$1,'owned','https://owned-provider.invalid','owned','uploads/source.zip','owned','test','source.zip',repeat('a',64),17,$2,$2,repeat('a',64),17,repeat('a',64),17,$3,'committed','owned-proof',now())`, owner, key, fileID); err != nil {
		t.Fatal(err)
	}
	unknown, err := InspectOSSUploadByteBindings(ctx, db)
	if err != nil || !unknown.EndpointMappingReady || unknown.Committed != 1 || unknown.LegacyActiveFiles != 1 {
		t.Fatalf("unknown endpoint was inferred from current file metadata: %+v, %v", unknown, err)
	}
	if _, err := db.Exec(ctx, `update oss_upload_byte_bindings set file_endpoint='https://owned-display.invalid' where file_id=$1`, fileID); err != nil {
		t.Fatal(err)
	}
	known, err := InspectOSSUploadByteBindings(ctx, db)
	if err != nil || known.Committed != 1 || known.LegacyActiveFiles != 0 {
		t.Fatalf("a distinct proved file endpoint was not recognized: %+v, %v", known, err)
	}
}

func TestOSSByteBindingMaintenanceJournalRequiresExplicitProofIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newOSSByteBindingSchemaTestDatabase(t, ctx)
	if err := RequireOSSByteMaintenanceSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	var fileID int64
	if err := db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,status)
		values('owned','https://example.invalid','owned','owned-journal-file','active') returning id`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	var schema string
	var declared bool
	var quiesced, locks []string
	if err := db.QueryRow(ctx, `insert into oss_upload_maintenance_actions(id,operation,state,file_id,bucket,endpoint,region,target_key,plan_sha256,backup_reference,file_preimage,run_token,lease_expires_at)
		values('22222222-2222-4222-8222-222222222222','cleanup_raw','failed',$1,'owned','https://example.invalid','owned','owned-input',repeat('a',64),'owned synthetic backup','{"original":true}','original-token',clock_timestamp())
		returning reference_schema_sha256,quiescence_declared,quiesced_reference_tables,write_fence_tables`, fileID).Scan(&schema, &declared, &quiesced, &locks); err != nil || schema != "" || declared || len(quiesced) != 0 || len(locks) != 0 {
		t.Fatalf("new row invented a maintenance proof: %q %v %v %v, %v", schema, declared, quiesced, locks, err)
	}
	for _, test := range []struct {
		name string
		ddl  string
		want string
	}{
		{"missing table", `drop table oss_upload_maintenance_actions`, "missing from the installed database baseline"},
		{"missing scope", `alter table oss_upload_maintenance_actions drop column write_fence_tables`, "missing from the installed database baseline"},
		{"different check", `alter table oss_upload_maintenance_actions drop constraint oss_upload_maintenance_shape; alter table oss_upload_maintenance_actions add constraint oss_upload_maintenance_shape check(true)`, "proof constraint"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, test.ddl); err != nil {
				t.Fatal(err)
			}
			if err = RequireOSSByteMaintenanceSchema(ctx, tx); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("maintenance accepted incompatible definition: %v", err)
			}
		})
	}
}
