package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestProjectCatalogCardMetricsAndAvatarQueryBudgetIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	counter := &storedImageQueryCounter{}
	pc := base.Config()
	pc.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "catalog-projection-test-key-32-bytes"}}
	oss := ossConfigPayload{Enabled: true, Bucket: "mcmods-test", Region: "cn-hangzhou", Endpoint: "https://oss-cn-hangzhou.aliyuncs.com",
		PublicEndpoint: "https://catalog.test.invalid", AccessKeyID: "synthetic", AccessKeySecret: "synthetic"}
	raw, err := json.Marshal(oss)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := security.EncryptSetting(s.cfg.SettingsEncryptionKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, string(sealed)); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		select 'd'||lpad(n::text,8,'0'),'reviewcard-'||n,'Reviewcard '||n,case when n=4 then 'pending' else 'approved' end from generate_series(1,4)n;
		insert into modpacks(slug,primary_name,review_status)
		select 'reviewcard-'||n,'Reviewcard '||n,case when n=4 then 'pending' else 'approved' end from generate_series(1,4)n;
		insert into oss_files(bucket,endpoint,object_key,category,source,original_name,content_type,size_bytes,sha256,status,scan_status)
		select 'mcmods-test','https://catalog.test.invalid','avatars/a'||n||'.png','creator/avatar','user','a.png','image/png',128,repeat('a',64),'active','clean' from generate_series(1,8)n;
		insert into creators(kind,name,normalized_name,review_status,avatar_url,avatar_file_id)
		select 'author',file.object_key,file.object_key,'approved','https://catalog.test.invalid/'||file.object_key,file.id from oss_files file where object_key like 'avatars/a%.png';
		insert into content_creator_bindings(subject_type,subject_id,creator_id,status,display_order)
		select 'mod',m.id,c.id,'approved',row_number() over (partition by m.id order by c.name) from mods m cross join creators c where m.slug like 'reviewcard-%' and c.name like 'avatars/a%.png';
		delete from content_popularity_stats p using public_routes r where p.object_route_id=r.id and
			((r.entity_type='mod' and r.internal_id in (select id from mods where slug like 'reviewcard-%'))
			or (r.entity_type='modpack' and r.internal_id in (select id from modpacks where slug like 'reviewcard-%')));
		insert into content_popularity_stats(object_route_id,view_count,download_count,favorite_count,comment_count,rating_count,rating_sum,rating_average)
		select r.id,37,11,5,3,2,9,4.5 from public_routes r where
			(r.entity_type='mod' and r.internal_id=(select id from mods where slug='reviewcard-1'))
			or (r.entity_type='modpack' and r.internal_id=(select id from modpacks where slug='reviewcard-1'));
		insert into content_popularity_stats(object_route_id)
		select r.id from public_routes r where (r.entity_type='mod' and r.internal_id=(select id from mods where slug='reviewcard-2'))
			or (r.entity_type='modpack' and r.internal_id=(select id from modpacks where slug='reviewcard-2'))`); err != nil {
		t.Fatal(err)
	}
	t.Run("icon read failure", func(t *testing.T) {
		for _, kind := range []string{"mod", "modpack", "plugin"} {
			read := func(requestCtx context.Context) int {
				req := httptest.NewRequestWithContext(requestCtx, http.MethodGet, "/icon", nil)
				req.SetPathValue("siteId", "reviewcard-2")
				req.SetPathValue("projectType", kind)
				rec := httptest.NewRecorder()
				if kind == "plugin" {
					s.simpleProjectIcon(rec, req)
				} else {
					s.publicProjectIcon(rec, req, kind)
				}
				return rec.Code
			}
			if code := read(ctx); code != http.StatusNotFound {
				t.Errorf("%s missing icon returned %d", kind, code)
			}
			unavailable, stop := context.WithCancel(ctx)
			stop()
			if code := read(unavailable); code != http.StatusInternalServerError {
				t.Errorf("%s unavailable icon read returned %d", kind, code)
			}
		}
	})
	type stats struct {
		Downloads, Views, Favorites, Comments, RatingCount int64
		Rating                                             *float64
	}
	type card struct {
		SiteID  string             `json:"siteId"`
		Stats   *stats             `json:"stats"`
		Authors []modAuthorPayload `json:"authors"`
	}
	for _, kind := range []string{"mods", "modpacks"} {
		t.Run(kind, func(t *testing.T) {
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/"+kind+"?q=Reviewcard&sort=name", nil)
			rec := httptest.NewRecorder()
			counter.queries.Store(0)
			if kind == "mods" {
				s.publicMods(rec, req)
			} else {
				s.modpacks(rec, req)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("GET catalog returned %d: %s", rec.Code, rec.Body)
			}
			got := decodeTEST022Data[struct{ Items []card }](t, rec.Body.Bytes()).Items
			if len(got) != 3 {
				t.Fatalf("anonymous list length = %d, want 3 approved projects", len(got))
			}
			if kind == "mods" {
				imageCount := 0
				for _, item := range got {
					for _, author := range item.Authors {
						if !strings.Contains(strings.ToLower(author.AvatarURL), "x-oss-signature") || !strings.Contains(author.AvatarURL, author.Name) {
							t.Fatalf("incorrect public avatar for %s", author.Name)
						}
						imageCount++
					}
				}
				t.Logf("%d approved author avatar positions, %d distinct stored objects, %d authorization queries", imageCount, 8, counter.queries.Load())
				if imageCount != 24 || counter.queries.Load() != 1 {
					t.Errorf("avatar positions/authorization queries = %d/%d", imageCount, counter.queries.Load())
				}
			}
			for _, item := range got {
				switch item.SiteID {
				case "reviewcard-1":
					v := item.Stats
					if v == nil || v.Downloads != 11 || v.Views != 37 || v.Favorites != 5 || v.Comments != 3 || v.RatingCount != 2 || v.Rating == nil || *v.Rating != 4.5 {
						t.Errorf("site projection differs at %s: %+v", item.SiteID, v)
					}
				case "reviewcard-2":
					if item.Stats == nil || *item.Stats != (stats{}) {
						t.Errorf("zero counts or absent rating differ: %+v", item.Stats)
					}
				case "reviewcard-3":
					if item.Stats != nil {
						t.Errorf("unknown projection became counts: %+v", item.Stats)
					}
				default:
					t.Errorf("unexpected catalog project %s", item.SiteID)
				}
			}
		})
	}
}
