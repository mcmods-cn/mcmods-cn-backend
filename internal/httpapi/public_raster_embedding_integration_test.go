package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func rasterEmbeddingFatal(t *testing.T, phase string, err error) {
	t.Helper()
	var state interface{ SQLState() string }
	code := ""
	if errors.As(err, &state) {
		code = state.SQLState()
	}
	t.Fatalf("public raster embedding %s: type=%T sqlstate=%s error_sha256=%x", phase, err, code, sha256.Sum256([]byte(err.Error())))
}

func TestPublicRasterEmbeddingMatchesAnonymousAuthorizationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		rasterEmbeddingFatal(t, "pool config", err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		rasterEmbeddingFatal(t, "pool", err)
	}
	t.Cleanup(pool.Close)
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		rasterEmbeddingFatal(t, "ephemeral schema", err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := database.DropEphemeralSchema(cleanup, pool); err != nil {
			t.Errorf("drop own ephemeral schema: type=%T", err)
		}
	})
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			rasterEmbeddingFatal(t, fmt.Sprintf("fixture SQL %x", sha256.Sum256([]byte(query))), err)
		}
	}

	// Real local HTTPS destinations intentionally omit ACAO. Public no-cors
	// embedding must not start requiring final OSS/CDN CORS or rewrite targets.
	var raster bytes.Buffer
	pixel := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	pixel.SetNRGBA(0, 0, color.NRGBA{R: 7, G: 59, B: 113, A: 255})
	if err := png.Encode(&raster, pixel); err != nil {
		rasterEmbeddingFatal(t, "PNG", err)
	}
	serveRaster := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(raster.Bytes())
	}
	provider := httptest.NewTLSServer(http.HandlerFunc(serveRaster))
	t.Cleanup(provider.Close)
	external := httptest.NewTLSServer(http.HandlerFunc(serveRaster))
	t.Cleanup(external.Close)

	cfg := config.Load()
	cfg.JWTSecret = "synthetic-public-raster-session-key-only"
	cfg.SettingsEncryptionKey = "synthetic-public-raster-settings-key-only"
	cfg.FrontendOrigin = "https://configured-frontend.example.test"
	cfg.Redis.Enabled = false
	cfg.NATS = config.NATSConfig{}
	cfg.SMTP = config.SMTPConfig{}
	server := NewServer(ctx, cfg, pool, nil, nil, nil, nil)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := server.Shutdown(cleanup); err != nil {
			t.Errorf("normal application shutdown: type=%T", err)
		}
		if err := server.cache.Close(); err != nil {
			t.Errorf("normal cache close: type=%T", err)
		}
	})
	ossCfg := ossConfigPayload{Enabled: true, Region: "cn-test", Bucket: "embedding-bucket", Endpoint: provider.URL, PublicEndpoint: provider.URL, UseCName: true, AccessKeyID: "synthetic-key", AccessKeySecret: "synthetic-secret"}
	sealed, err := server.sealSystemSetting(ossCfg)
	if err != nil {
		rasterEmbeddingFatal(t, "seal local OSS", err)
	}
	exec(`insert into system_settings(key,value) values('oss.aliyun',$1)`, sealed)
	origin := httptest.NewTLSServer(server)
	t.Cleanup(origin.Close)
	// httptest's client verifies the owned server certificate; TLS is not skipped.
	client := origin.Client()
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	t.Cleanup(client.CloseIdleConnections)

	type actor struct {
		id    int64
		token string
	}
	makeActor := func(name, status, permission string) actor {
		t.Helper()
		var user actor
		var publicID string
		var version int64
		if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,status,created_at,email_verified) values($1,$2,'unused-synthetic',$3,now()-interval '30 days',true) returning id,public_id,auth_version`, name, name+"@example.invalid", status).Scan(&user.id, &publicID, &version); err != nil {
			rasterEmbeddingFatal(t, "actor", err)
		}
		if permission != "" {
			exec(`insert into permissions(code,module,name) values($1,'public_raster_test',$1) on conflict(code) do nothing`, permission)
			exec(`insert into user_permissions(user_id,permission_id,allow,source,source_key) select $1,id,true,'manual','' from permissions where code=$2`, user.id, permission)
		}
		claims, err := security.NewClaims(publicID, name, name+"@example.invalid", version, time.Hour)
		if err != nil {
			rasterEmbeddingFatal(t, "claims", err)
		}
		exec(`insert into auth_sessions(session_hash,user_id,auth_version,expires_at) values($1,$2,$3,$4)`, security.SessionFingerprint(claims.SessionID), user.id, version, time.Unix(claims.ExpiresAt, 0))
		user.token, err = security.SignToken(cfg.JWTSecret, claims)
		if err != nil {
			rasterEmbeddingFatal(t, "session", err)
		}
		return user
	}
	owner := makeActor("raster-owner", "active", "")
	reviewer := makeActor("raster-reviewer", "active", "project.review")
	other := makeActor("raster-other", "active", "")
	deleted := makeActor("raster-deleted-binding", "deleted", "")

	file := func(key, mime string) int64 {
		t.Helper()
		var id int64
		err := pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,object_key,category,source,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status) values($1,$2,$3,'project','user','fixture.png',$4,$5,$6,$7,'active','clean') returning id`, ossCfg.Bucket, ossCfg.Endpoint, key, mime, raster.Len(), fmt.Sprintf("%x", sha256.Sum256(raster.Bytes())), owner.id).Scan(&id)
		if err != nil {
			rasterEmbeddingFatal(t, "file", err)
		}
		return id
	}
	publicFile := file("images/public.png", "image/png")
	privateFile := file("images/private.png", "image/png")
	unsafeFile := file("images/not-image.html", "text/html")
	publicURL, privateURL := provider.URL+"/images/public.png", provider.URL+"/images/private.png"

	type project struct {
		kind, slug, iconPath, galleryPath string
		id                                int64
	}
	sequence := 0
	makeProject := func(kind, status, storedIcon string, published bool, galleryFile int64) project {
		t.Helper()
		sequence++
		slug := fmt.Sprintf("raster-%s-%d", strings.ReplaceAll(kind, "_", "-"), sequence)
		p := project{kind: kind, slug: slug}
		var publicID string
		table := ""
		foreign := ""
		galleryTable := ""
		switch kind {
		case "mod":
			table, foreign, galleryTable = "mods", "mod_id", "mod_gallery_images"
			if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by,icon_url) values($1,$2,$2,$3,$4,$5) returning id,project_code`, fmt.Sprintf("raster%03d", sequence), slug, status, owner.id, storedIcon).Scan(&p.id, &publicID); err != nil {
				rasterEmbeddingFatal(t, "Mod", err)
			}
			p.iconPath = "/api/v1/mods/" + slug + "/icon"
		case "modpack":
			table, foreign, galleryTable = "modpacks", "modpack_id", "modpack_gallery_images"
			if err := pool.QueryRow(ctx, `insert into modpacks(slug,primary_name,review_status,submitted_by,icon_url) values($1,$1,$2,$3,$4) returning id,public_id`, slug, status, owner.id, storedIcon).Scan(&p.id, &publicID); err != nil {
				rasterEmbeddingFatal(t, "Modpack", err)
			}
			p.iconPath = "/api/v1/modpacks/" + slug + "/icon"
		default:
			table, foreign, galleryTable = "simple_projects", "project_id", "simple_project_gallery_images"
			if err := pool.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by,icon_url) values($1,$2,$2,$3,$4,$5) returning id,public_id`, kind, slug, status, owner.id, storedIcon).Scan(&p.id, &publicID); err != nil {
				rasterEmbeddingFatal(t, "simple project", err)
			}
			p.iconPath = "/api/v1/content-projects/" + kind + "/" + slug + "/icon"
		}
		if published {
			var revision int64
			if err := pool.QueryRow(ctx, `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by) values($1,$2,$1,$3,1,jsonb_build_object('iconUrl',$4::text),'synthetic-raster-test',$5) returning id`, kind, p.id, publicID, storedIcon, owner.id).Scan(&revision); err != nil {
				rasterEmbeddingFatal(t, "published snapshot", err)
			}
			exec(`insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,submitted_by) values($1,$2,$1,$3,$4,'approved',$5)`, kind, p.id, publicID, revision, owner.id)
			exec(`update `+table+` set published_revision_id=$2 where id=$1`, p.id, revision)
		}
		if galleryFile != 0 {
			var gallery string
			if err := pool.QueryRow(ctx, `insert into `+galleryTable+`(`+foreign+`,oss_file_id,created_by) values($1,$2,$3) returning public_id`, p.id, galleryFile, owner.id).Scan(&gallery); err != nil {
				rasterEmbeddingFatal(t, "gallery", err)
			}
			p.galleryPath = strings.TrimSuffix(p.iconPath, "/icon") + "/gallery/" + gallery
		}
		return p
	}
	type response struct {
		status int
		header http.Header
		body   []byte
	}
	request := func(path, token, originHeader string, cookie bool) response {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.URL+path, nil)
		if err != nil {
			rasterEmbeddingFatal(t, "request", err)
		}
		if token != "" {
			if cookie {
				r.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: token})
			} else {
				r.Header.Set("Authorization", "Bearer "+token)
			}
		}
		if originHeader != "" {
			r.Header.Set("Origin", originHeader)
		}
		got, err := client.Do(r)
		if err != nil {
			rasterEmbeddingFatal(t, "HTTP", err)
		}
		defer got.Body.Close()
		body, err := io.ReadAll(io.LimitReader(got.Body, 65537))
		if err != nil {
			rasterEmbeddingFatal(t, "HTTP read", err)
		}
		if len(body) > 65536 {
			t.Fatal("raster HTTP response over budget")
		}
		return response{got.StatusCode, got.Header.Clone(), body}
	}
	assertResponse := func(path, token string, cookie bool, wantStatus int, wantPolicy string) response {
		t.Helper()
		got := request(path, token, cfg.FrontendOrigin, cookie)
		if got.status != wantStatus || got.header.Get("Cross-Origin-Resource-Policy") != wantPolicy {
			t.Fatalf("raster path=%s status=%d policy=%q want=%d/%s", path, got.status, got.header.Get("Cross-Origin-Resource-Policy"), wantStatus, wantPolicy)
		}
		if got.header.Get("Access-Control-Allow-Origin") != cfg.FrontendOrigin || got.header.Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatal("existing exact CORS policy changed")
		}
		if wantStatus == http.StatusTemporaryRedirect && got.header.Get("Location") == "" {
			t.Fatal("real raster redirect destination lost")
		}
		return got
	}
	kinds := []string{"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"}
	var publicProjects []project
	for _, kind := range kinds {
		pub := makeProject(kind, "approved", publicURL, true, publicFile)
		pending := makeProject(kind, "pending", publicURL, false, publicFile) // Same globally public blob: parent route still matters.
		publicProjects = append(publicProjects, pub)
		for _, path := range []string{pub.iconPath, pub.galleryPath} {
			for _, token := range []string{"", owner.token, other.token} {
				assertResponse(path, token, false, 307, "cross-origin")
			}
		}
		for _, path := range []string{pending.iconPath, pending.galleryPath} {
			assertResponse(path, "", false, 404, "same-site")
			assertResponse(path, other.token, false, 404, "same-site")
			assertResponse(path, owner.token, true, 307, "same-site") // Real owner cookie must keep working.
		}
		if kind == "mod" {
			assertResponse(pending.galleryPath, reviewer.token, false, 307, "same-site")
		} else {
			assertResponse(pending.galleryPath, reviewer.token, false, 404, "same-site")
		}
	}
	// An approved parent referencing an unbound private blob does not make the
	// owner's successful redirect anonymously readable. Exercise the full
	// existing image authorization, including a deleted-avatar binding.
	legacy := makeProject("mod", "approved", privateURL, false, 0)
	exec(`update users set avatar_file_id=$2 where id=$1`, deleted.id, privateFile)
	assertResponse(legacy.iconPath, "", false, 404, "same-site")
	assertResponse(legacy.iconPath, other.token, false, 404, "same-site")
	assertResponse(legacy.iconPath, owner.token, true, 307, "same-site")
	exec(`update users set status='active' where id=$1`, deleted.id)
	assertResponse(legacy.iconPath, "", false, 307, "cross-origin")
	assertResponse(legacy.iconPath, owner.token, true, 307, "cross-origin")
	exec(`update users set status='deleted' where id=$1`, deleted.id)
	assertResponse(legacy.iconPath, owner.token, true, 307, "same-site")
	// A shared skin blob is public only through an active, approved,
	// public/unlisted binding. Private/pending/deleted siblings cannot supply it.
	blobHash := fmt.Sprintf("%x", sha256.Sum256(raster.Bytes()))
	exec(`insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes) values($1,$2,'images/private.png',2,2,$3)`, blobHash, privateFile, raster.Len())
	var skin int64
	if err := pool.QueryRow(ctx, `insert into skin_assets(owner_id,blob_hash,kind,display_name,visibility,review_status,status) values($1,$2,'skin','synthetic shared binding','private','approved','active') returning id`, other.id, blobHash).Scan(&skin); err != nil {
		rasterEmbeddingFatal(t, "private shared skin", err)
	}
	assertResponse(legacy.iconPath, owner.token, true, 307, "same-site")
	exec(`update skin_assets set visibility='public',review_status='pending' where id=$1`, skin)
	assertResponse(legacy.iconPath, "", false, 404, "same-site")
	exec(`update skin_assets set review_status='approved' where id=$1`, skin)
	assertResponse(legacy.iconPath, "", false, 307, "cross-origin")
	exec(`update skin_assets set status='deleted' where id=$1`, skin)
	assertResponse(legacy.iconPath, owner.token, true, 307, "same-site")
	exec(`update skin_assets set status='active',visibility='unlisted' where id=$1`, skin)
	assertResponse(legacy.iconPath, "", false, 307, "cross-origin")
	exec(`update skin_assets set visibility='private' where id=$1`, skin)
	assertResponse(legacy.iconPath, "", false, 404, "same-site")
	unsafe := makeProject("mod", "approved", provider.URL+"/images/not-image.html", false, unsafeFile)
	assertResponse(unsafe.iconPath, owner.token, true, 404, "same-site")
	assertResponse(unsafe.galleryPath, "", false, 415, "same-site")
	exec(`update oss_files set status='deleted' where id=$1`, publicFile)
	assertResponse(publicProjects[0].iconPath, "", false, 404, "same-site")
	assertResponse(publicProjects[0].galleryPath, "", false, 404, "same-site")
	exec(`update oss_files set status='active' where id=$1`, publicFile)
	exec(`update oss_files set scan_status='pending' where id=$1`, publicFile)
	assertResponse(publicProjects[0].iconPath, "", false, 404, "same-site")
	exec(`update oss_files set scan_status='clean' where id=$1`, publicFile)

	// Literal external URLs keep the original redirect and need no new ACAO.
	externalURL := external.URL + "/external.png?keep=original"
	ext := makeProject("modpack", "approved", externalURL, false, 0)
	got := assertResponse(ext.iconPath, "", false, 307, "cross-origin")
	if got.header.Get("Location") != externalURL {
		t.Fatal("external raster target or query changed")
	}
	final, err := client.Get(got.header.Get("Location"))
	if err != nil {
		rasterEmbeddingFatal(t, "external final HTTP", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(final.Body, 1<<20))
	closeErr := final.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal("external raster normal read/close failed")
	}
	if final.StatusCode != 200 || final.Header.Get("Access-Control-Allow-Origin") != "" || !bytes.Equal(body, raster.Bytes()) {
		t.Fatal("external no-CORS raster behavior changed")
	}
	public := assertResponse(publicProjects[0].galleryPath, "", false, 307, "cross-origin")
	target, err := url.Parse(public.header.Get("Location"))
	if err != nil {
		t.Fatal("OSS redirect URL invalid")
	}
	providerTarget, err := url.Parse(provider.URL)
	if err != nil {
		t.Fatal("local OSS origin invalid")
	}
	if target.Scheme != providerTarget.Scheme || target.Host != providerTarget.Host || !strings.HasSuffix(target.Path, "/images/public.png") {
		t.Fatal("existing signed OSS target changed")
	}
	final, err = client.Get(public.header.Get("Location"))
	if err != nil {
		rasterEmbeddingFatal(t, "signed OSS final HTTP", err)
	}
	body, readErr = io.ReadAll(io.LimitReader(final.Body, 1<<20))
	closeErr = final.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal("signed raster normal read/close failed")
	}
	if final.StatusCode != 200 || final.Header.Get("Access-Control-Allow-Origin") != "" || !bytes.Equal(body, raster.Bytes()) {
		t.Fatal("signed no-CORS raster behavior changed")
	}
	assertResponse("/live", "", false, 200, "same-site")
	unknown := request(publicProjects[0].iconPath, "", "https://unconfigured.example.test", false)
	if unknown.header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS origin allowlist widened")
	}
}
