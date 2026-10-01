package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/queue"
)

// Limits use the existing task rows as the authoritative reservation ledger.
// A failed call may still be billed, so failed tasks retain known usage and an
// unknown-usage reservation. Reservations are never treated as real usage.
func reserveAISiteQuotaTx(ctx context.Context, tx pgx.Tx, cfg aiConfigPayload, reserved int64) error {
	if err := validateAIQuotaConfiguration(cfg); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-ai-site-quota'))`); err != nil {
		return err
	}
	for _, quota := range cfg.Quotas {
		if quota.Scope != "site" {
			continue
		}
		period := quota.Period
		if period != "day" && period != "month" && period != "hour" {
			return errors.New("AI site quota period is invalid")
		}
		var requests, tokens int64
		if err := tx.QueryRow(ctx, `select count(*),coalesce(sum(case
    when input_tokens+output_tokens>0 then input_tokens+output_tokens
    when status<>'cancelled' or started_at is not null then quota_reserved_tokens else 0 end),0)
    from ai_tasks where created_at>=date_trunc($1,now() at time zone 'UTC') at time zone 'UTC'`, period).Scan(&requests, &tokens); err != nil {
			return err
		}
		if quota.RequestLimit > 0 && requests >= quota.RequestLimit || quota.TokenLimit > 0 && (reserved > quota.TokenLimit || tokens > quota.TokenLimit-reserved) {
			return errAIQuotaExceeded
		}
	}
	return nil
}

func validateAIQuotaConfiguration(cfg aiConfigPayload) error {
	if len(cfg.Translation.Glossary) > 1024 {
		return errors.New("AI glossary exceeds the 1024-byte limit")
	}
	for _, quota := range cfg.Quotas {
		// Only site/default has a defined identity and aggregation policy. Never
		// silently accept a user/role budget which this ledger cannot enforce.
		if quota.Scope != "site" || quota.Subject != "" && quota.Subject != "default" {
			return errors.New("AI quotas currently support only scope=site and subject=default")
		}
		if quota.Period != "hour" && quota.Period != "day" && quota.Period != "month" || quota.RequestLimit < 0 || quota.TokenLimit < 0 || quota.CostLimitCNY < 0 {
			return errors.New("AI quota periods or limits are invalid")
		}
	}
	return nil
}

// Freeze glossary data when creating a task. A later configuration edit must
// not change an already reserved request or a retry of that request.
func freezeAITranslationContext(payload map[string]any, cfg aiConfigPayload) error {
	if len(cfg.Translation.Glossary) > 1024 {
		return errors.New("AI glossary exceeds the 1024-byte limit")
	}
	payload["glossary"] = cfg.Translation.Glossary
	digest := sha256.Sum256([]byte(cfg.Translation.Glossary))
	payload["glossaryHash"] = hex.EncodeToString(digest[:])
	return nil
}

func aiTranslationContextKey(key string, cfg aiConfigPayload) string {
	if cfg.Translation.Glossary == "" {
		return key
	}
	digest := sha256.Sum256([]byte(cfg.Translation.Glossary))
	return key + ":glossary:" + hex.EncodeToString(digest[:])
}

func aiOutputTokenLimit(model aiModelConfig) int {
	if model.MaxOutputTokens > 0 {
		return min(model.MaxOutputTokens, 8192)
	}
	return 4096
}

// Publication and terminal state share one transaction. Cancellation either
// commits first and prevents publication, or observes completed and refuses.
func completeAITaskTx(ctx context.Context, tx pgx.Tx, taskID int64) error {
	tag, err := tx.Exec(ctx, `update ai_tasks set status='completed',finished_at=now(),updated_at=now() where id=$1 and status='running'`, taskID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("AI task is no longer running")
	}
	return nil
}

func estimatedAIReservation(rawPayload []byte, model aiModelConfig) int64 {
	// One token per UTF-8 byte is a conservative upper bound for ordinary text;
	// include output and fixed prompt overhead rather than reserving input only.
	return int64(len(rawPayload) + aiOutputTokenLimit(model) + 2048)
}

func (s *Server) enqueueAIOutboxTx(ctx context.Context, tx pgx.Tx, taskID int64, taskUID, taskType string) error {
	var provider, modelID string
	var reserved int64
	if err := tx.QueryRow(ctx, `select provider,model,quota_reserved_tokens from ai_tasks where id=$1`, taskID).Scan(&provider, &modelID, &reserved); err != nil {
		return err
	}
	cfg := aiConfigFromQuerier(ctx, tx, s.cfg.SettingsEncryptionKey)
	_, model, ok := resolveAIModel(cfg, provider+"/"+modelID)
	if !ok {
		return errors.New("AI task model is unavailable")
	}
	var costLimitPresent bool
	for _, quota := range cfg.Quotas {
		if quota.Scope == "site" && quota.CostLimitCNY > 0 {
			costLimitPresent = true
		}
	}
	if costLimitPresent {
		if model.InputPricePerMillion <= 0 || model.OutputPricePerMillion <= 0 || math.IsInf(model.InputPricePerMillion, 0) || math.IsInf(model.OutputPricePerMillion, 0) || math.IsNaN(model.InputPricePerMillion) || math.IsNaN(model.OutputPricePerMillion) {
			return errors.New("AI budget requires configured positive CNY model prices")
		}
		estimatedCost := math.Ceil(float64(reserved) * math.Max(model.InputPricePerMillion, model.OutputPricePerMillion))
		if estimatedCost >= float64(math.MaxInt64) {
			return errors.New("AI task cost exceeds the supported range")
		}
		cost := int64(estimatedCost)
		if cost < 0 {
			return errors.New("AI task cost reservation is invalid")
		}
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-ai-site-quota'))`); err != nil {
			return err
		}
		for _, quota := range cfg.Quotas {
			if quota.Scope != "site" || quota.CostLimitCNY <= 0 {
				continue
			}
			if quota.CostLimitCNY > math.MaxInt64/1000000 {
				return errors.New("AI budget exceeds the supported range")
			}
			var consumed int64
			if err := tx.QueryRow(ctx, `select coalesce(sum(case when cost_micros>0 then cost_micros
    when status<>'cancelled' or started_at is not null then coalesce((payload->>'quotaReservedCostMicros')::bigint,0) else 0 end),0)
    from ai_tasks where id<>$1 and created_at>=date_trunc($2,now() at time zone 'UTC') at time zone 'UTC'`, taskID, quota.Period).Scan(&consumed); err != nil {
				return err
			}
			limit := quota.CostLimitCNY * 1000000
			if cost > limit || consumed > limit-cost {
				return errAIQuotaExceeded
			}
		}
		if _, err := tx.Exec(ctx, `update ai_tasks set payload=payload||jsonb_build_object('quotaReservedCostMicros',$2::bigint) where id=$1`, taskID, cost); err != nil {
			return err
		}
	}
	if !s.cfg.NATS.OutboxEnabled {
		return nil
	}
	_, err := queue.EnqueueTx(ctx, tx, "ai", "ai.task.requested", "ai_task", taskUID, "", aiTaskMessage{TaskID: taskID, TaskUID: taskUID, TaskType: taskType})
	return err
}

func validateAITranslationPayload(taskType string, raw []byte) error {
	if len(raw) > 1<<20 {
		return errors.New("AI translation source exceeds the 1 MiB limit")
	}
	var payload struct {
		SourceLocale string              `json:"sourceLocale"`
		TargetLocale string              `json:"targetLocale"`
		Items        []map[string]string `json:"items"`
		Glossary     string              `json:"glossary"`
		GlossaryHash string              `json:"glossaryHash"`
	}
	if json.Unmarshal(raw, &payload) != nil || !validContentLocaleTag(payload.SourceLocale) || !validContentLocaleTag(payload.TargetLocale) || len(payload.Items) == 0 || len(payload.Items) > 100 {
		return errors.New("AI translation source locales or items are invalid")
	}
	if len(payload.Glossary) > 1024 {
		return errors.New("AI glossary exceeds the 1024-byte limit")
	}
	if payload.GlossaryHash != "" || payload.Glossary != "" {
		digest := sha256.Sum256([]byte(payload.Glossary))
		if payload.GlossaryHash != hex.EncodeToString(digest[:]) {
			return errors.New("AI glossary snapshot fingerprint is invalid")
		}
	}
	seen := map[string]bool{}
	for _, item := range payload.Items {
		if item["key"] == "" || seen[item["key"]] {
			return errors.New("AI translation source item keys are invalid")
		}
		seen[item["key"]] = true
		fields := []string{"text"}
		if taskType == aiTaskPermissionTranslation {
			fields = []string{"name", "description"}
		}
		for _, field := range fields {
			if _, ok := item[field]; !ok {
				return fmt.Errorf("AI translation source item is missing %s", field)
			}
		}
	}
	return nil
}
