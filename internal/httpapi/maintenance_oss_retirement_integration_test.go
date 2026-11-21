package httpapi

import "testing"

// Old file rows can omit storage coordinates. Expiration has to complete the
// target from the same encrypted setting as a request-time retirement.
func TestMaintenanceRetiresLegacyFileWithEncryptedSettingsIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	var fileID int64
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(source,bucket,object_key,created_at)
		values('sticker-upload','historical-bucket','owned/maintenance-old.png',now()-interval '2 hours')
		returning id`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	var saved []byte
	if err := f.db.QueryRow(f.ctx, `select value from system_settings where key='oss.aliyun'`).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	worker := NewMaintenanceWorker(f.server)
	if _, err := f.db.Exec(f.ctx, `update system_settings set value='"unreadable"'::jsonb where key='oss.aliyun'`); err != nil {
		t.Fatal(err)
	}
	worker.pruneStickerUploads(f.ctx)
	assertTemporaryStickerFileStatus(t, f.ctx, f.db, fileID, "active")
	var pending int
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_object_deletion_outbox where oss_file_id=$1`, fileID).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("unreadable settings left %d deletion intents: %v", pending, err)
	}
	if _, err := f.db.Exec(f.ctx, `update system_settings set value=$1 where key='oss.aliyun'`, saved); err != nil {
		t.Fatal(err)
	}
	worker.pruneStickerUploads(f.ctx)
	worker.pruneStickerUploads(f.ctx)
	assertTemporaryStickerFileStatus(t, f.ctx, f.db, fileID, "deleted")
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_object_deletion_outbox
		where oss_file_id=$1 and bucket='historical-bucket' and endpoint=$2 and region=$3 and object_key='owned/maintenance-old.png' and status='pending'`,
		fileID, f.cfg.Endpoint, f.cfg.Region).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("expected one deletion with the recorded bucket and configured endpoint/region, got %d: %v", pending, err)
	}
}
