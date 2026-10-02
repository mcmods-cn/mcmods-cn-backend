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

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestCreatorProfileAndAuditedTeamMemberUpdatesAreIsolatedIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify isolated creator member updates against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	if _, err = pool.Exec(ctx, `
		create temporary table creators (like public.creators including all);
		create temporary table creator_links (like public.creator_links including all);
		create temporary table creator_role_definitions (like public.creator_role_definitions including all);
		create temporary table creator_team_members (like public.creator_team_members including all);
		create temporary table permission_audit_logs (like public.permission_audit_logs including all);
		create temporary table runtime_versions (like public.runtime_versions including all);
		insert into creators(id,public_id,kind,name,normalized_name,review_status) values
			(11,'team00001','team','Example Team','example team','approved'),
			(12,'author001','author','First Author','first author','approved'),
			(13,'author002','author','Second Author','second author','approved');
		insert into creator_role_definitions(id,public_id,code,name) values
			(21,'role00001','developer','Developer');
		insert into creator_links(creator_id,link_type,url,label,display_order)
			values(11,'website','https://old.example','Old',0);
		insert into creator_team_members(team_id,member_creator_id,role_id,title,status,created_by,approved_by,approved_at,display_order)
			values(11,12,21,'Original','approved',99,99,now(),0);
		insert into runtime_versions(name,version) values('project_acl',7)`); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = applyCreatorRelationsTx(ctx, tx, 11, creatorSnapshot{
		Kind:  "team",
		Links: []creatorLinkPayload{{Type: "website", URL: "https://new.example", Label: "New"}},
	}, "approved", false, 99); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertCreatorMemberIntegrationState(t, ctx, pool, "author001", "Original")
	var linkURL string
	if err = pool.QueryRow(ctx, `select url from creator_links where creator_id=11`).Scan(&linkURL); err != nil || linkURL != "https://new.example" {
		t.Fatalf("profile relation update did not replace links independently: url=%q err=%v", linkURL, err)
	}
	preflight, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = replaceCreatorTeamMembersTx(ctx, preflight, 11, []creatorMemberPayload{{
		CreatorID: "author002", RoleID: "role00001", Title: "Lead",
	}}, "approved", true, 99)
	_ = preflight.Rollback(ctx)
	if err != nil {
		t.Fatalf("member replacement preflight: %v", err)
	}

	server := &Server{db: pool, cache: querycache.New(config.RedisConfig{})}
	response := invokeCreatorMemberUpdate(t, ctx, server, `{"members":[{"creatorId":"author002","roleId":"role00001","title":"Lead"}]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("member update returned %d: %s", response.Code, response.Body.String())
	}
	assertCreatorMemberIntegrationState(t, ctx, pool, "author002", "Lead")
	var action string
	var payload []byte
	if err = pool.QueryRow(ctx, `select action,payload from permission_audit_logs order by id desc limit 1`).Scan(&action, &payload); err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Before []creatorTeamMemberAuditFact `json:"before"`
		After  []creatorTeamMemberAuditFact `json:"after"`
	}
	if err = json.Unmarshal(payload, &audit); err != nil {
		t.Fatal(err)
	}
	if action != "creator.team_members.replace" || len(audit.Before) != 1 || audit.Before[0].CreatorID != "author001" ||
		len(audit.After) != 2 || creatorTeamMemberAuditStatus(audit.After, "author001") != "revoked" ||
		creatorTeamMemberAuditStatus(audit.After, "author002") != "approved" {
		t.Fatalf("unexpected member audit action=%q payload=%s", action, payload)
	}

	if _, err = pool.Exec(ctx, `alter table permission_audit_logs add constraint force_creator_member_audit_failure
		check(action <> 'creator.team_members.replace') not valid`); err != nil {
		t.Fatal(err)
	}
	response = invokeCreatorMemberUpdate(t, ctx, server, `{"members":[{"creatorId":"author001","roleId":"role00001","title":"Should Roll Back"}]}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("forced audit failure returned %d: %s", response.Code, response.Body.String())
	}
	assertCreatorMemberIntegrationState(t, ctx, pool, "author002", "Lead")
}

func invokeCreatorMemberUpdate(t *testing.T, ctx context.Context, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	claims := security.Claims{Subject: 99, PermissionRules: []security.PermissionRule{{
		Code: "team.members.manage", Allow: true, Priority: 100,
	}}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/creators/team00001/members", strings.NewReader(body))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	request.SetPathValue("publicId", "team00001")
	response := httptest.NewRecorder()
	server.updateCreatorMembers(response, request)
	return response
}

func assertCreatorMemberIntegrationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wantCreatorID, wantTitle string) {
	t.Helper()
	var creatorID, title, status string
	if err := pool.QueryRow(ctx, `select member.public_id,relation.title,relation.status
		from creator_team_members relation join creators member on member.id=relation.member_creator_id
		where relation.team_id=11 and relation.status='approved'`).Scan(&creatorID, &title, &status); err != nil {
		t.Fatal(err)
	}
	if creatorID != wantCreatorID || title != wantTitle || status != "approved" {
		t.Fatalf("member state = (%q,%q,%q), want (%q,%q,approved)", creatorID, title, status, wantCreatorID, wantTitle)
	}
}

func creatorTeamMemberAuditStatus(facts []creatorTeamMemberAuditFact, creatorID string) string {
	for _, fact := range facts {
		if fact.CreatorID == creatorID {
			return fact.Status
		}
	}
	return ""
}
