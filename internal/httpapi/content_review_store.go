package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const currentContentSchemaVersion = 1

type createContentRevisionParams struct {
	EntityType    string
	EntityID      int64
	AggregateType string
	AggregateKey  string
	BaseRevision  *int64
	Snapshot      []byte
	Reason        string
	ActorID       int64
	Source        string
	Status        string
	Metadata      map[string]any
	Request       *http.Request
}

type createdContentRevision struct {
	RevisionID            int64
	RevisionPublicID      string
	RevisionNo            int64
	ChangeRequestID       int64
	ChangeRequestPublicID string
	SnapshotHash          string
}

type contentChange struct {
	Path      string
	Operation string
	Before    any
	After     any
}

func createContentRevisionTx(ctx context.Context, tx pgx.Tx, params createContentRevisionParams) (createdContentRevision, error) {
	var result createdContentRevision
	params.AggregateType = strings.TrimSpace(params.AggregateType)
	params.AggregateKey = strings.TrimSpace(params.AggregateKey)
	params.EntityType = strings.TrimSpace(params.EntityType)
	if params.AggregateType == "" || params.AggregateKey == "" || len(params.Snapshot) == 0 {
		return result, fmt.Errorf("invalid content revision")
	}
	if (params.EntityID > 0) != (params.EntityType != "") {
		return result, fmt.Errorf("content revision entity type and id must be provided together")
	}
	if params.Source == "" {
		params.Source = "user"
	}
	if params.Status == "" {
		params.Status = "pending"
	}

	lockKey := params.AggregateType + ":" + params.AggregateKey
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return result, fmt.Errorf("lock content aggregate: %w", err)
	}
	if err := tx.QueryRow(ctx,
		`select coalesce(max(revision_no),0)+1 from content_revisions where aggregate_type=$1 and aggregate_key=$2`,
		params.AggregateType, params.AggregateKey,
	).Scan(&result.RevisionNo); err != nil {
		return result, fmt.Errorf("allocate revision number: %w", err)
	}

	actorSnapshot, err := actorSnapshotTx(ctx, tx, params.ActorID)
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(params.Snapshot)
	result.SnapshotHash = hex.EncodeToString(hash[:])
	if err = tx.QueryRow(ctx, `
		insert into content_revisions(
			entity_type,entity_id,aggregate_type,aggregate_key,revision_no,base_revision_id,schema_version,snapshot,snapshot_hash,
			created_by,created_by_snapshot,source
		) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) returning id,public_id`,
		nullableEntityType(params.EntityType), nullableEntityID(params.EntityID), params.AggregateType, params.AggregateKey, result.RevisionNo, params.BaseRevision,
		currentContentSchemaVersion, params.Snapshot, result.SnapshotHash, nullableActorID(params.ActorID), actorSnapshot, params.Source,
	).Scan(&result.RevisionID, &result.RevisionPublicID); err != nil {
		return result, fmt.Errorf("insert content revision: %w", err)
	}

	metadata, err := json.Marshal(nonNilMap(params.Metadata))
	if err != nil {
		return result, fmt.Errorf("encode change request metadata: %w", err)
	}
	resolvedAt := any(nil)
	if params.Status != "pending" {
		resolvedAt = time.Now().UTC()
	}
	if err = tx.QueryRow(ctx, `
		insert into change_requests(
			entity_type,entity_id,aggregate_type,aggregate_key,base_revision_id,proposed_revision_id,status,reason,
			submitted_by,submitted_by_snapshot,metadata,resolved_at
		) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) returning id,public_id`,
		nullableEntityType(params.EntityType), nullableEntityID(params.EntityID), params.AggregateType, params.AggregateKey, params.BaseRevision, result.RevisionID, params.Status,
		params.Reason, nullableActorID(params.ActorID), actorSnapshot, metadata, resolvedAt,
	).Scan(&result.ChangeRequestID, &result.ChangeRequestPublicID); err != nil {
		return result, fmt.Errorf("insert change request: %w", err)
	}

	ip, userAgent, traceID := auditRequestValues(params.Request)
	if _, err = tx.Exec(ctx, `
		insert into review_events(change_request_id,event_type,actor_id,actor_snapshot,ip,user_agent,metadata)
		values($1,'submitted',$2,$3,$4,$5,$6)`,
		result.ChangeRequestID, nullableActorID(params.ActorID), actorSnapshot, ip, userAgent, metadata,
	); err != nil {
		return result, fmt.Errorf("insert submit review event: %w", err)
	}

	if err = storeContentChangesTx(ctx, tx, result.RevisionID, params.BaseRevision, params.Snapshot); err != nil {
		return result, err
	}
	if err = appendAuditEventTx(ctx, tx, auditEventParams{
		EntityType:    params.EntityType,
		EntityID:      params.EntityID,
		AggregateType: params.AggregateType,
		AggregateKey:  params.AggregateKey,
		ActorID:       params.ActorID,
		ActorSnapshot: actorSnapshot,
		Action:        "content.revision.submitted",
		AfterHash:     result.SnapshotHash,
		TraceID:       traceID,
		IP:            ip,
		UserAgent:     userAgent,
		Metadata:      params.Metadata,
	}); err != nil {
		return result, err
	}
	return result, nil
}

func withdrawPendingContentRequestsTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateKey string, actorID int64, request *http.Request) error {
	rows, err := tx.Query(ctx, `
		update change_requests set status='withdrawn',resolved_at=now()
		where aggregate_type=$1 and aggregate_key=$2 and status='pending'
		returning id`, aggregateType, aggregateKey)
	if err != nil {
		return fmt.Errorf("withdraw pending content requests: %w", err)
	}
	defer rows.Close()
	requestIDs := make([]int64, 0)
	for rows.Next() {
		var requestID int64
		if err = rows.Scan(&requestID); err != nil {
			return fmt.Errorf("scan withdrawn content request: %w", err)
		}
		requestIDs = append(requestIDs, requestID)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate withdrawn content requests: %w", err)
	}
	if len(requestIDs) == 0 {
		return nil
	}
	actorSnapshot, err := actorSnapshotTx(ctx, tx, actorID)
	if err != nil {
		return err
	}
	ip, userAgent, _ := auditRequestValues(request)
	for _, requestID := range requestIDs {
		if _, err = tx.Exec(ctx, `
			insert into review_events(change_request_id,event_type,actor_id,actor_snapshot,note,ip,user_agent)
			values($1,'withdrawn',$2,$3,'Superseded by a newer initial submission',$4,$5)`,
			requestID, nullableActorID(actorID), actorSnapshot, ip, userAgent); err != nil {
			return fmt.Errorf("record withdrawn content request: %w", err)
		}
	}
	return nil
}

type auditEventParams struct {
	EntityType    string
	EntityID      int64
	AggregateType string
	AggregateKey  string
	ActorID       int64
	ActorSnapshot string
	Action        string
	BeforeHash    string
	AfterHash     string
	TraceID       string
	IP            string
	UserAgent     string
	Metadata      map[string]any
}

func appendAuditEventTx(ctx context.Context, tx pgx.Tx, params auditEventParams) error {
	metadata, err := json.Marshal(nonNilMap(params.Metadata))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		insert into audit_events(
			entity_type,entity_id,aggregate_type,aggregate_key,actor_id,actor_snapshot,action,before_hash,after_hash,
			trace_id,ip,user_agent,metadata
		) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		nullableEntityType(params.EntityType), nullableEntityID(params.EntityID), params.AggregateType, params.AggregateKey, nullableActorID(params.ActorID), params.ActorSnapshot,
		params.Action, params.BeforeHash, params.AfterHash, params.TraceID, params.IP, params.UserAgent, metadata,
	)
	return err
}

func storeContentChangesTx(ctx context.Context, tx pgx.Tx, revisionID int64, baseRevisionID *int64, snapshot []byte) error {
	var before any
	if baseRevisionID != nil {
		var raw []byte
		if err := tx.QueryRow(ctx, `select snapshot from content_revisions where id=$1`, *baseRevisionID).Scan(&raw); err != nil {
			return fmt.Errorf("read base revision: %w", err)
		}
		if err := json.Unmarshal(raw, &before); err != nil {
			return fmt.Errorf("decode base revision: %w", err)
		}
	}
	var after any
	if err := json.Unmarshal(snapshot, &after); err != nil {
		return fmt.Errorf("decode proposed revision: %w", err)
	}
	changes := diffJSON("", before, after)
	for _, change := range changes {
		beforeJSON, err := nullableJSON(change.Before)
		if err != nil {
			return err
		}
		afterJSON, err := nullableJSON(change.After)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `
			insert into content_change_items(revision_id,path,operation,before_value,after_value)
			values($1,$2,$3,$4,$5)`,
			revisionID, change.Path, change.Operation, beforeJSON, afterJSON,
		); err != nil {
			return fmt.Errorf("insert content change item: %w", err)
		}
	}
	return nil
}

func diffJSON(path string, before, after any) []contentChange {
	if jsonValuesEqual(before, after) {
		return nil
	}
	beforeMap, beforeIsMap := before.(map[string]any)
	afterMap, afterIsMap := after.(map[string]any)
	if beforeIsMap && afterIsMap {
		keys := make([]string, 0, len(beforeMap)+len(afterMap))
		seen := map[string]bool{}
		for key := range beforeMap {
			seen[key] = true
			keys = append(keys, key)
		}
		for key := range afterMap {
			if !seen[key] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		changes := make([]contentChange, 0)
		for _, key := range keys {
			childPath := path + "/" + escapeJSONPointer(key)
			beforeValue, beforeExists := beforeMap[key]
			afterValue, afterExists := afterMap[key]
			switch {
			case !beforeExists:
				changes = append(changes, contentChange{Path: childPath, Operation: "add", After: afterValue})
			case !afterExists:
				changes = append(changes, contentChange{Path: childPath, Operation: "remove", Before: beforeValue})
			default:
				changes = append(changes, diffJSON(childPath, beforeValue, afterValue)...)
			}
		}
		return changes
	}
	operation := "replace"
	if before == nil {
		operation = "add"
	} else if after == nil {
		operation = "remove"
	}
	if path == "" {
		path = "/"
	}
	return []contentChange{{Path: path, Operation: operation, Before: before, After: after}}
}

func jsonValuesEqual(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func nullableJSON(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func actorSnapshotTx(ctx context.Context, tx pgx.Tx, actorID int64) (string, error) {
	if actorID == 0 {
		return "system", nil
	}
	var username string
	if err := tx.QueryRow(ctx, `select username from users where id=$1`, actorID).Scan(&username); err != nil {
		return "", fmt.Errorf("read audit actor: %w", err)
	}
	return username, nil
}

func nullableActorID(actorID int64) any {
	if actorID == 0 {
		return nil
	}
	return actorID
}

func nullableEntityID(entityID int64) any {
	if entityID <= 0 {
		return nil
	}
	return entityID
}

func nullableEntityType(entityType string) any {
	entityType = strings.TrimSpace(entityType)
	if entityType == "" {
		return nil
	}
	return entityType
}

func auditRequestValues(r *http.Request) (ip, userAgent, traceID string) {
	if r == nil {
		return "", "", ""
	}
	return requestIP(r), r.UserAgent(), strings.TrimSpace(r.Header.Get("X-Request-ID"))
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
