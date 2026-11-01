package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

func TestAuditCMP005CopierKeepsTwoGiBBoundaryAndEncryptionRequestContract(t *testing.T) {
	for _, size := range []int64{128, maxOSSUploadBytes} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			var mu sync.Mutex
			var ranges []string
			single, initiated, completed := 0, 0, 0
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				w.Header().Set("Content-Type", "application/xml")
				if r.Method == http.MethodPost && q.Has("uploads") {
					if r.Header.Get("x-oss-server-side-encryption") != "KMS" || r.Header.Get("x-oss-server-side-encryption-key-id") != "owned-synthetic-key" || r.Header.Get("x-oss-storage-class") != "IA" {
						t.Error("multipart copy changed original SSE/KMS/storage request policy")
					}
					mu.Lock()
					initiated++
					mu.Unlock()
					fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>owned-copy</Bucket><Key>candidate</Key><UploadId>owned-copy-parts</UploadId></InitiateMultipartUploadResult>`)
					return
				}
				if r.Method == http.MethodPut && q.Get("uploadId") == "owned-copy-parts" {
					if r.Header.Get("x-oss-copy-source") != "/owned-copy/source.zip" {
						t.Error("part copy source identity changed")
					}
					mu.Lock()
					ranges = append(ranges, r.Header.Get("x-oss-copy-source-range"))
					mu.Unlock()
					fmt.Fprint(w, `<CopyPartResult><LastModified>2026-10-24T00:00:00.000Z</LastModified><ETag>owned-part</ETag></CopyPartResult>`)
					return
				}
				if r.Method == http.MethodPost && q.Get("uploadId") == "owned-copy-parts" {
					mu.Lock()
					completed++
					mu.Unlock()
					fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>owned-copy</Bucket><Key>candidate</Key><ETag>owned-final</ETag></CompleteMultipartUploadResult>`)
					return
				}
				if r.Method == http.MethodPut && r.Header.Get("x-oss-copy-source") != "" {
					if r.Header.Get("x-oss-server-side-encryption") != "KMS" || r.Header.Get("x-oss-server-side-encryption-key-id") != "owned-synthetic-key" || r.Header.Get("x-oss-storage-class") != "IA" {
						t.Error("single copy changed original SSE/KMS/storage request policy")
					}
					mu.Lock()
					single++
					mu.Unlock()
					fmt.Fprint(w, `<CopyObjectResult><LastModified>2026-10-24T00:00:00.000Z</LastModified><ETag>owned-copy</ETag></CopyObjectResult>`)
					return
				}
				http.Error(w, "unexpected copy operation", 500)
			}))
			defer provider.Close()
			cfg := defaultOSSConfig()
			cfg.Enabled = true
			cfg.Endpoint = provider.URL
			cfg.Bucket = "owned-copy"
			cfg.UseCName = true
			cfg.Region = "cn-test"
			cfg.AccessKeyID = "owned-copy-key"
			cfg.AccessKeySecret = "owned-copy-only-synthetic"
			client := newOSSClient(cfg, cfg.Endpoint, true)
			head := &aliyunoss.HeadObjectResult{ContentLength: size, ServerSideEncryption: aliyunoss.Ptr("KMS"), ServerSideEncryptionKeyId: aliyunoss.Ptr("owned-synthetic-key"), StorageClass: aliyunoss.Ptr("IA")}
			sealed := "uploads/" + ossSealedUploadComponent + "/11111111-1111-4111-8111-111111111111/source.zip"
			if err := copyOSSUploadToSealedKey(context.Background(), client, cfg, "source.zip", sealed, head); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if size == 128 {
				if single != 1 || initiated != 0 || len(ranges) != 0 || completed != 0 {
					t.Fatalf("ordinary copy operations %d/%d/%d/%d", single, initiated, len(ranges), completed)
				}
			} else {
				if single != 0 || initiated != 1 || completed != 1 || len(ranges) != 32 {
					t.Fatalf("2GiB copy operations %d/%d/%d/%d", single, initiated, len(ranges), completed)
				}
				sort.Slice(ranges, func(i, j int) bool {
					var a, b int64
					fmt.Sscanf(strings.TrimPrefix(ranges[i], "bytes="), "%d-", &a)
					fmt.Sscanf(strings.TrimPrefix(ranges[j], "bytes="), "%d-", &b)
					return a < b
				})
				for i, interval := range ranges {
					want := fmt.Sprintf("bytes=%d-%d", int64(i)*(64<<20), int64(i+1)*(64<<20)-1)
					if interval != want {
						t.Fatalf("2GiB copy range %d=%s want=%s", i, interval, want)
					}
				}
			}
			t.Log("Actual SDK request contract only:2GiB multipart ranges and original encryption/storage headers; payload hashing and actual vendor encryption are separate layers")
		})
	}
}
