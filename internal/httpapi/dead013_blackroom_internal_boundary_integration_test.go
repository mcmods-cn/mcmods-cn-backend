package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestDEAD013BlackroomPublicAndAdminResponsesAreSeparatedIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the blackroom visibility boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	if _, err = pool.Exec(ctx, `
		insert into users(id,public_id,username,email,password_hash) values
			(99165001,'udead013a','subject','dead013-subject@example.test','x'),
			(99165002,'udead013b','moderator','dead013-moderator@example.test','x'),
			(99165003,'udead013c','releaser','dead013-releaser@example.test','x');
		insert into ban_reasons(code,translations,sort_order) values
			('dead013','{"zh-CN":"DEAD013"}'::jsonb,99165);
		insert into ban_records(id,public_id,user_id,moderator_id,reason_code,custom_reason,
			public_record_markdown,internal_note,username_snapshot,status,revoked_at,revoked_by,revoke_reason)
		values(99165004,'bdead013a',99165001,99165002,'dead013','public reason',
			'public record','private moderation note','subject','revoked',now(),99165003,'private release reason')
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	publicResponse := httptest.NewRecorder()
	server.publicBlackroom(publicResponse, httptest.NewRequest("GET", "/api/v1/site-affairs/blackroom?limit=30", nil))
	if publicResponse.Code != 200 {
		t.Fatalf("public blackroom status=%d body=%s", publicResponse.Code, publicResponse.Body.String())
	}
	adminResponse := httptest.NewRecorder()
	server.adminBans(adminResponse, httptest.NewRequest("GET", "/api/v1/admin/bans?limit=30", nil))
	if adminResponse.Code != 200 {
		t.Fatalf("admin blackroom status=%d body=%s", adminResponse.Code, adminResponse.Body.String())
	}
	publicItem := firstBlackroomResponseItem(t, publicResponse.Body.Bytes())
	adminItem := firstBlackroomResponseItem(t, adminResponse.Body.Bytes())
	for _, field := range []string{"internalNote", "moderatorId", "moderatorName", "revokedById", "revokedByName", "revokeReason"} {
		if _, leaked := publicItem[field]; leaked {
			t.Fatalf("public blackroom leaked internal field %q: %v", field, publicItem[field])
		}
		if _, present := adminItem[field]; !present {
			t.Fatalf("admin blackroom omitted internal field %q", field)
		}
	}
	if adminItem["internalNote"] != "private moderation note" || adminItem["moderatorName"] != "moderator" ||
		adminItem["revokedByName"] != "releaser" || adminItem["revokeReason"] != "private release reason" {
		t.Fatalf("admin internal facts=%v", adminItem)
	}
	if strings.Contains(publicResponse.Body.String(), "private moderation note") || strings.Contains(publicResponse.Body.String(), "private release reason") {
		t.Fatalf("public blackroom body contains private moderation content: %s", publicResponse.Body.String())
	}
	detailResponse := httptest.NewRecorder()
	detailRequest := httptest.NewRequest("GET", "/api/v1/site-affairs/blackroom/bdead013a", nil)
	detailRequest.SetPathValue("id", "bdead013a")
	server.blackroomDetail(detailResponse, detailRequest)
	if detailResponse.Code != 200 || strings.Contains(detailResponse.Body.String(), "private moderation note") ||
		strings.Contains(detailResponse.Body.String(), "private release reason") {
		t.Fatalf("public blackroom detail status=%d leaked private content: %s", detailResponse.Code, detailResponse.Body.String())
	}
}

func firstBlackroomResponseItem(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var page struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &page); err != nil || len(page.Data.Items) != 1 {
		t.Fatalf("decode blackroom page items=%d err=%v body=%s", len(page.Data.Items), err, body)
	}
	return page.Data.Items[0]
}

func TestDEAD013BlackroomHandlersUseDistinctPublicAndAdminQueries(t *testing.T) {
	for _, contract := range []struct {
		file      string
		required  []string
		forbidden []string
	}{
		{
			file:      "governance_handlers.go",
			required:  []string{"queryPublicBlackroomPage", "queryAdminBlackroomPage"},
			forbidden: []string{"blackroomList(w http.ResponseWriter, r *http.Request, internal bool)"},
		},
		{
			file:      "governance_pagination.go",
			required:  []string{"ban.internal_note", "moderator.public_id", "ban.revoke_reason", "revoker.public_id"},
			forbidden: []string{"parseBlackroomPageRequest(values url.Values, internal bool)"},
		},
		{
			file:      "server.go",
			required:  []string{`GET /api/v1/admin/bans", s.requirePermission("ban.view_internal", s.adminBans)`},
			forbidden: nil,
		},
	} {
		raw, err := os.ReadFile(contract.file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		for _, required := range contract.required {
			if !strings.Contains(source, required) {
				t.Fatalf("%s is missing %q", contract.file, required)
			}
		}
		for _, forbidden := range contract.forbidden {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s retains %q", contract.file, forbidden)
			}
		}
	}
}
