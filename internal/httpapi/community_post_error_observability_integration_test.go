package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

func TestCommunityPostCorruptReferencesAndTranslationResultsFailClosedIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify community data failures")
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
		create temporary table community_post_project_refs(
			id bigint primary key,post_id bigint not null,target_type text not null,
			raw_identifier text not null,target_id bigint,display_order integer not null
		);
		create temporary table public_routes(entity_type text not null,internal_id bigint not null,public_id text not null);
		create temporary table community_post_resource_refs(
			id bigint primary key,post_id bigint not null,resource_id bigint,kind_code text not null,
			raw_resource_id text not null,display_order integer not null
		);
		create temporary table catalog_entities(id bigint primary key,public_id text not null);
		create temporary table game_resources(entity_id bigint primary key,canonical_id text not null);
		create temporary table mod_resource_version_details(
			resource_id bigint not null,status text not null,updated_at timestamptz not null,
			version_id bigint,icon_small_file_id bigint,icon_file_id bigint
		);
		create temporary table mod_content_versions(id bigint primary key,public_id text not null);
		create temporary table catalog_import_revisions(id text primary key,target_version_id bigint);
		create temporary table resource_import_snapshots(
			resource_id bigint not null,revision_id text not null,icon_path text not null,
			names jsonb not null,created_at timestamptz not null
		);
		create temporary table content_localizations(catalog_entity_id bigint not null,locale text not null,name text not null);
		insert into catalog_entities values(11,'r00000011');
		insert into game_resources values(11,'minecraft:stone');
		insert into catalog_import_revisions values('arch025-revision',null);
		insert into resource_import_snapshots values(11,'arch025-revision','', '"corrupt-names"'::jsonb,now());
		insert into community_post_resource_refs values(1,7,11,'block','minecraft:stone',0);

		create temporary table ai_tasks(
			task_uid text primary key,task_type text not null,status text not null,error text not null default '',
			created_by bigint,payload jsonb not null
		);
		create temporary table community_post_translations(
			post_id bigint not null,locale text not null,title text not null,body_markdown text not null,
			source_revision_id bigint not null
		);
		insert into ai_tasks values
			('arch025good','content_translation_completion','completed','',42,'{"postInternalId":7,"targetLocale":"zh-CN","sourceRevisionId":9}'::jsonb),
			('arch025bad','content_translation_completion','completed','',42,'"corrupt-payload"'::jsonb),
			('arch025missing','content_translation_completion','completed','',42,'{"postInternalId":8,"targetLocale":"zh-CN","sourceRevisionId":9}'::jsonb),
			('arch025other','content_translation_completion','completed','',77,'{"postInternalId":7,"targetLocale":"zh-CN","sourceRevisionId":9}'::jsonb);
		insert into community_post_translations values(7,'zh-CN','标题','正文',9);
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	if _, _, err = server.communityPostReferencesBatch(ctx, []int64{7}, security.Claims{Subject: 42}); err == nil || !strings.Contains(err.Error(), "decode community resource names") {
		t.Errorf("corrupt reference names error=%v", err)
	}

	for _, test := range []struct {
		name, taskID string
		want         int
		translation  bool
	}{
		{"healthy completed result", "arch025good", http.StatusOK, true},
		{"corrupt completed payload", "arch025bad", http.StatusInternalServerError, false},
		{"completed result missing", "arch025missing", http.StatusInternalServerError, false},
		{"wrong owner", "arch025other", http.StatusNotFound, false},
		{"missing task", "arch025absent", http.StatusNotFound, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, body := invokeCommunityTranslationResult(server, ctx, test.taskID)
			if status != test.want {
				t.Fatalf("status=%d body=%s; want %d", status, body, test.want)
			}
			if test.translation {
				var envelope struct {
					Data map[string]any `json:"data"`
				}
				if err := json.Unmarshal([]byte(body), &envelope); err != nil {
					t.Fatal(err)
				}
				if _, ok := envelope.Data["translation"]; !ok {
					t.Fatalf("completed task omitted translation: %s", body)
				}
			}
		})
	}

	if _, err = pool.Exec(ctx, `alter table ai_tasks rename column task_type to arch025_broken_task_type`); err != nil {
		t.Fatal(err)
	}
	if status, body := invokeCommunityTranslationResult(server, ctx, "arch025good"); status != http.StatusInternalServerError {
		t.Fatalf("task database failure status=%d body=%s; want 500", status, body)
	}
}

func invokeCommunityTranslationResult(server *Server, ctx context.Context, taskID string) (int, string) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/community/translations/"+taskID, nil)
	request.SetPathValue("taskId", taskID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 42}))
	response := httptest.NewRecorder()
	server.communityPostTranslationResult(response, request)
	return response.Code, response.Body.String()
}
