package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLocalizationEditChecksCurrentOwnerVisibilityAndGrantIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	owner, editor := f.userIDs[f.editor], f.userIDs[f.denied]
	grantTEST044Permissions(t, f.ctx, f.db, editor, "content.write")
	var assetID int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `with file as (
		insert into oss_files(object_key,category,source,original_name,source_original_name,content_type,
			size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status) values('localization/skin.png',$1,'minecraft_texture',
			'skin.png','skin.png','image/png',128,128,repeat('a',64),$2,'active','trusted_generated') returning id
	), blob as (
		insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes)
			select repeat('a',64),id,'localization/skin.png',64,64,128 from file returning hash
	) insert into skin_assets(owner_id,blob_hash,kind,model,display_name,visibility,review_status,status)
		select $2,hash,'skin','default','Localization fixture','private','approved','active' from blob returning id,public_id`,
		ossSharedSkinTextureCategory(), owner).Scan(&assetID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into content_subjects(subject_type,subject_id,default_locale) values('skin',$1,'en-US') on conflict do nothing`, assetID); err != nil {
		t.Fatal(err)
	}
	form := map[string]string{"locale": "en-US", "name": "Owner translation"}
	generic := "/api/v1/content/" + publicID
	owned := "/api/v1/skins/" + publicID + "/content"
	f.require(t, f.denied, http.MethodPut, generic, form, http.StatusNotFound)
	f.require(t, f.denied, http.MethodPut, owned, form, http.StatusForbidden)
	f.require(t, f.editor, http.MethodPut, owned, form, http.StatusOK)

	if _, err := f.db.Exec(f.ctx, `update skin_assets set visibility='public' where id=$1`, assetID); err != nil {
		t.Fatal(err)
	}
	barrier, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(f.ctx)
	var blocker int32
	if err = barrier.QueryRow(f.ctx, `select pg_backend_pid() from content_subjects where subject_id=$1 and subject_type='skin' for update`, assetID).Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodPut, f.origin.URL+generic, strings.NewReader(`{"locale":"zh-CN","name":"New locale"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+f.denied)
	result := make(chan int, 1)
	go func() {
		response, err := f.origin.Client().Do(request)
		if err != nil {
			result <- 0
			return
		}
		defer response.Body.Close()
		result <- response.StatusCode
	}()
	waitCtx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err = f.db.QueryRow(waitCtx, `select exists(select 1 from pg_stat_activity where datname=current_database() and $1=any(pg_blocking_pids(pid)))`, blocker).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case status := <-result:
			t.Fatalf("write finished before the target lock was released: %d", status)
		case <-time.After(5 * time.Millisecond):
		}
	}
	if _, err = f.db.Exec(f.ctx, `delete from user_permissions where user_id=$1 and permission_id=(select id from permissions where code='content.write')`, editor); err != nil {
		t.Fatal(err)
	}
	if err = barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-result:
		if status != http.StatusForbidden {
			t.Fatalf("write after grant withdrawal returned %d", status)
		}
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	var revisions int
	if err = f.db.QueryRow(f.ctx, `select count(*) from change_requests where aggregate_key=$1`, contentLocalizationAggregateKey(publicID, "skin", "zh-CN")).Scan(&revisions); err != nil || revisions != 0 {
		t.Fatalf("withdrawn write created revisions=%d err=%v", revisions, err)
	}
}
