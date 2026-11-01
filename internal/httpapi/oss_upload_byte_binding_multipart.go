package httpapi

import (
	"context"
	"errors"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

// A canceled SDK copy can fail to abort with its canceled request context. The
// durable receipt still owns its exact UUID candidate keys, including any
// incomplete multipart copies. The existing <=7-day bucket lifecycle remains
// required as a fallback; neither a HEAD404 nor a lost SDK response proves that
// no multipart upload exists.
func cleanupOSSByteBindingMultiparts(ctx context.Context, client *aliyunoss.Client, b *ossUploadByteBinding, key string) error {
	if !ownedOSSByteBindingCandidate(b, key) {
		return errors.New("multipart cleanup target is outside its recorded attempt")
	}
	var keyMarker, uploadMarker string
	for page := 0; page < 4; page++ {
		result, err := client.ListMultipartUploads(ctx, &aliyunoss.ListMultipartUploadsRequest{Bucket: aliyunoss.Ptr(b.cfg.Bucket), Prefix: aliyunoss.Ptr(key), KeyMarker: aliyunoss.Ptr(keyMarker), UploadIdMarker: aliyunoss.Ptr(uploadMarker), MaxUploads: 32})
		if err != nil {
			return err
		}
		for _, upload := range result.Uploads {
			if aliyunoss.ToString(upload.Key) != key {
				continue // A provider prefix is not exact object ownership.
			}
			if aliyunoss.ToString(upload.UploadId) == "" {
				return errors.New("multipart cleanup response omitted exact upload identity")
			}
			_, err = client.AbortMultipartUpload(ctx, &aliyunoss.AbortMultipartUploadRequest{Bucket: aliyunoss.Ptr(b.cfg.Bucket), Key: aliyunoss.Ptr(key), UploadId: upload.UploadId})
			if err != nil {
				var serviceErr *aliyunoss.ServiceError
				if !errors.As(err, &serviceErr) || serviceErr.Code != "NoSuchUpload" {
					return err
				}
			}
		}
		if !result.IsTruncated {
			return nil
		}
		nextKey, nextUpload := aliyunoss.ToString(result.NextKeyMarker), aliyunoss.ToString(result.NextUploadIdMarker)
		if nextKey == "" || (nextKey == keyMarker && nextUpload == uploadMarker) {
			return errors.New("multipart cleanup pagination made no progress")
		}
		keyMarker, uploadMarker = nextKey, nextUpload
	}
	return errors.New("multipart cleanup reached its bounded page budget")
}
