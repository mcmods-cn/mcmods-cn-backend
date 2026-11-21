package httpapi

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type auditTP008OwnedOSS struct {
	mu       sync.Mutex
	objects  map[string]test036Object
	requests map[string]int
	cfg      ossConfigPayload
	server   *httptest.Server
}

// Only this owned ordinary OSS protocol fixture is used. Production upload,
// byte sealing, quota, scan-status API and gallery binding code are unchanged.
func newAuditTP008OwnedOSS(t *testing.T) *auditTP008OwnedOSS {
	t.Helper()
	store := &auditTP008OwnedOSS{objects: map[string]test036Object{}, requests: map[string]int{}, cfg: defaultOSSConfig()}
	store.cfg.Enabled, store.cfg.UseCName = true, true
	store.cfg.Bucket, store.cfg.Region = "test037-owned", "cn-test"
	store.cfg.AccessKeyID, store.cfg.AccessKeySecret = "tp008-owned-synthetic-id", "tp008-owned-synthetic-secret"
	store.cfg.DownloadURLMode = "oss_presigned"
	store.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			if r.Header.Get("Origin") != "http://127.0.0.1:3551" {
				http.Error(w, "undeclared browser origin", 403)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "content-type,x-oss-meta-sha256")
			w.Header().Set("Access-Control-Expose-Headers", "ETag")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/")
		key = strings.TrimPrefix(key, store.cfg.Bucket+"/")
		if !strings.HasPrefix(key, "mcmods/user/") {
			http.NotFound(w, r)
			return
		}
		failure := func(status int, code string) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code><Message>owned TP008 protocol fixture</Message></Error>", code)
		}
		if r.Method == http.MethodGet && r.Header.Get("Authorization") == "" && !validTEST037PresignedRequest(r, key, store.cfg, time.Now()) {
			failure(403, "SignatureDoesNotMatch")
			return
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		store.requests[r.Method]++
		etag := func(data []byte) string { hash := md5.Sum(data); return `"` + hex.EncodeToString(hash[:]) + `"` }
		switch r.Method {
		case http.MethodPut:
			if source := r.Header.Get("x-oss-copy-source"); source != "" {
				if r.Header.Get("Authorization") == "" || !isOSSSealedObjectKey(key) {
					failure(403, "AccessDenied")
					return
				}
				parsed, err := url.PathUnescape(source)
				if err != nil {
					failure(400, "InvalidArgument")
					return
				}
				sourceKey := strings.TrimPrefix(parsed, "/"+store.cfg.Bucket+"/")
				object, ok := store.objects[sourceKey]
				if !ok || !strings.HasPrefix(sourceKey, "mcmods/user/") {
					failure(404, "NoSuchKey")
					return
				}
				object.data = bytes.Clone(object.data)
				store.objects[key] = object
				w.Header().Set("Content-Type", "application/xml")
				_ = xml.NewEncoder(w).Encode(struct {
					XMLName      xml.Name `xml:"CopyObjectResult"`
					LastModified string   `xml:"LastModified"`
					ETag         string   `xml:"ETag"`
				}{LastModified: time.Now().UTC().Format(time.RFC3339), ETag: etag(object.data)})
				return
			}
			if isOSSSealedObjectKey(key) && r.Header.Get("Authorization") == "" {
				failure(403, "AccessDenied")
				return
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
			if err != nil || len(data) < 1 || len(data) > 1<<20 {
				failure(400, "InvalidArgument")
				return
			}
			store.objects[key] = test036Object{data: data, contentType: r.Header.Get("Content-Type"), hash: r.Header.Get("x-oss-meta-sha256")}
			w.Header().Set("ETag", etag(data))
			w.WriteHeader(200)
		case http.MethodGet, http.MethodHead:
			object, ok := store.objects[key]
			if !ok {
				failure(404, "NoSuchKey")
				return
			}
			w.Header().Set("Content-Type", object.contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(object.data)))
			w.Header().Set("ETag", etag(object.data))
			w.Header().Set("x-oss-meta-sha256", object.hash)
			w.Header().Set("Last-Modified", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
			if r.Method == http.MethodGet {
				_, _ = w.Write(object.data)
			}
		case http.MethodDelete:
			if r.Header.Get("Authorization") == "" {
				failure(403, "AccessDenied")
				return
			}
			delete(store.objects, key)
			w.WriteHeader(204)
		default:
			failure(405, "MethodNotAllowed")
		}
	}))
	store.cfg.Endpoint, store.cfg.PublicEndpoint = store.server.URL, store.server.URL
	t.Cleanup(func() { store.server.CloseClientConnections(); store.server.Close() })
	return store
}

func (store *auditTP008OwnedOSS) snapshot() map[string]any {
	store.mu.Lock()
	defer store.mu.Unlock()
	counts := map[string]int{}
	for key, value := range store.requests {
		counts[key] = value
	}
	assets := []map[string]any{}
	for key, object := range store.objects {
		assets = append(assets, map[string]any{"keySha256": sha256Hex([]byte(key)), "bytes": len(object.data), "sha256": sha256Hex(object.data), "sealed": isOSSSealedObjectKey(key)})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i]["keySha256"].(string) < assets[j]["keySha256"].(string) })
	return map[string]any{"scope": "owned OSS protocol fixture, unchanged production upload/hash/scan/binding paths; no cloud/antivirus-engine claim", "requests": counts, "objects": assets}
}
