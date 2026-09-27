package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestPlayerProfileTextureAssemblyUsesConstantQueriesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify player profile texture batching")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `
		create temporary table users (
			id bigint primary key, public_id text not null unique, username text not null
		);
		create temporary table player_profiles (
			id bigint primary key, public_id text not null unique, user_id bigint not null,
			uuid uuid not null, name text not null, bio text not null default '',
			visibility text not null, is_default boolean not null default false,
			status text not null, created_at timestamptz not null, updated_at timestamptz not null
		);
		create temporary table skin_assets (
			id bigint primary key, public_id text not null unique, owner_id bigint not null,
			blob_hash text not null, kind text not null, model text not null,
			display_name text not null, description text not null default '', tags text[] not null default '{}',
			visibility text not null, review_status text not null, status text not null,
			downloads bigint not null default 0, created_at timestamptz not null, updated_at timestamptz not null
		);
		create temporary table player_profile_textures (
			profile_id bigint not null, kind text not null, asset_id bigint not null,
			primary key(profile_id,kind)
		);
		create temporary table skin_wardrobe (
			user_id bigint not null, asset_id bigint not null, primary key(user_id,asset_id)
		);`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into users(id,public_id,username)
		values(1,'owner047','Owner'),(2,'viewer047','Viewer')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into player_profiles(
		id,public_id,user_id,uuid,name,bio,visibility,is_default,status,created_at,updated_at)
		select 1000+value,'p'||lpad(value::text,8,'0'),
			case when value=1 then 10 else 1 end,
			('00000000-0000-0000-0000-'||lpad(value::text,12,'0'))::uuid,
			'Profile'||value,'','public',value=2,'active',now()+value*interval '1 second',now()
		from generate_series(1,101) value`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into users(id,public_id,username) values(10,'single047','Single')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into skin_assets(
		id,public_id,owner_id,blob_hash,kind,model,display_name,description,tags,
		visibility,review_status,status,downloads,created_at,updated_at)
		select 100000+profile_number*2+kind_number,
			case kind_number when 0 then 's' else 'c' end||lpad(profile_number::text,8,'0'),
			1,repeat(case kind_number when 0 then 'a' else 'b' end,64),
			case kind_number when 0 then 'skin' else 'cape' end,'default',
			'Texture '||profile_number||'-'||kind_number,'',array['batch'],
			'public','approved','active',profile_number,now(),now()
		from generate_series(1,101) profile_number cross join generate_series(0,1) kind_number`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into player_profile_textures(profile_id,kind,asset_id)
		select 1000+profile_number,case kind_number when 0 then 'skin' else 'cape' end,
			100000+profile_number*2+kind_number
		from generate_series(1,101) profile_number cross join generate_series(0,1) kind_number;
		insert into skin_wardrobe(user_id,asset_id)
		select 2,100000+profile_number*2 from generate_series(1,101) profile_number`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	for _, testCase := range []struct {
		name        string
		ownerID     int64
		wantCount   int
		wantQueries int64
	}{
		{name: "one profile", ownerID: 10, wantCount: 1, wantQueries: 2},
		{name: "one hundred profiles", ownerID: 1, wantCount: 100, wantQueries: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			counter.queries.Store(0)
			profiles, loadErr := server.loadPlayerProfiles(ctx, testCase.ownerID, security.Claims{Subject: 2}, true)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if len(profiles) != testCase.wantCount {
				t.Fatalf("profiles=%d want=%d", len(profiles), testCase.wantCount)
			}
			for index, profile := range profiles {
				if profile.Skin == nil || profile.Cape == nil {
					t.Fatalf("profile %d has incomplete textures: skin=%v cape=%v", index, profile.Skin, profile.Cape)
				}
				if !profile.Skin.InWardrobe || profile.Cape.InWardrobe {
					t.Fatalf("profile %d wardrobe flags skin=%v cape=%v", index, profile.Skin.InWardrobe, profile.Cape.InWardrobe)
				}
			}
			if got := counter.queries.Load(); got != testCase.wantQueries {
				t.Fatalf("%s executed %d SQL statements, want %d", testCase.name, got, testCase.wantQueries)
			}
		})
	}
}
