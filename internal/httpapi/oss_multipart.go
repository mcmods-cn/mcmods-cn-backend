package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

const (
	ossMultipartThreshold = int64(16 << 20)
	ossMultipartPartSize  = int64(8 << 20)
	ossMultipartMaxParts  = 10_000
)

type ossMultipartPartTicket struct {
	PartNumber int               `json:"partNumber"`
	SizeBytes  int64             `json:"sizeBytes"`
	Method     string            `json:"method"`
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers"`
}

type ossMultipartUploadTicket struct {
	UploadID string                   `json:"uploadId"`
	PartSize int64                    `json:"partSize"`
	Parts    []ossMultipartPartTicket `json:"parts"`
}

func shouldUseOSSMultipart(sizeBytes int64) bool {
	return sizeBytes >= ossMultipartThreshold
}

func ossMultipartPartCount(sizeBytes, partSize int64) int {
	if sizeBytes <= 0 || partSize <= 0 {
		return 0
	}
	return int(math.Ceil(float64(sizeBytes) / float64(partSize)))
}

func (s *Server) initiateOSSMultipartUpload(
	ctx context.Context,
	client *aliyunoss.Client,
	cfg ossConfigPayload,
	objectKey, contentType, sha256 string,
	sizeBytes int64,
	expires time.Duration,
) (ossMultipartUploadTicket, error) {
	var ticket ossMultipartUploadTicket
	partCount := ossMultipartPartCount(sizeBytes, ossMultipartPartSize)
	if partCount <= 0 || partCount > ossMultipartMaxParts {
		return ticket, errors.New("invalid OSS multipart upload size")
	}
	result, err := client.InitiateMultipartUpload(ctx, &aliyunoss.InitiateMultipartUploadRequest{
		Bucket:          aliyunoss.Ptr(cfg.Bucket),
		Key:             aliyunoss.Ptr(objectKey),
		ContentType:     aliyunoss.Ptr(contentType),
		ForbidOverwrite: aliyunoss.Ptr("true"),
		Metadata:        map[string]string{"sha256": sha256},
	})
	if err != nil {
		return ticket, fmt.Errorf("initiate OSS multipart upload: %w", err)
	}
	if result.UploadId == nil || *result.UploadId == "" {
		return ticket, errors.New("initiate OSS multipart upload: OSS returned an empty upload ID")
	}
	uploadID := *result.UploadId
	abort := func() {
		_, _ = client.AbortMultipartUpload(context.Background(), &aliyunoss.AbortMultipartUploadRequest{
			Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey), UploadId: aliyunoss.Ptr(uploadID),
		})
	}
	ticket, err = presignExistingOSSMultipartUpload(ctx, client, cfg, objectKey, uploadID, sizeBytes, expires)
	if err != nil {
		abort()
		return ticket, err
	}
	return ticket, nil
}

func presignExistingOSSMultipartUpload(
	ctx context.Context,
	client *aliyunoss.Client,
	cfg ossConfigPayload,
	objectKey, uploadID string,
	sizeBytes int64,
	expires time.Duration,
) (ossMultipartUploadTicket, error) {
	var ticket ossMultipartUploadTicket
	if !validOSSMultipartUploadID(uploadID) {
		return ticket, errors.New("invalid OSS multipart upload ID")
	}
	partCount := ossMultipartPartCount(sizeBytes, ossMultipartPartSize)
	if partCount <= 0 || partCount > ossMultipartMaxParts {
		return ticket, errors.New("invalid OSS multipart upload size")
	}
	parts := make([]ossMultipartPartTicket, 0, partCount)
	for partNumber := 1; partNumber <= partCount; partNumber++ {
		partLength := min(ossMultipartPartSize, sizeBytes-int64(partNumber-1)*ossMultipartPartSize)
		presigned, presignErr := client.Presign(ctx, &aliyunoss.UploadPartRequest{
			Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey),
			UploadId: aliyunoss.Ptr(uploadID), PartNumber: int32(partNumber),
		}, aliyunoss.PresignExpires(expires))
		if presignErr != nil {
			return ticket, fmt.Errorf("presign OSS multipart part %d: %w", partNumber, presignErr)
		}
		parts = append(parts, ossMultipartPartTicket{
			PartNumber: partNumber, SizeBytes: partLength, Method: presigned.Method,
			URL: presigned.URL, Headers: presigned.SignedHeaders,
		})
	}
	return ossMultipartUploadTicket{UploadID: uploadID, PartSize: ossMultipartPartSize, Parts: parts}, nil
}

func completeOSSMultipartUpload(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey, uploadID string) error {
	if !validOSSMultipartUploadID(uploadID) {
		return errors.New("invalid OSS multipart upload ID")
	}
	_, err := client.CompleteMultipartUpload(ctx, &aliyunoss.CompleteMultipartUploadRequest{
		Bucket:          aliyunoss.Ptr(cfg.Bucket),
		Key:             aliyunoss.Ptr(objectKey),
		UploadId:        aliyunoss.Ptr(uploadID),
		CompleteAll:     aliyunoss.Ptr("yes"),
		ForbidOverwrite: aliyunoss.Ptr("true"),
	})
	var serviceError *aliyunoss.ServiceError
	if errors.As(err, &serviceError) && serviceError.Code == "NoSuchUpload" {
		// The caller verifies the final object before persisting completion. This
		// recovers a lost success response without confusing a lifecycle abort
		// with a completed upload.
		return nil
	}
	return err
}

func verifyCompletedOSSMultipartObject(
	ctx context.Context,
	client *aliyunoss.Client,
	cfg ossConfigPayload,
	objectKey string,
	sizeBytes int64,
	sha256 string,
) (*aliyunoss.HeadObjectResult, error) {
	head, err := client.HeadObject(ctx, &aliyunoss.HeadObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(objectKey),
	})
	if err != nil {
		return nil, fmt.Errorf("verify completed OSS multipart object: %w", err)
	}
	if !completedOSSMultipartObjectMatches(head, sizeBytes, sha256) {
		if sizeBytes > 0 && head.ContentLength != sizeBytes {
			return nil, errors.New("completed OSS multipart object size does not match the session")
		}
		return nil, errors.New("completed OSS multipart object SHA-256 does not match the session")
	}
	return head, nil
}

func completedOSSMultipartObjectMatches(head *aliyunoss.HeadObjectResult, sizeBytes int64, sha256 string) bool {
	if head == nil || sizeBytes > 0 && head.ContentLength != sizeBytes {
		return false
	}
	metadataHash := normalizeSHA256(metadataValue(head.Metadata, "sha256"))
	return metadataHash != "" && metadataHash == sha256
}

func abortOSSMultipartUpload(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey, uploadID string) error {
	if !validOSSMultipartUploadID(uploadID) {
		return errors.New("invalid OSS multipart upload ID")
	}
	_, err := client.AbortMultipartUpload(ctx, &aliyunoss.AbortMultipartUploadRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey), UploadId: aliyunoss.Ptr(uploadID),
	})
	var serviceError *aliyunoss.ServiceError
	if errors.As(err, &serviceError) && serviceError.Code == "NoSuchUpload" {
		return nil
	}
	return err
}

func validOSSMultipartUploadID(value string) bool {
	if len(value) < 8 || len(value) > 256 {
		return false
	}
	for _, current := range value {
		if current <= 0x20 || current == '/' || current == '\\' {
			return false
		}
	}
	return true
}
