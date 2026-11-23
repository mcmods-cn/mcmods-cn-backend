package httpapi

import (
	"net/http"
	"testing"
)

func TestStickerDeletionPreservesReferencedImagesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "sticker.manage")
	var packID int64
	if err := f.db.QueryRow(f.ctx, `insert into sticker_packs(code,status,created_by)
		values('owned_pack','active',$1) returning id`, f.userIDs[f.editor]).Scan(&packID); err != nil {
		t.Fatal(err)
	}
	seed := func(code string) int64 {
		t.Helper()
		var fileID int64
		if err := f.db.QueryRow(f.ctx, `insert into oss_files(
			object_key,bucket,endpoint,region,original_name,content_type,size_bytes,source,uploader_id,status,scan_status
		) values($1,'owned-stickers','https://stickers.example.invalid','cn-test','owned.png','image/png',45,'sticker_derived',$2,'active','trusted_generated')
		returning id`, "owned-stickers/"+code+".png", f.userIDs[f.editor]).Scan(&fileID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(f.ctx, `insert into stickers(pack_id,code,image_file_id,mime_type,width,height,file_size,checksum)
			values($1,$2,$3,'image/png',1,1,45,'synthetic')`, packID, code, fileID); err != nil {
			t.Fatal(err)
		}
		return fileID
	}
	unreferenced := seed("unreferenced")
	referenced := seed("referenced")
	if _, err := f.db.Exec(f.ctx, `insert into sticker_content_references(source_type,source_key,source_field,pack_code,sticker_code)
		values('fixture','42','markdown','owned_pack','referenced')`); err != nil {
		t.Fatal(err)
	}
	const base = "/api/v1/admin/sticker-packs/owned_pack/stickers"
	f.require(t, f.editor, http.MethodDelete, base+"/referenced", nil, http.StatusConflict)
	f.require(t, f.editor, http.MethodDelete, base+"/unreferenced", nil, http.StatusNoContent)
	f.require(t, f.editor, http.MethodDelete, base+"/unreferenced", nil, http.StatusNotFound)
	for _, item := range []struct {
		id     int64
		bound  bool
		status string
	}{
		{referenced, true, "active"},
		{unreferenced, false, "deleted"},
	} {
		var bound bool
		var status string
		if err := f.db.QueryRow(f.ctx, `select status,exists(select 1 from stickers where image_file_id=$1)
			from oss_files where id=$1`, item.id).Scan(&status, &bound); err != nil {
			t.Fatal(err)
		}
		if status != item.status || bound != item.bound {
			t.Fatalf("file %d = %s, bound=%t; want %s, bound=%t", item.id, status, bound, item.status, item.bound)
		}
	}
}
