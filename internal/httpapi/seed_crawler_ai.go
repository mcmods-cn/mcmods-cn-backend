package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type seedTranslationPayload struct {
	Scope         string              `json:"scope"`
	CandidateID   int64               `json:"candidateId"`
	MetadataJobID string              `json:"metadataJobId"`
	SourceHash    string              `json:"sourceHash"`
	RunID         int64               `json:"runId"`
	RunToken      string              `json:"runToken"`
	SourceLocale  string              `json:"sourceLocale"`
	TargetLocale  string              `json:"targetLocale"`
	Items         []map[string]string `json:"items"`
}

func seedCrawlerLeaseToken(ctx context.Context) string {
	lease, _ := ctx.Value(seedCrawlerLeaseContextKey{}).(seedCrawlerLeaseIdentity)
	return lease.Token
}

func (worker *SeedCrawlerWorker) execSeedWrite(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockSeedCrawlerLeaseTx(ctx, tx); err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return tag, err
	}
	return tag, tx.Commit(ctx)
}

func seedSourceHash(raw []byte) string {
	// Canonical JSON gives the same identity to a source supplied by the importer
	// and that source subsequently read back from PostgreSQL jsonb.
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func validateSeedTranslationSourceTx(ctx context.Context, tx pgx.Tx, payload seedTranslationPayload) error {
	if payload.Scope != "seed_crawler" || payload.RunID <= 0 || payload.RunToken == "" || payload.CandidateID <= 0 || payload.MetadataJobID == "" || payload.SourceHash == "" {
		return errors.New("seed translation identity is invalid")
	}
	leasedCtx := withSeedCrawlerLease(ctx, payload.RunID, payload.RunToken)
	if err := lockSeedCrawlerLeaseTx(leasedCtx, tx); err != nil {
		return err
	}
	var enabled bool
	var budget int64
	if err := tx.QueryRow(ctx, `select enabled,ai_daily_token_budget from seed_crawler_configs where id for share`).Scan(&enabled, &budget); err != nil {
		return err
	}
	if !enabled || budget <= 0 {
		return errors.New("seed crawler AI translation is disabled")
	}
	var raw []byte
	// Ownership joins make the metadata snapshot a real reference to this run's
	// actor and candidate, rather than trusting a caller-supplied scope string.
	err := tx.QueryRow(ctx, `select j.result from mod_metadata_import_jobs j
 join seed_crawler_runs r on r.id=$1 and r.actor_id=j.user_id
 join users actor on actor.id=r.actor_id and actor.status='active'
 join seed_crawler_candidates c on c.id=$2 and c.run_id=r.id and c.project_type=j.project_type
 where j.public_id=$3 and j.status='completed' and c.status not in ('existing','submitted')
 and j.result->>'modrinthProjectId'=c.external_project_id for share of j,c,actor`, payload.RunID, payload.CandidateID, payload.MetadataJobID).Scan(&raw)
	if err != nil {
		return errors.New("seed metadata source is unavailable")
	}
	if seedSourceHash(raw) != payload.SourceHash {
		return errors.New("seed metadata source changed")
	}
	return nil
}

func reserveSeedCrawlerAIQuotaTx(ctx context.Context, tx pgx.Tx, reserved int64) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-seed-ai-quota'))`); err != nil {
		return err
	}
	var enabled bool
	var limit, used int64
	if err := tx.QueryRow(ctx, `select enabled,ai_daily_token_budget from seed_crawler_configs where id`).Scan(&enabled, &limit); err != nil {
		return err
	}
	if !enabled || limit <= 0 {
		return errAIQuotaExceeded
	}
	if err := tx.QueryRow(ctx, `select coalesce(sum(case when input_tokens+output_tokens>0 then input_tokens+output_tokens when status<>'cancelled' or started_at is not null then quota_reserved_tokens else 0 end),0)
 from ai_tasks where payload->>'scope' in ('seed_crawler','seed_crawler_recovery','seed_crawler_legacy') and created_at>=date_trunc('day',now() at time zone 'UTC') at time zone 'UTC'`).Scan(&used); err != nil {
		return err
	}
	if reserved > limit || used > limit-reserved {
		return errAIQuotaExceeded
	}
	return nil
}

func (worker *SeedCrawlerWorker) enqueueSeedTranslation(ctx context.Context, candidateID int64, jobID string, raw []byte, locale string, items []map[string]string) (aiTaskMessage, error) {
	lease, present := ctx.Value(seedCrawlerLeaseContextKey{}).(seedCrawlerLeaseIdentity)
	if !present {
		return aiTaskMessage{}, errSeedCrawlerLeaseLost
	}
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return aiTaskMessage{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockSeedCrawlerLeaseTx(ctx, tx); err != nil {
		return aiTaskMessage{}, err
	}
	cfg := aiConfigFromQuerier(ctx, tx, worker.server.cfg.SettingsEncryptionKey)
	binding, ok := findAITaskModel(cfg.TaskModels, aiTaskContentTranslation)
	if !ok {
		return aiTaskMessage{}, errors.New("seed AI model is not configured")
	}
	provider, model, ok := resolveAIModel(cfg, binding.ModelKey)
	if !ok {
		return aiTaskMessage{}, errors.New("seed AI model is unavailable")
	}
	payload := map[string]any{"scope": "seed_crawler", "candidateId": candidateID, "metadataJobId": jobID, "sourceHash": seedSourceHash(raw), "runId": lease.RunID, "runToken": lease.Token, "sourceLocale": "en-US", "targetLocale": locale, "items": items}
	if err = freezeAITranslationContext(payload, cfg); err != nil {
		return aiTaskMessage{}, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return aiTaskMessage{}, err
	}
	if err = validateAITranslationPayload(aiTaskContentTranslation, encoded); err != nil {
		return aiTaskMessage{}, err
	}
	var snapshot seedTranslationPayload
	_ = json.Unmarshal(encoded, &snapshot)
	if err = validateSeedTranslationSourceTx(ctx, tx, snapshot); err != nil {
		return aiTaskMessage{}, err
	}
	// No run token or metadata job UID in this identity: a recovered run reading
	// the same source reuses a completed/failed/possibly billed task.
	key := aiTranslationContextKey(fmt.Sprintf("seed:%d:%s:%s:%s", candidateID, snapshot.SourceHash, locale, binding.ModelKey), cfg)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
		return aiTaskMessage{}, err
	}
	msg := aiTaskMessage{TaskType: aiTaskContentTranslation}
	err = tx.QueryRow(ctx, `select id,task_uid from ai_tasks where concurrency_key=$1 and task_type=$2 order by id limit 1`, key, msg.TaskType).Scan(&msg.TaskID, &msg.TaskUID)
	if err == nil {
		return msg, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return aiTaskMessage{}, err
	}
	reserved := estimatedAIReservation(encoded, model)
	if err = preserveLegacySeedAIAccountingTx(ctx, tx, model); err != nil {
		return aiTaskMessage{}, err
	}
	if err = reserveAISiteQuotaTx(ctx, tx, cfg, reserved); err != nil {
		return aiTaskMessage{}, err
	}
	if err = reserveSeedCrawlerAIQuotaTx(ctx, tx, reserved); err != nil {
		return aiTaskMessage{}, err
	}
	msg.TaskUID = "ai_" + randomHex(16)
	err = tx.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,concurrency_key,payload,created_by,queued_at,quota_reserved_tokens)
 select $1,$2,$3,$4,'queued',$5,$6::jsonb,actor_id,now(),$7 from seed_crawler_runs where id=$8 returning id`, msg.TaskUID, msg.TaskType, provider.Code, model.Model, key, string(encoded), reserved, lease.RunID).Scan(&msg.TaskID)
	if err != nil {
		return aiTaskMessage{}, err
	}
	if err = worker.server.enqueueAIOutboxTx(ctx, tx, msg.TaskID, msg.TaskUID, msg.TaskType); err != nil {
		return aiTaskMessage{}, err
	}
	if _, err = tx.Exec(ctx, `insert into seed_crawler_translation_tasks(candidate_id,locale,status) values($1,$2,'pending') on conflict(candidate_id,locale) do update set status='pending',attempts=seed_crawler_translation_tasks.attempts+1,input_tokens=0,output_tokens=0,last_error='',updated_at=now()`, candidateID, locale); err != nil {
		return aiTaskMessage{}, err
	}
	return msg, tx.Commit(ctx)
}

func (worker *AIWorker) persistSeedTranslation(ctx context.Context, taskID int64, raw []byte) error {
	var payload seedTranslationPayload
	if json.Unmarshal(raw, &payload) != nil {
		return errors.New("seed translation payload is invalid")
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var input, output int64
	var result, source []byte
	if err = tx.QueryRow(ctx, `select status,input_tokens,output_tokens,result from ai_tasks where id=$1 for update`, taskID).Scan(&status, &input, &output, &result); err != nil {
		return err
	}
	if status != "running" {
		return errors.New("seed AI task is no longer running")
	}
	if err = validateSeedTranslationSourceTx(ctx, tx, payload); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `select result from mod_metadata_import_jobs where public_id=$1`, payload.MetadataJobID).Scan(&source); err != nil {
		return err
	}
	var sourceFields, resultFields map[string]any
	if json.Unmarshal(source, &sourceFields) != nil || json.Unmarshal(result, &resultFields) != nil {
		return errors.New("seed translation source/result is invalid")
	}
	translated := translationItemsToMap(resultFields)
	applySeedDraftTranslations(sourceFields, map[string]any{payload.TargetLocale: translated}, "mod")
	rows, _ := sourceFields["localizations"].([]map[string]string)
	found := false
	for _, row := range rows {
		if row["locale"] == payload.TargetLocale {
			found = true
		}
	}
	if !found {
		return errors.New("seed translation exceeds localized editor limits")
	}
	if _, err = tx.Exec(ctx, `update seed_crawler_translation_tasks set status='completed',input_tokens=$3,output_tokens=$4,last_error='',updated_at=now() where candidate_id=$1 and locale=$2`, payload.CandidateID, payload.TargetLocale, input, output); err != nil {
		return err
	}
	if err = completeAITaskTx(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (worker *SeedCrawlerWorker) translateSeedDraft(ctx context.Context, candidateID int64, jobID string, raw []byte, budget int64) map[string]any {
	translations := map[string]any{"en-US": map[string]any{"source": true}}
	if budget <= 0 {
		return translations
	}
	var source map[string]any
	if json.Unmarshal(raw, &source) != nil {
		return translations
	}
	items := []map[string]string{}
	for _, key := range []string{"primaryName", "summary", "bodyMarkdown"} {
		if value, ok := source[key].(string); ok && strings.TrimSpace(value) != "" {
			items = append(items, map[string]string{"key": key, "text": value})
		}
	}
	if len(items) == 0 {
		if localizations, ok := source["localizations"].([]any); ok && len(localizations) > 0 {
			if first, ok := localizations[0].(map[string]any); ok {
				for _, key := range []string{"name", "summary", "bodyMarkdown"} {
					if value, ok := first[key].(string); ok && strings.TrimSpace(value) != "" {
						items = append(items, map[string]string{"key": key, "text": value})
					}
				}
			}
		}
	}
	if len(items) == 0 {
		return translations
	}
	aiWorker := NewAIWorker(worker.server.db, worker.server.queue, worker.server.cfg.SettingsEncryptionKey)
	for _, locale := range supportedContentLocaleList() {
		if locale == "en-US" || ctx.Err() != nil {
			continue
		}
		msg, err := worker.enqueueSeedTranslation(ctx, candidateID, jobID, raw, locale, items)
		if err != nil {
			// Budget/model/permission failures stop new calls, while the source draft
			// and any completed translations remain usable.
			break
		}
		message, _ := json.Marshal(msg)
		// A synchronous caller may race the durable queue. The shared atomic claim
		// ensures only one provider request; a failed task is never auto-retried.
		_ = aiWorker.handleTask(ctx, message)
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		ticker := time.NewTicker(100 * time.Millisecond)
		for {
			var status string
			var result []byte
			err = worker.server.db.QueryRow(waitCtx, `select status,result from ai_tasks where id=$1`, msg.TaskID).Scan(&status, &result)
			if err != nil || status == "failed" || status == "cancelled" {
				break
			}
			if status == "completed" {
				var translated map[string]any
				if json.Unmarshal(result, &translated) == nil {
					translations[locale] = translationItemsToMap(translated)
				}
				break
			}
			select {
			case <-waitCtx.Done():
				err = waitCtx.Err()
			case <-ticker.C:
			}
			if err != nil {
				break
			}
		}
		ticker.Stop()
		cancel()
	}
	return translations
}

func seedSubmissionPayload(payload map[string]any) []byte {
	clean := make(map[string]any, len(payload))
	for key, value := range payload {
		if key != "importOrigin" && key != "externalProjectId" && key != "seedTranslations" {
			clean[key] = value
		}
	}
	raw, _ := json.Marshal(clean)
	return raw
}

func applySeedDraftTranslations(payload map[string]any, translations map[string]any, projectType string) {
	// Imported Modrinth metadata is English. Preserve any existing localization
	// rows, and expose completed translations through the normal editor contract.
	existing := map[string]catalogLocalizationEdit{}
	if raw, err := json.Marshal(payload["localizations"]); err == nil {
		var rows []map[string]string
		if json.Unmarshal(raw, &rows) == nil {
			for _, row := range rows {
				existing[row["locale"]] = catalogLocalizationEdit{Locale: row["locale"], Name: row["name"], Summary: row["summary"], ContentMarkdown: defaultString(row["contentMarkdown"], row["bodyMarkdown"])}
			}
		}
	}
	source := catalogLocalizationEdit{Locale: "en-US"}
	source.Name, _ = payload["primaryName"].(string)
	source.Summary, _ = payload["summary"].(string)
	source.ContentMarkdown, _ = payload["bodyMarkdown"].(string)
	if old, ok := existing["en-US"]; ok {
		source = old
	} else if strings.TrimSpace(source.Name) != "" {
		existing["en-US"] = source
	}
	for _, locale := range supportedContentLocaleList() {
		if locale == "en-US" {
			continue
		}
		if _, ok := existing[locale]; ok {
			continue
		}
		fields := map[string]string{}
		switch value := translations[locale].(type) {
		case map[string]string:
			fields = value
		case map[string]any:
			for key, text := range value {
				if translated, ok := text.(string); ok {
					fields[key] = translated
				}
			}
		default:
			continue
		}
		row := catalogLocalizationEdit{Locale: locale, Name: defaultString(fields["primaryName"], fields["name"]), Summary: source.Summary, ContentMarkdown: source.ContentMarkdown}
		if value, ok := fields["summary"]; ok {
			row.Summary = value
		}
		if value, ok := fields["bodyMarkdown"]; ok {
			row.ContentMarkdown = value
		}
		if _, rows, err := normalizeCatalogLocalizations(locale, []catalogLocalizationEdit{row}); err == nil && len(rows) == 1 {
			existing[locale] = rows[0]
		}
	}
	if len(existing) == 0 {
		return
	}
	rows := make([]catalogLocalizationEdit, 0, len(existing))
	for _, locale := range supportedContentLocaleList() {
		if row, ok := existing[locale]; ok {
			rows = append(rows, row)
		}
	}
	payload["defaultLocale"] = "en-US"
	serialized := make([]map[string]string, 0, len(rows))
	bodyField := "contentMarkdown"
	if projectType != "mod" {
		bodyField = "bodyMarkdown"
	}
	for _, row := range rows {
		serialized = append(serialized, map[string]string{"locale": row.Locale, "name": row.Name, "summary": row.Summary, bodyField: row.ContentMarkdown})
	}
	payload["localizations"] = serialized
}

// Automatic seed publication keeps the existing localization model's AI
// lineage. A later ordinary human edit still becomes human_corrected.
func markSeedModTranslationProvenanceTx(ctx context.Context, tx pgx.Tx, modID int64) error {
	lease, present := ctx.Value(seedCrawlerLeaseContextKey{}).(seedCrawlerLeaseIdentity)
	if !present {
		return nil
	}
	_, err := tx.Exec(ctx, `update content_localizations localization set provenance='ai',source_locale='en-US',ai_task_id=task.id
 from ai_tasks task,seed_crawler_candidates candidate,mods mod
 where localization.subject_type='mod' and localization.subject_id=$1 and mod.id=$1
 and localization.locale<>'en-US' and localization.provenance='human' and localization.revision_no=1
 and candidate.id::text=task.payload->>'candidateId' and candidate.external_project_id=mod.modrinth_project_id
 and task.payload->>'scope'='seed_crawler' and task.status='completed'
 and task.payload->>'runId'=$2 and task.payload->>'runToken'=$3
 and task.payload->>'targetLocale'=localization.locale
 and exists(select 1 from jsonb_array_elements(task.result->'items') item
 where item->>'key' in ('primaryName','name') and item->>'text'=localization.name)`, modID, fmt.Sprint(lease.RunID), lease.Token)
	return err
}

// Before a mutable legacy projection can be overwritten, capture its final
// known usage as a fact. Older direct calls did not retain failed usage, so
// zero-usage running/failed/completed rows conservatively reserve one crawler
// day budget. These records are explicitly legacy estimates, never enqueued.
func preserveLegacySeedAIAccountingTx(ctx context.Context, tx pgx.Tx, model aiModelConfig) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-ai-site-quota'))`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-seed-ai-quota'))`); err != nil {
		return err
	}
	var limit int64
	if err := tx.QueryRow(ctx, `select ai_daily_token_budget from seed_crawler_configs where id`).Scan(&limit); err != nil {
		return err
	}
	price := math.Max(model.InputPricePerMillion, model.OutputPricePerMillion)
	if price < 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return errors.New("legacy AI reservation model price is invalid")
	}
	_, err := tx.Exec(ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,payload,input_tokens,output_tokens,quota_reserved_tokens,started_at,finished_at,created_at,updated_at,error)
 select 'ai_seed_legacy_'||md5(legacy.candidate_id::text||':'||legacy.locale||':'||(legacy.updated_at at time zone 'UTC')::date::text),$1,'legacy-unverified','legacy-unverified',
 case when greatest(legacy.input_tokens,0)+greatest(legacy.output_tokens,0)>0 then 'completed' else 'failed' end,
 jsonb_build_object('scope','seed_crawler_legacy','candidateId',legacy.candidate_id,'targetLocale',legacy.locale,'legacyEstimate',true,
 'quotaReservedCostMicros',least(9223372036854775807::numeric,ceil((case when legacy.input_tokens+legacy.output_tokens>0 then greatest(legacy.input_tokens,0)+greatest(legacy.output_tokens,0) else $2::bigint end)::numeric*$3::numeric))),
 greatest(legacy.input_tokens,0),greatest(legacy.output_tokens,0),case when legacy.input_tokens+legacy.output_tokens>0 then greatest(legacy.input_tokens,0)+greatest(legacy.output_tokens,0) else $2::bigint end,
 legacy.updated_at,legacy.updated_at,legacy.updated_at,legacy.updated_at,'Legacy seed provider call: billing/model unverified; no automatic retry'
 from seed_crawler_translation_tasks legacy where legacy.status in ('running','failed','completed')
 and legacy.updated_at>=date_trunc('day',now() at time zone 'UTC') at time zone 'UTC'
 and not exists(select 1 from ai_tasks task where task.payload->>'scope' in ('seed_crawler','seed_crawler_recovery','seed_crawler_legacy')
 and task.payload->>'candidateId'=legacy.candidate_id::text and task.payload->>'targetLocale'=legacy.locale)
 on conflict(task_uid) do nothing`, aiTaskContentTranslation, max(limit, 0), price)
	return err
}
