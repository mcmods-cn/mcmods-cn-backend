package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02DeletedBlueprintCannotIssueVariantDownloadIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	if _, err := pool.Exec(context.Background(), `create table blueprints(id bigint,public_id text,owner_id bigint,status text,review_status text);
	 create table blueprint_variants(blueprint_id bigint,public_id text,status text,object_key text);
	 insert into blueprints values(7,'b00000007',42,'deleted','approved');
	 insert into blueprint_variants values(7,'v00000007','ready','fixture-private-original')`); err != nil {
		t.Fatal(err)
	}
	for _, claims := range []security.Claims{{}, {Subject: 42}, {Subject: 43, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true}}}} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/b00000007/variants/v00000007/download", nil)
		request.SetPathValue("publicId", "b00000007")
		request.SetPathValue("variantId", "v00000007")
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
		response := httptest.NewRecorder()
		(&Server{db: pool}).downloadBlueprintVariant(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("deleted download role=%d status=%d body=%s", claims.Subject, response.Code, response.Body.String())
		}
	}
}

func TestOCT02UploadCompletionDoesNotResurrectDeletedBlueprintIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `create table blueprints(id bigint,public_id text,owner_id bigint,status text,source_format text,original_object_key text,
	 original_file_id bigint,created_at timestamptz default now());
	 insert into blueprints(id,public_id,owner_id,status,source_format,original_object_key) values(7,'b00000007',42,'uploading','schem','fixture-source')`); err != nil {
		t.Fatal(err)
	}
	blocking, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocking.Rollback(context.Background())
	if _, err = blocking.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('blueprint:42:5',0))`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := (&Server{db: pool}).completeBlueprintUpload(ctx, 5, 42, "fixture-source", "fixture.schem", "application/octet-stream", 10, "fixture-hash")
		result <- err
	}()
	// Wait for this database's upload completion to reach the held admission
	// lock, proving its earlier unlocked read saw uploading before deletion.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err = pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where datname=current_database()
		 and wait_event='advisory' and query like '%pg_advisory_xact_lock(hashtextextended($1%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("upload completion did not reach its lock")
		case <-ticker.C:
		}
	}
	if _, err = pool.Exec(ctx, `update blueprints set status='deleted' where id=7`); err != nil {
		t.Fatal(err)
	}
	if err = blocking.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, errBlueprintNotFound) {
		t.Fatalf("late upload completion error=%v, want lifecycle rejection", err)
	}
	var state string
	if err = pool.QueryRow(ctx, `select status from blueprints where id=7`).Scan(&state); err != nil || state != "deleted" {
		t.Fatalf("deleted blueprint state=%s error=%v", state, err)
	}
}
