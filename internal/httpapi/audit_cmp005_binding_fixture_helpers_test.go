package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

// A failed attempt is allowed only its exact durable candidate compensation.
// Every pre-existing cleanup row and every original business assertion remains
// unchanged; no client input, unrelated object, file or reward may be adopted.
func auditOSSAssertFailureOnlyCompensatesOwnCandidates(t *testing.T, f test037Fixture, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	for table := range before {
		if _, ok := after[table]; !ok {
			var raw string
			if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(row) order by to_jsonb(row)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` row`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			after[table] = raw
		}
	}
	var oldRows, newRows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(before["oss_object_deletion_outbox"]), &oldRows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(after["oss_object_deletion_outbox"]), &newRows); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]json.RawMessage{}
	for _, row := range oldRows {
		byID[string(row["id"])] = row
	}
	seen := map[string]bool{}
	for _, row := range newRows {
		id := string(row["id"])
		if old, exists := byID[id]; exists {
			if !reflect.DeepEqual(old, row) {
				t.Fatal("failed upload changed an existing cleanup row")
			}
			seen[id] = true
			continue
		}
		var key, bucket, endpoint, status string
		for name, field := range map[string]*string{"object_key": &key, "bucket": &bucket, "endpoint": &endpoint, "status": &status} {
			if err := json.Unmarshal(row[name], field); err != nil {
				t.Fatal(err)
			}
		}
		if status != "pending" || !isOSSSealedObjectKey(key) || string(row["oss_file_id"]) != "null" {
			t.Fatal("failed upload queued an unauthorized deletion target")
		}
		var owned bool
		if err := f.db.QueryRow(f.ctx, `select exists(select 1 from oss_upload_byte_bindings where owner_id=$1 and bucket=$2 and endpoint=$3 and status='cleanup_pending' and file_id is null and upload_object_key<>$4 and (sealed_source_key=$4 or stored_object_key=$4))`, f.userIDs[f.editor], bucket, endpoint, key).Scan(&owned); err != nil || !owned {
			t.Fatalf("cleanup target lacks exact unadopted attempt ownership: %v", err)
		}
	}
	if len(seen) != len(byID) {
		t.Fatal("failed upload removed pre-existing cleanup intents")
	}
	after["oss_object_deletion_outbox"] = before["oss_object_deletion_outbox"]
	if !reflect.DeepEqual(before, after) {
		for table, raw := range before {
			if after[table] != raw {
				t.Errorf("failed upload changed business %s", table)
			}
		}
		t.FailNow()
	}
}

// This protocol fixture records a real WebP golden only when production asks
// to process this upload's verified sealed bytes and explicitly names the same
// reserved destination. It never supplies JSON/API results to the application.
func auditOSSInstallBindingWebPConversion(t *testing.T, f test037Fixture, ticket test037Ticket, converted []byte) func() (string, int) {
	t.Helper()
	original := f.provider.Config.Handler
	var mu sync.Mutex
	last := ""
	calls := 0
	f.provider.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), f.cfg.Bucket+"/")
		if r.Method == http.MethodPost && isOSSSealedObjectKey(key) && path.Base(key) == path.Base(ticket.ObjectKey) {
			destination := persistedWebPObjectKey(key)
			body, err := io.ReadAll(io.LimitReader(r.Body, 8193))
			if err != nil || len(body) > 8192 {
				http.Error(w, "invalid process body", 400)
				return
			}
			values, err := url.ParseQuery(string(body))
			if err != nil || values.Get("x-oss-process") != "image/format,webp|sys/saveas,o_"+base64.RawURLEncoding.EncodeToString([]byte(destination)) {
				http.Error(w, "wrong explicit process destination", 400)
				return
			}
			f.store.mu.Lock()
			source, exists := f.store.objects[key]
			if !exists || sha256Hex(source.data) != ticket.SHA256 {
				f.store.mu.Unlock()
				http.Error(w, "source bytes do not match upload", 400)
				return
			}
			f.store.objects[destination] = test036Object{data: bytes.Clone(converted), contentType: "image/webp"}
			f.store.mu.Unlock()
			mu.Lock()
			last = destination
			calls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"OK"}`)
			return
		}
		original.ServeHTTP(w, r)
	})
	return func() (string, int) { mu.Lock(); defer mu.Unlock(); return last, calls }
}
