package database

// skinCatalogSchemaStatements installs the narrow, write-maintained authority
// used by the public skin catalog. Request paths never aggregate or search the
// wide skin tables, and the projection can be rebuilt explicitly offline.
func skinCatalogSchemaStatements() []string {
	return []string{
		`create table skin_public_catalog (
			asset_id bigint primary key references skin_assets(id) on delete cascade,
			public_id text not null unique,
			kind text not null check(kind in ('skin','cape')),
			model text not null check(model in ('default','slim')),
			display_name text not null,
			created_at timestamptz not null,
			updated_at timestamptz not null,
			downloads bigint not null default 0 check(downloads>=0),
			heat_score numeric(16,6) not null default 0 check(heat_score>=0),
			favorite_count bigint not null default 0 check(favorite_count>=0),
			bayesian_rating numeric(6,4) not null default 0 check(bayesian_rating between 0 and 5),
			rating_count bigint not null default 0 check(rating_count>=0),
			view_count bigint not null default 0 check(view_count>=0),
			comment_count bigint not null default 0 check(comment_count>=0),
			search_document tsvector not null
		)`,
		`create index idx_skin_public_catalog_published
			on skin_public_catalog(created_at desc,updated_at desc,asset_id desc)`,
		`create index idx_skin_public_catalog_updated
			on skin_public_catalog(updated_at desc,asset_id desc)`,
		`create index idx_skin_public_catalog_heat
			on skin_public_catalog(heat_score desc,updated_at desc,asset_id desc)`,
		`create index idx_skin_public_catalog_downloads
			on skin_public_catalog(downloads desc,updated_at desc,asset_id desc)`,
		`create index idx_skin_public_catalog_favorites
			on skin_public_catalog(favorite_count desc,updated_at desc,asset_id desc)`,
		`create index idx_skin_public_catalog_rating
			on skin_public_catalog(bayesian_rating desc,rating_count desc,updated_at desc,asset_id desc)`,
		`create index idx_skin_public_catalog_views
			on skin_public_catalog(view_count desc,updated_at desc,asset_id desc)`,
		`create index idx_skin_public_catalog_comments
			on skin_public_catalog(comment_count desc,updated_at desc,asset_id desc)`,
		`create index idx_skin_public_catalog_name
			on skin_public_catalog(lower(display_name),asset_id)`,
		`create index idx_skin_public_catalog_search
			on skin_public_catalog using gin(search_document)`,
		`create or replace function refresh_skin_public_catalog(target_asset_id bigint) returns void as $$
		begin
			insert into skin_public_catalog(
				asset_id,public_id,kind,model,display_name,created_at,updated_at,downloads,
				heat_score,favorite_count,bayesian_rating,rating_count,view_count,comment_count,search_document)
			select asset.id,asset.public_id,asset.kind,asset.model,asset.display_name,asset.created_at,asset.updated_at,
				asset.downloads,coalesce(popularity.heat_score,0),coalesce(popularity.favorite_count,0),
				coalesce(popularity.bayesian_rating,0),coalesce(popularity.rating_count,0),
				coalesce(popularity.view_count,0),coalesce(popularity.comment_count,0),
				setweight(to_tsvector('simple',asset.public_id),'A') ||
				setweight(to_tsvector('simple',asset.display_name),'A') ||
				setweight(to_tsvector('simple',asset.description),'B') ||
				setweight(to_tsvector('simple',array_to_string(asset.tags,' ')),'A')
			from skin_assets asset
			left join public_routes route on route.entity_type='skin' and route.internal_id=asset.id
			left join content_popularity_stats popularity on popularity.object_route_id=route.id
			where asset.id=target_asset_id and asset.status='active'
				and asset.review_status='approved' and asset.visibility='public'
			on conflict(asset_id) do update set
				public_id=excluded.public_id,kind=excluded.kind,model=excluded.model,
				display_name=excluded.display_name,created_at=excluded.created_at,updated_at=excluded.updated_at,
				downloads=excluded.downloads,heat_score=excluded.heat_score,
				favorite_count=excluded.favorite_count,bayesian_rating=excluded.bayesian_rating,
				rating_count=excluded.rating_count,view_count=excluded.view_count,
				comment_count=excluded.comment_count,search_document=excluded.search_document;
			if not found then
				delete from skin_public_catalog where asset_id=target_asset_id;
			end if;
		end;
		$$ language plpgsql`,
		`create or replace function rebuild_skin_public_catalog() returns void as $$
		begin
			truncate skin_public_catalog;
			insert into skin_public_catalog(
				asset_id,public_id,kind,model,display_name,created_at,updated_at,downloads,
				heat_score,favorite_count,bayesian_rating,rating_count,view_count,comment_count,search_document)
			select asset.id,asset.public_id,asset.kind,asset.model,asset.display_name,asset.created_at,asset.updated_at,
				asset.downloads,coalesce(popularity.heat_score,0),coalesce(popularity.favorite_count,0),
				coalesce(popularity.bayesian_rating,0),coalesce(popularity.rating_count,0),
				coalesce(popularity.view_count,0),coalesce(popularity.comment_count,0),
				setweight(to_tsvector('simple',asset.public_id),'A') ||
				setweight(to_tsvector('simple',asset.display_name),'A') ||
				setweight(to_tsvector('simple',asset.description),'B') ||
				setweight(to_tsvector('simple',array_to_string(asset.tags,' ')),'A')
			from skin_assets asset
			left join public_routes route on route.entity_type='skin' and route.internal_id=asset.id
			left join content_popularity_stats popularity on popularity.object_route_id=route.id
			where asset.status='active' and asset.review_status='approved' and asset.visibility='public';
		end;
		$$ language plpgsql`,
		`create or replace function refresh_skin_public_catalog_from_asset() returns trigger as $$
		begin
			perform refresh_skin_public_catalog(new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_skin_public_catalog_assets
			after insert or update of public_id,kind,model,display_name,description,tags,visibility,review_status,status,downloads,created_at,updated_at
			on skin_assets for each row execute function refresh_skin_public_catalog_from_asset()`,
		`create or replace function refresh_skin_public_catalog_from_route() returns trigger as $$
		begin
			if tg_op='DELETE' then
				if old.entity_type='skin' then perform refresh_skin_public_catalog(old.internal_id); end if;
				return old;
			end if;
			if new.entity_type='skin' then perform refresh_skin_public_catalog(new.internal_id); end if;
			if tg_op='UPDATE' and old.entity_type='skin'
				and (old.entity_type,old.internal_id) is distinct from (new.entity_type,new.internal_id) then
				perform refresh_skin_public_catalog(old.internal_id);
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_skin_public_catalog_routes
			after insert or update of entity_type,internal_id or delete on public_routes
			for each row execute function refresh_skin_public_catalog_from_route()`,
		`create or replace function refresh_skin_public_catalog_from_popularity() returns trigger as $$
		declare target_route_id bigint; target_asset_id bigint;
		begin
			if tg_op='DELETE' then target_route_id:=old.object_route_id; else target_route_id:=new.object_route_id; end if;
			select internal_id into target_asset_id from public_routes
				where id=target_route_id and entity_type='skin';
			if target_asset_id is not null then perform refresh_skin_public_catalog(target_asset_id); end if;
			if tg_op='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_skin_public_catalog_popularity
			after insert or update or delete on content_popularity_stats
			for each row execute function refresh_skin_public_catalog_from_popularity()`,
		`select rebuild_skin_public_catalog()`,
	}
}
