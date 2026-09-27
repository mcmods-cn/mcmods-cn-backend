package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestBUG037ProjectUpdateNotificationsLocalizeStableSectionCodesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify localized project update section labels")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
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

	recipients := map[string]int64{}
	stamp := time.Now().UnixNano()
	for index, locale := range []string{"zh-CN", "en-US"} {
		var userID int64
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,preferred_ui_language)
			values($1,$2,'bug037',true,$3) returning id`, fmt.Sprintf("bug037-%d-%d", stamp, index),
			fmt.Sprintf("bug037-%d-%d@example.invalid", stamp, index), locale).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		recipients[locale] = userID
	}

	var projectID, routeID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values('b037m0001','bug037-localized-sections','BUG-037 test project','approved') returning id`,
	).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	for _, userID := range recipients {
		if _, err = pool.Exec(ctx, `insert into project_follows(user_id,project_route_id) values($1,$2)`, userID, routeID); err != nil {
			t.Fatal(err)
		}
	}

	stableSections := []string{"description", "download_files", "minecraft_versions", "future_internal_code"}
	var eventID int64
	if err = pool.QueryRow(ctx, `insert into project_update_events(project_route_id,update_kind,changed_sections,publication_batch_id)
		values($1,'content_updated',$2,'bug037:localization') returning id`, routeID, stableSections).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into project_update_notification_tasks(event_id,next_attempt_at) values($1,now())`, eventID); err != nil {
		t.Fatal(err)
	}

	fallbacksBefore := projectUpdateNotificationObservability.snapshot().UnknownSectionFallbacks
	worker := NewProjectUpdateNotificationWorker(pool, nil, nil)
	if err = worker.process(ctx, eventID); err != nil {
		t.Fatal(err)
	}
	if fallbacks := projectUpdateNotificationObservability.snapshot().UnknownSectionFallbacks - fallbacksBefore; fallbacks != 2 {
		t.Errorf("unknown section fallback metric increased by %d, want one fallback for each of two delivered notifications", fallbacks)
	}

	wantBodies := map[string]string{
		"zh-CN": "BUG-037 test project 更新了详情介绍、下载文件、Minecraft 版本、项目资料。",
		"en-US": "BUG-037 test project updated description, download files, Minecraft versions, project details.",
	}
	for locale, userID := range recipients {
		var sourceLocale, body string
		var dataRaw, paramsRaw []byte
		if err = pool.QueryRow(ctx, `select source_locale,body,data,template_params from notifications
			where recipient_id=$1 and project_update_event_id=$2`, userID, eventID).Scan(&sourceLocale, &body, &dataRaw, &paramsRaw); err != nil {
			t.Fatal(err)
		}
		if sourceLocale != locale || body != wantBodies[locale] {
			t.Errorf("locale %s notification=%q source=%q, want %q", locale, body, sourceLocale, wantBodies[locale])
		}
		for _, internalCode := range []string{"download_files", "minecraft_versions", "future_internal_code"} {
			if strings.Contains(body, internalCode) {
				t.Errorf("locale %s notification leaked internal section code %q: %q", locale, internalCode, body)
			}
		}
		var data struct {
			ChangedSections []string `json:"changedSections"`
		}
		if err = json.Unmarshal(dataRaw, &data); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(data.ChangedSections) != fmt.Sprint(stableSections) {
			t.Errorf("locale %s changedSections=%v, want stable enums %v", locale, data.ChangedSections, stableSections)
		}
		var params map[string]string
		if err = json.Unmarshal(paramsRaw, &params); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(params["changed_sections"], "future_internal_code") {
			t.Errorf("locale %s template params leaked unknown enum: %v", locale, params)
		}
	}
}
