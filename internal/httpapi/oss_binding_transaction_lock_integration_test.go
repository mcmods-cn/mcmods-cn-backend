package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOCT02RasterBindingHoldsDeletionUntilTransactionCompletesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	var user, file int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('raster-lock','raster-lock@example.invalid','unused') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status)
	 values('synthetic','https://storage.invalid','cn-test','images/lock.png','avatar','user','lock.png','image/png',10,repeat('f',64),$1,'active','clean') returning id,public_id`, user).Scan(&file, &publicID); err != nil {
		t.Fatal(err)
	}
	binding, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Rollback(context.Background())
	if _, err = resolveTrustedRasterOSSFilePublicID(ctx, binding, publicID, ossRasterBindingScope{UploaderID: user}); err != nil {
		t.Fatal(err)
	}
	deletion, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deletion.Rollback(context.Background())
	if _, err = deletion.Exec(ctx, `set local lock_timeout='100ms'`); err != nil {
		t.Fatal(err)
	}
	if _, err = deletion.Exec(ctx, `update oss_files set status='deleted' where id=$1`, file); err == nil {
		t.Fatal("file was deleted while a trusted binding transaction still depended on it")
	}
	if err = deletion.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = binding.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update oss_files set status='deleted' where id=$1`, file); err != nil {
		t.Fatal(err)
	}
	if _, err = resolveTrustedRasterOSSFilePublicID(ctx, pool, publicID, ossRasterBindingScope{UploaderID: user}); err == nil {
		t.Fatal("deleted object was accepted by a later binding")
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("post-transaction read remained locked")
	}
}
