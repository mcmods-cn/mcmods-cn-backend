package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const aiRequestBudgetEvent = "provider_request_usage"

type aiRequestBudget struct {
	TaskID             int64  `json:"taskId"`
	Provider           string `json:"provider"`
	Model              string `json:"model"`
	User               string `json:"user"`
	ReservedTokens     int64  `json:"reservedTokens"`
	ReservedCostMicros int64  `json:"reservedCostMicros"`
	InputTokens        int64  `json:"inputTokens"`
	OutputTokens       int64  `json:"outputTokens"`
	CostMicros         int64  `json:"costMicros"`
	State              string `json:"state"`
}

func validateAIQuotas(quotas []aiQuotaConfig) error {
	for _, quota := range quotas {
		if quota.Period != "hour" && quota.Period != "day" && quota.Period != "month" {
			return errors.New("AI quota period must be hour, day, or month")
		}
		switch quota.Scope {
		case "site":
			if quota.Subject != "" && quota.Subject != "default" {
				return errors.New("site AI quota subject must be default")
			}
		case "provider", "model", "user":
			if strings.TrimSpace(quota.Subject) == "" {
				return errors.New("AI quota subject is required")
			}
		default:
			return errors.New("unsupported AI quota scope")
		}
		if quota.RequestLimit < 0 || quota.TokenLimit < 0 || quota.CostLimitCNY < 0 || quota.CostLimitCNY > math.MaxInt64/1000000 {
			return errors.New("AI quota limits are invalid")
		}
	}
	return nil
}

func (worker *AIWorker) reserveProviderRequestBudget(ctx context.Context, prepared preparedAITask) (int64, aiRequestBudget, error) {
	if err := validateAIQuotas(prepared.quotas); err != nil {
		return 0, aiRequestBudget{}, err
	}
	budget := aiRequestBudget{Provider: prepared.provider.Code, Model: prepared.provider.Code + "/" + prepared.model.Model,
		ReservedTokens: prepared.reservationTokens, State: "reserved"}
	// Reserve the most expensive possible split of the conservative token cap.
	price := math.Max(prepared.model.InputPricePerMillion, prepared.model.OutputPricePerMillion)
	if prepared.model.InputPricePerMillion < 0 || prepared.model.OutputPricePerMillion < 0 ||
		math.IsNaN(prepared.model.InputPricePerMillion) || math.IsNaN(prepared.model.OutputPricePerMillion) ||
		math.IsInf(price, 0) || price*float64(budget.ReservedTokens) >= float64(math.MaxInt64) {
		return 0, budget, errors.New("AI model prices are invalid")
	}
	budget.ReservedCostMicros = int64(math.Ceil(float64(budget.ReservedTokens) * price))
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return 0, budget, err
	}
	defer tx.Rollback(ctx)
	// All replicas use the same lock: budget admission and the ledger insert are
	// atomic across overlapping site, provider, model and user limits.
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('ai-provider-request-budget'))`); err != nil {
		return 0, budget, err
	}
	if err = lockAITaskExecutionTx(ctx, tx); err != nil {
		return 0, budget, err
	}
	if run, ok := ctx.Value(aiTaskExecutionKey{}).(aiTaskExecution); ok {
		budget.TaskID = run.ID
		var actorID int64
		if err = tx.QueryRow(ctx, `select coalesce(created_by,0) from ai_tasks where id=$1`, run.ID).Scan(&actorID); err != nil {
			return 0, budget, err
		}
		if actorID > 0 {
			if err = tx.QueryRow(ctx, `select public_id from users where id=$1`, actorID).Scan(&budget.User); err != nil {
				return 0, budget, err
			}
		}
		var attempts int64
		if err = tx.QueryRow(ctx, `select count(*) from ai_task_logs where event=$1 and payload->>'taskId'=$2`, aiRequestBudgetEvent, strconv.FormatInt(run.ID, 10)).Scan(&attempts); err != nil {
			return 0, budget, err
		}
		if attempts >= 3 {
			return 0, budget, errors.New("AI task reached the provider request attempt limit")
		}
	}
	for _, quota := range prepared.quotas {
		filter := ""
		subject := ""
		switch quota.Scope {
		case "provider":
			if quota.Subject != budget.Provider {
				continue
			}
			filter, subject = " and payload->>'provider'=$3", budget.Provider
		case "model":
			if quota.Subject != budget.Model {
				continue
			}
			filter, subject = " and payload->>'model'=$3", budget.Model
		case "user":
			if quota.Subject != budget.User {
				continue
			}
			filter, subject = " and payload->>'user'=$3", budget.User
		}
		args := []any{aiRequestBudgetEvent, quota.Period}
		if filter != "" {
			args = append(args, subject)
		}
		var requests, tokens, cost int64
		err = tx.QueryRow(ctx, `select count(*),
			coalesce(sum((payload->>'inputTokens')::bigint+(payload->>'outputTokens')::bigint+(payload->>'reservedTokens')::bigint),0),
			coalesce(sum((payload->>'costMicros')::bigint+(payload->>'reservedCostMicros')::bigint),0)
			from ai_task_logs where event=$1 and created_at>=date_trunc($2,now())`+filter, args...).Scan(&requests, &tokens, &cost)
		if err != nil {
			return 0, budget, err
		}
		if (quota.RequestLimit > 0 && requests >= quota.RequestLimit) ||
			(quota.TokenLimit > 0 && (tokens > quota.TokenLimit || budget.ReservedTokens > quota.TokenLimit-tokens)) ||
			(quota.CostLimitCNY > 0 && (cost > quota.CostLimitCNY*1000000 || budget.ReservedCostMicros > quota.CostLimitCNY*1000000-cost)) {
			return 0, budget, fmt.Errorf("AI %s %s request budget is exhausted", quota.Scope, quota.Period)
		}
	}
	raw, err := json.Marshal(budget)
	if err != nil {
		return 0, budget, err
	}
	var ledgerID int64
	// Keep the ledger independent of task deletion (task_id's FK cascades).
	if err = tx.QueryRow(ctx, `insert into ai_task_logs(task_id,level,event,message,payload)
		values(null,'info',$1,'AI provider request budget reserved',$2::jsonb) returning id`, aiRequestBudgetEvent, string(raw)).Scan(&ledgerID); err != nil {
		return 0, budget, err
	}
	return ledgerID, budget, tx.Commit(ctx)
}

func (worker *AIWorker) settleProviderRequestBudget(ctx context.Context, ledgerID int64, budget aiRequestBudget, usage aiTaskUsage) error {
	budget.State = "settled"
	if usage.InputTokens+usage.OutputTokens > 0 {
		budget.InputTokens, budget.OutputTokens, budget.CostMicros = usage.InputTokens, usage.OutputTokens, usage.CostMicros
		budget.ReservedTokens, budget.ReservedCostMicros = 0, 0
	} else {
		// A timeout or absent usage is not evidence of a free request. Keep its
		// conservative reservation consumed until the configured period expires.
		budget.State = "usage_unknown"
	}
	raw, err := json.Marshal(budget)
	if err != nil {
		return err
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tag, err := worker.db.Exec(settleCtx, `update ai_task_logs set payload=$2::jsonb where id=$1 and event=$3`, ledgerID, string(raw), aiRequestBudgetEvent)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("AI request budget ledger is missing")
	}
	return nil
}
