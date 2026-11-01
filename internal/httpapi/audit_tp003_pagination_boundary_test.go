package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditTP003PaginationMetadataAndThousandRecordBoundary(t *testing.T) {
	for _, mode := range []string{"oversized", "wrong-index", "wrong-result-count", "negative-total", "empty-not-advanced", "legal-at-record-1000", "budget-1001"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				index, err := strconv.Atoi(r.URL.Query().Get("index"))
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if r.URL.Query().Get("pageSize") != "50" {
					t.Error("page budget changed")
				}
				count, total := 50, 1000
				switch mode {
				case "oversized":
					count = 51
				case "negative-total":
					total = -1
				case "empty-not-advanced":
					count = 0
					total = 51
				case "budget-1001":
					total = 1001
				}
				files := make([]curseForgePackFile, count)
				for i := range files {
					files[i].ID = int64(index + i + 1)
				}
				if mode == "legal-at-record-1000" && index == 950 {
					files[49] = curseForgePackFile{ID: 1000, ReleaseType: 1, FileStatus: 4, IsAvailable: true, FileName: "boundary.zip", FileDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
				}
				pagination := map[string]int{"index": index, "resultCount": count, "totalCount": total}
				if mode == "wrong-index" {
					pagination["index"] = index + 1
				}
				if mode == "wrong-result-count" {
					pagination["resultCount"] = count - 1
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": files, "pagination": pagination})
			}))
			defer upstream.Close()
			cfg := defaultModImportConfig()
			cfg.CurseForge.BaseURL = upstream.URL
			selected, err := loadCurseForgePackFile(context.Background(), upstream.Client(), cfg, 1, publicProviderHeaders("owned-pagination-regression"))
			if mode == "legal-at-record-1000" {
				if err != nil || selected.ID != 1000 || calls.Load() != 20 {
					t.Fatalf("record1000 selection=%d calls=%d error=%v", selected.ID, calls.Load(), err)
				}
				return
			}
			expected := map[string]string{"oversized": "oversized page", "wrong-index": "metadata is inconsistent", "wrong-result-count": "metadata is inconsistent", "negative-total": "metadata is inconsistent", "empty-not-advanced": "did not advance", "budget-1001": "budget exhausted"}[mode]
			wantCalls := int32(1)
			if mode == "budget-1001" {
				wantCalls = 20
			}
			if err == nil || !strings.Contains(err.Error(), expected) || selected.ID != 0 || calls.Load() != wantCalls {
				t.Fatalf("exact refusal=%s calls=%d selected=%d error=%v", expected, calls.Load(), selected.ID, err)
			}
		})
	}
}

func TestAuditTP003SecondPageTimeoutPreservesDeadlineAndTwoRequestBound(t *testing.T) {
	var calls atomic.Int32
	pageTwo := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("index") == "50" {
			close(pageTwo)
			<-r.Context().Done()
			return
		}
		files := make([]curseForgePackFile, 50)
		for i := range files {
			files[i].ID = int64(i + 1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": files, "pagination": map[string]int{"index": 0, "resultCount": 50, "totalCount": 51}})
	}))
	defer upstream.Close()
	cfg := defaultModImportConfig()
	cfg.CurseForge.BaseURL = upstream.URL
	cfg.RequestTimeoutSeconds = 1
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type outcome struct {
		selected curseForgePackFile
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		selected, err := loadCurseForgePackFile(ctx, upstream.Client(), cfg, 1, publicProviderHeaders("owned-pagination-regression"))
		done <- outcome{selected, err}
	}()
	select {
	case <-pageTwo:
	case <-ctx.Done():
		t.Fatal("second page never reached")
	}
	select {
	case result := <-done:
		if !errors.Is(result.err, context.DeadlineExceeded) || !strings.Contains(result.err.Error(), "pagination failed") || result.selected.ID != 0 || calls.Load() != 2 {
			t.Fatalf("second-page deadline result=%v selected=%d calls=%d", result.err, result.selected.ID, calls.Load())
		}
	case <-ctx.Done():
		t.Fatal("one-second total pagination deadline was not honored")
	}
	t.Log("TP003 page2 actually reached and withheld until production total deadline; exact wrapped DeadlineExceeded, two requests, no candidate selected; no retry or budget relaxation")
}
