package database

func communityPostCatalogSchemaStatements() []string {
	return []string{
		`create table community_post_catalog (
			id bigint primary key references community_posts(id) on delete cascade,
			public_id text not null unique,
			kind text not null,
			category text not null,
			author_id bigint not null,
			title text not null,
			source_locale text not null,
			body_summary text not null,
			minecraft_versions text[] not null,
			mod_version_min text not null,
			mod_version_max text not null,
			severity text not null,
			has_fix boolean not null,
			issue_url text not null,
			cover_file_id bigint,
			resolution_status text not null,
			accepted_comment_id bigint,
			resolved_at timestamptz,
			review_status text not null,
			created_at timestamptz not null,
			updated_at timestamptz not null,
			published_at timestamptz not null,
			heat_score numeric not null default 0,
			download_count bigint not null default 0,
			favorite_count bigint not null default 0,
			bayesian_rating numeric not null default 0,
			rating_count bigint not null default 0,
			view_count bigint not null default 0,
			comment_count bigint not null default 0,
			search_document tsvector not null
		)`,
		`create index idx_community_post_catalog_search on community_post_catalog using gin(search_document)`,
		`create index idx_community_post_catalog_versions on community_post_catalog using gin(minecraft_versions)`,
		`create index idx_community_post_catalog_published on community_post_catalog(
			kind,review_status,published_at desc,updated_at desc,id desc)`,
		`create index idx_community_post_catalog_updated on community_post_catalog(
			kind,review_status,updated_at desc,id desc)`,
		`create index idx_community_post_catalog_heat on community_post_catalog(
			kind,review_status,heat_score desc,updated_at desc,id desc)`,
		`create index idx_community_post_catalog_downloads on community_post_catalog(
			kind,review_status,download_count desc,updated_at desc,id desc)`,
		`create index idx_community_post_catalog_favorites on community_post_catalog(
			kind,review_status,favorite_count desc,updated_at desc,id desc)`,
		`create index idx_community_post_catalog_rating on community_post_catalog(
			kind,review_status,bayesian_rating desc,rating_count desc,updated_at desc,id desc)`,
		`create index idx_community_post_catalog_views on community_post_catalog(
			kind,review_status,view_count desc,updated_at desc,id desc)`,
		`create index idx_community_post_catalog_comments on community_post_catalog(
			kind,review_status,comment_count desc,updated_at desc,id desc)`,
		`create index idx_community_post_catalog_name on community_post_catalog(
			kind,review_status,lower(title),id)`,
		`create or replace function refresh_community_post_catalog(target_post_id bigint) returns void as $$
		begin
			insert into community_post_catalog(
				id,public_id,kind,category,author_id,title,source_locale,body_summary,minecraft_versions,
				mod_version_min,mod_version_max,severity,has_fix,issue_url,cover_file_id,resolution_status,
				accepted_comment_id,resolved_at,review_status,created_at,updated_at,published_at,
				heat_score,download_count,favorite_count,bayesian_rating,rating_count,view_count,comment_count,search_document)
			select post.id,post.public_id,post.kind,post.category,post.author_id,post.title,post.source_locale,
				left(regexp_replace(post.body_markdown,E'[\\n\\r\\t ]+',' ','g'),320),post.minecraft_versions,
				post.mod_version_min,post.mod_version_max,post.severity,post.has_fix,post.issue_url,post.cover_file_id,
				post.resolution_status,post.accepted_comment_id,post.resolved_at,post.review_status,post.created_at,
				post.updated_at,coalesce(post.published_at,post.created_at),coalesce(popularity.heat_score,0),
				coalesce(popularity.download_count,0),coalesce(popularity.favorite_count,0),
				coalesce(popularity.bayesian_rating,0),coalesce(popularity.rating_count,0),
				coalesce(popularity.view_count,0),coalesce(popularity.comment_count,0),
				to_tsvector('simple',post.title||' '||post.body_markdown)
			from community_posts post
			left join public_routes route on route.entity_type='community_post' and route.internal_id=post.id
			left join content_popularity_stats popularity on popularity.object_route_id=route.id
			where post.id=target_post_id and post.status='active'
			on conflict(id) do update set
				public_id=excluded.public_id,kind=excluded.kind,category=excluded.category,author_id=excluded.author_id,
				title=excluded.title,source_locale=excluded.source_locale,body_summary=excluded.body_summary,
				minecraft_versions=excluded.minecraft_versions,mod_version_min=excluded.mod_version_min,
				mod_version_max=excluded.mod_version_max,severity=excluded.severity,has_fix=excluded.has_fix,
				issue_url=excluded.issue_url,cover_file_id=excluded.cover_file_id,resolution_status=excluded.resolution_status,
				accepted_comment_id=excluded.accepted_comment_id,resolved_at=excluded.resolved_at,
				review_status=excluded.review_status,created_at=excluded.created_at,updated_at=excluded.updated_at,
				published_at=excluded.published_at,heat_score=excluded.heat_score,download_count=excluded.download_count,
				favorite_count=excluded.favorite_count,bayesian_rating=excluded.bayesian_rating,
				rating_count=excluded.rating_count,view_count=excluded.view_count,comment_count=excluded.comment_count,
				search_document=excluded.search_document;
			delete from community_post_catalog where id=target_post_id and not exists(
				select 1 from community_posts where id=target_post_id and status='active');
		end;
		$$ language plpgsql`,
		`create or replace function refresh_community_post_catalog_from_post() returns trigger as $$
		begin
			if TG_OP='DELETE' then delete from community_post_catalog where id=old.id; return old; end if;
			perform refresh_community_post_catalog(new.id); return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_community_post_catalog_refresh after insert or update or delete on community_posts
			for each row execute function refresh_community_post_catalog_from_post()`,
		`create or replace function refresh_community_post_catalog_from_route() returns trigger as $$
		declare route_type text; route_internal_id bigint;
		begin
			route_type=case when TG_OP='DELETE' then old.entity_type else new.entity_type end;
			route_internal_id=case when TG_OP='DELETE' then old.internal_id else new.internal_id end;
			if route_type='community_post' then perform refresh_community_post_catalog(route_internal_id); end if;
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_community_post_catalog_route after insert or update or delete on public_routes
			for each row execute function refresh_community_post_catalog_from_route()`,
		`create or replace function refresh_community_post_catalog_from_popularity() returns trigger as $$
		declare route_id bigint; post_id bigint;
		begin
			route_id=case when TG_OP='DELETE' then old.object_route_id else new.object_route_id end;
			select internal_id into post_id from public_routes where id=route_id and entity_type='community_post';
			if post_id is not null then perform refresh_community_post_catalog(post_id); end if;
			if TG_OP='DELETE' then return old; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_community_post_catalog_popularity after insert or update or delete on content_popularity_stats
			for each row execute function refresh_community_post_catalog_from_popularity()`,
		`select refresh_community_post_catalog(id) from community_posts where status='active'`,
	}
}
