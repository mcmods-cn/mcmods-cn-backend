package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestModpackReportLifecycleIntegration(t *testing.T) {
	if _, ok := reportTargetReasons["modpack"]; !ok {
		t.Fatal("modpack report reasons are not registered")
	}
	if projectType, ok := reportProjectAccessType("modpack"); !ok || projectType != "modpack" {
		t.Fatalf("modpack project access identity = %q/%t", projectType, ok)
	}

	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify the modpack report lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
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

	claims := governanceReviewTestActor(t, ctx, pool, "packreviewer", "report.create", "report.review", "report.action.delete")
	var modpackID, modpackPublicID string
	if err = pool.QueryRow(ctx, `insert into modpacks(slug,primary_name,summary,body_markdown,review_status,submitted_by)
		values('bug-113-pack','BUG-113 Pack','reportable modpack','modpack body','approved',$1)
		returning id::text,public_id`, claims.Subject).Scan(&modpackID, &modpackPublicID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/reports", bytes.NewBufferString(
		`{"targetType":"modpack","targetId":"`+modpackPublicID+`","reasonCode":"copyright_theft","detail":"copied pack"}`,
	))
	createRequest = createRequest.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	createResponse := httptest.NewRecorder()
	server.createUnifiedReport(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create modpack report status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}

	var reportPublicID string
	if err = pool.QueryRow(ctx, `select public_id from reports where target_type='modpack' and target_public_id=$1`, modpackPublicID).Scan(&reportPublicID); err != nil {
		t.Fatal(err)
	}
	claimResponse := invokeBUG113ReportAction(ctx, server, claims, reportPublicID, "claim", `{}`)
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("claim modpack report status=%d body=%s", claimResponse.Code, claimResponse.Body.String())
	}
	resolveResponse := invokeBUG113ReportAction(ctx, server, claims, reportPublicID, "resolve",
		`{"conclusion":"valid","note":"confirmed","deleteTarget":true,"idempotencyKey":"bug113-resolve"}`)
	if resolveResponse.Code != http.StatusOK {
		t.Fatalf("resolve modpack report status=%d body=%s", resolveResponse.Code, resolveResponse.Body.String())
	}

	var reportStatus, targetType, actorRole, snapshotTitle, snapshotRouteType, modpackStatus string
	var actions int
	if err = pool.QueryRow(ctx, `select report.status,report.target_type,report.target_actor_role,
		snapshot.payload->>'title',snapshot.payload->>'routeEntityType',pack.review_status,
		(select count(*) from moderation_actions action where action.report_id=report.id and action.target_type='modpack')
		from reports report join report_snapshots snapshot on snapshot.report_id=report.id
		join modpacks pack on pack.public_id=report.target_public_id where report.public_id=$1`, reportPublicID).
		Scan(&reportStatus, &targetType, &actorRole, &snapshotTitle, &snapshotRouteType, &modpackStatus, &actions); err != nil {
		t.Fatal(err)
	}
	if reportStatus != "resolved_valid" || targetType != "modpack" || actorRole != reportTargetActorSubmitter ||
		snapshotTitle != "BUG-113 Pack" || snapshotRouteType != "modpack" || modpackStatus != "rejected" || actions != 1 {
		t.Fatalf("modpack report facts status=%s type=%s actor=%s title=%q route=%s pack=%s actions=%d internal=%s",
			reportStatus, targetType, actorRole, snapshotTitle, snapshotRouteType, modpackStatus, actions, modpackID)
	}
}

func invokeBUG113ReportAction(ctx context.Context, server *Server, claims security.Claims, reportID, action, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports/"+reportID+"/"+action, bytes.NewBufferString(body))
	request.SetPathValue("id", reportID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	if action == "claim" {
		server.claimReport(response, request)
	} else {
		server.resolveUnifiedReport(response, request)
	}
	return response
}
