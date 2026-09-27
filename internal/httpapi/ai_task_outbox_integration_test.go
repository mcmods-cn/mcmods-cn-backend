package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestContentTranslationUsesSharedTransactionalAITaskOutbox(t *testing.T) {
	contentSource, err := os.ReadFile("content_localization_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(contentSource)
	start := strings.Index(content, "func (s *Server) enqueueCatalogContentTranslation(")
	end := strings.Index(content, "func contentTranslationConcurrencyKey(")
	if start < 0 || end <= start {
		t.Fatal("content translation enqueue boundary was not found")
	}
	enqueue := content[start:end]
	for _, forbidden := range []string{"PublishTask", "publishContentTranslationTask"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("content translation retains direct task path %q", forbidden)
		}
	}
	if outbox := strings.Index(enqueue, "enqueueAITaskTx(ctx, tx"); outbox < 0 || outbox > strings.LastIndex(enqueue, "tx.Commit(ctx)") {
		t.Fatal("content task and outbox are not committed by the same transaction")
	}

	aiSource, err := os.ReadFile("ai_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	ai := string(aiSource)
	start = strings.Index(ai, "func (s *Server) createAITask(")
	end = strings.Index(ai, "func (s *Server) adminAIStats(")
	if start < 0 || end <= start {
		t.Fatal("general AI task boundary was not found")
	}
	create := ai[start:end]
	for _, forbidden := range []string{"OutboxEnabled", "PublishTask"} {
		if strings.Contains(create, forbidden) {
			t.Fatalf("general AI task retains alternate queue path %q", forbidden)
		}
	}
	if !strings.Contains(create, "enqueueAITaskTx(") {
		t.Fatal("general AI task does not use the shared transactional helper")
	}
	communitySource, err := os.ReadFile("community_post_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	community := string(communitySource)
	start = strings.Index(community, "func (s *Server) enqueueCommunityPostTranslation(")
	end = strings.Index(community, "func (s *Server) communityPostTranslationResult(")
	if start < 0 || end <= start {
		t.Fatal("community translation enqueue boundary was not found")
	}
	communityEnqueue := community[start:end]
	if strings.Contains(community, "publishContentTranslationTask") || strings.Contains(communityEnqueue, "PublishTask") {
		t.Fatal("community translation retains the direct content task publisher")
	}
	if outbox := strings.Index(communityEnqueue, "enqueueAITaskTx(ctx, tx"); outbox < 0 || outbox > strings.LastIndex(communityEnqueue, "tx.Commit(ctx)") {
		t.Fatal("community task and outbox are not committed by the same transaction")
	}
}

func TestNotificationTranslationUsesSharedTransactionalAITaskOutbox(t *testing.T) {
	source, err := os.ReadFile("notification_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func (s *Server) translateNotification(")
	end := strings.Index(text, "func (s *Server) notificationTranslationResult(")
	if start < 0 || end <= start {
		t.Fatal("notification translation boundary was not found")
	}
	handler := text[start:end]
	for _, forbidden := range []string{"PublishTask", "NATS unavailable", "published to NATS"} {
		if strings.Contains(handler, forbidden) {
			t.Fatalf("notification translation retains direct queue semantic %q", forbidden)
		}
	}
	if outbox := strings.Index(handler, "enqueueAITaskTx("); outbox < 0 || outbox > strings.LastIndex(handler, "tx.Commit(") {
		t.Fatal("notification task and outbox are not committed by the same transaction")
	}
}

func TestAITaskAndOutboxCommitOrRollbackTogether(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `create temp table ai_tasks(
		id bigserial primary key,task_uid text not null unique,task_type text not null,status text not null default 'queued');
		create temp table nats_outbox(
		id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
		subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',
		payload jsonb not null,occurred_at timestamptz not null default now(),status text not null default 'pending',
		available_at timestamptz not null default now(),created_at timestamptz not null default now())`); err != nil {
		t.Fatal(err)
	}
	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var taskID int64
	if err = tx.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type) values('ai-content','content_translation') returning id`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	if err = enqueueAITaskTx(ctx, tx, "ai.content_translation.requested", taskID, "ai-content", "content_translation", "trace-content"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertAITaskOutboxState(t, ctx, connection, "ai-content", 1, "trace-content")
	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type) values('ai-notification','notification_translation') returning id`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	if err = enqueueAITaskTx(ctx, tx, "ai.notification_translation.requested", taskID, "ai-notification", "notification_translation", "trace-notification"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertAITaskOutboxState(t, ctx, connection, "ai-notification", 1, "trace-notification")

	if _, err = connection.Exec(ctx, `alter table nats_outbox add constraint reject_failed_ai check(aggregate_id<>'ai-fail')`); err != nil {
		t.Fatal(err)
	}
	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type) values('ai-fail','content_translation') returning id`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	if err = enqueueAITaskTx(ctx, tx, "ai.content_translation.requested", taskID, "ai-fail", "content_translation", ""); err == nil {
		t.Fatal("outbox constraint failure was not returned")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertAITaskOutboxState(t, ctx, connection, "ai-fail", 0, "")
}

func TestAITaskRecoveryRequeuesOrphansAndStaleRunsOnce(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `create temp table ai_tasks(
		id bigserial primary key,task_uid text not null unique,task_type text not null,status text not null default 'queued',
		started_at timestamptz,finished_at timestamptz,error text not null default '',queued_at timestamptz,
		updated_at timestamptz not null default now());
		create temp table nats_outbox(
		id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
		subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',
		payload jsonb not null,occurred_at timestamptz not null default now(),status text not null default 'pending',
		available_at timestamptz not null default now(),created_at timestamptz not null default now());
		insert into ai_tasks(task_uid,task_type,status,updated_at) values
			('ai-orphan','content_translation_completion','queued',now()),
			('ai-covered','content_translation_completion','queued',now()),
			('ai-stale','content_translation_completion','running',now()-interval '20 minutes'),
			('ai-fresh','content_translation_completion','running',now()),
			('ai-retrying','content_translation_completion','retrying',now());
		insert into nats_outbox(event_id,event_type,subject,aggregate_type,aggregate_id,payload)
		values('covered-event','ai.task.requested','ai','ai_task','ai-covered','{}'),
		      ('stale-old-event','ai.task.requested','ai','ai_task','ai-stale','{}')`); err != nil {
		t.Fatal(err)
	}
	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := recoverAITaskOutboxTx(ctx, tx, 15*time.Minute, 100)
	if err != nil || recovered != 3 {
		t.Fatalf("AI recovery = %d/%v", recovered, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertAIRecoveryState(t, ctx, connection, "ai-orphan", "queued", 1, 1)
	assertAIRecoveryState(t, ctx, connection, "ai-retrying", "retrying", 1, 1)
	assertAIRecoveryState(t, ctx, connection, "ai-stale", "retrying", 2, 1)
	assertAIRecoveryState(t, ctx, connection, "ai-covered", "queued", 1, 0)
	assertAIRecoveryState(t, ctx, connection, "ai-fresh", "running", 0, 0)

	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err = recoverAITaskOutboxTx(ctx, tx, 15*time.Minute, 100)
	if err != nil || recovered != 0 {
		t.Fatalf("duplicate AI recovery = %d/%v", recovered, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var orphanID int64
	if err = tx.QueryRow(ctx, `select id from ai_tasks where task_uid='ai-orphan'`).Scan(&orphanID); err != nil {
		t.Fatal(err)
	}
	claimed, err := claimAITaskForExecution(ctx, tx, orphanID)
	if err != nil || !claimed {
		t.Fatalf("first AI claim = %v/%v", claimed, err)
	}
	claimed, err = claimAITaskForExecution(ctx, tx, orphanID)
	if err != nil || claimed {
		t.Fatalf("duplicate AI claim = %v/%v", claimed, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = connection.Exec(ctx, `insert into ai_tasks(task_uid,task_type,status) values('ai-recovery-fail','content_translation_completion','queued');
		alter table nats_outbox add constraint reject_ai_recovery check(aggregate_id<>'ai-recovery-fail')`); err != nil {
		t.Fatal(err)
	}
	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = recoverAITaskOutboxTx(ctx, tx, 15*time.Minute, 100); err == nil {
		t.Fatal("recovery outbox failure was not returned")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertAIRecoveryState(t, ctx, connection, "ai-recovery-fail", "queued", 0, 0)
}

func TestAITaskRecoveryPlanUsesBoundedIndexes(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `create temp table ai_tasks(
		id bigserial primary key,task_uid text not null unique,task_type text not null,status text not null,updated_at timestamptz not null);
		create index idx_ai_tasks_recovery on ai_tasks(status,updated_at,id) where status in ('queued','retrying','running');
		create temp table nats_outbox(id bigserial primary key,aggregate_type text not null,aggregate_id text not null);
		create index idx_nats_outbox_aggregate on nats_outbox(aggregate_type,aggregate_id,id);
		insert into ai_tasks(task_uid,task_type,status,updated_at)
		select 'ai-completed-'||value,'content_translation_completion','completed',now() from generate_series(1,100000) value;
		insert into ai_tasks(task_uid,task_type,status,updated_at)
		select 'ai-active-'||value,'content_translation_completion','queued',now() from generate_series(1,500) value;
		insert into nats_outbox(aggregate_type,aggregate_id)
		select 'other','unrelated-'||value from generate_series(1,100000) value;
		insert into nats_outbox(aggregate_type,aggregate_id)
		select 'ai_task','ai-active-'||value from generate_series(1,499) value;
		analyze ai_tasks; analyze nats_outbox`); err != nil {
		t.Fatal(err)
	}
	rows, err := connection.Query(ctx, `explain (analyze,buffers,format text)
		select task.id,task.task_uid,task.task_type,task.status from ai_tasks task
		where (task.status in ('queued','retrying') and not exists(
			select 1 from nats_outbox event where event.aggregate_type='ai_task' and event.aggregate_id=task.task_uid
		)) or (task.status='running' and task.updated_at<now()-interval '15 minutes')
		order by task.updated_at,task.id limit 100`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(strings.ToLower(line))
		plan.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{"idx_ai_tasks_recovery", "idx_nats_outbox_aggregate"} {
		if !strings.Contains(plan.String(), index) {
			t.Fatalf("recovery plan did not use %s:\n%s", index, plan.String())
		}
	}
}

func assertAIRecoveryState(t *testing.T, ctx context.Context, queryer modExportAttemptQueryer, taskUID, wantStatus string, wantEvents, wantRecovery int) {
	t.Helper()
	var status string
	var events, recovery int
	if err := queryer.QueryRow(ctx, `select status from ai_tasks where task_uid=$1`, taskUID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := queryer.QueryRow(ctx, `select count(*),count(*) filter(where event_type='ai.task.recovered')
		from nats_outbox where aggregate_type='ai_task' and aggregate_id=$1`, taskUID).Scan(&events, &recovery); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || events != wantEvents || recovery != wantRecovery {
		t.Fatalf("AI recovery %s = %s/%d/%d, want %s/%d/%d", taskUID, status, events, recovery, wantStatus, wantEvents, wantRecovery)
	}
}

func assertAITaskOutboxState(t *testing.T, ctx context.Context, queryer modExportAttemptQueryer, taskUID string, want int, traceID string) {
	t.Helper()
	var tasks, events int
	if err := queryer.QueryRow(ctx, `select count(*) from ai_tasks where task_uid=$1`, taskUID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := queryer.QueryRow(ctx, `select count(*) from nats_outbox where subject='ai' and aggregate_type='ai_task'
		and aggregate_id=$1 and trace_id=$2 and payload->>'taskUid'=$1`, taskUID, traceID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if tasks != want || events != want {
		t.Fatalf("AI task/outbox %s = %d/%d, want %d/%d", taskUID, tasks, events, want, want)
	}
}
