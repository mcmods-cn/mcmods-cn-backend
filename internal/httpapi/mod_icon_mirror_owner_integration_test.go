package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
)

type localCreatorImage []byte

func (fixture localCreatorImage) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodGet || request.URL.String() != "https://cdn.modrinth.com/owned-avatar.png" {
		return nil, fmt.Errorf("unexpected URL in image fixture")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(fixture)), Header: http.Header{"Content-Type": {"image/png"}}, Request: request}, nil
}

func TestExternalAvatarKeepsIndependentUploadersAndRecoversInterruptedRegistrationIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	transport.RegisterProtocol("https", localCreatorImage(test039PNG(t, 4, 4, 1)))
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original; transport.CloseIdleConnections() })
	fetch := func(uploader int64) (mirroredExternalImage, error) {
		return f.server.mirrorExternalCreatorAvatar(f.ctx, "https://cdn.modrinth.com/owned-avatar.png", "author", "Shared upstream author", uploader)
	}
	first, second := f.userIDs[f.editor], f.userIDs[f.otherEditor]
	// An ordinary activation failure after the object was written must leave a
	// private receipt that a later import can complete without replacing the bytes.
	if _, err := f.db.Exec(f.ctx, `create function fixture_pause_activation() returns trigger language plpgsql as $$
      begin raise exception 'fixture activation unavailable'; end $$;
      create trigger pause_import_activation before update of status on oss_files for each row
      when (old.source='creator_metadata_import' and new.status='active') execute function fixture_pause_activation()`); err != nil {
		t.Fatal(err)
	}
	if _, err := fetch(first); err == nil {
		t.Fatal("activation failure returned a successful import")
	}
	var pending int
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_files where uploader_id=$1 and source='creator_metadata_import'
       and status='pending' and scan_status='pending'`, first).Scan(&pending); err != nil || pending != 1 || len(f.objects()) != 1 {
		t.Fatalf("pending receipts=%d objects=%d err=%v", pending, len(f.objects()), err)
	}
	third := f.userIDs[f.reviewer]
	if _, err := fetch(third); err == nil {
		t.Fatal("fixture should interrupt third import")
	}
	var expiredID int64
	var expiredKey string
	var dueTomorrow bool
	if err := f.db.QueryRow(f.ctx, `select f.id,f.object_key,o.next_attempt_at>now()+interval '23 hours' from oss_files f
        join oss_object_deletion_outbox o on o.oss_file_id=f.id where f.uploader_id=$1 and o.reason=$2`, third, externalImageExpiryReason).Scan(&expiredID, &expiredKey, &dueTomorrow); err != nil || !dueTomorrow {
		t.Fatalf("private import expiry was not scheduled: %v", err)
	}
	if _, err := f.db.Exec(f.ctx, `update oss_object_deletion_outbox set next_attempt_at=now() where oss_file_id=$1`, expiredID); err != nil {
		t.Fatal(err)
	}
	worker := NewOSSDeletionWorker(f.server.cfg, f.db)
	job, err := worker.claim(f.ctx)
	if err != nil || job.FileID == nil || *job.FileID != expiredID {
		t.Fatalf("scheduled cleanup selected another file: %v", err)
	}
	// Simulate loss of the first deletion acknowledgement after retirement.
	// A replacement must receive a new key while the old job is still live.
	if retired, err := worker.retireExternalImage(f.ctx, job); err != nil || !retired {
		t.Fatalf("cannot start expiry: %v", err)
	}
	if _, err := f.db.Exec(f.ctx, `drop trigger pause_import_activation on oss_files`); err != nil {
		t.Fatal(err)
	}
	replacement, err := fetch(third)
	if err != nil || replacement.FileInternalID == expiredID || len(f.objects()) != 3 {
		t.Fatalf("replacement reused outstanding deletion: %v", err)
	}
	if err = worker.deleteObject(f.ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = worker.complete(f.ctx, job); err != nil {
		t.Fatal(err)
	}
	if _, exists := f.objects()[expiredKey]; exists {
		t.Fatal("expired upload still exists in object storage")
	}

	a, err := fetch(first)
	if err != nil {
		t.Fatal(err)
	}
	same, err := fetch(first)
	if err != nil || same != a || len(f.objects()) != 2 {
		t.Fatalf("repeat changed the first receipt or bytes: %v", err)
	}
	b, err := fetch(second)
	if err != nil || a.FileInternalID == b.FileInternalID || a.URL == b.URL || len(f.objects()) != 3 {
		t.Fatalf("second submission did not receive an independent image: %v", err)
	}
	replacement, err = fetch(third)
	if err != nil || replacement.FileInternalID == expiredID || replacement.URL == "" || len(f.objects()) != 3 {
		t.Fatalf("new private import after expiry did not obtain its own live key: %v", err)
	}
	if _, exists := f.objects()[expiredKey]; exists {
		t.Fatal("reimport used a key targeted by an earlier deletion")
	}
	var outstanding int
	if err = f.db.QueryRow(f.ctx, `select count(*) from oss_object_deletion_outbox where reason=$1 and status<>'completed'`, externalImageExpiryReason).Scan(&outstanding); err != nil || outstanding != 0 {
		t.Fatalf("completed uploads retained a scheduled deletion: %d/%v", outstanding, err)
	}

	for _, entry := range []struct {
		image mirroredExternalImage
		owner int64
	}{{a, first}, {b, second}} {
		var owner int64
		if err = f.db.QueryRow(f.ctx, `select uploader_id from oss_files where public_id=$1 and id=$2 and status='active' and scan_status='clean'`, entry.image.FilePublicID, entry.image.FileInternalID).Scan(&owner); err != nil || owner != entry.owner {
			t.Fatalf("registered owner=%d expected=%d err=%v", owner, entry.owner, err)
		}
	}
}
