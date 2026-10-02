package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/progression"
)

type levelConfigPayload struct {
	RoleTrackCode   string                     `json:"roleTrackCode"`
	LevelThresholds []int64                    `json:"levelThresholds"`
	Version         int64                      `json:"version,omitempty"`
	Recalculation   *levelRecalculationPayload `json:"recalculation,omitempty"`
}

type levelRecalculationPayload struct {
	ConfigVersion  int64  `json:"configVersion"`
	Status         string `json:"status"`
	CursorUserID   int64  `json:"cursorUserId"`
	ProcessedCount int64  `json:"processedCount"`
	Attempts       int    `json:"attempts"`
	MaxAttempts    int    `json:"maxAttempts"`
	LastError      string `json:"lastError,omitempty"`
}

type taskPayload struct {
	PublicID      string         `json:"publicId,omitempty"`
	Code          string         `json:"code"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Icon          string         `json:"icon"`
	Translations  map[string]any `json:"translations"`
	RefreshPeriod string         `json:"refreshPeriod"`
	Condition     map[string]any `json:"condition"`
	Rewards       map[string]any `json:"rewards"`
	Status        string         `json:"status"`
}

var errTaskCurrencyRewardDuplicate = errors.New("task currency reward codes must be unique")
var errTaskRewardStoreUnavailable = errors.New("task reward store is unavailable")

func (payload *taskPayload) UnmarshalJSON(data []byte) error {
	if err := validateTaskCurrencyRewardJSONKeys(data); err != nil {
		return err
	}
	type taskPayloadAlias taskPayload
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded taskPayloadAlias
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*payload = taskPayload(decoded)
	return nil
}

func validateTaskCurrencyRewardJSONKeys(data []byte) error {
	var envelope struct {
		Rewards json.RawMessage `json:"rewards"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Rewards) == 0 {
		return err
	}
	var rewards struct {
		Currencies json.RawMessage `json:"currencies"`
	}
	if err := json.Unmarshal(envelope.Rewards, &rewards); err != nil || len(rewards.Currencies) == 0 || bytes.Equal(bytes.TrimSpace(rewards.Currencies), []byte("null")) {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(rewards.Currencies))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		rawCode, err := decoder.Token()
		if err != nil {
			return err
		}
		code, ok := rawCode.(string)
		if !ok {
			return errors.New("task currency reward code is invalid")
		}
		normalized := normalizeCode(code)
		if _, duplicate := seen[normalized]; duplicate {
			return errTaskCurrencyRewardDuplicate
		}
		seen[normalized] = struct{}{}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func (s *Server) userTasks(w http.ResponseWriter, r *http.Request) {
	userID := currentClaims(r).Subject
	var timezone string
	if err := s.db.QueryRow(r.Context(), `select timezone from users where id=$1`, userID).Scan(&timezone); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user timezone")
		return
	}
	location := loadLocation(timezone)
	rows, err := s.db.Query(r.Context(), `select id,public_id,code,name,description,icon,translations,
		refresh_period,condition,rewards,status
		from task_definitions where status='active' order by created_at,id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tasks")
		return
	}
	defer rows.Close()
	type taskRow struct {
		id            int64
		publicID      string
		code          string
		name          string
		description   string
		icon          string
		refreshPeriod string
		status        string
		translations  map[string]any
		condition     map[string]any
		rewards       map[string]any
		periodKey     string
	}
	tasks := make([]taskRow, 0)
	taskIDs := make([]int64, 0)
	periodKeys := make([]string, 0, 4)
	periodKeySet := make(map[string]struct{}, 4)
	now := time.Now()
	for rows.Next() {
		var item taskRow
		var translationsRaw, conditionRaw, rewardsRaw []byte
		if err = rows.Scan(&item.id, &item.publicID, &item.code, &item.name, &item.description, &item.icon, &translationsRaw,
			&item.refreshPeriod, &conditionRaw, &rewardsRaw, &item.status); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode tasks")
			return
		}
		item.translations, err = decodeStoredJSONObject(translationsRaw, "task translations")
		if err != nil {
			log.Printf("load user task %s translations: %v", item.publicID, err)
			writeError(w, http.StatusInternalServerError, "failed to decode task translations")
			return
		}
		item.condition, item.rewards, err = decodeTaskConfigurationMaps(conditionRaw, rewardsRaw)
		if err != nil {
			log.Printf("load user task %s configuration: %v", item.publicID, err)
			writeError(w, http.StatusInternalServerError, "failed to decode task configuration")
			return
		}
		item.periodKey = progression.PeriodKey(item.refreshPeriod, now, location)
		tasks = append(tasks, item)
		taskIDs = append(taskIDs, item.id)
		if _, exists := periodKeySet[item.periodKey]; !exists {
			periodKeySet[item.periodKey] = struct{}{}
			periodKeys = append(periodKeys, item.periodKey)
		}
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tasks")
		return
	}

	type taskProgress struct {
		progress    int64
		completedAt *time.Time
		rewardedAt  *time.Time
	}
	progressByTask := make(map[string]taskProgress, len(tasks))
	if len(taskIDs) > 0 {
		progressRows, progressErr := s.db.Query(r.Context(), `select task_id,period_key,progress,completed_at,rewarded_at
			from user_task_progress
			where user_id=$1 and task_id=any($2) and period_key=any($3)`,
			userID, taskIDs, periodKeys)
		if progressErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load task progress")
			return
		}
		for progressRows.Next() {
			var taskID int64
			var periodKey string
			var progress taskProgress
			if progressErr = progressRows.Scan(&taskID, &periodKey, &progress.progress, &progress.completedAt, &progress.rewardedAt); progressErr != nil {
				progressRows.Close()
				writeError(w, http.StatusInternalServerError, "failed to decode task progress")
				return
			}
			progressByTask[strconv.FormatInt(taskID, 10)+"\x00"+periodKey] = progress
		}
		progressErr = progressRows.Err()
		progressRows.Close()
		if progressErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load task progress")
			return
		}
	}

	items := make([]map[string]any, 0, len(tasks))
	for _, item := range tasks {
		progress := progressByTask[strconv.FormatInt(item.id, 10)+"\x00"+item.periodKey]
		items = append(items, map[string]any{
			"publicId": item.publicID, "code": item.code, "name": item.name, "description": item.description,
			"icon": item.icon, "translations": item.translations, "refreshPeriod": item.refreshPeriod,
			"condition": item.condition, "rewards": item.rewards, "status": item.status,
			"periodKey": item.periodKey, "progress": progress.progress,
			"completedAt": progress.completedAt, "rewardedAt": progress.rewardedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "timezone": timezone})
}

func (s *Server) adminLevelConfig(w http.ResponseWriter, r *http.Request) {
	var roleTrack *string
	var thresholds []int64
	var payload levelConfigPayload
	var job levelRecalculationPayload
	if err := s.db.QueryRow(r.Context(), `select config.role_track_code,config.level_thresholds,config.version,
		coalesce(job.config_version,0),coalesce(job.status,''),coalesce(job.cursor_user_id,0),
		coalesce(job.processed_count,0),coalesce(job.attempts,0),coalesce(job.max_attempts,0),coalesce(job.last_error,'')
		from level_system_config config left join lateral (
			select config_version,status,cursor_user_id,processed_count,attempts,max_attempts,last_error
			from level_recalculation_jobs order by config_version desc limit 1
		) job on true where config.singleton`).Scan(&roleTrack, &thresholds, &payload.Version,
		&job.ConfigVersion, &job.Status, &job.CursorUserID, &job.ProcessedCount,
		&job.Attempts, &job.MaxAttempts, &job.LastError); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load level configuration")
		return
	}
	payload.LevelThresholds = thresholds
	if roleTrack != nil {
		payload.RoleTrackCode = *roleTrack
	}
	if job.Status != "" {
		payload.Recalculation = &job
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) updateLevelConfig(w http.ResponseWriter, r *http.Request) {
	var payload levelConfigPayload
	if decodeJSON(r, &payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid level configuration")
		return
	}
	if payload.LevelThresholds == nil {
		payload.LevelThresholds = []int64{}
	}
	roleIDs := make([]int64, 0)
	payload.RoleTrackCode = strings.TrimSpace(payload.RoleTrackCode)
	for index, threshold := range payload.LevelThresholds {
		if threshold < 0 || (index > 0 && threshold <= payload.LevelThresholds[index-1]) {
			writeError(w, http.StatusBadRequest, "level thresholds must be non-negative and strictly increasing")
			return
		}
	}
	if payload.RoleTrackCode == "" && len(payload.LevelThresholds) != 0 {
		writeError(w, http.StatusBadRequest, "a role track is required when thresholds are configured")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start level configuration update")
		return
	}
	defer tx.Rollback(r.Context())
	if payload.RoleTrackCode != "" {
		if err = tx.QueryRow(r.Context(), `select coalesce(array_agg(role_id order by position),'{}'::bigint[])
			from (select track_role.role_id,track_role.position from permission_role_tracks track
				join permission_role_track_roles track_role on track_role.track_code=track.code
				where track.code=$1 order by track_role.position for share of track,track_role) locked_roles`,
			payload.RoleTrackCode).Scan(&roleIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load role track")
			return
		}
		if len(roleIDs) == 0 {
			writeError(w, http.StatusBadRequest, "role track was not found")
			return
		}
		if len(payload.LevelThresholds) != len(roleIDs) {
			writeError(w, http.StatusBadRequest, "the number of levels must match the number of roles in the selected track")
			return
		}
	}
	var track any
	if payload.RoleTrackCode != "" {
		track = payload.RoleTrackCode
	}
	var configVersion int64
	if err = tx.QueryRow(r.Context(), `update level_system_config set role_track_code=$1,
		level_thresholds=$2,version=version+1,updated_by=$3,updated_at=now() where singleton
		returning version`, track, payload.LevelThresholds, currentClaims(r).Subject).Scan(&configVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save level configuration")
		return
	}
	if _, err = tx.Exec(r.Context(), `update level_recalculation_jobs set status='superseded',locked_by='',
		lease_expires_at=null,finished_at=now(),updated_at=now()
		where status in ('queued','processing')`); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to supersede the previous level recalculation")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into level_recalculation_jobs(
		config_version,role_track_code,level_thresholds,role_ids,created_by)
		values($1,$2,$3,$4,$5)`, configVersion, payload.RoleTrackCode,
		payload.LevelThresholds, roleIDs, currentClaims(r).Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue level recalculation")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit level configuration")
		return
	}
	payload.Version = configVersion
	payload.Recalculation = &levelRecalculationPayload{
		ConfigVersion: configVersion, Status: "queued", MaxAttempts: 8,
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) adminTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `select public_id,code,name,description,icon,translations,
		refresh_period,condition,rewards,status from task_definitions order by created_at,id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tasks")
		return
	}
	defer rows.Close()
	items := make([]taskPayload, 0)
	for rows.Next() {
		var item taskPayload
		var translationsRaw, conditionRaw, rewardsRaw []byte
		if err = rows.Scan(&item.PublicID, &item.Code, &item.Name, &item.Description, &item.Icon,
			&translationsRaw, &item.RefreshPeriod, &conditionRaw, &rewardsRaw, &item.Status); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode tasks")
			return
		}
		item.Translations, err = decodeStoredJSONObject(translationsRaw, "task translations")
		if err != nil {
			log.Printf("load admin task %s translations: %v", item.PublicID, err)
			writeError(w, http.StatusInternalServerError, "failed to decode task translations")
			return
		}
		item.Condition, item.Rewards, err = decodeTaskConfigurationMaps(conditionRaw, rewardsRaw)
		if err != nil {
			log.Printf("load admin task %s configuration: %v", item.PublicID, err)
			writeError(w, http.StatusInternalServerError, "failed to decode task configuration")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tasks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	s.saveTask(w, r, "")
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	s.saveTask(w, r, strings.ToLower(strings.TrimSpace(r.PathValue("publicId"))))
}

func (s *Server) saveTask(w http.ResponseWriter, r *http.Request, currentPublicID string) {
	var payload taskPayload
	if err := decodeJSON(r, &payload); err != nil {
		if errors.Is(err, errTaskCurrencyRewardDuplicate) {
			writeError(w, http.StatusBadRequest, errTaskCurrencyRewardDuplicate.Error())
		} else {
			writeError(w, http.StatusBadRequest, "invalid task")
		}
		return
	}
	payload.Code = normalizeCode(payload.Code)
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.Icon = strings.TrimSpace(payload.Icon)
	payload.RefreshPeriod = strings.ToLower(strings.TrimSpace(payload.RefreshPeriod))
	payload.Status = strings.ToLower(strings.TrimSpace(payload.Status))
	if payload.Status == "" {
		payload.Status = "active"
	}
	if payload.Code == "" || payload.Name == "" ||
		!stringIn(payload.RefreshPeriod, "never", "daily", "weekly", "monthly") ||
		!stringIn(payload.Status, "active", "disabled") {
		writeError(w, http.StatusBadRequest, "task fields are invalid")
		return
	}
	if err := validateTaskCondition(payload.Condition); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validateTaskRewards(r, payload.Rewards); err != nil {
		if errors.Is(err, errTaskRewardStoreUnavailable) {
			writeError(w, http.StatusInternalServerError, "failed to validate task rewards")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	translations, err := json.Marshal(payload.Translations)
	if err != nil {
		writeError(w, http.StatusBadRequest, "task translations are invalid")
		return
	}
	condition, err := json.Marshal(payload.Condition)
	if err != nil {
		writeError(w, http.StatusBadRequest, "task condition is invalid")
		return
	}
	rewards, err := json.Marshal(payload.Rewards)
	if err != nil {
		writeError(w, http.StatusBadRequest, "task rewards are invalid")
		return
	}
	var publicID string
	if currentPublicID == "" {
		err = s.db.QueryRow(r.Context(), `insert into task_definitions(
			code,name,description,icon,translations,refresh_period,condition,rewards,status,created_by
		) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) returning public_id`,
			payload.Code, payload.Name, payload.Description, payload.Icon, translations,
			payload.RefreshPeriod, condition, rewards, payload.Status, currentClaims(r).Subject).Scan(&publicID)
	} else {
		err = s.db.QueryRow(r.Context(), `update task_definitions set code=$2,name=$3,description=$4,
			icon=$5,translations=$6,refresh_period=$7,condition=$8,rewards=$9,status=$10,updated_at=now()
			where public_id=$1 returning public_id`,
			currentPublicID, payload.Code, payload.Name, payload.Description, payload.Icon, translations,
			payload.RefreshPeriod, condition, rewards, payload.Status).Scan(&publicID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task was not found")
		return
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "task code already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save task")
		return
	}
	payload.PublicID = publicID
	status := http.StatusOK
	if currentPublicID == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, payload)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	tag, err := s.db.Exec(r.Context(), `delete from task_definitions where public_id=$1`, publicID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete task")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "task was not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

type adminActivityEventRow struct {
	id                 string
	userID             *string
	username           *string
	actionCode         string
	actionName         string
	objectCode         string
	objectName         string
	objectPublicID     string
	markdownAddedBytes int
	occurredAt         time.Time
}

func collectAdminActivityEvents(rows checkedRows) ([]map[string]any, error) {
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var item adminActivityEventRow
		if err := rows.Scan(&item.id, &item.userID, &item.username, &item.actionCode, &item.actionName,
			&item.objectCode, &item.objectName, &item.objectPublicID, &item.markdownAddedBytes, &item.occurredAt); err != nil {
			return nil, fmt.Errorf("scan activity event: %w", err)
		}
		items = append(items, map[string]any{
			"id": item.id, "userId": item.userID, "username": item.username,
			"action": item.actionCode, "actionName": item.actionName,
			"objectType": item.objectCode, "objectTypeName": item.objectName,
			"objectPublicId": item.objectPublicID, "markdownAddedBytes": item.markdownAddedBytes,
			"occurredAt": item.occurredAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity events: %w", err)
	}
	return items, nil
}

func (s *Server) adminActivityEvents(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := boundedInt(query.Get("limit"), 100, 1, 500)
	offset := boundedInt(query.Get("offset"), 0, 0, 1_000_000)
	conditions := []string{"true"}
	args := make([]any, 0, 8)
	addCondition := func(sql string, value any) {
		args = append(args, value)
		conditions = append(conditions, strings.ReplaceAll(sql, "?", "$"+strconv.Itoa(len(args))))
	}
	if value := strings.TrimSpace(query.Get("userId")); value != "" {
		addCondition("account.public_id=?", strings.ToLower(value))
	}
	if value := normalizeCode(query.Get("action")); value != "" {
		addCondition("action.code=?", value)
	}
	if value := normalizeCode(query.Get("objectType")); value != "" {
		addCondition("object_type.code=?", value)
	}
	if value := strings.TrimSpace(query.Get("objectPublicId")); value != "" {
		addCondition("route.public_id=?", value)
	}
	if value := strings.TrimSpace(query.Get("from")); value != "" {
		if timestamp, err := time.Parse(time.RFC3339, value); err == nil {
			addCondition("event.occurred_at>=?", timestamp)
		}
	}
	if value := strings.TrimSpace(query.Get("to")); value != "" {
		if timestamp, err := time.Parse(time.RFC3339, value); err == nil {
			addCondition("event.occurred_at<=?", timestamp)
		}
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(r.Context(), `select event.id::text,account.public_id,account.username,
		action.code,action.name,object_type.code,object_type.name,coalesce(route.public_id,''),
		event.markdown_added_bytes,event.occurred_at
		from user_activity_events event
		left join users account on account.id=event.user_id
		join activity_actions action on action.id=event.action_id
		join activity_object_types object_type on object_type.id=event.object_type_id
		left join public_routes route on route.id=event.object_route_id
		where `+strings.Join(conditions, " and ")+`
		order by event.occurred_at desc,event.id desc
		limit $`+strconv.Itoa(len(args)-1)+` offset $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity events")
		return
	}
	items, err := collectAdminActivityEvents(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func validateTaskCondition(condition map[string]any) error {
	action := normalizeCode(stringValue(condition["action"]))
	objectType := normalizeCode(stringValue(condition["objectType"]))
	metric := normalizeCode(stringValue(condition["metric"]))
	target := int64Value(condition["target"])
	if action == "" || objectType == "" || !stringIn(metric, "count", "markdown_bytes") || target <= 0 {
		return errors.New("task condition requires action, objectType, metric and a positive target")
	}
	if !stringIn(action, "edit", "create", "view", "delete", "claim", "download", "upload", "purchase", "transfer", "checkin", "use") {
		return errors.New("task condition action is unsupported")
	}
	if !stringIn(objectType, "recipe", "mod", "resource", "blueprint", "plugin", "author", "team", "user", "comment", "tag", "file", "economy", "task", "shop_item", "modpack", "server", "map", "resource_pack", "shader_pack", "datapack", "addon", "community_post", "review", "skin", "player_profile") {
		return errors.New("task condition object type is unsupported")
	}
	condition["action"] = action
	condition["objectType"] = objectType
	condition["metric"] = metric
	condition["target"] = target
	if value := strings.TrimSpace(stringValue(condition["objectPublicId"])); value != "" {
		condition["objectPublicId"] = value
	}
	return nil
}

func (s *Server) validateTaskRewards(r *http.Request, rewards map[string]any) error {
	experience := int64Value(rewards["experience"])
	if experience < 0 {
		return errors.New("task experience reward cannot be negative")
	}
	rewards["experience"] = experience
	rawCurrencies, _ := rewards["currencies"].(map[string]any)
	currencies, err := normalizeTaskCurrencyRewards(rawCurrencies)
	if err != nil {
		return err
	}
	for code := range currencies {
		var exists bool
		if queryErr := s.db.QueryRow(r.Context(), `select exists(select 1 from currencies where code=$1 and status='active')`, code).
			Scan(&exists); queryErr != nil {
			return fmt.Errorf("%w: %v", errTaskRewardStoreUnavailable, queryErr)
		}
		if !exists {
			return errors.New("task reward references an unknown currency")
		}
	}
	rewards["currencies"] = currencies
	if experience == 0 && len(currencies) == 0 {
		return errors.New("task must provide at least one reward")
	}
	return nil
}

func decodeTaskConfigurationMaps(conditionRaw, rewardsRaw []byte) (map[string]any, map[string]any, error) {
	if err := progression.ValidateTaskConfiguration(conditionRaw, rewardsRaw); err != nil {
		return nil, nil, err
	}
	condition := make(map[string]any)
	if err := json.Unmarshal(conditionRaw, &condition); err != nil {
		return nil, nil, fmt.Errorf("decode task condition: %w", err)
	}
	rewards := make(map[string]any)
	if err := json.Unmarshal(rewardsRaw, &rewards); err != nil {
		return nil, nil, fmt.Errorf("decode task rewards: %w", err)
	}
	return condition, rewards, nil
}

func normalizeTaskCurrencyRewards(rawCurrencies map[string]any) (map[string]int64, error) {
	currencies := make(map[string]int64, len(rawCurrencies))
	for rawCode, value := range rawCurrencies {
		code := normalizeCode(rawCode)
		amount := int64Value(value)
		if code == "" || amount <= 0 {
			return nil, errors.New("task currency rewards must use a currency code and positive amount")
		}
		if _, duplicate := currencies[code]; duplicate {
			return nil, errTaskCurrencyRewardDuplicate
		}
		currencies[code] = amount
	}
	return currencies, nil
}

func stringIn(value string, allowed ...string) bool {
	index := sort.SearchStrings(appendSorted(allowed), value)
	sorted := appendSorted(allowed)
	return index < len(sorted) && sorted[index] == value
}

func appendSorted(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
