package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/progression"
)

type levelConfigPayload struct {
	RoleTrackCode   string  `json:"roleTrackCode"`
	LevelThresholds []int64 `json:"levelThresholds"`
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
		item.translations = map[string]any{}
		item.condition = map[string]any{}
		item.rewards = map[string]any{}
		_ = json.Unmarshal(translationsRaw, &item.translations)
		_ = json.Unmarshal(conditionRaw, &item.condition)
		_ = json.Unmarshal(rewardsRaw, &item.rewards)
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
	if err := s.db.QueryRow(r.Context(), `select role_track_code,level_thresholds
		from level_system_config where singleton`).Scan(&roleTrack, &thresholds); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load level configuration")
		return
	}
	code := ""
	if roleTrack != nil {
		code = *roleTrack
	}
	writeJSON(w, http.StatusOK, levelConfigPayload{RoleTrackCode: code, LevelThresholds: thresholds})
}

func (s *Server) updateLevelConfig(w http.ResponseWriter, r *http.Request) {
	var payload levelConfigPayload
	if decodeJSON(r, &payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid level configuration")
		return
	}
	payload.RoleTrackCode = strings.TrimSpace(payload.RoleTrackCode)
	for index, threshold := range payload.LevelThresholds {
		if threshold < 0 || (index > 0 && threshold <= payload.LevelThresholds[index-1]) {
			writeError(w, http.StatusBadRequest, "level thresholds must be non-negative and strictly increasing")
			return
		}
	}
	if payload.RoleTrackCode == "" {
		if len(payload.LevelThresholds) != 0 {
			writeError(w, http.StatusBadRequest, "a role track is required when thresholds are configured")
			return
		}
	} else {
		var roleCount int
		if err := s.db.QueryRow(r.Context(), `select count(*) from permission_role_track_roles
			where track_code=$1`, payload.RoleTrackCode).Scan(&roleCount); err != nil || roleCount == 0 {
			writeError(w, http.StatusBadRequest, "role track was not found")
			return
		}
		if len(payload.LevelThresholds) != roleCount {
			writeError(w, http.StatusBadRequest, "the number of levels must match the number of roles in the selected track")
			return
		}
	}
	var track any
	if payload.RoleTrackCode != "" {
		track = payload.RoleTrackCode
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start level configuration update")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `update level_system_config set role_track_code=$1,
		level_thresholds=$2,updated_by=$3,updated_at=now() where singleton`,
		track, payload.LevelThresholds, currentClaims(r).Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save level configuration")
		return
	}
	rows, err := tx.Query(r.Context(), `select user_id,experience from user_experience for update`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user experience")
		return
	}
	type experienceRow struct {
		userID     int64
		experience int64
	}
	users := make([]experienceRow, 0)
	for rows.Next() {
		var item experienceRow
		if err = rows.Scan(&item.userID, &item.experience); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to decode user experience")
			return
		}
		users = append(users, item)
	}
	rows.Close()
	for _, user := range users {
		level := progression.LevelForExperience(user.experience, payload.LevelThresholds)
		if _, err = tx.Exec(r.Context(), `update user_experience set level=$2,updated_at=now() where user_id=$1`,
			user.userID, level); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update user level")
			return
		}
		if payload.RoleTrackCode != "" {
			if err = progression.SyncTrackRole(r.Context(), tx, user.userID, payload.RoleTrackCode, level); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to synchronize level role")
				return
			}
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit level configuration")
		return
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
		item.Translations = map[string]any{}
		item.Condition = map[string]any{}
		item.Rewards = map[string]any{}
		_ = json.Unmarshal(translationsRaw, &item.Translations)
		_ = json.Unmarshal(conditionRaw, &item.Condition)
		_ = json.Unmarshal(rewardsRaw, &item.Rewards)
		items = append(items, item)
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
	if decodeJSON(r, &payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid task")
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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	translations, _ := json.Marshal(payload.Translations)
	condition, _ := json.Marshal(payload.Condition)
	rewards, _ := json.Marshal(payload.Rewards)
	var publicID string
	var err error
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
		addCondition("event.object_public_id=?", value)
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
	rows, err := s.db.Query(r.Context(), `select event.public_id,account.public_id,account.username,
		action.code,action.name,object_type.code,object_type.name,event.object_public_id,
		event.markdown_added_bytes,event.metadata,event.occurred_at
		from user_activity_events event
		left join users account on account.id=event.user_id
		join activity_actions action on action.id=event.action_id
		join activity_object_types object_type on object_type.id=event.object_type_id
		where `+strings.Join(conditions, " and ")+`
		order by event.occurred_at desc,event.id desc
		limit $`+strconv.Itoa(len(args)-1)+` offset $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity events")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id string
		var userID *string
		var username *string
		var actionCode, actionName, objectCode, objectName, objectPublicID string
		var markdownAddedBytes int
		var metadataRaw []byte
		var occurredAt time.Time
		if err = rows.Scan(&id, &userID, &username, &actionCode, &actionName, &objectCode, &objectName,
			&objectPublicID, &markdownAddedBytes, &metadataRaw, &occurredAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode activity events")
			return
		}
		metadata := map[string]any{}
		_ = json.Unmarshal(metadataRaw, &metadata)
		items = append(items, map[string]any{
			"id": id, "userId": userID, "username": username,
			"action": actionCode, "actionName": actionName,
			"objectType": objectCode, "objectTypeName": objectName,
			"objectPublicId": objectPublicID, "markdownAddedBytes": markdownAddedBytes,
			"metadata": metadata, "occurredAt": occurredAt,
		})
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
	if !stringIn(objectType, "recipe", "mod", "resource", "blueprint", "plugin", "author", "team", "user", "comment", "tag", "file", "economy", "task", "shop_item") {
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
	currencies := make(map[string]int64, len(rawCurrencies))
	for code, value := range rawCurrencies {
		code = normalizeCode(code)
		amount := int64Value(value)
		if code == "" || amount <= 0 {
			return errors.New("task currency rewards must use a currency code and positive amount")
		}
		var exists bool
		if err := s.db.QueryRow(r.Context(), `select exists(select 1 from currencies where code=$1 and status='active')`, code).
			Scan(&exists); err != nil || !exists {
			return errors.New("task reward references an unknown currency")
		}
		currencies[code] = amount
	}
	rewards["currencies"] = currencies
	if experience == 0 && len(currencies) == 0 {
		return errors.New("task must provide at least one reward")
	}
	return nil
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
