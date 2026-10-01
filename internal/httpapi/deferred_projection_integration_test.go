package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectUpdateFailuresEventuallyStopAndDoNotAdvanceRecipientsIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	var mod, route, event, user int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'notify-retry-'||gen_random_uuid()::text,'Synthetic failed template','approved') returning id`).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, mod).Scan(&route); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('notify-recipient-'||gen_random_uuid()::text,gen_random_uuid()::text||'@test.invalid','synthetic-unused') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_follows(user_id,project_route_id) values($1,$2)`, user, route); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into project_update_events(project_route_id,update_kind,changed_sections,publication_batch_id) values($1,'edit',array['metadata'],gen_random_uuid()::text) returning id`, route).Scan(&event); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_update_notification_tasks(event_id) values($1)`, event); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into system_settings(key,value) values('notifications.templates','{"templates":[{"code":"project_updated","variables":["unexpected"],"translations":{"zh-CN":{"title":"{unexpected}","body":"Missing input"}}}]}')`); err != nil {
		t.Fatal(err)
	}
	worker := NewProjectUpdateNotificationWorker(pool, nil, nil)
	for attempt := 1; attempt <= 8; attempt++ {
		if _, err := pool.Exec(ctx, `update project_update_notification_tasks set next_attempt_at=now() where event_id=$1`, event); err != nil {
			t.Fatal(err)
		}
		err := worker.process(ctx, event)
		if err == nil {
			t.Fatal("bad template unexpectedly published")
		}
		worker.retry(ctx, event, err)
		var status string
		var attempts, next int
		if err = pool.QueryRow(ctx, `select status,attempt_count,next_user_id from project_update_notification_tasks where event_id=$1`, event).Scan(&status, &attempts, &next); err != nil {
			t.Fatal(err)
		}
		if attempts != attempt || next != 0 || (attempt < 8 && status != "pending") || (attempt == 8 && status != "failed") {
			t.Fatalf("failure state attempt=%d stored=%d status=%s next=%d", attempt, attempts, status, next)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from notifications where project_update_event_id=$1`, event).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed template partially published count=%d err=%v", count, err)
	}
}

func TestStaleStatisticsClaimCannotOverwriteNewProjectionIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	var mod, route int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'stats-fence-'||gen_random_uuid()::text,'Synthetic statistics fence','approved') returning id`).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, mod).Scan(&route); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into content_route_metrics(object_route_id,direct_view_count,total_view_count,created_at) values($1,999,999,now()) on conflict(object_route_id) do update set direct_view_count=999,total_view_count=999`, route); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into content_stats_refresh_queue(object_route_id,attempts,locked_at) values($1,2,now()) on conflict(object_route_id) do update set attempts=2,locked_at=now()`, route); err != nil {
		t.Fatal(err)
	}
	processContentStatsTask(ctx, pool, contentStatsRefreshTask{routeID: route, attempt: 1, refreshMetrics: true})
	var views int
	if err := pool.QueryRow(ctx, `select total_view_count from content_route_metrics where object_route_id=$1`, route).Scan(&views); err != nil || views != 999 {
		t.Fatalf("stale claim changed newer projection=%d err=%v", views, err)
	}
	processContentStatsTask(ctx, pool, contentStatsRefreshTask{routeID: route, attempt: 2, refreshMetrics: true})
	var pending int
	if err := pool.QueryRow(ctx, `select count(*) from content_stats_refresh_queue where object_route_id=$1`, route).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("current claim did not finish pending=%d err=%v", pending, err)
	}
	if err := pool.QueryRow(ctx, `select total_view_count from content_route_metrics where object_route_id=$1`, route).Scan(&views); err != nil || views != 0 {
		t.Fatalf("current claim did not rebuild views=%d err=%v", views, err)
	}
}

func TestCatalogResourceAssetRejectsUnapprovedSourceVersionIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var mod, version, resource, file int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'asset-private-'||gen_random_uuid()::text,'Synthetic hidden asset','pending') returning id`).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id) values($1) returning id`, mod).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type) values('asset-private-'||gen_random_uuid()::text,'resource') returning id,public_id`).Scan(&resource, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id) values($1,'minecraft.item','audit:hidden_asset','audit','hidden_asset',$2)`, resource, mod); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resource, mod); err != nil {
		t.Fatal(err)
	}
	// Non-image MIME distinguishes an authorized asset lookup (415) without
	// invoking any object-storage supplier or making a signed network request.
	if err := pool.QueryRow(ctx, `insert into oss_files(object_key,content_type,scan_status) values('synthetic-hidden-asset','application/octet-stream','trusted_generated') returning id`).Scan(&file); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,icon_file_id,status) values($1,$2,$3,'active')`, resource, version, file); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: cfg}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/resources/"+publicID+"/icon", nil)
	request.SetPathValue("publicId", publicID)
	request.SetPathValue("assetKind", "icon")
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	server.catalogResourceAsset(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("hidden asset exposed status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, mod); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	server.catalogResourceAsset(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("approved asset lookup status=%d body=%s", response.Code, response.Body.String())
	}
}
