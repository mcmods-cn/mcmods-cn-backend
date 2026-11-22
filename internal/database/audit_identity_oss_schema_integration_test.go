package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestAuditIdentityOSSFreshAndExistingUpgradeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires an owned loopback PostgreSQL integration server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "::1" {
		t.Fatal("requires explicitly configured loopback test server")
	}
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("audit_identity_oss_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg = cfg.Copy()
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if strings.HasPrefix(name, "audit_identity_oss_") {
			if _, e := admin.Exec(cleanup, "drop database "+pgx.Identifier{name}.Sanitize()); e != nil {
				t.Error(e)
			}
		}
	}()
	if err = Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	fresh, err := InspectAuditIdentityOSS(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	for column, present := range fresh.Columns {
		if !present {
			t.Fatalf("fresh missing %s", column)
		}
	}
	var fileID int64
	if err = pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,sha256,status) values('fixture','https://example.test','fixture','synthetic-preserve',repeat('a',64),'active') returning id`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into oss_object_deletion_outbox(id,oss_file_id,bucket,endpoint,region,object_key,status) values(9007199254740993,$1,'fixture','https://example.test','fixture','synthetic-preserve','dead')`, fileID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table oss_files drop column stored_sha256`); err != nil {
		t.Fatal(err)
	}
	// Recreate the exact pre-expansion column boundary in this exclusively owned
	// fresh database. Business facts and their internal keys remain unchanged.
	for table, column := range map[string]string{"oss_files": "source_hash_verified", "oss_object_deletion_outbox": "public_id", "oss_rehome_jobs": "public_id", "oss_multipart_sessions": "public_id"} {
		if _, err = pool.Exec(ctx, "alter table "+pgx.Identifier{table}.Sanitize()+" drop column "+pgx.Identifier{column}.Sanitize()); err != nil {
			t.Fatal(err)
		}
	}
	before, err := InspectAuditIdentityOSS(ctx, pool)
	if err != nil || before.UnverifiedActiveFiles != 1 {
		t.Fatalf("legacy diagnosis=%+v err=%v", before, err)
	}
	if _, err = ApplyAuditIdentityOSSUpgrade(ctx, pool, name+"_wrong", "owned-copy-backup"); err == nil {
		t.Fatal("wrong database confirmation accepted")
	}
	if _, err = ApplyAuditIdentityOSSUpgrade(ctx, pool, name, ""); err == nil {
		t.Fatal("missing backup reference accepted")
	}
	after, err := ApplyAuditIdentityOSSUpgrade(ctx, pool, name, "owned-copy-backup")
	if err != nil {
		t.Fatal(err)
	}
	if after.UnverifiedActiveFiles != 1 {
		t.Fatalf("legacy hashes incorrectly marked verified: %+v", after)
	}
	var publicID, again string
	var internalID int64
	if err = pool.QueryRow(ctx, `select id,public_id from oss_object_deletion_outbox where object_key='synthetic-preserve'`).Scan(&internalID, &publicID); err != nil || internalID != 9007199254740993 || len(publicID) != 9 {
		t.Fatalf("retained queue id=%d public=%q err=%v", internalID, publicID, err)
	}
	if _, err = ApplyAuditIdentityOSSUpgrade(ctx, pool, name, "owned-copy-backup"); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select public_id from oss_object_deletion_outbox where id=$1`, internalID).Scan(&again); err != nil || again != publicID {
		t.Fatalf("repeat upgrade changed identity: %q %q %v", publicID, again, err)
	}
	if _, err = pool.Exec(ctx, `alter table oss_files alter column source_hash_verified set default true`); err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyAuditIdentityOSSUpgrade(ctx, pool, name, "owned-copy-backup"); err == nil {
		t.Fatal("incompatible legacy trust default was silently accepted")
	}
	if _, err = pool.Exec(ctx, `alter table oss_files alter column source_hash_verified set default false`); err != nil {
		t.Fatal(err)
	}

	if err = Initialize(ctx, pool); err != nil {
		t.Fatalf("expanded old-generation startup: %v", err)
	}
}
