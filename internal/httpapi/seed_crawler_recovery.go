package httpapi

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

const seedRecoveryScope = "seed_crawler_recovery"

type seedRecoveryPayload struct {
	seedTranslationPayload
	SeedRunID      int64     `json:"seedRunId"`
	DraftID        int64     `json:"draftId"`
	DraftUID       string    `json:"draftUid"`
	DraftHash      string    `json:"draftHash"`
	DraftUpdatedAt time.Time `json:"draftUpdatedAt"`
}

// Recovery never creates a draft or a public resource. A stable binding to the
// actual saved draft prevents deletion/recreation from reviving old tasks.
func loadSeedRecoveryDraftTx(ctx context.Context, tx pgx.Tx, payload seedRecoveryPayload) ([]byte, time.Time, int64, string, error) {
	var enabled bool
	var budget int64
	if err := tx.QueryRow(ctx, `select enabled,ai_daily_token_budget from seed_crawler_configs where id for share`).Scan(&enabled, &budget); err != nil || !enabled || budget <= 0 {
		return nil, time.Time{}, 0, "", errors.New("seed recovery is disabled or unavailable")
	}
	var metadata, draft []byte
	var updated time.Time
	var actor int64
	var projectType string
	err := tx.QueryRow(ctx, `select j.result,d.payload,d.updated_at,d.user_id,c.project_type
 from seed_crawler_candidates c join seed_crawler_runs original on original.id=c.run_id
 join users actor on actor.id=original.actor_id and actor.status='active'
 join mod_metadata_import_jobs j on j.public_id=$3 and j.user_id=actor.id and j.project_type=c.project_type
 join user_drafts d on d.public_id=c.payload->>'draftPublicId' and d.user_id=actor.id
 where c.id=$1 and c.run_id=$2 and c.status='draft' and original.status in ('completed','failed','paused')
 and j.status='completed' and j.result->>'modrinthProjectId'=c.external_project_id
 and d.kind='seed_crawler_import' and d.draft_key='seed-crawler:'||c.external_project_id
 and d.project_key=c.project_type||':'||c.external_project_id and d.submitted_at is null and d.expires_at>clock_timestamp()
 and ($4::bigint=0 or d.id=$4) and ($5::text='' or d.public_id=$5)
 for share of c,original,actor,j for update of d`, payload.CandidateID, payload.SeedRunID, payload.MetadataJobID, payload.DraftID, payload.DraftUID).Scan(&metadata, &draft, &updated, &actor, &projectType)
	if err != nil || seedSourceHash(metadata) != payload.SourceHash {
		return nil, time.Time{}, 0, "", errors.New("seed recovery source or original draft is unavailable or changed")
	}
	var fields map[string]any
	if json.Unmarshal(draft, &fields) != nil {
		return nil, time.Time{}, 0, "", errors.New("seed recovery draft is invalid")
	}
	source := map[string]string{}
	for _, key := range []string{"primaryName", "summary", "bodyMarkdown"} {
		source[key], _ = fields[key].(string)
	}
	source["name"] = source["primaryName"]
	if value, exists := fields["localizations"]; exists {
		if _, ok := value.([]any); !ok {
			return nil, time.Time{}, 0, "", errors.New("seed recovery draft locales are invalid")
		}
	}
	if rows, ok := fields["localizations"].([]any); ok {
		for _, value := range rows {
			row, ok := value.(map[string]any)
			if !ok {
				continue
			}
			locale, _ := row["locale"].(string)
			if normalizeContentLocale(locale) == normalizeContentLocale(payload.TargetLocale) {
				return nil, time.Time{}, 0, "", errors.New("seed recovery target already exists and is protected")
			}
			if normalizeContentLocale(locale) == "en-US" {
				source["name"], _ = row["name"].(string)
				source["primaryName"] = source["name"]
				source["summary"], _ = row["summary"].(string)
				source["bodyMarkdown"], _ = row["bodyMarkdown"].(string)
				if body, ok := row["contentMarkdown"].(string); ok {
					source["bodyMarkdown"] = body
				}
			}
		}
	}
	for _, item := range payload.Items {
		if source[item["key"]] != item["text"] {
			return nil, time.Time{}, 0, "", errors.New("seed recovery draft source was edited")
		}
	}
	return draft, updated, actor, projectType, nil
}

func prepareSeedRecoveryTx(ctx context.Context, tx pgx.Tx, raw []byte, actor int64) ([]byte, error) {
	var payload seedRecoveryPayload
	if json.Unmarshal(raw, &payload) != nil || payload.Scope != "seed_crawler" && payload.Scope != seedRecoveryScope {
		return nil, errors.New("seed recovery task is invalid")
	}
	if payload.Scope == "seed_crawler" {
		payload.SeedRunID = payload.RunID
	}
	// Old installations did not store a draft UID. Bind only an existing
	// synthetic draft proven to have been created and last saved within this
	// completed run; a deleted/recreated or later edited row is not guessed.
	var bound bool
	if err := tx.QueryRow(ctx, `select payload ? 'draftPublicId' from seed_crawler_candidates where id=$1 for update`, payload.CandidateID).Scan(&bound); err != nil {
		return nil, err
	}
	if !bound && payload.Scope == "seed_crawler" {
		var uid string
		if err := tx.QueryRow(ctx, `select d.public_id from seed_crawler_candidates c
   join seed_crawler_runs r on r.id=c.run_id join user_drafts d on d.user_id=r.actor_id
   and d.draft_key='seed-crawler:'||c.external_project_id and d.project_key=c.project_type||':'||c.external_project_id
   where c.id=$1 and r.id=$2 and r.actor_id=$3 and r.status='completed'
   and r.started_at is not null and r.finished_at is not null
   and d.created_at>=r.started_at and d.created_at<=r.finished_at and d.updated_at<=r.finished_at
   and d.kind='seed_crawler_import' and d.submitted_at is null and d.expires_at>clock_timestamp()
   for update of d`, payload.CandidateID, payload.SeedRunID, actor).Scan(&uid); err != nil {
			return nil, errors.New("legacy seed draft binding cannot be verified")
		}
		if _, err := tx.Exec(ctx, `update seed_crawler_candidates set payload=payload||jsonb_build_object('draftPublicId',$2::text) where id=$1`, payload.CandidateID, uid); err != nil {
			return nil, err
		}
	}
	draft, updated, owner, _, err := loadSeedRecoveryDraftTx(ctx, tx, payload)
	if err != nil || owner != actor {
		return nil, errors.New("original seed draft/source is unavailable or protected")
	}
	if err = tx.QueryRow(ctx, `select id,public_id from user_drafts where user_id=$1 and draft_key=(select 'seed-crawler:'||external_project_id from seed_crawler_candidates where id=$2) and submitted_at is null`, actor, payload.CandidateID).Scan(&payload.DraftID, &payload.DraftUID); err != nil {
		return nil, err
	}
	var token [24]byte
	if _, err = cryptorand.Read(token[:]); err != nil {
		return nil, err
	}
	payload.Scope, payload.RunToken = seedRecoveryScope, hex.EncodeToString(token[:])
	payload.DraftHash, payload.DraftUpdatedAt = seedSourceHash(draft), updated
	if err = tx.QueryRow(ctx, `insert into seed_crawler_runs(status,dry_run,actor_id,lease_owner,stats) values('pending',true,$1,$2,jsonb_build_object('kind','translation_recovery','sourceRunId',$3::bigint)) returning id`, actor, payload.RunToken, payload.SeedRunID).Scan(&payload.RunID); err != nil {
		return nil, err
	}
	// Keep existing glossary/prompt/accounting metadata; replace only the new
	// explicit recovery identity and immutable draft snapshot.
	var merged map[string]any
	_ = json.Unmarshal(raw, &merged)
	encoded, _ := json.Marshal(payload)
	var recovery map[string]any
	_ = json.Unmarshal(encoded, &recovery)
	for key, value := range recovery {
		merged[key] = value
	}
	return json.Marshal(merged)
}

func validateSeedRecoveryTx(ctx context.Context, tx pgx.Tx, payload seedRecoveryPayload) ([]byte, string, error) {
	if payload.Scope != seedRecoveryScope || payload.DraftID <= 0 || payload.DraftUID == "" || payload.DraftHash == "" || payload.DraftUpdatedAt.IsZero() {
		return nil, "", errors.New("seed recovery identity is invalid")
	}
	if err := lockSeedCrawlerLeaseTx(withSeedCrawlerLease(ctx, payload.RunID, payload.RunToken), tx); err != nil {
		return nil, "", err
	}
	draft, updated, actor, projectType, err := loadSeedRecoveryDraftTx(ctx, tx, payload)
	if err != nil {
		return nil, "", err
	}
	var matches bool
	if err = tx.QueryRow(ctx, `select actor_id=$2 and stats->>'kind'='translation_recovery' and stats->>'sourceRunId'=$3 from seed_crawler_runs where id=$1`, payload.RunID, actor, fmt.Sprint(payload.SeedRunID)).Scan(&matches); err != nil || !matches {
		return nil, "", errors.New("seed recovery owner changed")
	}
	if !updated.Equal(payload.DraftUpdatedAt) || seedSourceHash(draft) != payload.DraftHash {
		return nil, "", errors.New("seed recovery draft was edited during the task")
	}
	return draft, projectType, nil
}

func (worker *AIWorker) persistSeedRecovery(ctx context.Context, taskID int64, raw []byte, result map[string]any) error {
	var payload seedRecoveryPayload
	if json.Unmarshal(raw, &payload) != nil {
		return errors.New("seed recovery payload is invalid")
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var running bool
	if err = tx.QueryRow(ctx, `select status='running' from ai_tasks where id=$1 for update`, taskID).Scan(&running); err != nil || !running {
		return errors.New("seed recovery task is no longer running")
	}
	draft, projectType, err := validateSeedRecoveryTx(ctx, tx, payload)
	if err != nil {
		return err
	}
	var fields map[string]any
	_ = json.Unmarshal(draft, &fields)
	generated := make(map[string]any, len(fields))
	for key, value := range fields {
		generated[key] = value
	}
	applySeedDraftTranslations(generated, map[string]any{payload.TargetLocale: translationItemsToMap(result)}, projectType)
	rows, _ := generated["localizations"].([]map[string]string)
	found := false
	var newLocalization map[string]string
	for _, row := range rows {
		if row["locale"] == payload.TargetLocale {
			found = true
			newLocalization = row
		}
	}
	if !found {
		return errors.New("seed recovery result exceeds editor limits")
	}
	updated, err := json.Marshal(newLocalization)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `update user_drafts set payload=jsonb_set(payload,'{localizations}',coalesce(payload->'localizations','[]'::jsonb)||jsonb_build_array($2::jsonb)),updated_at=clock_timestamp() where id=$1 and submitted_at is null and expires_at>clock_timestamp() and updated_at=$3`, payload.DraftID, updated, payload.DraftUpdatedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return errors.New("seed recovery draft changed before publication")
	}
	if _, err = tx.Exec(ctx, `insert into seed_crawler_translation_tasks(candidate_id,locale,status,input_tokens,output_tokens) select $2,$3,'completed',t.input_tokens,t.output_tokens from ai_tasks t where t.id=$1 on conflict(candidate_id,locale) do update set status='completed',input_tokens=excluded.input_tokens,output_tokens=excluded.output_tokens,last_error='',updated_at=now()`, taskID, payload.CandidateID, payload.TargetLocale); err != nil {
		return err
	}
	if err = completeAITaskTx(ctx, tx, taskID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update seed_crawler_runs set status='completed',lease_owner='',lease_expires_at=null,finished_at=now(),stats=stats||jsonb_build_object('draftUid',$2::text,'targetLocale',$3::text) where id=$1`, payload.RunID, payload.DraftUID, payload.TargetLocale); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Terminal cleanup never requeues a potentially billed recovery request.
func (worker *AIWorker) finishSeedRecoveryRun(ctx context.Context, taskID int64) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, cleanupErr := worker.db.Exec(cleanup, `update seed_crawler_runs r set status='failed',lease_owner='',lease_expires_at=null,finished_at=now(),last_error=left(t.error,1000) from ai_tasks t where t.id=$1 and t.status in ('failed','cancelled') and t.payload->>'scope'=$2 and r.id::text=t.payload->>'runId' and r.lease_owner=t.payload->>'runToken' and r.stats->>'kind'='translation_recovery'`, taskID, seedRecoveryScope)
	if cleanupErr != nil {
		log.Printf("AI seed recovery terminal cleanup deferred for task %d: %v", taskID, cleanupErr)
	}
}

func isSeedTaskScope(scope string) bool { return scope == "seed_crawler" || scope == seedRecoveryScope }
