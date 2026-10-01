package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (worker *AIWorker) preflightTranslation(ctx context.Context, taskType string, raw []byte) error {
	if err := validateAITranslationPayload(taskType, raw); err != nil {
		return err
	}
	if taskType != aiTaskContentTranslation && taskType != aiTaskNotificationTranslation {
		return nil
	}
	var scope struct {
		Scope          string `json:"scope"`
		PostID         int64  `json:"postInternalId"`
		SourceRevision int64  `json:"sourceRevisionId"`
	}
	if json.Unmarshal(raw, &scope) != nil {
		return errors.New("translation payload is invalid")
	}
	if taskType == aiTaskContentTranslation && scope.Scope == seedRecoveryScope {
		var payload seedRecoveryPayload
		if json.Unmarshal(raw, &payload) != nil {
			return errors.New("seed recovery payload is invalid")
		}
		tx, err := worker.db.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		_, _, err = validateSeedRecoveryTx(ctx, tx, payload)
		return err
	}
	if taskType == aiTaskContentTranslation && scope.Scope == "seed_crawler" {
		var payload seedTranslationPayload
		if json.Unmarshal(raw, &payload) != nil {
			return errors.New("seed translation payload is invalid")
		}
		tx, err := worker.db.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		return validateSeedTranslationSourceTx(ctx, tx, payload)
	}
	if taskType == aiTaskContentTranslation && scope.Scope == "community_post" {
		var exists bool
		if err := worker.db.QueryRow(ctx, `select exists(select 1 from community_posts where id=$1 and status='active' and review_status='approved' and published_revision_id=$2)`, scope.PostID, scope.SourceRevision).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("community post translation source is stale or unavailable")
		}
		return nil
	}
	if taskType == aiTaskNotificationTranslation {
		var snapshot struct {
			NotificationID  int64               `json:"notificationId"`
			SourceLocale    string              `json:"sourceLocale"`
			SourceUpdatedAt time.Time           `json:"sourceUpdatedAt"`
			Items           []map[string]string `json:"items"`
		}
		if json.Unmarshal(raw, &snapshot) != nil || snapshot.SourceUpdatedAt.IsZero() {
			return errors.New("notification translation source snapshot is invalid")
		}
		var title, body, locale string
		var updated time.Time
		if err := worker.db.QueryRow(ctx, `select title,body,source_locale,updated_at from notifications where id=$1 and kind<>'system'`, snapshot.NotificationID).Scan(&title, &body, &locale, &updated); err != nil {
			return errors.New("notification translation source is unavailable")
		}
		source := map[string]string{}
		for _, item := range snapshot.Items {
			source[item["key"]] = item["text"]
		}
		if !snapshot.SourceUpdatedAt.Equal(updated) || normalizeContentLocale(snapshot.SourceLocale) != normalizeContentLocale(locale) || source["title"] != title || source["body"] != body {
			return errors.New("notification translation source is stale")
		}
		return nil
	}
	var payload contentTranslationTaskPayload
	if json.Unmarshal(raw, &payload) != nil {
		return errors.New("translation payload is invalid")
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	entity, _, err := loadEditableContentSubjectTx(ctx, tx, payload.PublicID)
	if err != nil || entity.EntityID != payload.EntityID || entity.EntityType != payload.EntityType {
		return errors.New("content translation source is no longer available")
	}
	var revision int64
	if err = tx.QueryRow(ctx, `select revision_no from content_localizations where subject_type=$1 and subject_id=$2 and locale=$3 and review_status='approved'`, payload.EntityType, payload.EntityID, payload.SourceLocale).Scan(&revision); err != nil || revision != payload.SourceRevisionNo {
		return errors.New("content translation source is stale")
	}
	var targetRevision int64
	var provenance string
	err = tx.QueryRow(ctx, `select revision_no,provenance from content_localizations where subject_type=$1 and subject_id=$2 and locale=$3`, payload.EntityType, payload.EntityID, payload.TargetLocale).Scan(&targetRevision, &provenance)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if provenance == "human" || provenance == "human_corrected" || payload.TargetRevisionNo == nil && targetRevision != 0 || payload.TargetRevisionNo != nil && *payload.TargetRevisionNo != targetRevision {
		return errors.New("content translation target changed or is human protected")
	}
	return nil
}

func (worker *AIWorker) recoverTasks(ctx context.Context) {
	cfg := aiConfigFromDatabase(ctx, worker.db, worker.settingsEncryptionKey)
	for _, binding := range cfg.TaskModels {
		// A possibly billed crashed request is never retried automatically.
		_, _ = worker.db.Exec(ctx, `update ai_tasks set status='failed',error='AI worker lease expired; retry may incur another provider charge',finished_at=now(),updated_at=now()
   where status='running' and task_type=$1 and started_at<now()-($2::integer*interval '1 second')`, binding.TaskType, binding.TimeoutSeconds+60)
	}
	_, _ = worker.db.Exec(ctx, `update seed_crawler_runs r set status='failed',lease_owner='',lease_expires_at=null,finished_at=now(),last_error=left(t.error,1000) from ai_tasks t where t.status in ('failed','cancelled') and t.payload->>'scope'=$1 and r.id::text=t.payload->>'runId' and r.lease_owner=t.payload->>'runToken' and r.stats->>'kind'='translation_recovery'`, seedRecoveryScope)
	rows, err := worker.db.Query(ctx, `select id,task_uid,task_type from ai_tasks where status in ('queued','retrying') order by priority desc,created_at,id limit 100`)
	if err != nil {
		return
	}
	defer rows.Close()
	var pending []aiTaskMessage
	for rows.Next() {
		var msg aiTaskMessage
		if rows.Scan(&msg.TaskID, &msg.TaskUID, &msg.TaskType) != nil {
			return
		}
		pending = append(pending, msg)
	}
	if rows.Err() != nil {
		return
	}
	// Release the query connection before publishing to prevent pool starvation.
	rows.Close()
	for _, msg := range pending {
		if worker.queue == nil || worker.queue.PublishTask(ctx, "ai", msg) != nil {
			return
		}
	}
}

func (s *Server) cancelAITask(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("id"))
	tag, err := s.db.Exec(r.Context(), `update ai_tasks set status='cancelled',error='Cancelled by administrator; provider charges may still apply',finished_at=now(),updated_at=now()
  where task_uid=$1 and status in ('queued','running','retrying')`, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel AI task")
		return
	}
	if tag.RowsAffected() == 0 {
		writeAPIError(w, http.StatusConflict, "AI_TASK_INACTIVE", "AI task is not active", 0, nil)
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "cancel_ai_task", uid, currentClaims(r).Subject, r, http.StatusOK, 0, nil)
	writeJSON(w, http.StatusOK, map[string]any{"id": uid, "taskUid": uid, "status": "cancelled"})
}

func (s *Server) retryAITask(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("id"))
	var preliminaryType string
	var preliminaryPayload []byte
	if err := s.db.QueryRow(r.Context(), `select task_type,payload from ai_tasks where task_uid=$1`, uid).Scan(&preliminaryType, &preliminaryPayload); err != nil {
		writeError(w, http.StatusNotFound, "AI task does not exist")
		return
	}
	worker := NewAIWorker(s.db, s.queue, s.cfg.SettingsEncryptionKey)
	var preliminaryScope struct {
		Scope string `json:"scope"`
	}
	_ = json.Unmarshal(preliminaryPayload, &preliminaryScope)
	seedRecovery := preliminaryType == aiTaskContentTranslation && isSeedTaskScope(preliminaryScope.Scope)
	if !seedRecovery {
		if err := worker.preflightTranslation(r.Context(), preliminaryType, preliminaryPayload); err != nil {
			writeAPIError(w, http.StatusConflict, "AI_SOURCE_CHANGED", "translation source or target changed; create a fresh task", 0, nil)
			return
		}
	}
	// Permission resolution uses the existing uncached RBAC path before opening
	// the write transaction, so a small connection pool cannot starve itself.
	var preliminaryActor int64
	if err := s.db.QueryRow(r.Context(), `select coalesce(created_by,0) from ai_tasks where task_uid=$1`, uid).Scan(&preliminaryActor); err != nil {
		writeError(w, http.StatusNotFound, "AI task does not exist")
		return
	}
	var quotaMetadata struct {
		QuotaBacked bool `json:"quotaBacked"`
	}
	_ = json.Unmarshal(preliminaryPayload, &quotaMetadata)
	userQuotaRequired := preliminaryType == aiTaskNotificationTranslation || quotaMetadata.QuotaBacked || seedRecovery
	var userTokenLimit int64
	if userQuotaRequired {
		var active bool
		if err := s.db.QueryRow(r.Context(), `select status='active' from users where id=$1`, preliminaryActor).Scan(&active); err != nil || !active {
			writeAPIError(w, http.StatusConflict, "AI_ACTOR_UNAVAILABLE", "original AI task creator is no longer active", 0, nil)
			return
		}
		_, rules, err := s.resolveUserRootPermissionsUncached(r.Context(), preliminaryActor)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "original AI task creator permissions are unavailable")
			return
		}
		userTokenLimit = int64(permissionRulesNumericValue(rules, "user.ai.daily_token_limit"))
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry AI task")
		return
	}
	defer tx.Rollback(r.Context())
	var oldID, actor int64
	var taskType, provider, modelID, status string
	var raw []byte
	err = tx.QueryRow(r.Context(), `select id,task_type,provider,model,status,payload,coalesce(created_by,0) from ai_tasks where task_uid=$1 for update`, uid).Scan(&oldID, &taskType, &provider, &modelID, &status, &raw, &actor)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "AI task does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read AI task")
		return
	}
	if status != "failed" {
		writeAPIError(w, http.StatusConflict, "AI_TASK_NOT_FAILED", "only failed AI tasks may be retried", 0, nil)
		return
	}
	// One retry per failed task. The old request and its usage remain immutable
	// accounting facts; administrators explicitly authorize a new provider call.
	var exists bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from ai_tasks where payload->>'retryOf'=$1)`, uid).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect AI retry")
		return
	}
	if exists {
		writeAPIError(w, http.StatusConflict, "AI_RETRY_EXISTS", "this failed task already has a retry", 0, nil)
		return
	}
	cfg := aiConfigFromQuerier(r.Context(), tx, s.cfg.SettingsEncryptionKey)
	_, model, ok := resolveAIModel(cfg, provider+"/"+modelID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI model is unavailable")
		return
	}
	if seedRecovery {
		raw, err = prepareSeedRecoveryTx(r.Context(), tx, raw, actor)
		if err != nil {
			writeAPIError(w, http.StatusConflict, "AI_SOURCE_CHANGED", err.Error(), 0, nil)
			return
		}
	}
	reserved := estimatedAIReservation(raw, model)
	var scopeForAccounting struct {
		Scope string `json:"scope"`
	}
	_ = json.Unmarshal(raw, &scopeForAccounting)
	if isSeedTaskScope(scopeForAccounting.Scope) {
		if err = preserveLegacySeedAIAccountingTx(r.Context(), tx, model); err != nil {
			writeError(w, http.StatusServiceUnavailable, "seed legacy accounting could not be checked")
			return
		}
	}
	if err = reserveAISiteQuotaTx(r.Context(), tx, cfg, reserved); err != nil {
		writeAPIError(w, http.StatusTooManyRequests, "AI_QUOTA_EXCEEDED", "AI site quota is insufficient", 0, nil)
		return
	}
	var seedScope struct {
		Scope string `json:"scope"`
	}
	_ = json.Unmarshal(raw, &seedScope)
	if isSeedTaskScope(seedScope.Scope) {
		if err = reserveSeedCrawlerAIQuotaTx(r.Context(), tx, reserved); err != nil {
			writeAPIError(w, http.StatusTooManyRequests, "AI_QUOTA_EXCEEDED", "seed crawler daily quota is insufficient", 0, nil)
			return
		}
	}
	if userQuotaRequired {
		if actor != preliminaryActor {
			writeAPIError(w, http.StatusConflict, "AI_ACTOR_UNAVAILABLE", "original AI task creator changed", 0, nil)
			return
		}
		if err = reserveAITaskQuotaTx(r.Context(), tx, actor, userTokenLimit, reserved); err != nil {
			if errors.Is(err, errAIQuotaExceeded) {
				writeAPIError(w, http.StatusTooManyRequests, "AI_QUOTA_EXCEEDED", "original AI task creator daily quota is insufficient", 0, nil)
			} else {
				writeError(w, http.StatusServiceUnavailable, "original AI task creator quota could not be checked")
			}
			return
		}
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		writeError(w, http.StatusConflict, "AI task payload is invalid")
		return
	}
	payload["retryOf"] = uid
	delete(payload, "quotaReservedCostMicros")
	raw, _ = json.Marshal(payload)
	newUID := "ai_" + randomHex(16)
	var taskID int64
	createdBy := any(nil)
	if actor > 0 {
		createdBy = actor
	}
	if err = tx.QueryRow(r.Context(), `insert into ai_tasks(task_uid,task_type,provider,model,status,payload,created_by,queued_at,quota_reserved_tokens)
  values($1,$2,$3,$4,'queued',$5::jsonb,$6,now(),$7) returning id`, newUID, taskType, provider, modelID, string(raw), createdBy, reserved).Scan(&taskID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create AI retry")
		return
	}
	if err = s.enqueueAIOutboxTx(r.Context(), tx, taskID, newUID, taskType); err != nil {
		writeError(w, http.StatusServiceUnavailable, "AI retry could not reserve its budget or queue")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit AI retry")
		return
	}
	if !s.cfg.NATS.OutboxEnabled && (s.queue == nil || s.queue.PublishTask(r.Context(), "ai", aiTaskMessage{TaskID: taskID, TaskUID: newUID, TaskType: taskType}) != nil) {
		_, _ = s.db.Exec(r.Context(), `update ai_tasks set status='failed',error='NATS unavailable',finished_at=now() where id=$1`, taskID)
		writeError(w, http.StatusServiceUnavailable, "AI retry queue is unavailable")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "retry_ai_task", newUID, currentClaims(r).Subject, r, http.StatusCreated, 0, map[string]any{"retryOf": uid})
	writeJSON(w, http.StatusCreated, map[string]any{"id": newUID, "taskUid": newUID, "status": "queued", "retryOf": uid})
}

func (worker *AIWorker) claimTask(ctx context.Context, taskID int64) (bool, error) {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var taskType string
	if err = tx.QueryRow(ctx, `select task_type from ai_tasks where id=$1`, taskID).Scan(&taskType); err != nil {
		return false, err
	}
	cfg := aiConfigFromQuerier(ctx, tx, worker.settingsEncryptionKey)
	binding, ok := findAITaskModel(cfg.TaskModels, taskType)
	if !ok {
		return false, errors.New("AI task binding is unavailable")
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-ai-concurrency'),hashtext($1))`, taskType); err != nil {
		return false, err
	}
	var status string
	if err = tx.QueryRow(ctx, `select status from ai_tasks where id=$1`, taskID).Scan(&status); err != nil {
		return false, err
	}
	if status != "queued" && status != "retrying" {
		return false, nil
	}
	var running int
	if err = tx.QueryRow(ctx, `select count(*) from ai_tasks where status='running' and task_type=$1`, taskType).Scan(&running); err != nil {
		return false, err
	}
	if running >= binding.ConcurrencyLimit {
		return false, errors.New("AI task concurrency limit reached")
	}
	tag, err := tx.Exec(ctx, `update ai_tasks set status='running',started_at=now(),updated_at=now() where id=$1 and status in ('queued','retrying')`, taskID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		var raw []byte
		if err = tx.QueryRow(ctx, `select payload from ai_tasks where id=$1`, taskID).Scan(&raw); err != nil {
			return false, err
		}
		var recovery seedRecoveryPayload
		if json.Unmarshal(raw, &recovery) == nil && recovery.Scope == seedRecoveryScope {
			leaseTag, leaseErr := tx.Exec(ctx, `update seed_crawler_runs set status='running',attempts=1,started_at=now(),lease_expires_at=clock_timestamp()+interval '5 minutes' where id=$1 and status='pending' and lease_owner=$2 and stats->>'kind'='translation_recovery'`, recovery.RunID, recovery.RunToken)
			if leaseErr != nil || leaseTag.RowsAffected() != 1 {
				return false, errors.New("seed recovery lease cannot be claimed")
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
