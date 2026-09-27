package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestBUG036ProjectFollowCreationAndRemovalUseSeparateVisibilityBoundariesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify hidden project follow lifecycle")
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

	userIDs := make([]int64, 2)
	for index := range userIDs {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
			values($1,$2,'bug036',true) returning id`, fmt.Sprintf("bug036-%d-%d", time.Now().UnixNano(), index),
			fmt.Sprintf("bug036-%d-%d@example.invalid", time.Now().UnixNano(), index)).Scan(&userIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	ownerClaims := security.Claims{Subject: userIDs[0]}
	followerClaims := security.Claims{Subject: userIDs[1]}
	reviewerClaims := security.Claims{Subject: userIDs[1], PermissionRules: []security.PermissionRule{{Code: "project.review", Allow: true}}}

	var pendingInternalID, pendingRouteID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('b036m0001','bug036-pending','BUG036 private title','pending',$1) returning id`, userIDs[0]).Scan(&pendingInternalID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, pendingInternalID).Scan(&pendingRouteID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	for label, claims := range map[string]security.Claims{"submitter": ownerClaims, "reviewer": reviewerClaims} {
		response := invokeBUG036ProjectFollow(t, ctx, server.followProject, http.MethodPut, "b036m0001", claims)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s followed pending project status=%d body=%s, want 404", label, response.Code, response.Body.String())
		}
	}
	var pendingFollowCount int
	if err = pool.QueryRow(ctx, `select count(*) from project_follows where project_route_id=$1`, pendingRouteID).Scan(&pendingFollowCount); err != nil {
		t.Fatal(err)
	}
	if pendingFollowCount != 0 {
		t.Fatalf("pending project accepted %d follow relationships", pendingFollowCount)
	}

	var publicInternalID, publicRouteID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('b036m0002','bug036-public','BUG036 formerly public title','approved',$1) returning id`, userIDs[0]).Scan(&publicInternalID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, publicInternalID).Scan(&publicRouteID); err != nil {
		t.Fatal(err)
	}
	followResponse := invokeBUG036ProjectFollow(t, ctx, server.followProject, http.MethodPut, "b036m0002", followerClaims)
	if followResponse.Code != http.StatusOK {
		t.Fatalf("follow public project status=%d body=%s", followResponse.Code, followResponse.Body.String())
	}
	if _, err = pool.Exec(ctx, `update mods set review_status='pending' where id=$1`, publicInternalID); err != nil {
		t.Fatal(err)
	}

	statusResponse := invokeBUG036ProjectFollow(t, ctx, server.projectFollowStatus, http.MethodGet, "b036m0002", followerClaims)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("hidden followed project status lookup=%d body=%s, want safe placeholder", statusResponse.Code, statusResponse.Body.String())
	}
	var statusEnvelope struct {
		Data struct {
			Followed             bool `json:"followed"`
			NotificationsEnabled bool `json:"notificationsEnabled"`
			Target               struct {
				ID          string `json:"id"`
				Type        string `json:"type"`
				Name        string `json:"name"`
				URL         string `json:"url"`
				UpdatedAt   string `json:"updatedAt"`
				Unavailable bool   `json:"unavailable"`
			} `json:"target"`
		} `json:"data"`
	}
	if err = json.Unmarshal(statusResponse.Body.Bytes(), &statusEnvelope); err != nil {
		t.Fatal(err)
	}
	statusPayload := statusEnvelope.Data
	if !statusPayload.Followed || !statusPayload.NotificationsEnabled || statusPayload.Target.ID != "b036m0002" ||
		statusPayload.Target.Type != "mod" || !statusPayload.Target.Unavailable || statusPayload.Target.Name != "" ||
		statusPayload.Target.URL != "" || statusPayload.Target.UpdatedAt != "" || strings.Contains(statusResponse.Body.String(), "formerly public") {
		t.Fatalf("unsafe hidden follow status payload=%s", statusResponse.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/project-follows?limit=100", nil)
	listRequest = listRequest.WithContext(context.WithValue(ctx, claimsContextKey, followerClaims))
	listResponse := httptest.NewRecorder()
	server.myProjectFollows(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("hidden follow list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listEnvelope struct {
		Data struct {
			Items []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				URL         string `json:"url"`
				UpdatedAt   string `json:"updatedAt"`
				Unavailable bool   `json:"unavailable"`
			} `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(listResponse.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	listPayload := listEnvelope.Data
	if len(listPayload.Items) != 1 || listPayload.Items[0].ID != "b036m0002" || !listPayload.Items[0].Unavailable ||
		listPayload.Items[0].Name != "" || listPayload.Items[0].URL != "" || listPayload.Items[0].UpdatedAt != "" ||
		strings.Contains(listResponse.Body.String(), "formerly public") {
		t.Fatalf("unsafe hidden follow list payload=%s", listResponse.Body.String())
	}

	unfollowResponse := invokeBUG036ProjectFollow(t, ctx, server.unfollowProject, http.MethodDelete, "b036m0002", followerClaims)
	if unfollowResponse.Code != http.StatusNoContent {
		t.Fatalf("unfollow hidden project status=%d body=%s, want 204", unfollowResponse.Code, unfollowResponse.Body.String())
	}
	var remaining int
	if err = pool.QueryRow(ctx, `select count(*) from project_follows where user_id=$1 and project_route_id=$2`, userIDs[1], publicRouteID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("hidden project retained %d ghost follows after DELETE", remaining)
	}
}

func invokeBUG036ProjectFollow(t *testing.T, ctx context.Context, handler http.HandlerFunc, method, publicID string, claims security.Claims) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/projects/"+publicID+"/follow", nil)
	request.SetPathValue("publicId", publicID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}
