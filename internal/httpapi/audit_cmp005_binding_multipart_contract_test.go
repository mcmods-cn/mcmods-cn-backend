package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditCMP005CandidateMultipartCleanupUsesExactOwnedKeyAndBoundedPages(t *testing.T) {
	for _, mode := range []string{"owned-and-foreign", "budget", "pagination-stuck", "list-denied"} {
		t.Run(mode, func(t *testing.T) {
			b := &ossUploadByteBinding{ID: "11111111-1111-4111-8111-111111111111"}
			b.SourceKey = "uploads/" + ossSealedUploadComponent + "/" + b.ID + "/source.zip"
			var lists, aborts int
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if r.Method == http.MethodGet && q.Has("uploads") {
					lists++
					if q.Get("prefix") != b.SourceKey || q.Get("max-uploads") != "32" {
						t.Error("multipart cleanup widened its exact receipt candidate or page budget")
					}
					w.Header().Set("Content-Type", "application/xml")
					if mode == "list-denied" {
						w.WriteHeader(403)
						fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>owned protocol denial</Message></Error>`)
						return
					}
					if mode == "owned-and-foreign" {
						fmt.Fprintf(w, `<ListMultipartUploadsResult><IsTruncated>false</IsTruncated><Upload><Key>%s</Key><UploadId>owned-abandoned-copy</UploadId></Upload><Upload><Key>%s-unrelated</Key><UploadId>foreign-prefix-only</UploadId></Upload></ListMultipartUploadsResult>`, b.SourceKey, b.SourceKey)
						return
					}
					marker := fmt.Sprint(lists)
					if mode == "pagination-stuck" {
						marker = "unchanged-marker"
					}
					fmt.Fprintf(w, `<ListMultipartUploadsResult><IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextUploadIdMarker>%s</NextUploadIdMarker></ListMultipartUploadsResult>`, b.SourceKey, marker)
					return
				}
				if r.Method == http.MethodDelete && q.Get("uploadId") != "" {
					requestKey := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), b.cfg.Bucket+"/")
					if requestKey != b.SourceKey || q.Get("uploadId") != "owned-abandoned-copy" {
						t.Error("multipart cleanup attempted a foreign key or upload ID")
					}
					aborts++
					w.WriteHeader(204)
					return
				}
				http.Error(w, "unexpected ordinary cleanup operation", 400)
			}))
			defer provider.Close()
			cfg := defaultOSSConfig()
			cfg.Bucket, cfg.Endpoint, cfg.Region = "owned-multipart-copy", provider.URL, "cn-test"
			cfg.AccessKeyID, cfg.AccessKeySecret = "owned-synthetic", "owned-synthetic-only"
			b.cfg = cfg
			client := newOSSClient(cfg, cfg.Endpoint, true)
			err := cleanupOSSByteBindingMultiparts(context.Background(), client, b, b.SourceKey)
			if mode == "owned-and-foreign" {
				if err != nil || lists != 1 || aborts != 1 {
					t.Fatalf("exact owned multipart cleanup=%v lists=%d aborts=%d", err, lists, aborts)
				}
			} else if err == nil || aborts != 0 || lists > 4 {
				t.Fatalf("budget/denial/stuck pagination was claimed complete: err=%v lists=%d aborts=%d", err, lists, aborts)
			}
			if err = cleanupOSSByteBindingMultiparts(context.Background(), client, b, "uploads/another-attempt/source.zip"); err == nil {
				t.Fatal("cleanup accepted a key outside this receipt UUID")
			}
		})
	}
}
