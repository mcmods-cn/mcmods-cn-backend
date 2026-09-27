package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

func TestFavoriteCollectionWritesClassifyDatabaseFailuresIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify favorite write errors")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err = pool.Exec(ctx, `
		create temporary table favorite_collections(
			id bigint generated always as identity primary key,
			public_id text not null default 'c00000006',
			user_id bigint not null,
			name text not null,
			is_default boolean not null default false,
			is_public boolean not null default false,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now()
		);
		create unique index arch024_favorite_names on favorite_collections(user_id,lower(name));
		create temporary table favorite_modpack_export_tasks(
			id bigint generated always as identity primary key,collection_id bigint,status text,stage text,
			error_code text,error_detail text,finished_at timestamptz,lease_token text,lease_expires_at timestamptz,updated_at timestamptz
		);
		create function pg_temp.arch024_reject_favorite_write() returns trigger language plpgsql as $$
		begin
			if tg_op='DELETE' then
				if old.public_id='c00000005' then
					raise exception 'ARCH024 forced delete failure' using errcode='XX000';
				end if;
				return old;
			end if;
			if tg_op='INSERT' and new.name='explode-create' then
				raise exception 'ARCH024 forced create failure' using errcode='XX000';
			end if;
			if tg_op='UPDATE' and new.name='explode-update' then
				raise exception 'ARCH024 forced update failure' using errcode='XX000';
			end if;
			return new;
		end $$;
		create trigger arch024_reject_write before insert or update or delete on favorite_collections
			for each row execute function pg_temp.arch024_reject_favorite_write();
		insert into favorite_collections(public_id,user_id,name,is_default,is_public) values
			('c00000001',42,'Default',true,false),
			('c00000002',42,'alpha',false,false),
			('c00000003',42,'beta',false,false),
			('c00000004',42,'delete me',false,false),
			('c00000005',42,'delete fail',false,false);
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	tests := []struct {
		name, method, path, publicID, body string
		handler                            http.HandlerFunc
		want                               int
	}{
		{"create healthy", http.MethodPost, "/api/v1/users/me/favorite-collections", "", `{"name":"gamma","isPublic":true}`, server.createFavoriteCollection, http.StatusCreated},
		{"create duplicate", http.MethodPost, "/api/v1/users/me/favorite-collections", "", `{"name":"alpha"}`, server.createFavoriteCollection, http.StatusConflict},
		{"create database failure", http.MethodPost, "/api/v1/users/me/favorite-collections", "", `{"name":"explode-create"}`, server.createFavoriteCollection, http.StatusInternalServerError},
		{"update missing", http.MethodPatch, "/api/v1/users/me/favorite-collections/c00000099", "c00000099", `{"isPublic":true}`, server.updateFavoriteCollection, http.StatusNotFound},
		{"update duplicate", http.MethodPatch, "/api/v1/users/me/favorite-collections/c00000002", "c00000002", `{"name":"beta"}`, server.updateFavoriteCollection, http.StatusConflict},
		{"update database failure", http.MethodPatch, "/api/v1/users/me/favorite-collections/c00000002", "c00000002", `{"name":"explode-update"}`, server.updateFavoriteCollection, http.StatusInternalServerError},
		{"delete default", http.MethodDelete, "/api/v1/users/me/favorite-collections/c00000001", "c00000001", "", server.deleteFavoriteCollection, http.StatusConflict},
		{"delete missing", http.MethodDelete, "/api/v1/users/me/favorite-collections/c00000099", "c00000099", "", server.deleteFavoriteCollection, http.StatusConflict},
		{"delete database failure", http.MethodDelete, "/api/v1/users/me/favorite-collections/c00000005", "c00000005", "", server.deleteFavoriteCollection, http.StatusInternalServerError},
		{"delete healthy", http.MethodDelete, "/api/v1/users/me/favorite-collections/c00000004", "c00000004", "", server.deleteFavoriteCollection, http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, body := invokeFavoriteCollectionWrite(test.handler, ctx, test.method, test.path, test.publicID, test.body)
			if status != test.want {
				t.Errorf("status=%d body=%s; want %d", status, body, test.want)
			}
		})
	}
}

func invokeFavoriteCollectionWrite(handler http.HandlerFunc, ctx context.Context, method, path, publicID, body string) (int, string) {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if publicID != "" {
		request.SetPathValue("id", publicID)
	}
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 42}))
	response := httptest.NewRecorder()
	handler(response, request)
	return response.Code, response.Body.String()
}
