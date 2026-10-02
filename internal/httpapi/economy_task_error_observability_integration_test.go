package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestEconomyAndTaskReadErrorsAreObservableIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify economy and task read failures")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = config.Load().DB.ConnString()
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,timezone)
		values($1,$2,'arch-027-test',true,'Asia/Shanghai') returning id`,
		fmt.Sprintf("arch027-%d", suffix), fmt.Sprintf("arch027-%d@example.invalid", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}

	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		server.userEconomyOverview(response, arch027UserRequest(ctx, userID, "/api/v1/economy"))
	})
	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		server.adminEconomyConfig(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/economy/config", nil).WithContext(ctx))
	})
	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		server.userTasks(response, arch027UserRequest(ctx, userID, "/api/v1/tasks"))
	})

	for _, table := range []string{"user_experience", "users", "user_checkins"} {
		t.Run("overview database failure "+table, func(t *testing.T) {
			arch027WithRenamedTable(t, ctx, pool, table, func() {
				assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
					server.userEconomyOverview(response, arch027UserRequest(ctx, userID, "/api/v1/economy"))
				})
			})
		})
	}

	if _, err = pool.Exec(ctx, `insert into system_settings(key,value,updated_at)
		values($1,'null'::jsonb,now())
		on conflict(key) do update set value=excluded.value,updated_at=now()`, economyConfigSettingKey); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.adminEconomyConfig(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/economy/config", nil).WithContext(ctx))
	})
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.userEconomyOverview(response, arch027UserRequest(ctx, userID, "/api/v1/economy"))
	})
	if _, err = pool.Exec(ctx, `delete from system_settings where key=$1`, economyConfigSettingKey); err != nil {
		t.Fatal(err)
	}

	var currencyID int64
	if err = pool.QueryRow(ctx, `insert into currencies(code,name,translations,status)
		values($1,'ARCH-027 currency','null'::jsonb,'active') returning id`, fmt.Sprintf("arch027_currency_%d", suffix)).Scan(&currencyID); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.publicCurrencies(response, httptest.NewRequest(http.MethodGet, "/api/v1/economy/currencies", nil).WithContext(ctx))
	})
	if _, err = pool.Exec(ctx, `update currencies set translations='{}'::jsonb where id=$1`, currencyID); err != nil {
		t.Fatal(err)
	}

	var shopItemID int64
	if err = pool.QueryRow(ctx, `insert into shop_items(
		code,item_type,name,translations,price_currency_id,price_amount,config,status)
		values($1,'project_heat_boost','ARCH-027 item','null'::jsonb,$2,1,'{}'::jsonb,'active') returning id`,
		fmt.Sprintf("arch027_item_%d", suffix), currencyID).Scan(&shopItemID); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.publicShopItems(response, httptest.NewRequest(http.MethodGet, "/api/v1/economy/shop", nil).WithContext(ctx))
	})
	if _, err = pool.Exec(ctx, `update shop_items set translations='{}'::jsonb,config='null'::jsonb where id=$1`, shopItemID); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.publicShopItems(response, httptest.NewRequest(http.MethodGet, "/api/v1/economy/shop", nil).WithContext(ctx))
	})

	var taskID int64
	if err = pool.QueryRow(ctx, `insert into task_definitions(
		code,name,translations,condition,rewards,status)
		values($1,'ARCH-027 task','null'::jsonb,
		'{"action":"create","objectType":"user","metric":"count","target":1}'::jsonb,
		'{"experience":1,"currencies":{}}'::jsonb,'active') returning id`,
		fmt.Sprintf("arch027_task_%d", suffix)).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.userTasks(response, arch027UserRequest(ctx, userID, "/api/v1/tasks"))
	})
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.adminTasks(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/tasks", nil).WithContext(ctx))
	})
}

func arch027UserRequest(ctx context.Context, userID int64, path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, path, nil).WithContext(
		context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}),
	)
}

func arch027WithRenamedTable(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table string,
	run func(),
) {
	t.Helper()
	broken := "arch027_broken_" + table
	if _, err := pool.Exec(ctx, `alter table `+table+` rename to `+broken); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), `alter table `+broken+` rename to `+table); err != nil {
			t.Errorf("restore %s: %v", table, err)
		}
	}()
	run()
}

func assertHandlerStatus(t *testing.T, want int, call func(*httptest.ResponseRecorder)) {
	t.Helper()
	response := httptest.NewRecorder()
	call(response)
	if response.Code != want {
		t.Fatalf("status=%d body=%s; want %d", response.Code, response.Body.String(), want)
	}
}
