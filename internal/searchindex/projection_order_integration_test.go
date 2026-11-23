package searchindex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestSearchProjectionRetainsLatestCreatorNameAcrossWorkersIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("enable the owned PostgreSQL fixture for search projection ordering")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	fixture, err := database.NewIsolatedTestSchema(ctx, config.Load().DB.ConnString(), 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := fixture.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	pool := fixture.Pool
	if _, err := pool.Exec(ctx, `truncate search_index_queue`); err != nil {
		t.Fatal(err)
	}
	var creatorID int64
	const previousName, currentName = "Previous author name", "Current author name"
	if err := pool.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status)
		values('author',$1,lower($1),'approved') returning id`, previousName).Scan(&creatorID); err != nil {
		t.Fatal(err)
	}

	previousAtProvider := make(chan struct{})
	releasePrevious := make(chan struct{})
	var releaseOnce sync.Once
	allowPrevious := func() { releaseOnce.Do(func() { close(releasePrevious) }) }
	var mutex sync.Mutex
	var applied []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/collections/order_creators/documents/import" {
			http.Error(w, "unexpected fixture request", http.StatusBadRequest)
			return
		}
		var document struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&document); err != nil || document.ID != documentKey("creator", creatorID) {
			http.Error(w, "unexpected fixture document", http.StatusBadRequest)
			return
		}
		if document.Name == previousName {
			close(previousAtProvider)
			select {
			case <-releasePrevious:
			case <-r.Context().Done():
				return
			}
		}
		mutex.Lock()
		applied = append(applied, document.Name)
		mutex.Unlock()
		_, _ = io.WriteString(w, "{\"success\":true}\n")
	}))
	defer server.Close()
	defer allowPrevious()
	client := New(config.TypesenseConfig{
		Enabled: true, URL: server.URL, CollectionPrefix: "order", Timeout: 8 * time.Second,
	})
	workerA, workerB := NewWorker(pool, client), NewWorker(pool, client)
	firstDone := make(chan error, 1)
	go func() { firstDone <- workerA.drain(ctx) }()
	select {
	case <-previousAtProvider:
	case err := <-firstDone:
		t.Fatalf("previous worker returned before its write reached the provider: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := pool.Exec(ctx, `update creators set name=$2,normalized_name=lower($2),updated_at=clock_timestamp() where id=$1`, creatorID, currentName); err != nil {
		t.Fatal(err)
	}
	if err := workerB.drain(ctx); err != nil {
		t.Fatal(err)
	}
	allowPrevious()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	// The losing worker polls again after the active projection writer releases
	// its lease. A newer committed title must remain queued until that poll.
	if err := workerB.drain(ctx); err != nil {
		t.Fatal(err)
	}
	var authoritative string
	var pending int
	if err := pool.QueryRow(ctx, `select name,(select count(*) from search_index_queue where document_type='creator' and document_id=$1) from creators where id=$1`, creatorID).Scan(&authoritative, &pending); err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if authoritative != currentName || pending != 0 || len(applied) != 2 || applied[len(applied)-1] != currentName {
		t.Fatalf("creator projection after both acknowledgements: PostgreSQL name=%q pending=%d applied=%q", authoritative, pending, applied)
	}
}
