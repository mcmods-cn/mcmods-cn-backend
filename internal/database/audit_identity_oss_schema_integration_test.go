package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestAuditIdentityOSSCurrentBaselinePreservesIdentityAndUnverifiedBytesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newBaselineTestDatabase(t, ctx)
	if err := Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var fileID, modID int64
	if err := pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,sha256,status)
		values('fixture','https://example.test','fixture','synthetic-preserve',repeat('a',64),'active') returning id`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name) values('ossid0001','oss-identity','OSS fixture') returning id`).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	const expected = int64(9007199254740993)
	var deletionID, rehomeID, multipartID string
	if err := pool.QueryRow(ctx, `insert into oss_object_deletion_outbox(id,oss_file_id,bucket,endpoint,region,object_key,status)
		values($1,$2,'fixture','https://example.test','fixture','synthetic-preserve','dead') returning public_id`, expected, fileID).Scan(&deletionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into oss_rehome_jobs(id,mod_id) values($1,$2) returning public_id`, expected, modID).Scan(&rehomeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into oss_multipart_sessions(id,bucket,endpoint,region,object_key,upload_id,original_name,content_type,size_bytes,
		sha256,category,source,expires_at) values($1,'fixture','https://example.test','fixture','synthetic-preserve','fixture-upload',
		'synthetic.log','text/plain',17,repeat('a',64),'fixture','test',now()+interval '1 hour') returning public_id`, expected).Scan(&multipartID); err != nil {
		t.Fatal(err)
	}
	if len(deletionID) != 9 || len(rehomeID) != 9 || len(multipartID) != 9 || deletionID == rehomeID || rehomeID == multipartID || multipartID == deletionID {
		t.Fatal("new OSS activities did not receive independent public identities")
	}
	fresh, err := InspectAuditIdentityOSS(ctx, pool)
	if err != nil || fresh.UnverifiedActiveFiles != 1 || fresh.Generation != schemaGeneration {
		t.Fatalf("unexpected read-only inventory: %+v, %v", fresh, err)
	}
	for column, present := range fresh.Columns {
		if !present {
			t.Fatalf("current schema is missing %s", column)
		}
	}
	var invalid *pgconn.PgError
	if _, err := pool.Exec(ctx, `update oss_files set stored_sha256='invalid' where id=$1`, fileID); !errors.As(err, &invalid) || invalid.Code != "23514" {
		t.Fatalf("invalid stored hash hit an unrelated boundary: %v", err)
	}
	if err := Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var internalID int64
	var deletionAfter, rehomeAfter, multipartAfter, stored string
	var verified bool
	if err := pool.QueryRow(ctx, `select deletion.id,deletion.public_id,rehome.public_id,multipart.public_id,f.source_hash_verified,f.stored_sha256
		from oss_object_deletion_outbox deletion join oss_files f on f.id=deletion.oss_file_id
		join oss_rehome_jobs rehome on rehome.id=$1 join oss_multipart_sessions multipart on multipart.id=$1 where deletion.id=$1`, expected).
		Scan(&internalID, &deletionAfter, &rehomeAfter, &multipartAfter, &verified, &stored); err != nil ||
		internalID != expected || deletionAfter != deletionID || rehomeAfter != rehomeID || multipartAfter != multipartID || verified || stored != "" {
		t.Fatalf("audit or restart altered trust or queue identities: %v", err)
	}
	if _, err := pool.Exec(ctx, `alter table oss_files alter column source_hash_verified set default true`); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectAuditIdentityOSS(ctx, pool); err == nil || !strings.Contains(err.Error(), "incompatible identity/OSS column") {
		t.Fatalf("audit accepted an unsafe default: %v", err)
	}
	if err := Initialize(ctx, pool); err == nil || !strings.Contains(err.Error(), "catalog differs") {
		t.Fatalf("startup accepted an unsafe default: %v", err)
	}
}
