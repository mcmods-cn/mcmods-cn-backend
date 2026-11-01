package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOSSRawRetirementFreshUpgradeShadowAndTypedGuardsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := newOSSByteBindingSchemaTestDatabase(t, ctx)
	fresh, err := InspectOSSUploadByteBindings(ctx, db)
	if err != nil || !fresh.RawRetirementReady {
		t.Fatalf("fresh terminal raw capability expansion=%+v err=%v", fresh, err)
	}
	var fileID int64
	if err = db.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,status) values('owned','https://example.invalid','cn-owned','legacy-owned-terminal','deleted') returning id`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if e := db.QueryRow(ctx, `select to_jsonb(file)::text from oss_files file where id=$1`, fileID).Scan(&value); e != nil {
			t.Fatal(e)
		}
		return value
	}
	before := snapshot()
	if _, err = db.Exec(ctx, `alter table oss_object_deletion_outbox drop column raw_upload_binding_id,drop column raw_hold_reason;drop table oss_upload_capabilities;drop function oss_upload_capability_shape(text,text,text,bigint,text,text,timestamptz,timestamptz,timestamptz,bigint)`); err != nil {
		t.Fatal(err)
	}
	legacy, err := InspectOSSUploadByteBindings(ctx, db)
	if err != nil || legacy.RawRetirementReady || !legacy.Present {
		t.Fatalf("old typed source missing only raw expansion must remain inspectable: %+v err=%v", legacy, err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "expansion required") {
		t.Fatalf("old writer guard=%v", err)
	}
	if _, err = db.Exec(ctx, `create temporary table oss_upload_capabilities(fake text);create temporary table oss_object_deletion_outbox(raw_upload_binding_id text)`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		after, e := ApplyOSSUploadByteBindingUpgrade(ctx, db, legacy.Database, "owned schema-only backup")
		if e != nil || !after.RawRetirementReady {
			t.Fatalf("explicit public raw expansion=%+v err=%v", after, e)
		}
		var shadow, grants int
		if e = db.QueryRow(ctx, `select (select count(*) from pg_attribute where attrelid='pg_temp.oss_upload_capabilities'::regclass and attnum>0 and not attisdropped),(select count(*) from public.oss_upload_capabilities)`).Scan(&shadow, &grants); e != nil || shadow != 1 || grants != 0 {
			t.Fatalf("expansion edited shadow or invented old authority: shadow%d grants%d err=%v", shadow, grants, e)
		}
		if snapshot() != before {
			t.Fatal("raw expansion rewrote historical terminal file identity, source or audit")
		}
	}
	if _, err = db.Exec(ctx, `drop table pg_temp.oss_upload_capabilities;drop table pg_temp.oss_object_deletion_outbox`); err != nil {
		t.Fatal(err)
	}
	// The earlier typed-raw expansion can exist without the new separate HOLD
	// field. Inspect it without mutation, then explicitly add that final column.
	if _, err = db.Exec(ctx, `alter table oss_object_deletion_outbox drop column raw_hold_reason`); err != nil {
		t.Fatal(err)
	}
	oldHold, err := InspectOSSUploadByteBindings(ctx, db)
	if err != nil || oldHold.RawRetirementReady {
		t.Fatalf("missing hold column was not an inspectable partial expansion: %+v err%v", oldHold, err)
	}
	if _, err = ApplyOSSUploadByteBindingUpgrade(ctx, db, legacy.Database, "owned schema-only backup"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `alter table oss_object_deletion_outbox alter column raw_hold_reason drop not null`); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "raw hold reason column") {
		t.Fatalf("nullable HOLD domain passed typed guard: %v", err)
	}
	if _, err = db.Exec(ctx, `alter table oss_object_deletion_outbox alter column raw_hold_reason set not null`); err != nil {
		t.Fatal(err)
	}
	// A named FK on some other UUID column is not the typed retirement link.
	if _, err = db.Exec(ctx, `alter table oss_object_deletion_outbox drop constraint oss_deletion_raw_binding_fk,add column foreign_binding uuid;alter table oss_object_deletion_outbox add constraint oss_deletion_raw_binding_fk foreign key(foreign_binding) references oss_upload_byte_bindings(id) on delete restrict`); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "terminal raw deletion link") {
		t.Fatalf("foreign-column FK passed typed guard: %v", err)
	}
	if _, err = db.Exec(ctx, `alter table oss_object_deletion_outbox drop constraint oss_deletion_raw_binding_fk,drop column foreign_binding`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, OSSRawRetirementSchemaStatements()[3]); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `alter table oss_upload_capabilities alter column max_expires_at drop not null`); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "issued capability column: max_expires_at") {
		t.Fatalf("nullable maximum deadline passed typed guard: %v", err)
	}
	if _, err = db.Exec(ctx, `alter table oss_upload_capabilities alter column max_expires_at set not null`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `drop index oss_deletion_raw_binding;create unique index oss_deletion_raw_binding on oss_object_deletion_outbox(raw_upload_binding_id) where raw_upload_binding_id is null`); err != nil {
		t.Fatal(err)
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err == nil || !strings.Contains(err.Error(), "terminal raw deletion link or index") {
		t.Fatalf("wrong partial identity predicate passed typed guard: %v", err)
	}
	if _, err = db.Exec(ctx, `drop index oss_deletion_raw_binding`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, OSSRawRetirementSchemaStatements()[4]); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"''", "'multipart'"} {
		_, err = db.Exec(ctx, `insert into oss_upload_capabilities(bucket,endpoint,object_key,last_actor_id,grant_kind,upload_id,max_expires_at) values('owned','https://example.invalid','typed-shape',1,`+value+`,'',clock_timestamp()+interval '1 hour')`)
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != "23514" || failure.ConstraintName != "oss_upload_capabilities_shape" {
			t.Fatalf("invalid issued capability hit unrelated boundary: %v", err)
		}
	}
	if err = RequireOSSUploadByteBindingSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("migration/guard checks changed terminal legacy data")
	}
}
