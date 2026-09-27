package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestInstallEphemeralSchemaStaysInSessionTemporaryNamespaceIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the temporary full-schema harness")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
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
	var publicGenerationBefore int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationBefore); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	cleaned := false
	defer func() {
		if !cleaned {
			if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
				t.Errorf("drop ephemeral schema: %v", dropErr)
			}
		}
	}()
	var namespace string
	var generation int
	var modsExist bool
	if err = pool.QueryRow(ctx, `select current_schema(),
		(select generation from schema_metadata where singleton),to_regclass('mods') is not null`).
		Scan(&namespace, &generation, &modsExist); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if namespace == "public" || generation != schemaGeneration || !modsExist {
		t.Fatalf("ephemeral namespace=%q generation=%d modsExist=%t", namespace, generation, modsExist)
	}
	if _, err = pool.Exec(ctx, `
		insert into unresolved_references(
			id,source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier)
		values(987059001,'scale_source',987059001,'field','mod','Needle059','needle059');
		insert into catalog_entities(id,identity_key,public_id,entity_type)
		values(987059002,'minecraft:needle059','rperf059a','resource');
		insert into unresolved_resource_references(
			id,source_entity_id,field_path,kind_code,raw_resource_id)
		values(987059003,987059002,'ingredient','minecraft.item','minecraft:missing059')
	`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var projectedRows, projectedResources int
	if err = pool.QueryRow(ctx, `select count(*),count(*) filter(where origin=1 and source_label='minecraft:needle059'
		and source_public_id='rperf059a') from unresolved_reference_catalog
		where (origin=0 and source_row_id=987059001) or (origin=1 and source_row_id=987059003)`).
		Scan(&projectedRows, &projectedResources); err != nil || projectedRows != 2 || projectedResources != 1 {
		pool.Close()
		t.Fatalf("unresolved projection insert rows=%d resources=%d err=%v", projectedRows, projectedResources, err)
	}
	if _, err = pool.Exec(ctx, `
		update unresolved_references set status='ignored' where id=987059001;
		update catalog_entities set identity_key='minecraft:renamed059' where id=987059002
	`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var projectedIgnored, projectedRenamed int
	if err = pool.QueryRow(ctx, `select count(*) filter(where origin=0 and status='ignored'),
		count(*) filter(where origin=1 and source_label='minecraft:renamed059')
		from unresolved_reference_catalog where source_row_id in (987059001,987059003)`).
		Scan(&projectedIgnored, &projectedRenamed); err != nil || projectedIgnored != 1 || projectedRenamed != 1 {
		pool.Close()
		t.Fatalf("unresolved projection update ignored=%d renamed=%d err=%v", projectedIgnored, projectedRenamed, err)
	}
	if _, err = pool.Exec(ctx, `delete from unresolved_references where id=987059001;
		delete from catalog_entities where id=987059002`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from unresolved_reference_catalog
		where source_row_id in (987059001,987059003)`).Scan(&projectedRows); err != nil || projectedRows != 0 {
		pool.Close()
		t.Fatalf("unresolved projection delete rows=%d err=%v", projectedRows, err)
	}
	if _, err = pool.Exec(ctx, `
		insert into users(id,public_id,username,email,password_hash)
		values(987654,'uperf029a','PERF 029','perf029@example.test','not-a-real-hash');
		insert into notifications(id,public_id,recipient_id,kind)
		values(98765001,'nperf029a',null,'system'),(98765002,'nperf029b',987654,'review')
	`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var broadcastCount, maxBroadcastID int64
	if err = pool.QueryRow(ctx, `select live_count,max_notification_id from notification_broadcast_state where singleton`).
		Scan(&broadcastCount, &maxBroadcastID); err != nil || broadcastCount != 1 || maxBroadcastID != 98765001 {
		pool.Close()
		t.Fatalf("broadcast state after insert count=%d max=%d err=%v", broadcastCount, maxBroadcastID, err)
	}
	if _, err = pool.Exec(ctx, `delete from notifications where id=98765001;
		update notifications set recipient_id=null where id=98765002;
		update notifications set recipient_id=987654 where id=98765002`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select live_count,max_notification_id from notification_broadcast_state where singleton`).
		Scan(&broadcastCount, &maxBroadcastID); err != nil || broadcastCount != 0 || maxBroadcastID != 98765002 {
		pool.Close()
		t.Fatalf("broadcast state after delete/moves count=%d max=%d err=%v", broadcastCount, maxBroadcastID, err)
	}
	if _, err = pool.Exec(ctx, `
		insert into users(id,public_id,username,email,password_hash)
		values(987655,'uperf030a','PERF 030','perf030@example.test','not-a-real-hash');
		insert into direct_conversations(id,public_id,user_low_id,user_high_id)
		values(987660,'cperf030a',987654,987655);
		insert into direct_messages(id,public_id,conversation_id,sender_id,recipient_id,body)
		values(987670,'mperf030a',987660,987655,987654,'unread')
	`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var directUnread int64
	if err = pool.QueryRow(ctx, `select unread_count from direct_conversation_unread_counts
		where conversation_id=987660 and user_id=987654`).Scan(&directUnread); err != nil || directUnread != 1 {
		pool.Close()
		t.Fatalf("direct unread after insert=%d err=%v", directUnread, err)
	}
	if _, err = pool.Exec(ctx, `update direct_messages set read_at=clock_timestamp() where id=987670`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var unreadRows int
	if err = pool.QueryRow(ctx, `select count(*) from direct_conversation_unread_counts
		where conversation_id=987660 and user_id=987654`).Scan(&unreadRows); err != nil || unreadRows != 0 {
		pool.Close()
		t.Fatalf("direct unread after read rows=%d err=%v", unreadRows, err)
	}
	if _, err = pool.Exec(ctx, `update direct_messages set read_at=null,recipient_id=987655 where id=987670`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select unread_count from direct_conversation_unread_counts
		where conversation_id=987660 and user_id=987655`).Scan(&directUnread); err != nil || directUnread != 1 {
		pool.Close()
		t.Fatalf("direct unread after recipient move=%d err=%v", directUnread, err)
	}
	if _, err = pool.Exec(ctx, `delete from direct_messages where id=987670`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from direct_conversation_unread_counts where conversation_id=987660`).Scan(&unreadRows); err != nil || unreadRows != 0 {
		pool.Close()
		t.Fatalf("direct unread after delete rows=%d err=%v", unreadRows, err)
	}
	if _, err = pool.Exec(ctx, `
		insert into direct_messages(id,public_id,conversation_id,sender_id,recipient_id,body)
		values(987671,'mperf030b',987660,987654,987655,'latest');
		update direct_conversations set last_message_id=987671 where id=987660;
		delete from direct_conversations where id=987660
	`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var directMessageRows int
	if err = pool.QueryRow(ctx, `select count(*) from direct_messages where conversation_id=987660`).Scan(&directMessageRows); err != nil || directMessageRows != 0 {
		pool.Close()
		t.Fatalf("direct conversation cascade left messages=%d err=%v", directMessageRows, err)
	}
	if _, err = pool.Exec(ctx, `
		insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		values('sperf021a','minecraft_server',4242,'/servers/sperf021a');
		delete from search_index_queue;
		insert into content_popularity_stats(object_route_id)
		select id from public_routes where public_id='sperf021a'`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var queuedOperation string
	if err = pool.QueryRow(ctx, `select operation from search_index_queue
		where document_type='server' and document_id=4242`).Scan(&queuedOperation); err != nil || queuedOperation != "upsert" {
		pool.Close()
		t.Fatalf("server popularity projection operation=%q err=%v", queuedOperation, err)
	}
	if _, err = pool.Exec(ctx, `
		insert into app_logs(id,category,actor_id,action) values(987680,'system',987654,'perf033needle');
		insert into permission_audit_logs(id,operator_id,target_user_id,action) values(987681,987654,987655,'perf033needle');
		insert into user_login_logs(id,user_id,account,success,reason) values(987682,987654,'perf033@example.test',false,'perf033needle');
		insert into oss_upload_logs(id,uploader_id,object_key,result,message) values(987683,987654,'perf033/object','failed','perf033needle')
	`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var searchableLogRows int
	if err = pool.QueryRow(ctx, `select
		(select count(*) from app_logs where search_document @@ websearch_to_tsquery('simple','perf033needle'))+
		(select count(*) from permission_audit_logs where search_document @@ websearch_to_tsquery('simple','perf033needle'))+
		(select count(*) from user_login_logs where search_document @@ websearch_to_tsquery('simple','perf033needle'))+
		(select count(*) from oss_upload_logs where search_document @@ websearch_to_tsquery('simple','perf033needle'))`).Scan(&searchableLogRows); err != nil || searchableLogRows != 4 {
		pool.Close()
		t.Fatalf("write-maintained searchable log rows=%d err=%v", searchableLogRows, err)
	}
	var popularityModID, popularityRouteID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values('pperf034a','perf-034-incremental','PERF 034','approved') returning id`).Scan(&popularityModID); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, popularityModID).Scan(&popularityRouteID); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `with updated_project as (
		update mods set primary_name='PERF 036 searchable project',updated_at=clock_timestamp() where id=$1 returning id
	) insert into content_route_metrics(object_route_id,total_view_count,edit_count,created_at)
		select $2,13,2,clock_timestamp() from updated_project`, popularityModID, popularityRouteID); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var adminCatalogName string
	var adminCatalogViews, adminCatalogEdits int64
	var adminCatalogSearchable bool
	if err = pool.QueryRow(ctx, `select name,view_count,edit_count,
		search_document @@ websearch_to_tsquery('simple','searchable')
		from admin_project_catalog where object_route_id=$1`, popularityRouteID).
		Scan(&adminCatalogName, &adminCatalogViews, &adminCatalogEdits, &adminCatalogSearchable); err != nil ||
		adminCatalogName != "PERF 036 searchable project" || adminCatalogViews != 13 || adminCatalogEdits != 2 || !adminCatalogSearchable {
		pool.Close()
		t.Fatalf("PERF036 project projection name=%q views=%d edits=%d searchable=%t err=%v",
			adminCatalogName, adminCatalogViews, adminCatalogEdits, adminCatalogSearchable, err)
	}
	popularitySetupOperations := []struct {
		query string
		args  []any
	}{
		{`insert into content_view_daily(object_route_id,view_date,counter_shard,views) values($1,current_date,0,7)`, []any{popularityRouteID}},
		{`update content_view_daily set views=9 where object_route_id=$1 and view_date=current_date and counter_shard=0`, []any{popularityRouteID}},
		{`insert into content_unique_views(object_route_id,viewer_hash,viewer_user_id) values($1,decode(md5('perf034'),'hex'),987654)`, []any{popularityRouteID}},
		{`insert into content_project_pages(object_route_id,page_hash) values($1,decode(md5('perf034-page'),'hex'))`, []any{popularityRouteID}},
		{`insert into content_download_counters(object_route_id,owner_id,downloads) values($1,987654,4)`, []any{popularityRouteID}},
		{`insert into favorite_collections(id,user_id,name) values(987690,987654,'PERF 034')`, nil},
		{`insert into favorite_collection_items(id,collection_id,entity_type,entity_id) values(987691,987690,'mod',$1)`, []any{popularityModID}},
		{`insert into comments(id,public_id,target_type,target_id,author_id,body) values(987692,'xperf034a','mod',$1,987654,'incremental')`, []any{popularityModID}},
		{`insert into content_ratings(id,public_id,object_route_id,author_id,overall_score) values(987693,'rperf034a',$1,987654,4)`, []any{popularityRouteID}},
		{`insert into content_rating_scores(rating_id,dimension_code,score) values(987693,'quality',5)`, nil},
	}
	for _, operation := range popularitySetupOperations {
		if _, err = pool.Exec(ctx, operation.query, operation.args...); err != nil {
			t.Fatalf("PERF034 setup operation %q: %v", operation.query, err)
		}
	}
	assertGlobalRatingStats := func(stage string, wantCount, wantSum int64, wantAverage float64) {
		t.Helper()
		var ratingCount, ratingSum int64
		var ratingAverage float64
		queryErr := pool.QueryRow(ctx, `select rating_count,rating_sum,average_rating::double precision
			from content_rating_global_stats where entity_type='mod'`).
			Scan(&ratingCount, &ratingSum, &ratingAverage)
		if queryErr != nil || ratingCount != wantCount || ratingSum != wantSum || ratingAverage != wantAverage {
			t.Fatalf("%s global rating count=%d sum=%d average=%v want=%d/%d/%v err=%v",
				stage, ratingCount, ratingSum, ratingAverage, wantCount, wantSum, wantAverage, queryErr)
		}
	}
	assertGlobalRatingStats("insert", 1, 4, 4)
	if _, err = pool.Exec(ctx, `update content_rating_global_stats set rating_count=77,rating_sum=77,average_rating=1
		where entity_type='mod'; select pg_temp.rebuild_content_rating_global_stats('mod')`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	assertGlobalRatingStats("set rebuild", 1, 4, 4)
	var factViews, factUnique, factPages, factDownloads, factFavorites, factComments, factCommenters, factRatings, factRatingSum int64
	var dimensionCount, dimensionSum int64
	if err = pool.QueryRow(ctx, `select
		(select coalesce(sum(views),0) from content_popularity_view_totals where object_route_id=$1),
		unique_view_count,page_count,download_count,favorite_count,comment_count,effective_commenter_count,rating_count,rating_sum
		from content_popularity_lifetime_facts where object_route_id=$1`, popularityRouteID).
		Scan(&factViews, &factUnique, &factPages, &factDownloads, &factFavorites, &factComments, &factCommenters, &factRatings, &factRatingSum); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select rating_count,score_sum from content_popularity_rating_dimension_facts
		where object_route_id=$1 and dimension_code='quality'`, popularityRouteID).Scan(&dimensionCount, &dimensionSum); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if factViews != 9 || factUnique != 1 || factPages != 1 || factDownloads != 4 || factFavorites != 1 ||
		factComments != 1 || factCommenters != 1 || factRatings != 1 || factRatingSum != 4 || dimensionCount != 1 || dimensionSum != 5 {
		pool.Close()
		t.Fatalf("incremental facts views=%d unique=%d pages=%d downloads=%d favorites=%d comments=%d commenters=%d ratings=%d sum=%d dimension=%d/%d",
			factViews, factUnique, factPages, factDownloads, factFavorites, factComments, factCommenters, factRatings, factRatingSum, dimensionCount, dimensionSum)
	}
	if _, err = pool.Exec(ctx, `select pg_temp.refresh_content_popularity($1::bigint)`, popularityRouteID); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var projectedViews, projectedFavorites, projectedComments, projectedRatings int64
	if err = pool.QueryRow(ctx, `select view_count,favorite_count,comment_count,rating_count from content_popularity_stats where object_route_id=$1`, popularityRouteID).
		Scan(&projectedViews, &projectedFavorites, &projectedComments, &projectedRatings); err != nil ||
		projectedViews != 9 || projectedFavorites != 1 || projectedComments != 1 || projectedRatings != 1 {
		pool.Close()
		t.Fatalf("projected incremental facts views=%d favorites=%d comments=%d ratings=%d err=%v",
			projectedViews, projectedFavorites, projectedComments, projectedRatings, err)
	}
	var adminCatalogFavorites, adminCatalogComments int64
	var adminCatalogRating float64
	if err = pool.QueryRow(ctx, `select favorite_count,comment_count,rating_average::double precision
		from admin_project_catalog where object_route_id=$1`, popularityRouteID).
		Scan(&adminCatalogFavorites, &adminCatalogComments, &adminCatalogRating); err != nil ||
		adminCatalogFavorites != 1 || adminCatalogComments != 1 || adminCatalogRating != 4 {
		pool.Close()
		t.Fatalf("PERF036 popularity projection favorites=%d comments=%d rating=%v err=%v",
			adminCatalogFavorites, adminCatalogComments, adminCatalogRating, err)
	}
	popularityMutationOperations := []struct {
		query             string
		args              []any
		globalRatingCount int64
		globalRatingSum   int64
		globalAverage     float64
	}{
		{`update content_view_daily set views=11 where object_route_id=$1 and view_date=current_date and counter_shard=0`, []any{popularityRouteID}, 1, 4, 4},
		{`update content_download_counters set downloads=6 where object_route_id=$1 and owner_id=987654`, []any{popularityRouteID}, 1, 4, 4},
		{`update content_ratings set overall_score=3 where id=987693`, nil, 1, 3, 3},
		{`update content_rating_scores set score=2 where rating_id=987693 and dimension_code='quality'`, nil, 1, 3, 3},
		{`update content_ratings set status='hidden' where id=987693`, nil, 0, 0, 3.5},
		{`update content_ratings set status='published' where id=987693`, nil, 1, 3, 3},
		{`delete from favorite_collection_items where id=987691`, nil, 1, 3, 3},
		{`update comments set status='hidden' where id=987692`, nil, 1, 3, 3},
		{`delete from content_ratings where id=987693`, nil, 0, 0, 3.5},
	}
	for operationIndex, operation := range popularityMutationOperations {
		if _, err = pool.Exec(ctx, operation.query, operation.args...); err != nil {
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) {
				t.Fatalf("PERF034 mutation operation %q: %v; detail=%s; where=%s",
					operation.query, err, postgresError.Detail, postgresError.Where)
			}
			t.Fatalf("PERF034 mutation operation %q: %v", operation.query, err)
		}
		assertGlobalRatingStats(fmt.Sprintf("mutation %d", operationIndex),
			operation.globalRatingCount, operation.globalRatingSum, operation.globalAverage)
	}
	if err = pool.QueryRow(ctx, `select
		(select coalesce(sum(views),0) from content_popularity_view_totals where object_route_id=$1),
		download_count,favorite_count,comment_count,effective_commenter_count,rating_count,rating_sum,
		(select count(*) from content_popularity_rating_dimension_facts where object_route_id=$1)
		from content_popularity_lifetime_facts where object_route_id=$1`, popularityRouteID).
		Scan(&factViews, &factDownloads, &factFavorites, &factComments, &factCommenters, &factRatings, &factRatingSum, &dimensionCount); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if factViews != 11 || factDownloads != 6 || factFavorites != 0 || factComments != 0 || factCommenters != 0 || factRatings != 0 || factRatingSum != 0 || dimensionCount != 0 {
		pool.Close()
		t.Fatalf("incremental mutation facts views=%d downloads=%d favorites=%d comments=%d commenters=%d ratings=%d sum=%d dimensionRows=%d",
			factViews, factDownloads, factFavorites, factComments, factCommenters, factRatings, factRatingSum, dimensionCount)
	}
	if _, err = pool.Exec(ctx, `update content_popularity_lifetime_facts set
		unique_view_count=77,page_count=77,download_count=77,favorite_count=77,
		comment_count=77,effective_commenter_count=77,rating_count=77,rating_sum=77
		where object_route_id=$1`, popularityRouteID); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `select pg_temp.rebuild_content_popularity_lifetime_facts($1::bigint)`, popularityRouteID); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var calibrated bool
	if err = pool.QueryRow(ctx, `select unique_view_count,page_count,download_count,favorite_count,
		comment_count,effective_commenter_count,rating_count,rating_sum,calibrated_at is not null
		from content_popularity_lifetime_facts where object_route_id=$1`, popularityRouteID).
		Scan(&factUnique, &factPages, &factDownloads, &factFavorites, &factComments, &factCommenters, &factRatings, &factRatingSum, &calibrated); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if factUnique != 1 || factPages != 1 || factDownloads != 6 || factFavorites != 0 || factComments != 0 ||
		factCommenters != 0 || factRatings != 0 || factRatingSum != 0 || !calibrated {
		pool.Close()
		t.Fatalf("calibrated facts unique=%d pages=%d downloads=%d favorites=%d comments=%d commenters=%d ratings=%d sum=%d calibrated=%t",
			factUnique, factPages, factDownloads, factFavorites, factComments, factCommenters, factRatings, factRatingSum, calibrated)
	}
	if _, err = pool.Exec(ctx, `alter table content_unique_views rename to content_unique_views_history;
		create temporary table content_unique_views(sample_id bigint not null)`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	var previousScale int64
	for _, scale := range []int64{100_000, 1_000_000, 10_000_000} {
		command, insertErr := pool.Exec(ctx, `insert into content_unique_views(sample_id)
			select value from generate_series($1::bigint+1,$2::bigint) value`, previousScale, scale)
		if insertErr != nil {
			pool.Close()
			t.Fatalf("populate PERF034 history trap at %d rows: %v", scale, insertErr)
		}
		if command.RowsAffected() != scale-previousScale {
			pool.Close()
			t.Fatalf("populate PERF034 history trap at %d rows affected=%d", scale, command.RowsAffected())
		}
		if _, err = pool.Exec(ctx, `discard plans`); err != nil {
			pool.Close()
			t.Fatal(err)
		}
		started := time.Now()
		var refreshPlan string
		if err = pool.QueryRow(ctx, `explain(analyze,buffers,format json)
			select pg_temp.refresh_content_popularity($1::bigint)`, popularityRouteID).Scan(&refreshPlan); err != nil {
			pool.Close()
			t.Fatalf("refresh PERF034 with %d history rows: %v", scale, err)
		}
		elapsed := time.Since(started)
		if elapsed > time.Second {
			pool.Close()
			t.Fatalf("refresh PERF034 with %d history rows took %s", scale, elapsed)
		}
		if strings.Contains(strings.ToLower(refreshPlan), "content_unique_views") {
			pool.Close()
			t.Fatalf("refresh PERF034 plan touched lifetime history at %d rows: %s", scale, refreshPlan)
		}
		t.Logf("PERF034 historyRows=%d refreshPlanWall=%s", scale, elapsed)
		previousScale = scale
	}
	if _, err = pool.Exec(ctx, `insert into comments(id,public_id,target_type,target_id,author_id,body,status)
		values(987045001,'cperf045a','mod',987045,987654,'counter fact','published')`); err != nil {
		pool.Close()
		t.Fatalf("insert PERF045 counter fixture: %v", err)
	}
	var targetCommentCount, authorCommentCount int64
	if err = pool.QueryRow(ctx, `select
		(select visible_count from comment_target_counts
			where target_type='mod' and target_id=987045 and target_version_key=0),
		(select visible_count from comment_target_author_counts
			where target_type='mod' and target_id=987045 and target_version_key=0 and author_id=987654)`).
		Scan(&targetCommentCount, &authorCommentCount); err != nil || targetCommentCount != 1 || authorCommentCount != 1 {
		pool.Close()
		t.Fatalf("PERF045 counters after insert target=%d author=%d err=%v", targetCommentCount, authorCommentCount, err)
	}
	if _, err = pool.Exec(ctx, `update comments set status='hidden' where id=987045001`); err != nil {
		pool.Close()
		t.Fatalf("hide PERF045 counter fixture: %v", err)
	}
	var countersRemain bool
	if err = pool.QueryRow(ctx, `select exists(select 1 from comment_target_counts
		where target_type='mod' and target_id=987045 and target_version_key=0)`).Scan(&countersRemain); err != nil || countersRemain {
		pool.Close()
		t.Fatalf("PERF045 hidden counter remains=%t err=%v", countersRemain, err)
	}
	if _, err = pool.Exec(ctx, `update comments set status='deleted' where id=987045001;
		update comment_target_counts set visible_count=99 where target_type='mod' and target_id=987045;
		select pg_temp.rebuild_comment_target_counts()`); err != nil {
		pool.Close()
		t.Fatalf("rebuild PERF045 counters: %v", err)
	}
	if err = pool.QueryRow(ctx, `select visible_count from comment_target_counts
		where target_type='mod' and target_id=987045 and target_version_key=0`).Scan(&targetCommentCount); err != nil || targetCommentCount != 1 {
		pool.Close()
		t.Fatalf("PERF045 rebuilt target count=%d err=%v", targetCommentCount, err)
	}
	if _, err = pool.Exec(ctx, `insert into oss_files(
		id,public_id,object_key,category,content_type,size_bytes,source_size_bytes,uploader_id,status,scan_status)
		values(987048001,'fperf048a','perf048/texture','skin_texture','image/png',64,64,987654,'active','clean');
		insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes)
		values(repeat('f',64),987048001,'perf048/texture',64,64,64);
		insert into skin_assets(id,public_id,owner_id,blob_hash,kind,model,display_name,description,tags,
			visibility,review_status,status,downloads)
		values(987048002,'sperf048a',987654,repeat('f',64),'skin','slim','PERF048 searchable',
			'perf048needle description',array['catalog048'],'public','approved','active',7)`); err != nil {
		pool.Close()
		t.Fatalf("insert PERF048 skin projection fixture: %v", err)
	}
	var skinRouteID int64
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='skin' and internal_id=987048002`).Scan(&skinRouteID); err != nil {
		pool.Close()
		t.Fatalf("load PERF048 skin route: %v", err)
	}
	if _, err = pool.Exec(ctx, `insert into content_popularity_stats(
		object_route_id,view_count,favorite_count,comment_count,rating_count,rating_sum,
		rating_average,bayesian_rating,heat_score)
		values($1,11,3,4,2,9,4.5,4.25,12.5)`, skinRouteID); err != nil {
		pool.Close()
		t.Fatalf("update PERF048 popularity projection: %v", err)
	}
	assertSkinProjection := func(stage string, wantRows, wantViews int64) {
		t.Helper()
		var rows, views, searchable int64
		if err = pool.QueryRow(ctx, `select count(*),coalesce(max(view_count),0),
			count(*) filter(where search_document@@websearch_to_tsquery('simple','perf048needle'))
			from skin_public_catalog where asset_id=987048002`).Scan(&rows, &views, &searchable); err != nil ||
			rows != wantRows || views != wantViews || searchable != wantRows {
			pool.Close()
			t.Fatalf("PERF048 %s rows=%d views=%d searchable=%d want=%d/%d/%d err=%v",
				stage, rows, views, searchable, wantRows, wantViews, wantRows, err)
		}
	}
	assertSkinProjection("insert/popularity", 1, 11)
	if _, err = pool.Exec(ctx, `update skin_assets set visibility='private' where id=987048002`); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	assertSkinProjection("hidden", 0, 0)
	if _, err = pool.Exec(ctx, `update skin_assets set visibility='public' where id=987048002;
		update skin_public_catalog set view_count=999 where asset_id=987048002;
		select pg_temp.rebuild_skin_public_catalog()`); err != nil {
		pool.Close()
		t.Fatalf("rebuild PERF048 skin projection: %v", err)
	}
	assertSkinProjection("rebuild", 1, 11)
	if err = DropEphemeralSchema(ctx, pool); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	cleaned = true
	pool.Close()

	verificationPool, err := pgxpool.New(ctx, loaded.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer verificationPool.Close()
	var publicGenerationAfter int
	var temporaryRelationsRemain, temporaryRoutinesRemain bool
	if err = verificationPool.QueryRow(ctx, `select
		(select generation from public.schema_metadata where singleton),exists(
			select 1 from pg_class relation join pg_namespace namespace_row on namespace_row.oid=relation.relnamespace
			where namespace_row.nspname=$1
		),exists(
			select 1 from pg_proc routine join pg_namespace namespace_row on namespace_row.oid=routine.pronamespace
			where namespace_row.nspname=$1
		)`, namespace).Scan(&publicGenerationAfter, &temporaryRelationsRemain, &temporaryRoutinesRemain); err != nil {
		t.Fatal(err)
	}
	if publicGenerationAfter != publicGenerationBefore || temporaryRelationsRemain || temporaryRoutinesRemain {
		t.Fatalf("public generation %d -> %d; temporary relations retained=%t routines retained=%t",
			publicGenerationBefore, publicGenerationAfter, temporaryRelationsRemain, temporaryRoutinesRemain)
	}
}
