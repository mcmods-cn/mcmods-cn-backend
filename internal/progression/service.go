package progression

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/activity"
)

type Service struct {
	db *pgxpool.Pool
}

type taskCondition struct {
	Action         string `json:"action"`
	ObjectType     string `json:"objectType"`
	ObjectPublicID string `json:"objectPublicId"`
	Metric         string `json:"metric"`
	Target         int64  `json:"target"`
}

type taskRewards struct {
	Experience int64            `json:"experience"`
	Currencies map[string]int64 `json:"currencies"`
}

type taskDefinition struct {
	ID            int64
	RefreshPeriod string
	Condition     taskCondition
	Rewards       taskRewards
}

type progressKey struct {
	UserID    int64
	TaskID    int64
	PeriodKey string
}

type progressDelta struct {
	Task   taskDefinition
	Amount int64
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) ProcessActivityBatch(ctx context.Context, events []activity.Event) error {
	if s == nil || s.db == nil || len(events) == 0 {
		return nil
	}
	tasks, err := s.activeTasks(ctx)
	if err != nil || len(tasks) == 0 {
		return err
	}
	timezones, err := s.userTimezones(ctx, events)
	if err != nil {
		return err
	}
	deltas := make(map[progressKey]progressDelta)
	for _, event := range events {
		location := time.UTC
		if timezone := timezones[event.UserID]; timezone != "" {
			if loaded, loadErr := time.LoadLocation(timezone); loadErr == nil {
				location = loaded
			}
		}
		for _, task := range tasks {
			if !matchesTask(task.Condition, event) {
				continue
			}
			amount := int64(1)
			if task.Condition.Metric == "markdown_bytes" {
				amount = int64(event.MarkdownAddedBytes)
			}
			if amount <= 0 {
				continue
			}
			key := progressKey{
				UserID:    event.UserID,
				TaskID:    task.ID,
				PeriodKey: PeriodKey(task.RefreshPeriod, event.OccurredAt, location),
			}
			value := deltas[key]
			value.Task = task
			value.Amount += amount
			deltas[key] = value
		}
	}
	if len(deltas) == 0 {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for key, delta := range deltas {
		if err = s.applyTaskProgress(ctx, tx, key, delta); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) userTimezones(ctx context.Context, events []activity.Event) (map[int64]string, error) {
	userIDs := make([]int64, 0, len(events))
	seen := make(map[int64]struct{}, len(events))
	for _, event := range events {
		if event.UserID <= 0 {
			continue
		}
		if _, exists := seen[event.UserID]; exists {
			continue
		}
		seen[event.UserID] = struct{}{}
		userIDs = append(userIDs, event.UserID)
	}
	result := make(map[int64]string, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	rows, err := s.db.Query(ctx, `select id,timezone from users where id=any($1::bigint[])`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		var timezone string
		if err = rows.Scan(&userID, &timezone); err != nil {
			return nil, err
		}
		result[userID] = timezone
	}
	return result, rows.Err()
}

func (s *Service) activeTasks(ctx context.Context) ([]taskDefinition, error) {
	rows, err := s.db.Query(ctx, `select id,refresh_period,condition,rewards from task_definitions where status='active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]taskDefinition, 0)
	for rows.Next() {
		var task taskDefinition
		var conditionRaw, rewardsRaw []byte
		if err = rows.Scan(&task.ID, &task.RefreshPeriod, &conditionRaw, &rewardsRaw); err != nil {
			return nil, err
		}
		if json.Unmarshal(conditionRaw, &task.Condition) != nil || task.Condition.Target <= 0 {
			continue
		}
		_ = json.Unmarshal(rewardsRaw, &task.Rewards)
		result = append(result, task)
	}
	return result, rows.Err()
}

func matchesTask(condition taskCondition, event activity.Event) bool {
	if actionID(condition.Action) != 0 && actionID(condition.Action) != event.ActionID {
		return false
	}
	if objectTypeID(condition.ObjectType) != 0 && objectTypeID(condition.ObjectType) != event.ObjectTypeID {
		return false
	}
	if condition.ObjectPublicID != "" && condition.ObjectPublicID != event.ObjectPublicID {
		return false
	}
	return condition.Metric == "" || condition.Metric == "count" || condition.Metric == "markdown_bytes"
}

func PeriodKey(refreshPeriod string, timestamp time.Time, location *time.Location) string {
	if location == nil {
		location = time.UTC
	}
	timestamp = timestamp.In(location)
	switch refreshPeriod {
	case "daily":
		return timestamp.Format("2006-01-02")
	case "weekly":
		year, week := timestamp.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case "monthly":
		return timestamp.Format("2006-01")
	default:
		return "all"
	}
}

func (s *Service) applyTaskProgress(ctx context.Context, tx pgx.Tx, key progressKey, delta progressDelta) error {
	var progress int64
	var rewardedAt *time.Time
	err := tx.QueryRow(ctx, `insert into user_task_progress(user_id,task_id,period_key,progress,updated_at)
		values($1,$2,$3,$4,now())
		on conflict(user_id,task_id,period_key) do update
		set progress=user_task_progress.progress+excluded.progress,updated_at=now()
		returning progress,rewarded_at`,
		key.UserID, key.TaskID, key.PeriodKey, delta.Amount,
	).Scan(&progress, &rewardedAt)
	if err != nil || progress < delta.Task.Condition.Target || rewardedAt != nil {
		return err
	}
	var rewarded time.Time
	err = tx.QueryRow(ctx, `update user_task_progress
		set completed_at=coalesce(completed_at,now()),rewarded_at=now(),updated_at=now()
		where user_id=$1 and task_id=$2 and period_key=$3 and rewarded_at is null
		returning rewarded_at`, key.UserID, key.TaskID, key.PeriodKey).Scan(&rewarded)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	referenceKey := strconv.FormatInt(key.TaskID, 10) + ":" + key.PeriodKey
	if delta.Task.Rewards.Experience > 0 {
		if err = s.grantExperience(ctx, tx, key.UserID, delta.Task.Rewards.Experience, "task_reward", referenceKey); err != nil {
			return err
		}
	}
	for currencyCode, amount := range delta.Task.Rewards.Currencies {
		if amount <= 0 {
			continue
		}
		if err = grantCurrency(ctx, tx, key.UserID, currencyCode, amount, "task_reward", "task", referenceKey); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) grantExperience(ctx context.Context, tx pgx.Tx, userID, amount int64, reason, referenceKey string) error {
	var experience int64
	err := tx.QueryRow(ctx, `insert into user_experience(user_id,experience,level,updated_at)
		values($1,$2,0,now())
		on conflict(user_id) do update
		set experience=user_experience.experience+excluded.experience,updated_at=now()
		returning experience`, userID, amount).Scan(&experience)
	if err != nil {
		return err
	}
	thresholds, trackCode, err := levelConfiguration(ctx, tx)
	if err != nil {
		return err
	}
	level := LevelForExperience(experience, thresholds)
	if _, err = tx.Exec(ctx, `update user_experience set level=$2,updated_at=now() where user_id=$1`, userID, level); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into experience_transactions(user_id,amount_delta,experience_after,reason,reference_type,reference_key)
		values($1,$2,$3,$4,'task',$5)`, userID, amount, experience, reason, referenceKey); err != nil {
		return err
	}
	if trackCode != "" {
		return SyncTrackRole(ctx, tx, userID, trackCode, level)
	}
	return nil
}

func levelConfiguration(ctx context.Context, tx pgx.Tx) ([]int64, string, error) {
	var thresholds []int64
	var trackCode *string
	err := tx.QueryRow(ctx, `select level_thresholds,role_track_code from level_system_config where singleton`).Scan(&thresholds, &trackCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if trackCode == nil {
		return thresholds, "", nil
	}
	return thresholds, *trackCode, nil
}

func LevelForExperience(experience int64, thresholds []int64) int {
	level := 0
	for index, threshold := range thresholds {
		if experience < threshold {
			break
		}
		level = index + 1
	}
	return level
}

func SyncTrackRole(ctx context.Context, tx pgx.Tx, userID int64, trackCode string, level int) error {
	rows, err := tx.Query(ctx, `select role_id from permission_role_track_roles where track_code=$1 order by position`, trackCode)
	if err != nil {
		return err
	}
	roleIDs := make([]int64, 0)
	for rows.Next() {
		var roleID int64
		if err = rows.Scan(&roleID); err != nil {
			rows.Close()
			return err
		}
		roleIDs = append(roleIDs, roleID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(roleIDs) == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `delete from user_role_bindings where user_id=$1 and role_id=any($2)`, userID, roleIDs); err != nil {
		return err
	}
	if level <= 0 {
		return nil
	}
	position := level - 1
	if position >= len(roleIDs) {
		position = len(roleIDs) - 1
	}
	_, err = tx.Exec(ctx, `insert into user_role_bindings(user_id,role_id) values($1,$2) on conflict do nothing`, userID, roleIDs[position])
	return err
}

func grantCurrency(ctx context.Context, tx pgx.Tx, userID int64, currencyCode string, amount int64, transactionType, referenceType, referenceKey string) error {
	var currencyID, balance int64
	err := tx.QueryRow(ctx, `select id from currencies where code=$1 and status='active'`, currencyCode).Scan(&currencyID)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `insert into user_currency_balances(user_id,currency_id,balance,updated_at)
		values($1,$2,$3,now())
		on conflict(user_id,currency_id) do update
		set balance=user_currency_balances.balance+excluded.balance,updated_at=now()
		returning balance`, userID, currencyID, amount).Scan(&balance)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into currency_transactions(
		user_id,currency_id,amount_delta,balance_after,transaction_type,reference_type,reference_key
	) values($1,$2,$3,$4,$5,$6,$7)`,
		userID, currencyID, amount, balance, transactionType, referenceType, referenceKey,
	)
	return err
}

func actionID(code string) int16 {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "edit":
		return activity.ActionEdit
	case "create":
		return activity.ActionCreate
	case "view":
		return activity.ActionView
	case "delete":
		return activity.ActionDelete
	case "claim":
		return activity.ActionClaim
	case "download":
		return activity.ActionDownload
	case "upload":
		return activity.ActionUpload
	case "purchase":
		return activity.ActionPurchase
	case "transfer":
		return activity.ActionTransfer
	case "checkin":
		return activity.ActionCheckIn
	case "use":
		return activity.ActionUse
	default:
		return 0
	}
}

func objectTypeID(code string) int16 {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "recipe":
		return activity.ObjectRecipe
	case "mod":
		return activity.ObjectMod
	case "blueprint":
		return activity.ObjectBlueprint
	case "plugin":
		return activity.ObjectPlugin
	case "author":
		return activity.ObjectAuthor
	case "team":
		return activity.ObjectTeam
	case "user":
		return activity.ObjectUser
	case "comment":
		return activity.ObjectComment
	case "tag":
		return activity.ObjectTag
	case "file":
		return activity.ObjectFile
	case "economy":
		return activity.ObjectEconomy
	case "task":
		return activity.ObjectTask
	case "shop_item":
		return activity.ObjectShopItem
	default:
		return 0
	}
}
