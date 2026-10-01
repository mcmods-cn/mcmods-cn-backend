package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

const activityRetentionSettingKey = "activity.retention"

var activityActionIDs = map[string]int16{
	"edit": 1, "create": 2, "view": 3, "delete": 4, "claim": 5, "download": 6,
	"upload": 7, "purchase": 8, "transfer": 9, "checkin": 10, "use": 11,
}

var activityObjectTypeIDs = map[string]int16{
	"recipe": 1, "mod": 2, "blueprint": 3, "plugin": 4, "author": 5, "team": 6, "user": 7,
	"comment": 8, "tag": 9, "file": 10, "economy": 11, "task": 12, "shop_item": 13, "resource": 14,
	"modpack": 15, "server": 16, "map": 17, "resource_pack": 18, "shader_pack": 19, "datapack": 20,
	"addon": 21, "community_post": 22, "review": 23, "skin": 24, "player_profile": 25, "changelog": 26, "rating": 27,
}

var activityObjectRouteTypes = map[string]string{
	"server": "minecraft_server",
}

type activityRetentionPolicy struct {
	Enabled       bool `json:"enabled"`
	AllowDelete   bool `json:"allowDelete"`
	RetentionDays int  `json:"retentionDays"`
	BatchSize     int  `json:"batchSize"`
}

type activityRetentionConfig struct {
	Enabled            bool                               `json:"enabled"`
	RunIntervalMinutes int                                `json:"runIntervalMinutes"`
	Default            activityRetentionPolicy            `json:"default"`
	Actions            map[string]activityRetentionPolicy `json:"actions"`
}

type activityObjectFilter struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type activityCleanupFilterRequest struct {
	Users       []string               `json:"users"`
	Objects     []activityObjectFilter `json:"objects"`
	ObjectTypes []string               `json:"objectTypes"`
	Actions     []string               `json:"actions"`
	From        string                 `json:"from"`
	To          string                 `json:"to"`
}

type normalizedActivityCleanupFilter struct {
	UserIDs        []int64                      `json:"userIds,omitempty"`
	ObjectRouteIDs []int64                      `json:"objectRouteIds,omitempty"`
	ObjectTypeIDs  []int16                      `json:"objectTypeIds,omitempty"`
	ActionIDs      []int16                      `json:"actionIds,omitempty"`
	From           *time.Time                   `json:"from,omitempty"`
	To             *time.Time                   `json:"to,omitempty"`
	SnapshotBefore time.Time                    `json:"snapshotBefore"`
	BatchSize      int                          `json:"batchSize"`
	Summary        activityCleanupFilterRequest `json:"summary"`
}

type activityCleanupExecuteRequest struct {
	PreviewID         string `json:"previewId"`
	ConfirmationToken string `json:"confirmationToken"`
	Confirmation      string `json:"confirmation"`
}

func defaultActivityRetentionConfig() activityRetentionConfig {
	return activityRetentionConfig{
		Enabled: false, RunIntervalMinutes: 60,
		Default: activityRetentionPolicy{Enabled: false, AllowDelete: false, RetentionDays: 365, BatchSize: 1000},
		Actions: map[string]activityRetentionPolicy{
			"view": {Enabled: true, AllowDelete: true, RetentionDays: 30, BatchSize: 2000},
		},
	}
}

func normalizeActivityRetentionConfig(config activityRetentionConfig) (activityRetentionConfig, error) {
	if config.RunIntervalMinutes == 0 {
		config.RunIntervalMinutes = 60
	}
	if config.RunIntervalMinutes < 10 || config.RunIntervalMinutes > 1440 {
		return config, errors.New("run interval must be between 10 and 1440 minutes")
	}
	normalize := func(policy activityRetentionPolicy) (activityRetentionPolicy, error) {
		if policy.RetentionDays == 0 {
			policy.RetentionDays = 365
		}
		if policy.BatchSize == 0 {
			policy.BatchSize = 1000
		}
		if policy.RetentionDays < 1 || policy.RetentionDays > 3650 {
			return policy, errors.New("retention days must be between 1 and 3650")
		}
		if policy.BatchSize < 100 || policy.BatchSize > 5000 {
			return policy, errors.New("cleanup batch size must be between 100 and 5000")
		}
		return policy, nil
	}
	var err error
	config.Default, err = normalize(config.Default)
	if err != nil {
		return config, err
	}
	if config.Actions == nil {
		config.Actions = map[string]activityRetentionPolicy{}
	}
	for action, policy := range config.Actions {
		if _, allowed := activityActionIDs[action]; !allowed {
			return config, fmt.Errorf("activity action %q is not allowed", action)
		}
		policy, err = normalize(policy)
		if err != nil {
			return config, fmt.Errorf("activity action %s: %w", action, err)
		}
		config.Actions[action] = policy
	}
	return config, nil
}

func (s *Server) getActivityRetentionConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.activityRetentionConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity retention configuration")
		return
	}
	actions := make([]string, 0, len(activityActionIDs))
	for action := range activityActionIDs {
		actions = append(actions, action)
	}
	objectTypes := make([]string, 0, len(activityObjectTypeIDs))
	for objectType := range activityObjectTypeIDs {
		objectTypes = append(objectTypes, objectType)
	}
	sort.Strings(actions)
	sort.Strings(objectTypes)
	writeJSON(w, http.StatusOK, map[string]any{"config": config, "actions": actions, "objectTypes": objectTypes})
}

func (s *Server) updateActivityRetentionConfig(w http.ResponseWriter, r *http.Request) {
	var config activityRetentionConfig
	if err := decodeJSON(r, &config); err != nil {
		writeError(w, http.StatusBadRequest, "invalid activity retention configuration")
		return
	}
	config, err := normalizeActivityRetentionConfig(config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, _ := json.Marshal(config)
	_, err = s.db.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at) values($1,$2::jsonb,$3,now())
		on conflict(key) do update set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`,
		activityRetentionSettingKey, raw, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save activity retention configuration")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) activityRetentionConfig(ctx context.Context) (activityRetentionConfig, error) {
	config := defaultActivityRetentionConfig()
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key=$1`, activityRetentionSettingKey).Scan(&raw)
	if err == pgx.ErrNoRows {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		return defaultActivityRetentionConfig(), err
	}
	return normalizeActivityRetentionConfig(config)
}

func parseCleanupBoundary(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("cleanup time boundaries must use RFC3339 with a timezone")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func (s *Server) normalizeActivityCleanupFilter(ctx context.Context, request activityCleanupFilterRequest, snapshotBefore time.Time) (normalizedActivityCleanupFilter, error) {
	if len(request.Users) > 50 || len(request.Objects) > 100 || len(request.Actions) > len(activityActionIDs) || len(request.ObjectTypes) > len(activityObjectTypeIDs) {
		return normalizedActivityCleanupFilter{}, errors.New("too many cleanup filter values")
	}
	result := normalizedActivityCleanupFilter{SnapshotBefore: snapshotBefore.UTC(), BatchSize: 1000, Summary: request}
	result.Summary.Actions = nil
	var err error
	result.From, err = parseCleanupBoundary(request.From)
	if err != nil {
		return result, err
	}
	result.To, err = parseCleanupBoundary(request.To)
	if err != nil {
		return result, err
	}
	if result.From != nil && result.To != nil && result.From.After(*result.To) {
		return result, errors.New("cleanup start time cannot be after end time")
	}
	seenStrings := map[string]bool{}
	for _, action := range request.Actions {
		action = strings.ToLower(strings.TrimSpace(action))
		id, ok := activityActionIDs[action]
		if !ok {
			return result, fmt.Errorf("activity action %q is not allowed", action)
		}
		if !seenStrings["a:"+action] {
			result.ActionIDs = append(result.ActionIDs, id)
			result.Summary.Actions = append(result.Summary.Actions, action)
			seenStrings["a:"+action] = true
		}
	}
	for _, objectType := range request.ObjectTypes {
		objectType = strings.ToLower(strings.TrimSpace(objectType))
		id, ok := activityObjectTypeIDs[objectType]
		if !ok {
			return result, fmt.Errorf("activity object type %q is not allowed", objectType)
		}
		if !seenStrings["t:"+objectType] {
			result.ObjectTypeIDs = append(result.ObjectTypeIDs, id)
			seenStrings["t:"+objectType] = true
		}
	}
	for _, publicID := range request.Users {
		publicID = strings.ToLower(strings.TrimSpace(publicID))
		if publicID == "" || seenStrings["u:"+publicID] {
			continue
		}
		var userID int64
		if err = s.db.QueryRow(ctx, `select id from users where public_id=$1`, publicID).Scan(&userID); err != nil {
			return result, fmt.Errorf("cleanup user %q does not exist", publicID)
		}
		result.UserIDs = append(result.UserIDs, userID)
		seenStrings["u:"+publicID] = true
	}
	for _, object := range request.Objects {
		object.Type = strings.ToLower(strings.TrimSpace(object.Type))
		object.ID = strings.ToLower(strings.TrimSpace(object.ID))
		key := "o:" + object.Type + ":" + object.ID
		if seenStrings[key] {
			continue
		}
		if _, allowed := activityObjectTypeIDs[object.Type]; !allowed {
			return result, fmt.Errorf("activity object type %q is not allowed", object.Type)
		}
		routeType := object.Type
		if mapped := activityObjectRouteTypes[object.Type]; mapped != "" {
			routeType = mapped
		}
		var routeID int64
		if err = s.db.QueryRow(ctx, `select id from public_routes where entity_type=$1 and public_id=$2`, routeType, object.ID).Scan(&routeID); err != nil {
			return result, fmt.Errorf("cleanup object %s/%s does not exist", object.Type, object.ID)
		}
		result.ObjectRouteIDs = append(result.ObjectRouteIDs, routeID)
		seenStrings[key] = true
	}
	return result, nil
}

func activityCleanupWhere(filter normalizedActivityCleanupFilter, firstPlaceholder int) (string, []any) {
	conditions := make([]string, 0, 6)
	args := make([]any, 0, 6)
	add := func(sql string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(sql, firstPlaceholder+len(args)-1))
	}
	add("event.occurred_at<$%d", filter.SnapshotBefore)
	if filter.From != nil {
		add("event.occurred_at>=$%d", *filter.From)
	}
	if filter.To != nil {
		add("event.occurred_at<$%d", *filter.To)
	}
	if len(filter.UserIDs) > 0 {
		add("event.user_id=any($%d::bigint[])", filter.UserIDs)
	}
	if len(filter.ObjectRouteIDs) > 0 {
		add("event.object_route_id=any($%d::bigint[])", filter.ObjectRouteIDs)
	}
	if len(filter.ObjectTypeIDs) > 0 {
		add("event.object_type_id=any($%d::smallint[])", filter.ObjectTypeIDs)
	}
	if len(filter.ActionIDs) > 0 {
		add("event.action_id=any($%d::smallint[])", filter.ActionIDs)
	}
	return strings.Join(conditions, " and "), args
}

func (s *Server) previewActivityCleanup(w http.ResponseWriter, r *http.Request) {
	var request activityCleanupFilterRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid activity cleanup filter")
		return
	}
	filter, err := s.normalizeActivityCleanupFilter(r.Context(), request, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(filter.ActionIDs) == 0 {
		writeError(w, http.StatusBadRequest, "at least one allowed activity action is required")
		return
	}
	config, err := s.activityRetentionConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity cleanup policy")
		return
	}
	for _, action := range filter.Summary.Actions {
		policy := config.Default
		if override, exists := config.Actions[action]; exists {
			policy = override
		}
		if !policy.AllowDelete {
			writeError(w, http.StatusForbidden, "activity action is protected by the retention policy: "+action)
			return
		}
		if policy.BatchSize < filter.BatchSize {
			filter.BatchSize = policy.BatchSize
		}
	}
	where, args := activityCleanupWhere(filter, 1)
	var total int64
	if err = s.db.QueryRow(r.Context(), `select count(*) from user_activity_events event where `+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preview activity cleanup")
		return
	}
	byAction, err := s.activityCleanupGroupedCounts(r.Context(), where, args, "action.code", "join activity_actions action on action.id=event.action_id")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to group activity cleanup preview")
		return
	}
	byObjectType, err := s.activityCleanupGroupedCounts(r.Context(), where, args, "object_type.code", "join activity_object_types object_type on object_type.id=event.object_type_id")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to group activity cleanup preview")
		return
	}
	byUser, err := s.activityCleanupGroupedCounts(r.Context(), where, args, "coalesce(account.public_id,'anonymous')", "left join users account on account.id=event.user_id")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to group activity cleanup preview")
		return
	}
	samples, err := s.activityCleanupSamples(r.Context(), where, args)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity cleanup samples")
		return
	}
	token, tokenHash, err := newCleanupConfirmationToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create cleanup confirmation")
		return
	}
	rawFilter, _ := json.Marshal(filter)
	var previewID string
	err = s.db.QueryRow(r.Context(), `insert into activity_cleanup_runs(source,initiated_by,status,filters,confirmation_hash,matched_count,expires_at)
		values('manual',$1,'preview',$2::jsonb,$3,$4,now()+interval '15 minutes') returning public_id`,
		currentClaims(r).Subject, rawFilter, tokenHash, total).Scan(&previewID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save activity cleanup preview")
		return
	}
	dangerous := total >= 100000 || len(filter.UserIDs)+len(filter.ObjectRouteIDs)+len(filter.ObjectTypeIDs) == 0 && filter.From == nil && filter.To == nil
	writeJSON(w, http.StatusOK, map[string]any{
		"previewId": previewID, "confirmationToken": token, "confirmationText": "DELETE " + strconv.FormatInt(total, 10),
		"dangerous": dangerous, "matchedCount": total, "byAction": byAction, "byObjectType": byObjectType,
		"byUser": byUser, "samples": samples, "filters": request, "snapshotBefore": filter.SnapshotBefore, "expiresInSeconds": 900,
	})
}

func (s *Server) activityCleanupSamples(ctx context.Context, where string, args []any) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select event.id,
		case when account.id is null then 'anonymous' else substr(md5(account.public_id),1,8) end user_ref,
		action.code action,object_type.code object_type,
		case when route.id is null then '' else substr(md5(route.public_id),1,8) end object_ref,event.occurred_at
		from user_activity_events event join activity_actions action on action.id=event.action_id
		join activity_object_types object_type on object_type.id=event.object_type_id
		left join users account on account.id=event.user_id left join public_routes route on route.id=event.object_route_id
		where `+where+` order by event.occurred_at,event.id limit 10`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	samples := make([]map[string]any, 0, 10)
	for rows.Next() {
		var id int64
		var userRef, action, objectType, objectRef string
		var occurredAt time.Time
		if err = rows.Scan(&id, &userRef, &action, &objectType, &objectRef, &occurredAt); err != nil {
			return nil, err
		}
		samples = append(samples, map[string]any{
			"id": id, "user_ref": userRef, "action": action, "object_type": objectType,
			"object_ref": objectRef, "occurred_at": occurredAt,
		})
	}
	return samples, rows.Err()
}

func (s *Server) activityCleanupGroupedCounts(ctx context.Context, where string, args []any, groupExpression, join string) (map[string]int64, error) {
	result := map[string]int64{}
	// A cleanup spanning many users must not build an unbounded response. The
	// action/object dimensions are finite; for users this returns the 100
	// largest groups, which is sufficient for a destructive-action preview.
	rows, err := s.db.Query(ctx, `select `+groupExpression+`,count(*) from user_activity_events event `+join+` where `+where+` group by 1 order by 2 desc,1 limit 100`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int64
		if err = rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		result[key] = count
	}
	return result, rows.Err()
}

func newCleanupConfirmationToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(digest[:]), nil
}

func cleanupTokenMatches(token, expectedHash string) bool {
	digest := sha256.Sum256([]byte(token))
	actual, err := hex.DecodeString(expectedHash)
	if err != nil || len(actual) != len(digest) {
		return false
	}
	return subtle.ConstantTimeCompare(actual, digest[:]) == 1
}

func (s *Server) executeActivityCleanup(w http.ResponseWriter, r *http.Request) {
	var request activityCleanupExecuteRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid activity cleanup confirmation")
		return
	}
	request.PreviewID = strings.ToLower(strings.TrimSpace(request.PreviewID))
	var rawFilter []byte
	var expectedHash, status string
	var matched int64
	err := s.db.QueryRow(r.Context(), `select filters,confirmation_hash,status,matched_count from activity_cleanup_runs
		where public_id=$1 and source='manual' and initiated_by=$2 and expires_at>now()`, request.PreviewID, currentClaims(r).Subject).
		Scan(&rawFilter, &expectedHash, &status, &matched)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "activity cleanup preview does not exist or expired")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity cleanup preview")
		return
	}
	if status != "preview" || !cleanupTokenMatches(request.ConfirmationToken, expectedHash) || request.Confirmation != "DELETE "+strconv.FormatInt(matched, 10) {
		writeError(w, http.StatusConflict, "activity cleanup confirmation does not match the preview")
		return
	}
	var filter normalizedActivityCleanupFilter
	if err = json.Unmarshal(rawFilter, &filter); err != nil {
		writeError(w, http.StatusInternalServerError, "invalid stored cleanup filter")
		return
	}
	config, err := s.activityRetentionConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reload activity cleanup policy")
		return
	}
	for _, action := range filter.Summary.Actions {
		policy := config.Default
		if override, exists := config.Actions[action]; exists {
			policy = override
		}
		if !policy.AllowDelete {
			writeError(w, http.StatusForbidden, "activity action is no longer allowed to be deleted: "+action)
			return
		}
	}
	tag, err := s.db.Exec(r.Context(), `update activity_cleanup_runs set status='running',started_at=now()
		where public_id=$1 and status='preview'`, request.PreviewID)
	if err != nil || tag.RowsAffected() != 1 {
		writeError(w, http.StatusConflict, "activity cleanup preview was already used")
		return
	}
	deleted, cleanupErr := deleteActivityEventsInBatches(r.Context(), s.db, filter)
	if cleanupErr != nil {
		_, _ = s.db.Exec(context.Background(), `update activity_cleanup_runs set status='failed',deleted_count=$2,finished_at=now(),error_message=$3 where public_id=$1`, request.PreviewID, deleted, cleanupErr.Error())
		writeError(w, http.StatusInternalServerError, "activity cleanup failed")
		return
	}
	if _, err = s.db.Exec(r.Context(), `update activity_cleanup_runs set status='completed',deleted_count=$2,finished_at=now(),confirmation_hash='' where public_id=$1`, request.PreviewID, deleted); err != nil {
		writeError(w, http.StatusInternalServerError, "activity deletion finished but failed to record completion")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"previewId": request.PreviewID, "matchedCount": matched, "deletedCount": deleted, "status": "completed"})
}

func deleteActivityEventsInBatches(ctx context.Context, db *pgxpool.Pool, filter normalizedActivityCleanupFilter) (int64, error) {
	where, args := activityCleanupWhere(filter, 1)
	args = append(args, filter.BatchSize)
	limitPlaceholder := len(args)
	var deleted int64
	for ctx.Err() == nil {
		tag, err := db.Exec(ctx, `delete from user_activity_events where id in (
			select event.id from user_activity_events event where `+where+` order by event.occurred_at,event.id limit $`+strconv.Itoa(limitPlaceholder)+`)`, args...)
		if err != nil {
			return deleted, err
		}
		deleted += tag.RowsAffected()
		if tag.RowsAffected() < int64(filter.BatchSize) {
			return deleted, nil
		}
	}
	return deleted, ctx.Err()
}

type ActivityRetentionWorker struct{ db *pgxpool.Pool }

func NewActivityRetentionWorker(db *pgxpool.Pool) *ActivityRetentionWorker {
	return &ActivityRetentionWorker{db: db}
}

func (worker *ActivityRetentionWorker) Start(ctx context.Context) {
	if worker == nil || worker.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *ActivityRetentionWorker) run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	worker.prune(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.prune(ctx)
		}
	}
}

func (worker *ActivityRetentionWorker) prune(ctx context.Context) {
	conn, err := worker.db.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `select pg_try_advisory_lock(hashtext('mcmods-activity-retention'))`).Scan(&locked); err != nil || !locked {
		return
	}
	defer conn.Exec(context.Background(), `select pg_advisory_unlock(hashtext('mcmods-activity-retention'))`)
	config := defaultActivityRetentionConfig()
	var raw []byte
	if err := worker.db.QueryRow(ctx, `select value from system_settings where key=$1`, activityRetentionSettingKey).Scan(&raw); err == nil {
		if json.Unmarshal(raw, &config) != nil {
			return
		}
	}
	config, err = normalizeActivityRetentionConfig(config)
	if err != nil || !config.Enabled {
		return
	}
	var recentlyRan bool
	if err = worker.db.QueryRow(ctx, `select exists(select 1 from activity_cleanup_runs where source='automatic' and started_at>now()-make_interval(mins=>$1))`, config.RunIntervalMinutes).Scan(&recentlyRan); err != nil || recentlyRan {
		return
	}
	for action, id := range activityActionIDs {
		policy := config.Default
		if override, exists := config.Actions[action]; exists {
			policy = override
		}
		if !policy.Enabled || !policy.AllowDelete {
			continue
		}
		filter := normalizedActivityCleanupFilter{ActionIDs: []int16{id}, To: timePointer(time.Now().UTC().AddDate(0, 0, -policy.RetentionDays)), SnapshotBefore: time.Now().UTC(), BatchSize: policy.BatchSize,
			Summary: activityCleanupFilterRequest{Actions: []string{action}}}
		rawFilter, _ := json.Marshal(filter)
		var runID string
		if worker.db.QueryRow(ctx, `insert into activity_cleanup_runs(source,status,filters) values('automatic','running',$1::jsonb) returning public_id`, rawFilter).Scan(&runID) != nil {
			continue
		}
		deleted, cleanupErr := deleteActivityEventsInBatches(ctx, worker.db, filter)
		status, message := "completed", ""
		if cleanupErr != nil {
			status, message = "failed", cleanupErr.Error()
		}
		if _, err = worker.db.Exec(ctx, `update activity_cleanup_runs set status=$2,deleted_count=$3,finished_at=now(),error_message=$4 where public_id=$1`, runID, status, deleted, message); err != nil {
			log.Printf("automatic activity cleanup %s failed to record completion", action)
			return
		}
		if cleanupErr != nil {
			log.Printf("automatic activity cleanup %s failed: %v", action, cleanupErr)
		}
	}
}

func timePointer(value time.Time) *time.Time { return &value }
