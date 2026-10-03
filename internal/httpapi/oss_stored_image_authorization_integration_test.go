package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

type storedImageQueryCounter struct {
	queries        atomic.Int64
	previewQueries atomic.Int64
}

func (counter *storedImageQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "select file.object_key from oss_files file") {
		counter.queries.Add(1)
	}
	if strings.Contains(data.SQL, "unnest($1::bigint[],$2::text[],$3::bigint[],$4::text[],$5::text[])") {
		counter.previewQueries.Add(1)
	}
	return ctx
}

func (*storedImageQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestStoredOSSImagesDoNotSignUnboundPrivateFilesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	counter := &storedImageQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	}()
	if _, err = pool.Exec(ctx, `
		insert into users(id,username,email,password_hash) values
		(99280001,'private-image-owner','private-image-owner@example.invalid','test-only'),
		(99280002,'private-image-other','private-image-other@example.invalid','test-only');
		insert into oss_files(id,bucket,endpoint,object_key,category,source,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status)
		values(99280003,'mcmods-test','https://oss.example.invalid','private/evidence.png','user/private','user',
		'evidence.png','image/png',128,repeat('a',64),99280001,'active','clean');
		insert into mods(id,project_code,slug,primary_name,review_status,submitted_by,icon_url)
		values(99280004,'image0001','image-project','synthetic project','approved',99280002,'https://oss.example.invalid/private/evidence.png');
	`); err != nil {
		t.Fatal(err)
	}
	cfg := ossConfigPayload{
		Enabled: true, Region: "cn-hangzhou", Endpoint: "https://oss-cn-hangzhou.aliyuncs.com",
		PublicEndpoint: "https://oss.example.invalid", Bucket: "mcmods-test",
		AccessKeyID: "synthetic-access-key", AccessKeySecret: "synthetic-test-secret",
	}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-image-test-key-at-least-32-characters"}}
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := security.EncryptSetting(server.cfg.SettingsEncryptionKey, rawCfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, string(sealed)); err != nil {
		t.Fatal(err)
	}
	const image = "https://oss.example.invalid/private/evidence.png"
	assertAccess := func(requestCtx context.Context, raw string, want bool) {
		t.Helper()
		resolved, err := server.resolveStoredOSSObjectAccessURLWithConfig(requestCtx, cfg, raw)
		if err != nil {
			t.Fatal(err)
		}
		if want {
			if !strings.Contains(strings.ToLower(resolved), "x-oss-signature") {
				t.Fatalf("authorized image was not signed: %q", resolved)
			}
		} else if resolved != "" {
			t.Fatal("unbound private image received an access URL")
		}
	}
	assertAccess(ctx, image, false)
	assertAccess(ctx, cfg.Endpoint+"/private/evidence.png", false)
	ownerCtx := context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 99280001})
	assertAccess(ownerCtx, image, true)
	assertAccess(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 99280002}), image, false)
	for _, actorID := range []int64{0, 99280002, 99280001} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		validated, validationErr := validateStoredProjectIconURL(ctx, tx, cfg, image+"?old-signature=synthetic", image, actorID)
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if actorID == 99280001 {
			if validationErr != nil || validated != image {
				t.Fatalf("owner could not bind normalized icon: %v", validationErr)
			}
		} else if !errors.Is(validationErr, errInvalidStoredProjectIcon) {
			t.Fatal("private icon accepted merely because it was already in a snapshot")
		}
	}
	// A published malicious snapshot is not proof that its submitter owns the file.
	if _, err = pool.Exec(ctx, `insert into content_revisions(id,entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by)
		values(99280005,'mod',99280004,'mod','image0001',1,jsonb_build_object('iconUrl',$1::text),'synthetic',99280002)`, image); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,submitted_by)
		values('mod',99280004,'mod','image0001',99280005,'approved',99280002)`); err != nil {
		t.Fatal(err)
	}
	assertAccess(ctx, image, false)
	if _, err = pool.Exec(ctx, `insert into content_revisions(id,entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by)
		values(99280006,'mod',99280004,'mod','image0001',2,jsonb_build_object('iconUrl',$1::text),'synthetic',99280001)`, image); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,submitted_by)
		values('mod',99280004,'mod','image0001',99280006,'pending',99280001)`); err != nil {
		t.Fatal(err)
	}
	assertAccess(ctx, image, false)
	var maliciousRevision, ownedRevision string
	if err = pool.QueryRow(ctx, `select public_id from content_revisions where id=99280005`).Scan(&maliciousRevision); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select public_id from content_revisions where id=99280006`).Scan(&ownedRevision); err != nil {
		t.Fatal(err)
	}
	scopedReviewer := security.Claims{Subject: 99280002, PermissionRules: []security.PermissionRule{{Code: "project.review.image0001", Allow: true, Priority: 100}}}
	crossReviewer := security.Claims{Subject: 99280002, PermissionRules: []security.PermissionRule{{Code: "project.review.other0001", Allow: true, Priority: 100}}}
	for _, test := range []struct {
		name     string
		claims   security.Claims
		entityID int64
		revision string
		want     bool
	}{
		{"scoped reviewer legitimate pending", scopedReviewer, 99280004, ownedRevision, true},
		{"global reviewer legitimate pending", security.Claims{Subject: 99280002, PermissionRules: []security.PermissionRule{{Code: "content.review", Allow: true, Priority: 100}}}, 99280004, ownedRevision, true},
		{"foreign uploader snapshot", scopedReviewer, 99280004, maliciousRevision, false},
		{"administrator still requires uploader proof", security.Claims{Subject: 99280002, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 100}}}, 99280004, maliciousRevision, false},
		{"cross project scope", crossReviewer, 99280004, ownedRevision, false},
		{"mismatched resource", scopedReviewer, 99280099, ownedRevision, false},
		{"anonymous pending", security.Claims{}, 99280004, ownedRevision, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			previewCtx := context.WithValue(ctx, claimsContextKey, test.claims)
			preview, err := server.resolveStoredProjectRevisionIconURL(previewCtx, cfg, "mod", test.entityID, test.revision, image)
			if err != nil {
				t.Fatal(err)
			}
			if test.want != strings.Contains(strings.ToLower(preview), "x-oss-signature") {
				t.Fatalf("preview authorization mismatch: allowed=%t", test.want)
			}
		})
	}
	previewInputs := make([]storedProjectRevisionIcon, 100)
	for i := range previewInputs {
		previewInputs[i] = storedProjectRevisionIcon{"mod", 99280004, ownedRevision, image}
	}
	beforeQueries, beforeProofs := counter.queries.Load(), counter.previewQueries.Load()
	previewURLs, err := server.resolveStoredProjectRevisionIconURLs(context.WithValue(ctx, claimsContextKey, scopedReviewer), cfg, previewInputs)
	if err != nil {
		t.Fatal(err)
	}
	if counter.queries.Load()-beforeQueries != 1 || counter.previewQueries.Load()-beforeProofs != 1 {
		t.Fatal("100 pending previews did not use two bounded authorization queries")
	}
	for _, previewURL := range previewURLs {
		if !strings.Contains(strings.ToLower(previewURL), "x-oss-signature") {
			t.Fatal("batch omitted legitimate pending preview")
		}
	}
	// Exercise the real HTTP history path with one connection, not just the helper.
	for _, viewer := range []struct {
		claims    security.Claims
		wantOwned bool
	}{{scopedReviewer, true}, {crossReviewer, false}} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/mods/image-project/revisions", nil)
		request.SetPathValue("siteId", "image-project")
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, viewer.claims))
		response := httptest.NewRecorder()
		server.modRevisionHistory(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("revision history returned %d", response.Code)
		}
		var envelope struct {
			Data struct {
				Items []modRevisionResponse `json:"items"`
			} `json:"data"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		seenOwned := false
		for _, item := range envelope.Data.Items {
			if item.ID == maliciousRevision && item.Snapshot.IconURL != "" {
				t.Fatal("HTTP history exposed foreign uploader file")
			}
			if item.ID == ownedRevision {
				seenOwned = true
				if !strings.Contains(strings.ToLower(item.Snapshot.IconURL), "x-oss-signature") {
					t.Fatal("reviewer HTTP history lost legitimate pending icon")
				}
			}
		}
		if seenOwned != viewer.wantOwned {
			t.Fatal("HTTP history escaped precise review scope")
		}
	}
	if err = pool.QueryRow(ctx, `select snapshot->>'iconUrl' from content_revisions where id=99280006`).Scan(&ownedRevision); err != nil || ownedRevision != image {
		t.Fatal("preview changed immutable snapshot")
	}
	if _, err = pool.Exec(ctx, `update change_requests set status='approved' where proposed_revision_id=99280006`); err != nil {
		t.Fatal(err)
	}
	assertAccess(ctx, image, true)
	if _, err = pool.Exec(ctx, `update mods set review_status='pending' where id=99280004`); err != nil {
		t.Fatal(err)
	}
	assertAccess(ctx, image, false)
	if _, err = pool.Exec(ctx, `update users set avatar_file_id=99280003 where id=99280001`); err != nil {
		t.Fatal(err)
	}
	assertAccess(ctx, image, true)
	for _, state := range []struct{ column, value string }{
		{"scan_status", "pending"}, {"status", "deleted"}, {"content_type", "text/plain"},
	} {
		if _, err = pool.Exec(ctx, `update oss_files set `+state.column+`=$1 where id=99280003`, state.value); err != nil {
			t.Fatal(err)
		}
		assertAccess(ctx, image, false)
		assertAccess(ownerCtx, image, false)
		if _, err = pool.Exec(ctx, `update oss_files set status='active',scan_status='clean',content_type='image/png' where id=99280003`); err != nil {
			t.Fatal(err)
		}
	}
	// Signing itself is local cryptography; no object store or external AI is called.
	access, err := server.resolveOSSObjectAccessWithConfig(ctx, cfg, "private/evidence.png", ossObjectAccessOptions{})
	if err != nil || !strings.Contains(strings.ToLower(access.URL), "x-oss-signature") {
		t.Fatal("authorized private download signer changed")
	}
	if _, err = pool.Exec(ctx, `insert into oss_files(bucket,endpoint,object_key,category,source,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status)
		select 'mcmods-test','https://oss.example.invalid','private/page-'||n||'.png','user/private','user','page.png',
		'image/png',128,repeat('a',64),99280001,'active','clean' from generate_series(1,100) n`); err != nil {
		t.Fatal(err)
	}
	urls := make([]string, 100)
	for i := range urls {
		urls[i] = cfg.PublicEndpoint + "/private/page-" + strconv.Itoa(i+1) + ".png"
	}
	for _, requestCtx := range []context.Context{ctx, ownerCtx} {
		before := counter.queries.Load()
		resolved, err := server.resolveStoredOSSImageURLsWithConfig(requestCtx, cfg, urls)
		if err != nil {
			t.Fatal(err)
		}
		if counter.queries.Load()-before != 1 || len(resolved) != 100 {
			t.Fatal("100-image page did not use one authorization query")
		}
		for _, url := range resolved {
			if requestCtx == ctx && url != "" {
				t.Fatal("batch exposed unbound private image")
			}
			if requestCtx == ownerCtx && !strings.Contains(strings.ToLower(url), "x-oss-signature") {
				t.Fatal("batch omitted owned private image")
			}
		}
	}
}
