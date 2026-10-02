package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/systemactor"
)

func TestSEC021AutomationActorRequiresTheNonInteractiveSystemSubject(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated PostgreSQL database to verify the automation subject")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	schemaName := fmt.Sprintf("sec021_actor_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, dropErr := adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated SEC-021 actor schema: %v", dropErr)
		}
	}()

	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `
		create table users (
			id bigserial primary key,public_id text not null unique,username text not null unique,
			email text not null unique,auth_version bigint not null,status text not null
		);
		create table roles (
			id bigserial primary key,code text not null unique,weight integer not null,
			parents text[] not null default '{}',status text not null
		);
		create table permissions (id bigserial primary key,code text not null unique);
		create table role_permissions (
			role_id bigint not null,permission_id bigint not null,allow boolean not null,expires_at timestamptz
		);
		create table user_role_bindings (user_id bigint not null,role_id bigint not null,expires_at timestamptz);
		create table effective_project_access (user_id bigint not null,access_level text not null,project_public_id text not null);
		create table creators (id bigserial primary key,public_id text not null,kind text not null);
		create table creator_claims (user_id bigint not null,creator_id bigint not null,status text not null);
		create table user_permissions (
			user_id bigint not null,permission_id bigint not null,allow boolean not null,
			source text not null,expires_at timestamptz
		)`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `with account as (
		insert into users(public_id,username,email,auth_version,status)
		values('autobot1',$1,$2,1,$3) returning id
	), permission as (
		insert into permissions(code) values('project.create') returning id
	)
	insert into user_permissions(user_id,permission_id,allow,source)
	select account.id,permission.id,true,'system_seed' from account,permission`,
		systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	claims, err := server.automationActor(ctx)
	if err != nil {
		t.Fatalf("resolve system automation subject: %v", err)
	}
	if claims.Username != systemactor.AutobotUsername || !claimsAllow(claims, "project.create") {
		t.Fatalf("unexpected automation claims: %+v", claims)
	}
	if _, err = pool.Exec(ctx, `update users set status='active' where username=$1`, systemactor.AutobotUsername); err != nil {
		t.Fatal(err)
	}
	if _, err = server.automationActor(ctx); err == nil {
		t.Fatal("an interactive active account was accepted as the automation subject")
	}
}
