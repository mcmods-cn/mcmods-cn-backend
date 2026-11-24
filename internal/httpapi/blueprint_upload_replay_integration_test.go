package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"
)

func TestBlueprintFileReceiptsKeepTheCurrentOriginalAndCoverIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	const presign = "/api/v1/users/me/oss/uploads/presign"
	const complete = "/api/v1/users/me/oss/uploads/complete"
	owner := f.userIDs[f.editor]
	settle := func(ticket test037Ticket, want int) map[string]any {
		t.Helper()
		return decodeTEST022Data[map[string]any](t, f.require(t, f.editor, http.MethodPost, complete, ticket.completion(), want))
	}
	reuse := func(ticket test037Ticket) map[string]any {
		t.Helper()
		return decodeTEST022Data[map[string]any](t, f.require(t, f.editor, http.MethodPost, presign, map[string]any{
			"originalName": ticket.OriginalName, "contentType": ticket.ContentType, "source": ticket.Source,
			"category": "test037", "sizeBytes": ticket.SizeBytes, "sha256": ticket.SHA256,
		}, 200))
	}
	raw, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	original := f.ticket(t, f.editor, presign, "receipt.nbt", "application/octet-stream", "blueprint", raw)
	f.put(t, original, raw)
	first := settle(original, 201)
	blueprint, _ := first["blueprintId"].(string)
	if !validCatalogPublicID(blueprint) || settle(original, 200)["blueprintId"] != blueprint {
		t.Fatalf("initial blueprint and its file receipt disagree: %q", blueprint)
	}
	if selected := reuse(original); selected["uploadRequired"] != false || selected["blueprintId"] != blueprint {
		t.Fatalf("explicit original reuse: %#v", selected)
	}

	uploadCover := func(name string, sample color.RGBA) (test037Ticket, string) {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.SetRGBA(1, 1, sample)
		var raw bytes.Buffer
		if err := png.Encode(&raw, img); err != nil {
			t.Fatal(err)
		}
		ticket := f.ticket(t, f.editor, presign, name, "image/png", "blueprint_cover:"+blueprint, raw.Bytes())
		f.put(t, ticket, raw.Bytes())
		auditOSSInstallTransparentCoverConversion(t, f, ticket)
		result := settle(ticket, 201)
		if result["blueprintId"] != blueprint {
			t.Fatalf("first completion did not bind cover: %#v", result)
		}
		return ticket, result["id"].(string)
	}
	a, aID := uploadCover("first.png", color.RGBA{R: 255, A: 255})
	_, bID := uploadCover("second.png", color.RGBA{B: 255, A: 255})
	if aID == bID {
		t.Fatal("two distinct sources unexpectedly produced the same registered file")
	}
	quota := func() string {
		t.Helper()
		var value string
		if err := f.db.QueryRow(f.ctx, `select jsonb_build_array(
			(select to_jsonb(q)-'updated_at' from oss_user_quota_usage q where user_id=$1),
			(select jsonb_agg(to_jsonb(q)-'updated_at' order by to_jsonb(q)::text) from oss_user_daily_quota_usage q where user_id=$1),
			(select count(*) from oss_user_upload_quota_reservations where user_id=$1))::text`, owner).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := quota()
	assertCover := func(want string) {
		t.Helper()
		var current string
		if err := f.db.QueryRow(f.ctx, `select file.public_id from blueprints bp join oss_files file on file.id=bp.cover_file_id
			where bp.public_id=$1 and bp.cover_object_key=file.object_key`, blueprint).Scan(&current); err != nil || current != want {
			t.Errorf("current cover=%q want=%q err=%v", current, want, err)
		}
	}
	assertCover(bID)
	if old := settle(a, 200); old["blueprintId"] != nil || old["id"] != aID {
		t.Errorf("stale cover receipt advertised a current association: %#v", old)
	}
	assertCover(bID)
	if got := quota(); got != before {
		t.Errorf("cover replay changed quota: %s -> %s", before, got)
	}
	if selected := reuse(a); selected["uploadRequired"] != false || selected["blueprintId"] != blueprint {
		t.Fatalf("new cover selection failed: %#v", selected)
	}
	assertCover(aID)
	if current := settle(a, 200); current["blueprintId"] != blueprint {
		t.Errorf("current cover omitted its blueprint: %#v", current)
	}

	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err = f.server.moderationHideTarget(f.ctx, tx, "blueprint", blueprint); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	before = quota()
	for _, sample := range []struct {
		ticket test037Ticket
		fileID string
	}{{original, first["id"].(string)}, {a, aID}} {
		// A fresh request fails the existing project-directory check. An upload
		// which passed that check before withdrawal still reaches the shared
		// receipt projection, and must never restore the withdrawn association.
		f.require(t, f.editor, http.MethodPost, complete, sample.ticket.completion(), http.StatusBadRequest)
		fileID, err := f.server.resolveActiveOSSFileInternalIDForUploader(f.ctx, sample.fileID, owner)
		if err != nil {
			t.Fatal(err)
		}
		retired := make(map[string]any)
		if err = f.server.attachCompletedOSSUploadAssociations(f.ctx, retired, fileID, owner, sample.ticket.Source); err != nil {
			t.Fatal(err)
		}
		if retired["blueprint"] != nil || retired["blueprintId"] != nil {
			t.Errorf("withdrawn blueprint attached to file receipt: %#v", retired)
		}
	}
	var total, jobs int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from blueprints where owner_id=$1),
		(select count(*) from blueprint_jobs job join blueprints bp on bp.id=job.blueprint_id where bp.owner_id=$1)`, owner).Scan(&total, &jobs); err != nil || total != 1 || jobs != 1 || quota() != before {
		t.Errorf("replay recreated a blueprint, a job or a quota debit: blueprints=%d jobs=%d err=%v", total, jobs, err)
	}
}
