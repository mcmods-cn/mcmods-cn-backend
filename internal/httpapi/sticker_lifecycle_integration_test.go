package httpapi

import (
	"context"
	"testing"
	"time"
)

func TestStickerImageTombstoneRequiresNoRemainingOwner(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, statement := range []string{
		`create temp table oss_files(
			id bigint primary key,bucket text not null,endpoint text not null,region text not null,
			object_key text not null,status text not null,updated_at timestamptz not null default now()
		) on commit drop`,
		`create temp table stickers(id bigint primary key,image_file_id bigint not null) on commit drop`,
		`create temp table log_shares(
			source_file_id bigint,status text not null,deleted_at timestamptz
		) on commit drop`,
		`insert into oss_files values(10,'bucket','https://oss-cn-test.aliyuncs.com','cn-test','stickers/derived/test.png','active',now())`,
		`insert into stickers values(20,10)`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	installAccessCurrentOSSDeletionFixture(t, ctx, tx)
	server := &Server{db: db}
	if err = server.tombstoneUnreferencedStickerFileTx(ctx, tx, 10, "still_owned"); err != nil {
		t.Fatal(err)
	}
	assertStickerFileLifecycleState(t, ctx, tx, "active", 0)

	if _, err = tx.Exec(ctx, `delete from stickers where id=20`); err != nil {
		t.Fatal(err)
	}
	if err = server.tombstoneUnreferencedStickerFileTx(ctx, tx, 10, "sticker_replaced"); err != nil {
		t.Fatal(err)
	}
	assertStickerFileLifecycleState(t, ctx, tx, "deleted", 1)
	beforeReplay := accessFixtureBusinessRows(t, ctx, tx, "oss_files", "stickers", "log_shares", "oss_object_deletion_outbox", "oss_upload_byte_bindings")
	if err = server.tombstoneUnreferencedStickerFileTx(ctx, tx, 10, "sticker_replaced_retry"); err != nil {
		t.Fatal(err)
	}
	assertStickerFileLifecycleState(t, ctx, tx, "deleted", 1)
	if after := accessFixtureBusinessRows(t, ctx, tx, "oss_files", "stickers", "log_shares", "oss_object_deletion_outbox", "oss_upload_byte_bindings"); after != beforeReplay {
		t.Fatal("replayed sticker tombstone changed business or deletion rows")
	}
}

func assertStickerFileLifecycleState(t *testing.T, ctx context.Context, queryer stickerReferenceQueryer, wantStatus string, wantOutbox int) {
	t.Helper()
	var status string
	var outbox int
	if err := queryer.QueryRow(ctx, `select status from oss_files where id=10`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := queryer.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || outbox != wantOutbox {
		t.Fatalf("status/outbox = %s/%d, want %s/%d", status, outbox, wantStatus, wantOutbox)
	}
}
