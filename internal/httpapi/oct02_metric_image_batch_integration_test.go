package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02MetricImagesReleaseRowsAndAuthorizeInOneBatchIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `insert into users(username,email,password_hash,avatar_url)
	 select 'oct02-metric-'||n,'oct02-metric-'||n||'@example.invalid','not-a-password','https://images.example.invalid/private-'||n||'.png' from generate_series(1,100) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into project_editor_assignments(target_route_id,user_id)
	 select route.id,account.id from public_routes route,users account where route.entity_type='mod' and route.internal_id=$1 and account.username like 'oct02-metric-%'`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into creators(kind,name,normalized_name,avatar_url,review_status)
	 select 'author','oct02-metric-'||n,'oct02-metric-'||n,'https://images.example.invalid/private-'||n||'.png','approved' from generate_series(1,100) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,status)
	 select 'mod',$1,id,'approved' from creators where normalized_name like 'oct02-metric-%'`, f.modID); err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	pc := f.db.Config()
	pc.MaxConns = 1
	pc.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(f.ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-metric-key-at-least-32-characters"}}
	raw, err := json.Marshal(ossConfigPayload{Bucket: "fixture", PublicEndpoint: "https://images.example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := security.EncryptSetting(s.cfg.SettingsEncryptionKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(f.ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, string(sealed)); err != nil {
		t.Fatal(err)
	}
	target := metricTarget{Type: "mod", InternalID: f.modID}
	for _, kind := range []string{"editors", "developers", "recent avatars"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
			defer cancel()
			counter.queries.Store(0)
			count := 0
			switch kind {
			case "editors":
				items, err := s.loadMetricProjectEditors(ctx, target)
				if err != nil {
					t.Fatal(err)
				}
				count = len(items)
				for _, item := range items {
					if item.AvatarURL != "" {
						t.Errorf("unauthorized avatar leaked %q", item.AvatarURL)
					}
				}
			case "developers":
				items, err := s.loadMetricDevelopers(ctx, target)
				if err != nil {
					t.Fatal(err)
				}
				count = len(items)
				for _, item := range items {
					if item.AvatarURL != "" {
						t.Errorf("unauthorized avatar leaked %q", item.AvatarURL)
					}
				}
			default:
				items := make([]contentMetricActor, 100)
				for i := range items {
					items[i].AvatarURL = "https://images.example.invalid/private.png"
				}
				items, err = s.resolveMetricActorAvatars(ctx, items)
				if err != nil {
					t.Fatal(err)
				}
				count = len(items)
				for _, item := range items {
					if item.AvatarURL != "" {
						t.Errorf("unauthorized avatar leaked %q", item.AvatarURL)
					}
				}
			}
			if count != 100 || counter.queries.Load() > 3 {
				t.Fatalf("count=%d queries=%d, want100 and <=3", count, counter.queries.Load())
			}
		})
	}
}
