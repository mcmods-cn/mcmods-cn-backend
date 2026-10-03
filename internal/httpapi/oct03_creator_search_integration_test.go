package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/searchindex"
)

func TestOCT03CreatorCatalogRealTypesenseCursorIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_TYPESENSE_INTEGRATION") != "1" {
		t.Skip("requires explicit owned Typesense binary and isolated PostgreSQL")
	}
	binary := os.Getenv("MCMODS_TEST_TYPESENSE_BINARY")
	if binary == "" || !filepath.IsAbs(binary) {
		t.Fatal("requires an explicit absolute Typesense test binary path")
	}
	f := newTEST013Fixture(t)
	var databaseName string
	if err := f.db.QueryRow(f.ctx, "select current_database()").Scan(&databaseName); err != nil || !strings.HasPrefix(databaseName, "test018_project_update_") {
		t.Fatal("requires the nonce child database")
	}
	root := t.TempDir()
	dataDirectory := filepath.Join(root, "data")
	if err := os.Mkdir(dataDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	peerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	peerPort := peerListener.Addr().(*net.TCPAddr).Port
	if err := peerListener.Close(); err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	key := hex.EncodeToString(nonce)
	configPath := filepath.Join(root, "typesense.ini")
	configuration := fmt.Sprintf("[server]\ndata-dir=%s\napi-key=%s\napi-address=127.0.0.1\napi-port=%d\npeering-address=127.0.0.1\npeering-port=%d\nthread-pool-size=2\n", dataDirectory, key, port, peerPort)
	if err := os.WriteFile(configPath, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--config", configPath)
	// Native TYPESENSE_* selectors from another owned service must not select
	// its key, listener or data directory; retain the session proxy/CA variables.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TYPESENSE_") {
			command.Env = append(command.Env, entry)
		}
	}
	// Configuration contains a random synthetic key. Neither it nor raw server
	// logs are printed, and no inherited production endpoint is selected.
	if err := command.Start(); err != nil {
		t.Fatalf("start owned Typesense process: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = command.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("owned Typesense process did not exit")
		}
	})
	client := searchindex.New(config.TypesenseConfig{Enabled: true, URL: fmt.Sprintf("http://127.0.0.1:%d", port), APIKey: key, CollectionPrefix: "oct03_creator", Timeout: 2 * time.Second})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := client.Health(f.ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned Typesense failed its loopback health check")
		}
		select {
		case <-done:
			t.Fatal("owned Typesense exited during startup")
		case <-time.After(25 * time.Millisecond):
		}
	}
	// Match the production creator projection's typed fields, filters and alias;
	// real Typesense ranks rows that the production SQL handler then materializes.
	schema := searchindex.CollectionSchema{Name: "oct03_creator_owned", DefaultSortingField: "updated_at", Fields: []searchindex.Field{
		{Name: "internal_id", Type: "int64"}, {Name: "kind", Type: "string", Facet: true}, {Name: "name", Type: "string", Infix: true}, {Name: "text", Type: "string[]"},
		{Name: "review_status", Type: "string", Facet: true}, {Name: "created_by", Type: "int64", Facet: true}, {Name: "claimed_user_id", Type: "int64", Facet: true}, {Name: "updated_at", Type: "int64"},
	}}
	if err := client.CreateCollection(f.ctx, schema); err != nil {
		t.Fatal(err)
	}
	names := []string{"Needle Alpha", "Needle Beta", "Needle Gamma", "Needle Hidden", "Needle Team"}
	var documents []map[string]any
	publicIDs := make([]string, len(names))
	for index, name := range names {
		kind, status := "author", "approved"
		if index == 3 {
			status = "pending"
		}
		if index == 4 {
			kind = "team"
		}
		var internalID int64
		if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values($1,$2,lower($2),$3) returning id,public_id`, kind, name, status).Scan(&internalID, &publicIDs[index]); err != nil {
			t.Fatal(err)
		}
		documents = append(documents, map[string]any{"id": fmt.Sprint(internalID), "internal_id": internalID, "kind": kind, "name": name, "text": []string{"Synthetic shared needle"}, "review_status": status, "created_by": int64(0), "claimed_user_id": int64(0), "updated_at": int64(index + 1)})
	}
	if err := client.ImportDocuments(f.ctx, schema.Name, documents); err != nil {
		t.Fatal(err)
	}
	if err := client.UpsertAlias(f.ctx, client.Alias("creators"), schema.Name); err != nil {
		t.Fatal(err)
	}
	client.SetReady(true)
	server := f.origin.Config.Handler.(*Server)
	server.search = client
	type responsePage struct {
		Items      []creatorSummary `json:"items"`
		HasMore    bool             `json:"hasMore"`
		NextCursor string           `json:"nextCursor"`
		Counts     struct {
			Author int `json:"author"`
			Team   int `json:"team"`
		} `json:"counts"`
	}
	read := func(path string, wantStatus int) responsePage {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(f.ctx)
		response := httptest.NewRecorder()
		server.creators(response, request)
		if response.Code != wantStatus {
			t.Fatalf("creator handler status=%d want=%d body=%s", response.Code, wantStatus, response.Body.String())
		}
		if wantStatus != http.StatusOK {
			return responsePage{}
		}
		var envelope struct {
			Data responsePage `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	path := "/api/v1/creators?query=Needle&kind=author&sort=relevance&order=desc&limit=2"
	first := read(path, http.StatusOK)
	if len(first.Items) != 2 || first.Items[0].PublicID != publicIDs[2] || first.Items[1].PublicID != publicIDs[1] || !first.HasMore || first.NextCursor == "" || first.Counts.Author != 3 || first.Counts.Team != 1 {
		t.Fatalf("real relevance first page=%+v", first)
	}
	secondPath := path + "&cursor=" + url.QueryEscape(first.NextCursor)
	second := read(secondPath, http.StatusOK)
	if len(second.Items) != 1 || second.Items[0].PublicID != publicIDs[0] || second.HasMore || second.NextCursor != "" || second.Counts.Author != 3 || second.Counts.Team != 1 {
		t.Fatalf("real relevance second page=%+v", second)
	}
	read(strings.Replace(secondPath, "kind=author", "kind=team", 1), http.StatusBadRequest)
	read(strings.Replace(secondPath, "query=Needle", "query=Different", 1), http.StatusBadRequest)
	client.SetReady(false)
	read(secondPath, http.StatusServiceUnavailable)
	client.SetReady(true)
	recovered := read(secondPath, http.StatusOK)
	if len(recovered.Items) != 1 || recovered.Items[0].PublicID != publicIDs[0] {
		t.Fatalf("index cursor changed after recovery: %+v", recovered)
	}
	if err := client.DeleteCollection(f.ctx, schema.Name); err != nil {
		t.Fatal(err)
	}
}
