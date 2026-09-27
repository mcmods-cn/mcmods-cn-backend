package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestExternalImageReuseRequiresExactOwnerAndPurposeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify OSS image reuse authorization")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var ownerID, otherID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('oss_reuse_owner','oss-reuse-owner@example.invalid','test-only',true) returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('oss_reuse_other','oss-reuse-other@example.invalid','test-only',true) returning id`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}

	const (
		objectKey = "mcmods/project/mods/reuse0001/icons/project/original/icon-deadbeef.png"
		category  = "project/mods/reuse0001/icons/project/original"
		source    = "mod_metadata_import"
		size      = int64(128)
	)
	digest := strings.Repeat("a", 64)
	var fileID int64
	var filePublicID string
	if err = pool.QueryRow(ctx, `insert into oss_files(
		bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
		content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values('test','https://oss.example.invalid','test',$1,$2,$3,'icon.png','icon.png',
		'image/png',$4,$4,$5,$6,'active','clean') returning id,public_id`,
		objectKey, category, source, size, digest, ownerID).Scan(&fileID, &filePublicID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	options := externalImageMirrorOptions{category: category, source: source}
	record, reusable := server.authorizedReusableExternalImageRecord(ctx, objectKey, options, ownerID, digest, size)
	if !reusable || record.result.FileInternalID != fileID || record.result.FilePublicID != filePublicID {
		t.Fatalf("exact owner and purpose was not reusable: reusable=%t record=%+v", reusable, record)
	}

	tests := []struct {
		name       string
		options    externalImageMirrorOptions
		uploaderID int64
	}{
		{name: "different owner", options: options, uploaderID: otherID},
		{name: "different category", options: externalImageMirrorOptions{category: category + "/other", source: source}, uploaderID: ownerID},
		{name: "different source", options: externalImageMirrorOptions{category: category, source: "creator_metadata_import"}, uploaderID: ownerID},
		{name: "missing owner", options: options, uploaderID: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, allowed := server.reusableExternalImageObject(ctx, nil, ossConfigPayload{}, objectKey, test.options, test.uploaderID, digest, size); allowed {
				t.Fatal("cross-boundary OSS image reuse was authorized")
			}
		})
	}
}
