package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditGR001RawMultipartAbsenceUsesExactKeyAndBoundedReadOnlyListing(t *testing.T) {
	for _, mode := range []string{"none", "foreign-prefix", "exact-unsettled", "denied", "stuck", "budget"} {
		t.Run(mode, func(t *testing.T) {
			const key = "owned-original/source.zip"
			calls := 0
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || !r.URL.Query().Has("uploads") || r.URL.Query().Get("prefix") != key || r.URL.Query().Get("max-uploads") != "32" {
					t.Error("raw eligibility guard widened prefix/budget or performed a provider write")
					http.Error(w, "wrong request", 400)
					return
				}
				w.Header().Set("Content-Type", "application/xml")
				switch mode {
				case "none":
					fmt.Fprint(w, `<ListMultipartUploadsResult><IsTruncated>false</IsTruncated></ListMultipartUploadsResult>`)
				case "foreign-prefix":
					fmt.Fprintf(w, `<ListMultipartUploadsResult><IsTruncated>false</IsTruncated><Upload><Key>%s-neighbor</Key><UploadId>foreign-upload</UploadId></Upload></ListMultipartUploadsResult>`, key)
				case "exact-unsettled":
					fmt.Fprintf(w, `<ListMultipartUploadsResult><IsTruncated>false</IsTruncated><Upload><Key>%s</Key><UploadId>owned-unsettled</UploadId></Upload></ListMultipartUploadsResult>`, key)
				case "denied":
					w.WriteHeader(403)
					fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>owned synthetic listing denial</Message></Error>`)
				case "stuck":
					fmt.Fprintf(w, `<ListMultipartUploadsResult><IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextUploadIdMarker>same-upload</NextUploadIdMarker></ListMultipartUploadsResult>`, key)
				case "budget":
					fmt.Fprintf(w, `<ListMultipartUploadsResult><IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextUploadIdMarker>page-%d</NextUploadIdMarker></ListMultipartUploadsResult>`, key, calls)
				}
			}))
			defer provider.Close()
			cfg := defaultOSSConfig()
			cfg.Bucket, cfg.Region, cfg.Endpoint = "owned-raw", "cn-owned", provider.URL
			cfg.AccessKeyID, cfg.AccessKeySecret = "owned-synthetic", "owned-synthetic-only"
			absent, err := ossDeletedRawMultipartAbsent(context.Background(), newOSSClient(cfg, cfg.Endpoint, true), cfg.Bucket, key)
			want := mode == "none" || mode == "foreign-prefix"
			if absent != want || (mode == "denied") != (err != nil) || calls > 4 {
				t.Fatalf("raw multipart absence mode%s got%v err%v calls%d", mode, absent, err, calls)
			}
		})
	}
}
