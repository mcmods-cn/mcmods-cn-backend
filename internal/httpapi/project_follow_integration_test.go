package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestProjectFollowPreservesNotificationSettingAndPrivateUnfollowIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	unique := "audit_follow_" + randomHex(8)
	var ownerID, viewerID, blueprintID int64
	for index, dest := range []*int64{&ownerID, &viewerID} {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'test') returning id`, unique+string(rune('a'+index)), unique+string(rune('a'+index))+"@example.test").Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `delete from blueprints where id=$1`, blueprintID); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(context.Background(), `delete from users where id=any($1::bigint[])`, []int64{ownerID, viewerID}); err != nil {
			t.Error(err)
		}
	})
	var publicID string
	if err = pool.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format,status,review_status)
		values($1,'synthetic follow fixture','json','ready','approved') returning id,public_id`, ownerID).Scan(&blueprintID, &publicID); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: pool}
	request := func(method string, actor int64) *http.Request {
		r := httptest.NewRequest(method, "/api/v1/projects/"+publicID+"/follow", nil)
		r.SetPathValue("publicId", publicID)
		return r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor}))
	}
	response := httptest.NewRecorder()
	s.followProject(response, request(http.MethodPost, viewerID))
	if response.Code != http.StatusOK {
		t.Fatalf("initial follow failed: status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = pool.Exec(ctx, `update project_follows set notifications_enabled=false where user_id=$1`, viewerID); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	s.followProject(response, request(http.MethodPost, viewerID))
	var result struct {
		Data struct{ Followed, NotificationsEnabled bool } `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != http.StatusOK || !result.Data.Followed || result.Data.NotificationsEnabled {
		t.Fatalf("idempotent follow misrepresented disabled notifications: status=%d body=%s err=%v", response.Code, response.Body.String(), err)
	}
	if _, err = pool.Exec(ctx, `update blueprints set review_status='pending' where id=$1`, blueprintID); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	s.projectFollowStatus(response, request(http.MethodGet, viewerID))
	if response.Code != http.StatusNotFound {
		t.Fatalf("private project remained visible: status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	s.unfollowProject(response, request(http.MethodDelete, viewerID))
	if response.Code != http.StatusNoContent {
		t.Fatalf("viewer could not remove own private follow: status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err = pool.QueryRow(ctx, `select count(*) from project_follows where user_id=$1`, viewerID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unfollow did not persist: count=%d err=%v", count, err)
	}
}
