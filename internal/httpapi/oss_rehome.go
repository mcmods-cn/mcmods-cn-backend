package httpapi

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"golang.org/x/sync/errgroup"
)

const maxOSSRehomeConcurrency = 4

type modGalleryObjectForRehome struct {
	galleryPublicID string
	fileID          int64
	objectKey       string
	originalName    string
}

func (s *Server) rehomeModGalleryOSSObjects(ctx context.Context, modID int64) error {
	var projectPublicID string
	if err := s.db.QueryRow(ctx, `select project_code from mods where id=$1`, modID).Scan(&projectPublicID); err != nil {
		return err
	}
	rows, err := s.db.Query(ctx, `select gallery.public_id,file.id,file.object_key,file.original_name
		from mod_gallery_images gallery join oss_files file on file.id=gallery.oss_file_id and file.status='active'
		where gallery.mod_id=$1 order by gallery.id`, modID)
	if err != nil {
		return err
	}
	items := make([]modGalleryObjectForRehome, 0)
	for rows.Next() {
		var item modGalleryObjectForRehome
		if err = rows.Scan(&item.galleryPublicID, &item.fileID, &item.objectKey, &item.originalName); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(items) == 0 {
		return nil
	}

	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return err
	}
	category := ossProjectTextCategory("mod", projectPublicID, projectPublicID, "gallery")
	targetPrefix := ossObjectPrefix(cfg.Prefix, category)
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(maxOSSRehomeConcurrency)
	for index := range items {
		item := items[index]
		group.Go(func() error {
			extension := strings.ToLower(filepath.Ext(item.objectKey))
			if extension == "" {
				extension = strings.ToLower(filepath.Ext(item.originalName))
			}
			targetKey := path.Join(targetPrefix, normalizeProjectObjectSegment(item.galleryPublicID)+extension)
			if item.objectKey == targetKey {
				return nil
			}
			if _, copyErr := client.CopyObject(groupContext, &aliyunoss.CopyObjectRequest{
				Bucket:          aliyunoss.Ptr(cfg.Bucket),
				Key:             aliyunoss.Ptr(targetKey),
				SourceBucket:    aliyunoss.Ptr(cfg.Bucket),
				SourceKey:       aliyunoss.Ptr(item.objectKey),
				ForbidOverwrite: aliyunoss.Ptr("false"),
			}); copyErr != nil {
				return fmt.Errorf("copy gallery %s: %w", item.galleryPublicID, copyErr)
			}
			tx, updateErr := s.db.Begin(groupContext)
			if updateErr != nil {
				s.deleteOSSObjectIfUnregistered(groupContext, cfg, targetKey, "mod-gallery-rehome-begin-failed")
				return fmt.Errorf("begin gallery %s rehome transaction: %w", item.galleryPublicID, updateErr)
			}
			defer tx.Rollback(groupContext)
			command, updateErr := tx.Exec(groupContext, `update oss_files set object_key=$2,category=$3,updated_at=now()
				where id=$1 and object_key=$4 and status='active'`, item.fileID, targetKey, category, item.objectKey)
			if updateErr != nil {
				rollbackErr := tx.Rollback(groupContext)
				s.deleteOSSObjectIfUnregistered(groupContext, cfg, targetKey, "mod-gallery-rehome-record-failed")
				return errors.Join(fmt.Errorf("record gallery %s destination: %w", item.galleryPublicID, updateErr), rollbackErr)
			}
			if command.RowsAffected() != 1 {
				var currentKey string
				if queryErr := tx.QueryRow(groupContext, `select object_key from oss_files where id=$1 and status='active'`, item.fileID).Scan(&currentKey); queryErr == nil && currentKey == targetKey {
					return nil
				}
				rollbackErr := tx.Rollback(groupContext)
				s.deleteOSSObjectIfUnregistered(groupContext, cfg, targetKey, "mod-gallery-rehome-source-changed")
				return errors.Join(fmt.Errorf("record gallery %s destination: source changed", item.galleryPublicID), rollbackErr)
			}
			if updateErr = enqueueOSSObjectDeletionTx(groupContext, tx, ossDeletionTarget{
				Bucket: cfg.Bucket, Endpoint: cfg.Endpoint, Region: cfg.Region, UseCName: cfg.UseCName,
				ObjectKey: item.objectKey, Reason: "mod-gallery-rehome",
			}); updateErr != nil {
				rollbackErr := tx.Rollback(groupContext)
				s.deleteOSSObjectIfUnregistered(groupContext, cfg, targetKey, "mod-gallery-rehome-source-delete-queue-failed")
				return errors.Join(fmt.Errorf("queue gallery %s source deletion: %w", item.galleryPublicID, updateErr), rollbackErr)
			}
			if updateErr = tx.Commit(groupContext); updateErr != nil {
				s.deleteOSSObjectIfUnregistered(groupContext, cfg, targetKey, "mod-gallery-rehome-commit-failed")
				return fmt.Errorf("commit gallery %s rehome: %w", item.galleryPublicID, updateErr)
			}
			return nil
		})
	}
	return group.Wait()
}
