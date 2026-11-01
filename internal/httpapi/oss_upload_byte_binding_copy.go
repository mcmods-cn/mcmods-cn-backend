package httpapi

import (
	"context"
	"errors"
	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

func copyOSSUploadToSealedKey(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, uploadKey, sealedKey string, head *aliyunoss.HeadObjectResult) error {
	if head == nil || head.ContentLength <= 0 || head.ContentLength > maxOSSUploadBytes || uploadKey == sealedKey || !isOSSSealedObjectKey(sealedKey) {
		return errors.New("invalid sealed upload copy identity or size")
	}
	request := &aliyunoss.CopyObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(sealedKey), SourceBucket: aliyunoss.Ptr(cfg.Bucket), SourceKey: aliyunoss.Ptr(uploadKey), ForbidOverwrite: aliyunoss.Ptr("true"), ServerSideEncryption: head.ServerSideEncryption, ServerSideDataEncryption: head.ServerSideDataEncryption, ServerSideEncryptionKeyId: head.ServerSideEncryptionKeyId}
	if request.ServerSideEncryptionKeyId == nil {
		request.ServerSideEncryptionKeyId = head.SSEKMSKeyId
	}
	if head.StorageClass != nil {
		request.StorageClass = aliyunoss.StorageClassType(*head.StorageClass)
	}
	// The SDK's multipart copier preserves the existing2GiB upload ceiling.
	// Never infer a hash proof from CopyObject/ETag/version metadata.
	copier := aliyunoss.NewCopier(client, func(options *aliyunoss.CopierOptions) {
		options.MultipartCopyThreshold = 512 << 20
		options.PartSize = 64 << 20
		options.ParallelNum = 2
		options.DisableShallowCopy = true
	})
	_, err := copier.Copy(ctx, request, func(options *aliyunoss.CopierOptions) { options.MetadataProperties = head })
	return err
}
