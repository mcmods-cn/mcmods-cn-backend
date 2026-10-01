package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

type applicationFixtureCore struct {
	pool         *pgxpool.Pool
	server       *Server
	userID       int64
	userPublicID string
	modID        int64
	modPublicID  string
	routeID      int64
}

func newApplicationFixtureCore(t *testing.T) *applicationFixtureCore {
	t.Helper()
	ctx, originalPool, cfg := isolatedAITestDatabase(t)
	if err := database.SeedRBAC(ctx, originalPool); err != nil {
		t.Fatal(err)
	}
	configuration := originalPool.Config().Copy()
	// Preserve the real schema, FKs and append-only audit triggers. The owned
	// database is dropped as a unit; it never needs to hard-delete audit actors.
	configuration.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := &applicationFixtureCore{pool: pool, server: &Server{db: pool, cfg: cfg}}
	key := "application-core-" + randomHex(8)
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'synthetic') returning id,public_id`, key, key+"@example.test").Scan(&f.userID, &f.userPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by,review_status) values(new_public_id(),$1,'Synthetic application project',$2,'approved') returning id`, key, f.userID).Scan(&f.modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id,public_id from public_routes where entity_type='mod' and internal_id=$1`, f.modID).Scan(&f.routeID, &f.modPublicID); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *applicationFixtureCore) request(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	t.Cleanup(cancel)
	return r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: f.userID}))
}

func (f *applicationFixtureCore) creator(t *testing.T) string {
	t.Helper()
	var id int64
	var publicID string
	name := "Synthetic author " + randomHex(8)
	if err := f.pool.QueryRow(context.Background(), `insert into creators(kind,name,normalized_name,review_status,created_by) values('author',$1,$2,'approved',$3) returning id,public_id`, name, strings.ToLower(name), f.userID).Scan(&id, &publicID); err != nil {
		t.Fatal(err)
	}
	return publicID
}

func TestCreatorClaimProofBoundariesCoreIntegration(t *testing.T) {
	f := newApplicationFixtureCore(t)
	for _, testcase := range []struct {
		name, proof string
		files       []string
		status      int
	}{
		{"empty", "   ", nil, http.StatusBadRequest},
		{"arbitrary attachment", "", []string{"not-a-file"}, http.StatusBadRequest},
		{"too long", strings.Repeat("x", 10001), nil, http.StatusBadRequest},
		{"unicode proof", strings.Repeat("证", 10000), nil, http.StatusCreated},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			id := f.creator(t)
			body, err := json.Marshal(map[string]any{"proofMarkdown": testcase.proof, "proofFileIds": testcase.files})
			if err != nil {
				t.Fatal(err)
			}
			r := f.request(t, http.MethodPost, "/claims", string(body))
			r.SetPathValue("publicId", id)
			response := httptest.NewRecorder()
			f.server.claimCreator(response, r)
			if response.Code != testcase.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, testcase.status, response.Body.String())
			}
			var count int
			if err = f.pool.QueryRow(context.Background(), `select count(*) from creator_claims claim join creators creator on creator.id=claim.creator_id where creator.public_id=$1`, id).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if testcase.status == http.StatusBadRequest && count != 0 {
				t.Fatalf("invalid proof persisted %d claims", count)
			}
		})
	}
}

func TestProjectEditorPaginationCoreIntegration(t *testing.T) {
	f := newApplicationFixtureCore(t)
	if _, err := f.pool.Exec(context.Background(), `insert into project_editor_applications(target_route_id,user_id,proof_markdown,status) select $1,$2,'Synthetic proof','rejected' from generate_series(1,5)`, f.routeID, f.userID); err != nil {
		t.Fatal(err)
	}
	r := f.request(t, http.MethodGet, "/applications?limit=2&offset=2", "")
	r.SetPathValue("projectType", "mod")
	r.SetPathValue("projectId", f.modPublicID)
	response := httptest.NewRecorder()
	f.server.projectEditorApplications(response, r)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var reply struct {
		Data struct {
			Items                []projectEditorApplicationResponse
			Total, Limit, Offset int
			HasMore              bool
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Data.Items) != 2 || reply.Data.Total != 5 || reply.Data.Limit != 2 || reply.Data.Offset != 2 || !reply.Data.HasMore {
		t.Fatalf("unexpected bounded page: %#v", reply.Data)
	}
	for _, item := range reply.Data.Items {
		if item.TargetName != "Synthetic application project" {
			t.Fatalf("project name was not resolved: %#v", item)
		}
	}
	// A different user can query this published project, but cannot obtain the
	// applicant's proof through the self-service history endpoint.
	r = f.request(t, http.MethodGet, "/applications?limit=2", "")
	r.SetPathValue("projectType", "mod")
	r.SetPathValue("projectId", f.modPublicID)
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, security.Claims{Subject: 0}))
	response = httptest.NewRecorder()
	f.server.projectEditorApplications(response, r)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing identity status=%d body=%s", response.Code, response.Body.String())
	}
	r = f.request(t, http.MethodGet, "/applications?limit=2", "")
	r.SetPathValue("projectType", "mod")
	r.SetPathValue("projectId", f.modPublicID)
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, security.Claims{Subject: 9223372036854775806}))
	response = httptest.NewRecorder()
	f.server.projectEditorApplications(response, r)
	if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || reply.Data.Total != 0 || len(reply.Data.Items) != 0 {
		t.Fatalf("other identity received applicant proof: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCreatorOwnedAttachmentProofCoreIntegration(t *testing.T) {
	f := newApplicationFixtureCore(t)
	for _, owned := range []bool{true, false} {
		t.Run(fmt.Sprintf("owned=%t", owned), func(t *testing.T) {
			var id int64
			var publicID string
			uploaderID := f.userID
			if !owned {
				key := "synthetic-other-" + randomHex(8)
				if err := f.pool.QueryRow(context.Background(), `insert into users(username,email,password_hash) values($1,$2,'synthetic') returning id`, key, key+"@example.test").Scan(&uploaderID); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.pool.QueryRow(context.Background(), `insert into oss_files(object_key,original_name,size_bytes,uploader_id,scan_status) values($1,'proof.txt',16,nullif($2,0),'clean') returning id,public_id`, "synthetic/application-core/"+randomHex(8), uploaderID).Scan(&id, &publicID); err != nil {
				t.Fatal(err)
			}
			r := f.request(t, http.MethodPost, "/claims", fmt.Sprintf(`{"proofMarkdown":"","proofFileIds":[%q]}`, publicID))
			r.SetPathValue("publicId", f.creator(t))
			response := httptest.NewRecorder()
			f.server.claimCreator(response, r)
			expected := http.StatusBadRequest
			if owned {
				expected = http.StatusCreated
			}
			if response.Code != expected {
				t.Fatalf("owned=%t status=%d body=%s", owned, response.Code, response.Body.String())
			}
		})
	}
}

func TestEditorPermissionLifecycleAndNotificationLocaleCoreIntegration(t *testing.T) {
	f := newApplicationFixtureCore(t)
	ctx := context.Background()
	var id string
	if err := f.pool.QueryRow(ctx, `insert into project_editor_applications(target_route_id,user_id,proof_markdown) values($1,$2,'Synthetic proof') returning public_id`, f.routeID, f.userID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	r := f.request(t, http.MethodPatch, "/application", `{"status":"approved"}`)
	r.SetPathValue("id", id)
	response := httptest.NewRecorder()
	f.server.reviewProjectEditorApplication(response, r)
	if response.Code != http.StatusOK {
		t.Fatalf("approval status=%d body=%s", response.Code, response.Body.String())
	}
	f.deliverNotification(t, "Project editor application")
	var locale string
	if err := f.pool.QueryRow(ctx, `select source_locale from notifications where recipient_id=$1 and title='Project editor application' order by id desc limit 1`, f.userID).Scan(&locale); err != nil {
		t.Fatal(err)
	}
	if locale != "en-US" {
		t.Fatalf("English notification recorded source locale %q", locale)
	}
	var access int
	if err := f.pool.QueryRow(ctx, `select count(*) from effective_project_access where user_id=$1 and project_type='mod' and project_id=$2 and source_type='editor_assignment'`, f.userID, f.modID).Scan(&access); err != nil {
		t.Fatal(err)
	}
	if access != 1 {
		t.Fatalf("approved editor access count=%d", access)
	}
	r = f.request(t, http.MethodPost, "/assignment/revoke", `{"reason":"Synthetic revocation"}`)
	r.SetPathValue("projectType", "mod")
	r.SetPathValue("projectId", f.modPublicID)
	r.SetPathValue("userId", f.userPublicID)
	response = httptest.NewRecorder()
	f.server.revokeProjectEditorAssignment(response, r)
	if response.Code != http.StatusOK {
		t.Fatalf("revocation status=%d body=%s", response.Code, response.Body.String())
	}
	if err := f.pool.QueryRow(ctx, `select count(*) from effective_project_access where user_id=$1 and project_type='mod' and project_id=$2 and source_type='editor_assignment'`, f.userID, f.modID).Scan(&access); err != nil {
		t.Fatal(err)
	}
	if access != 0 {
		t.Fatalf("revoked editor access remains: %d", access)
	}
	f.server.enqueueOrCreateDirectNotification(ctx, f.userID, f.userID, "comment_watch_reply", "评论收到回复", "合成评论", nil)
	f.deliverNotification(t, "评论收到回复")
	if err := f.pool.QueryRow(ctx, `select source_locale from notifications where recipient_id=$1 and title='评论收到回复' order by id desc limit 1`, f.userID).Scan(&locale); err != nil {
		t.Fatal(err)
	}
	if locale != "zh-CN" {
		t.Fatalf("Chinese comment source locale changed to %q", locale)
	}
}

// Deliver the persisted intent through the real worker without an external
// broker or email provider; broker redelivery is tested by the queue suite.
func (f *applicationFixtureCore) deliverNotification(t *testing.T, title string) {
	t.Helper()
	if !f.server.cfg.NATS.OutboxEnabled {
		return
	}
	var raw []byte
	if err := f.pool.QueryRow(context.Background(), `select payload from nats_outbox where subject='notifications' and payload->>'title'=$1 and (payload->>'recipientId')::bigint=$2 order by id desc limit 1`, title, f.userID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	worker := &NotificationWorker{db: f.pool}
	if err := worker.handleEvent(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
}

func TestCreatorClaimPaginationCoreIntegration(t *testing.T) {
	f := newApplicationFixtureCore(t)
	for index := 0; index < 3; index++ {
		id := f.creator(t)
		if _, err := f.pool.Exec(context.Background(), `insert into creator_claims(creator_id,user_id,proof_markdown,status,created_at) select id,$2,'Synthetic proof','pending','1970-01-01' from creators where public_id=$1`, id, f.userID); err != nil {
			t.Fatal(err)
		}
	}
	r := f.request(t, http.MethodGet, "/admin/creator-claims?limit=1&offset=1", "")
	response := httptest.NewRecorder()
	f.server.adminCreatorClaims(response, r)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var reply struct {
		Data struct {
			Items                []map[string]any
			Limit, Offset, Total int
			HasMore              bool
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Data.Items) != 1 || reply.Data.Limit != 1 || reply.Data.Offset != 1 || reply.Data.Total < 3 || !reply.Data.HasMore {
		t.Fatalf("unexpected bounded claim page: %#v", reply.Data)
	}
}

func TestEditorAuditFailureRollsBackCoreIntegration(t *testing.T) {
	f := newApplicationFixtureCore(t)
	ctx := context.Background()
	var id string
	if err := f.pool.QueryRow(ctx, `insert into project_editor_applications(target_route_id,user_id,proof_markdown) values($1,$2,'Synthetic proof') returning public_id`, f.routeID, f.userID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	server := f.failingAuditServer(t)
	r := f.request(t, http.MethodPatch, "/application", `{"status":"approved"}`)
	r.SetPathValue("id", id)
	response := httptest.NewRecorder()
	server.reviewProjectEditorApplication(response, r)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "failed to record editor permission grant") {
		t.Fatalf("audit failure status=%d body=%s", response.Code, response.Body.String())
	}
	var status string
	if err := f.pool.QueryRow(ctx, `select status from project_editor_applications where public_id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `select count(*) from project_editor_assignments where target_route_id=$1 and user_id=$2`, f.routeID, f.userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || count != 0 {
		t.Fatalf("failed audit changed permission state: status=%s assignments=%d", status, count)
	}
}

func (f *applicationFixtureCore) failingAuditServer(t *testing.T) *Server {
	t.Helper()
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `alter table permission_audit_logs add constraint core_reject_synthetic_audit check (false)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(ctx, `alter table permission_audit_logs drop constraint core_reject_synthetic_audit`); err != nil {
			t.Errorf("owned audit fault cleanup: %v", err)
		}
	})
	return f.server
}

func TestClaimAuditFailureRollsBackCoreIntegration(t *testing.T) {
	f := newApplicationFixtureCore(t)
	for _, initialStatus := range []string{"pending", "approved"} {
		t.Run(initialStatus, func(t *testing.T) {
			creatorID := f.creator(t)
			var claimID string
			if err := f.pool.QueryRow(context.Background(), `insert into creator_claims(creator_id,user_id,proof_markdown,status) select id,$2,'Synthetic proof',$3 from creators where public_id=$1 returning public_id`, creatorID, f.userID, initialStatus).Scan(&claimID); err != nil {
				t.Fatal(err)
			}
			server := f.failingAuditServer(t)
			r := f.request(t, http.MethodPatch, "/claim", `{"status":"approved"}`)
			r.SetPathValue("id", claimID)
			response := httptest.NewRecorder()
			if initialStatus == "pending" {
				server.reviewCreatorClaim(response, r)
			} else {
				r = f.request(t, http.MethodPost, "/claim/revoke", `{"reason":"Synthetic revocation"}`)
				r.SetPathValue("id", claimID)
				server.revokeCreatorClaim(response, r)
			}
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("audit failure status=%d body=%s", response.Code, response.Body.String())
			}
			var status string
			if err := f.pool.QueryRow(context.Background(), `select status from creator_claims where public_id=$1`, claimID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != initialStatus {
				t.Fatalf("failed audit persisted status %q instead of %q", status, initialStatus)
			}
		})
	}
}

func TestEditorRevokeAuditFailureRollsBackCoreIntegration(t *testing.T) {
	f := newApplicationFixtureCore(t)
	if _, err := f.pool.Exec(context.Background(), `insert into project_editor_assignments(target_route_id,user_id,granted_by) values($1,$2,$2)`, f.routeID, f.userID); err != nil {
		t.Fatal(err)
	}
	server := f.failingAuditServer(t)
	r := f.request(t, http.MethodPost, "/assignment/revoke", `{"reason":"Synthetic revocation"}`)
	r.SetPathValue("projectType", "mod")
	r.SetPathValue("projectId", f.modPublicID)
	r.SetPathValue("userId", f.userPublicID)
	response := httptest.NewRecorder()
	server.revokeProjectEditorAssignment(response, r)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "failed to record editor permission revocation") {
		t.Fatalf("audit failure status=%d body=%s", response.Code, response.Body.String())
	}
	var status string
	if err := f.pool.QueryRow(context.Background(), `select status from project_editor_assignments where target_route_id=$1 and user_id=$2`, f.routeID, f.userID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("failed audit persisted assignment status %q", status)
	}
}
