package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func oct02AIDeliveryPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := oct02AITestPool(t)
	_, err := pool.Exec(context.Background(), `alter table ai_tasks add column priority integer default 0,
	 add column concurrency_key text,add column queued_at timestamptz default now();
	 alter table users add column username text default 'fixture';
	 create table nats_outbox(id bigint generated always as identity primary key,event_id text unique,event_type text,
	 subject text,aggregate_type text,aggregate_id text,payload jsonb,status text,published_at timestamptz,
	 attempts integer default 10,last_error text default '',available_at timestamptz default now(),
	 locked_at timestamptz,locked_by text default '',updated_at timestamptz default now());
	 create table dead_letter_events(id bigint generated always as identity primary key,event_id text,failure_stage text,
	 replayed_at timestamptz);
	 insert into ai_tasks(task_uid,task_type) values('ai_delivery_fixture','i18n_translation_completion');
	 insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload,status)
	 values('delivery-event','ai.task.requested','ai','ai_task','ai_delivery_fixture','{"taskId":1}','dead');
	 insert into dead_letter_events(event_id,failure_stage) values('delivery-event','publish');`)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func oct02RetryAIDelivery(server *Server, claims security.Claims) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai/tasks/ai_delivery_fixture/retry-delivery", nil)
	request.SetPathValue("id", "ai_delivery_fixture")
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.retryAITaskDelivery(response, request)
	return response
}

func oct02AIDeliveryClaims() security.Claims {
	return security.Claims{Subject: 42, PermissionRules: []security.PermissionRule{
		{Code: "ai.read", Allow: true}, {Code: "ai.task.enqueue", Allow: true},
	}}
}

func TestOCT02AIDeliveryRetryIsAtomicAndConcurrentIntegration(t *testing.T) {
	pool := oct02AIDeliveryPool(t)
	server := &Server{db: pool}
	var group sync.WaitGroup
	statuses := make(chan int, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			statuses <- oct02RetryAIDelivery(server, oct02AIDeliveryClaims()).Code
		}()
	}
	group.Wait()
	close(statuses)
	accepted, conflicts := 0, 0
	for status := range statuses {
		switch status {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("delivery retry status=%d", status)
		}
	}
	var eventStatus string
	var attempts, events, replayed, audit, providerRequests int
	if err := pool.QueryRow(context.Background(), `select status,attempts,(select count(*) from nats_outbox),
	 (select count(*) from dead_letter_events where replayed_at is not null),
	 (select count(*) from app_logs where action='ai_delivery_retried'),
	 (select count(*) from ai_task_logs where event='provider_request_usage') from nats_outbox`).
		Scan(&eventStatus, &attempts, &events, &replayed, &audit, &providerRequests); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || conflicts != 7 || eventStatus != "pending" || attempts != 0 || events != 1 ||
		replayed != 1 || audit != 1 || providerRequests != 0 {
		t.Fatalf("retry atomicity: accepted/conflicts=%d/%d status=%s attempts=%d events=%d replayed=%d audit=%d usage=%d",
			accepted, conflicts, eventStatus, attempts, events, replayed, audit, providerRequests)
	}
}

func TestOCT02AIDeliveryRetryNeverResendsStartedOrAccountedTasksIntegration(t *testing.T) {
	for name, mutation := range map[string]string{
		"running":       `update ai_tasks set status='running',started_at=now()`,
		"failed":        `update ai_tasks set status='failed'`,
		"retrying":      `update ai_tasks set status='retrying'`,
		"started":       `update ai_tasks set started_at=now()`,
		"started_log":   `insert into ai_task_logs(task_id,event) values(1,'task_started')`,
		"accounted":     `insert into ai_task_logs(event,payload) values('provider_request_usage','{"taskId":1,"state":"usage_unknown"}')`,
		"known_usage":   `update ai_tasks set input_tokens=10`,
		"published":     `update nats_outbox set published_at=now()`,
		"pending":       `update nats_outbox set status='pending'`,
		"already_retry": `update dead_letter_events set replayed_at=now()`,
		"newer_event":   `insert into nats_outbox(event_id,aggregate_type,aggregate_id,status) values('newer','ai_task','ai_delivery_fixture','pending')`,
	} {
		t.Run(name, func(t *testing.T) {
			pool := oct02AIDeliveryPool(t)
			if _, err := pool.Exec(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}
			response := oct02RetryAIDelivery(&Server{db: pool}, oct02AIDeliveryClaims())
			if response.Code != http.StatusConflict {
				t.Fatalf("unsafe delivery was retried: status=%d body=%s", response.Code, response.Body.String())
			}
			var audit int
			if err := pool.QueryRow(context.Background(), `select count(*) from app_logs`).Scan(&audit); err != nil || audit != 0 {
				t.Fatalf("refusal wrote recovery audit: count=%d error=%v", audit, err)
			}
		})
	}
}

func TestOCT02AIDeliveryRetryRequiresBothExistingPermissionsIntegration(t *testing.T) {
	pool := oct02AIDeliveryPool(t)
	for _, claims := range []security.Claims{
		{},
		{Subject: 42, PermissionRules: []security.PermissionRule{{Code: "ai.read", Allow: true}}},
		{Subject: 42, PermissionRules: []security.PermissionRule{{Code: "ai.task.enqueue", Allow: true}}},
		{Subject: 42, PermissionRules: []security.PermissionRule{{Code: "ai.*", Allow: true}, {Code: "ai.task.enqueue", Allow: false, Priority: 10}}},
	} {
		response := oct02RetryAIDelivery(&Server{db: pool}, claims)
		if response.Code != http.StatusForbidden {
			t.Fatalf("unauthorized retry status=%d body=%s", response.Code, response.Body.String())
		}
	}
	var status string
	if err := pool.QueryRow(context.Background(), `select status from nats_outbox`).Scan(&status); err != nil || status != "dead" {
		t.Fatalf("unauthorized retry changed outbox=%s error=%v", status, err)
	}
}

func TestOCT02AIDeliveryRetryAuditFailureRollsBackIntegration(t *testing.T) {
	pool := oct02AIDeliveryPool(t)
	if _, err := pool.Exec(context.Background(), `alter table app_logs add constraint refuse_delivery_retry_audit
	 check(action<>'ai_delivery_retried')`); err != nil {
		t.Fatal(err)
	}
	response := oct02RetryAIDelivery(&Server{db: pool}, oct02AIDeliveryClaims())
	var status string
	var attempts int
	var unreplayed bool
	if err := pool.QueryRow(context.Background(), `select status,attempts,
	 (select replayed_at is null from dead_letter_events) from nats_outbox`).Scan(&status, &attempts, &unreplayed); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusInternalServerError || status != "dead" || attempts != 10 || !unreplayed {
		t.Fatalf("audit failure escaped transaction: status=%d outbox=%s attempts=%d unreplayed=%t",
			response.Code, status, attempts, unreplayed)
	}
}

func TestOCT02AIDeadPublisherIsVisibleIntegration(t *testing.T) {
	pool := oct02AIDeliveryPool(t)
	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai/tasks/ai_delivery_fixture", nil)
	request.SetPathValue("id", "ai_delivery_fixture")
	response := httptest.NewRecorder()
	server.adminAITask(response, request)
	var envelope struct {
		Data struct {
			Failure *struct {
				DeadLetterID string `json:"deadLetterId"`
				Stage        string `json:"stage"`
				Retryable    bool   `json:"retryable"`
			} `json:"delivery_failure"`
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || envelope.Data.Failure == nil || envelope.Data.Failure.DeadLetterID != "1" ||
		envelope.Data.Failure.Stage != "publish" || !envelope.Data.Failure.Retryable {
		t.Fatalf("publisher-dead recovery is invisible: status=%d response=%s", response.Code, response.Body.String())
	}
	listResponse := httptest.NewRecorder()
	server.adminAITasks(listResponse, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai/tasks?status=queued", nil))
	var list struct {
		Data []map[string]any
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if listResponse.Code != http.StatusOK || len(list.Data) != 1 {
		t.Fatalf("task list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	failure, ok := list.Data[0]["delivery_failure"].(map[string]any)
	if !ok || len(failure) != 3 || failure["stage"] != "publish" || failure["retryable"] != true || failure["deadLetterId"] != "1" {
		t.Fatalf("task list did not expose bounded publisher recovery metadata: %#v", failure)
	}
}
