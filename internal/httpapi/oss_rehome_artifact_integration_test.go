package httpapi

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestGalleryRehomeKeepsTheRegisteredFileWhenItIsReadded(t *testing.T) {
	f := newTEST037Fixture(t)
	actorID := f.userIDs[f.editor]
	type fixtureImage struct {
		id    int64
		bytes []byte
	}
	makeImage := func(name string, width int) fixtureImage {
		t.Helper()
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, width, 2))); err != nil {
			t.Fatal(err)
		}
		key := ossUserPrefix(f.cfg.Prefix, actorID) + "/" + name
		id, err := f.server.writeGeneratedOSSObject(f.ctx, key, name, "image/png", encoded.Bytes(), actorID, "test037-generated")
		if err != nil {
			t.Fatal(err)
		}
		return fixtureImage{id, encoded.Bytes()}
	}
	add := func(img fixtureImage) {
		t.Helper()
		if _, err := f.db.Exec(f.ctx, `insert into mod_gallery_images(mod_id,oss_file_id,created_by) values ($1,$2,$3)`, f.modID, img.id, actorID); err != nil {
			t.Fatal(err)
		}
	}
	registered := func(img fixtureImage) string {
		t.Helper()
		var key string
		if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where id=$1 and status='active'`, img.id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		f.store.mu.Lock()
		got := f.store.objects[key]
		f.store.mu.Unlock()
		if !bytes.Equal(got.data, img.bytes) {
			t.Fatalf("registered gallery image %d has different bytes", img.id)
		}
		return key
	}
	first, second := makeImage("first.png", 2), makeImage("second.png", 3)
	add(first)
	if err := f.server.rehomeModGalleryOSSObjects(f.ctx, f.modID); err != nil {
		t.Fatal(err)
	}
	initial := registered(first)

	if _, err := f.db.Exec(f.ctx, `delete from mod_gallery_images where mod_id=$1 and oss_file_id=$2`, f.modID, first.id); err != nil {
		t.Fatal(err)
	}
	add(second)
	add(first)
	for range 2 {
		if err := f.server.rehomeModGalleryOSSObjects(f.ctx, f.modID); err != nil {
			t.Fatal(err)
		}
	}
	NewOSSDeletionWorker(f.server.cfg, f.db).drain(f.ctx)
	if key := registered(first); key != initial {
		t.Errorf("re-adding the same registered image changed its destination from %q to %q", initial, key)
	}
	if registered(second) == initial {
		t.Fatal("different registered images share a destination")
	}
}
