package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02EconomyAndProfileWritesUseOnePoolConnectionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	poolConfig := base.Config()
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	server := &Server{db: pool, cfg: config.Load(), cache: cache}
	settings, err := server.sealSystemSetting(ossConfigPayload{Enabled: true, Bucket: "test", Region: "cn-hangzhou", Endpoint: "https://storage.invalid", AccessKeyID: "synthetic-key", AccessKeySecret: "synthetic-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, settings); err != nil {
		t.Fatal(err)
	}
	var user, currency, fileID int64
	var filePublicID, userPublicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('pool-economy-user','pool@example.invalid','test') returning id,public_id`).Scan(&user, &userPublicID); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, ctx, pool, user, "user.avatar.update", "shop.oct02", "project.edit.poolmod01")
	if err = pool.QueryRow(ctx, `select id from currencies where code='diamond'`).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_currency_balances(user_id,currency_id,balance) values($1,$2,1000)`, user, currency); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
	 insert into shop_items(code,name,item_type,price_currency_id,price_amount,purchase_permission,use_permission)
	 values('oct02-background','Background','profile_background',$1,10,'shop.oct02','shop.oct02'),('oct02-boost','Boost','project_heat_boost',$1,10,'shop.oct02','shop.oct02')`, currency); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values('poolmod01','poolmod','Pool mod','approved',$1)`, user); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status) values('test','','','images/pool.jpg','avatar','user','pool.jpg','image/jpeg',100,repeat('a',64),$1,'active','clean') returning id,public_id`, user).Scan(&fileID, &filePublicID); err != nil {
		t.Fatal(err)
	}
	_, rules, err := server.resolveUserRootPermissions(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	claims := security.Claims{Subject: user, PublicSubject: userPublicID, PermissionRules: rules}
	invoke := func(t *testing.T, handler http.HandlerFunc, body string) {
		t.Helper()
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me", strings.NewReader(body))
		req = req.WithContext(context.WithValue(callCtx, claimsContextKey, claims))
		response := httptest.NewRecorder()
		handler(response, req)
		if response.Code != http.StatusOK || callCtx.Err() != nil {
			t.Fatalf("single-connection status=%d body=%s ctx=%v", response.Code, response.Body.String(), callCtx.Err())
		}
	}
	t.Run("profile_clear", func(t *testing.T) { invoke(t, server.updateUserProfileSettings, `{"clearAvatar":true}`) })
	t.Run("profile_bind", func(t *testing.T) {
		invoke(t, server.updateUserProfileSettings, fmt.Sprintf(`{"avatarFileId":%q}`, filePublicID))
	})
	for _, item := range []string{"oct02-background", "oct02-boost"} {
		t.Run(item, func(t *testing.T) {
			invoke(t, server.purchaseShopItem, fmt.Sprintf(`{"itemCode":%q,"quantity":1}`, item))
			invoke(t, server.useShopItem, fmt.Sprintf(`{"itemCode":%q,"fileId":%q,"targetType":"mod","targetId":"poolmod01"}`, item, filePublicID))
		})
	}
	t.Run("shop_list", func(t *testing.T) {
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := server.loadShopItems(callCtx, false, user); err != nil || callCtx.Err() != nil {
			t.Fatalf("single-connection shop list: %v/%v", err, callCtx.Err())
		}
	})
	t.Run("legacy_tombstone", func(t *testing.T) {
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		tx, err := pool.Begin(callCtx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = server.tombstoneOSSFileTx(callCtx, tx, fileID, "test legacy"); err != nil || callCtx.Err() != nil {
			t.Fatalf("single-connection tombstone: %v/%v", err, callCtx.Err())
		}
	})
	t.Run("configured_account_role", func(t *testing.T) {
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		tx, err := pool.Begin(callCtx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(callCtx, `insert into roles(code,name,status) values('oct02-default','Default','active')`); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(callCtx, `insert into system_settings(key,value) values('permission.default_roles','{"registeredRole":"oct02-default"}') on conflict(key) do update set value=excluded.value`); err != nil {
			t.Fatal(err)
		}
		if err = server.assignConfiguredRoleTx(callCtx, tx, user, "registered"); err != nil || callCtx.Err() != nil {
			t.Fatalf("single-connection account role: %v/%v", err, callCtx.Err())
		}
		var assigned bool
		if err = tx.QueryRow(callCtx, `select exists(select 1 from user_role_bindings binding join roles role on role.id=binding.role_id where binding.user_id=$1 and binding.source='account_status' and role.code='oct02-default')`, user).Scan(&assigned); err != nil || !assigned {
			t.Fatalf("uncommitted configured role was not assigned: %t/%v", assigned, err)
		}
	})
}
