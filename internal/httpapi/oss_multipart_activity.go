package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mcmods-cn-backend/internal/activity"
)

// A session retains a bounded typed intent, not an HTTP request or provider
// capability. Queued means it entered activity_event_outbox in the same commit
// as terminal cancellation; it does not assert reward processing completed.
type ossMultipartAbortIntent struct {
	Version      int                    `json:"version"`
	Kind         string                 `json:"kind"`
	ID           string                 `json:"id"`
	Queued       bool                   `json:"queued"`
	ActorDeleted bool                   `json:"actorDeleted,omitempty"`
	Event        ossMultipartAbortEvent `json:"event"`
}
type ossMultipartAbortEvent struct {
	UserID               int64     `json:"userId"`
	ActionID             int16     `json:"actionId"`
	ObjectTypeID         int16     `json:"objectTypeId"`
	ObjectRouteID        int64     `json:"objectRouteId,omitempty"`
	ObjectPublicID       string    `json:"objectPublicId,omitempty"`
	ObjectEntityType     string    `json:"objectEntityType,omitempty"`
	ObjectInternalID     int64     `json:"objectInternalId,omitempty"`
	MarkdownAddedBytes   int       `json:"markdownAddedBytes,omitempty"`
	MarkdownDeletedBytes int       `json:"markdownDeletedBytes,omitempty"`
	OccurredAt           time.Time `json:"occurredAt"`
}

func (event ossMultipartAbortEvent) activityEvent() activity.Event {
	return activity.Event{UserID: event.UserID, ActionID: event.ActionID, ObjectTypeID: event.ObjectTypeID, ObjectRouteID: event.ObjectRouteID, ObjectPublicID: event.ObjectPublicID, ObjectEntityType: event.ObjectEntityType, ObjectInternalID: event.ObjectInternalID, MarkdownAddedBytes: event.MarkdownAddedBytes, MarkdownDeletedBytes: event.MarkdownDeletedBytes, OccurredAt: event.OccurredAt}
}

func newOSSMultipartAbortIntent(r *http.Request, ownerID int64) ([]byte, error) {
	annotation, _ := r.Context().Value(requestActivityContextKey).(*requestActivity)
	event, valid := requestActivityEvent(r, annotation)
	if !valid || !activity.IsDurableAction(event.ActionID) {
		return []byte(`{}`), nil
	}
	if event.UserID != ownerID {
		return nil, errOSSMultipartSessionForbidden
	}
	intent := ossMultipartAbortIntent{Version: 1, Kind: "oss_multipart_abort", ID: "oss-multipart-abort:" + newExportID(), Event: ossMultipartAbortEvent{UserID: event.UserID, ActionID: event.ActionID, ObjectTypeID: event.ObjectTypeID, ObjectRouteID: event.ObjectRouteID, ObjectPublicID: event.ObjectPublicID, ObjectEntityType: event.ObjectEntityType, ObjectInternalID: event.ObjectInternalID, MarkdownAddedBytes: event.MarkdownAddedBytes, MarkdownDeletedBytes: event.MarkdownDeletedBytes, OccurredAt: event.OccurredAt}}
	raw, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	_, err = decodeOSSMultipartAbortIntent(raw, ownerID)
	return raw, err
}

func decodeOSSMultipartAbortIntent(raw []byte, ownerID int64) (*ossMultipartAbortIntent, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte(`{}`)) {
		return nil, nil
	}
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, errors.New("invalid bounded multipart abort activity intent")
	}
	var intent ossMultipartAbortIntent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return nil, errors.New("invalid typed multipart abort activity intent")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing multipart abort activity data")
	}
	event := intent.Event
	if intent.Version != 1 || intent.Kind != "oss_multipart_abort" || len(intent.ID) != len("oss-multipart-abort:")+36 || !strings.HasPrefix(intent.ID, "oss-multipart-abort:") || event.UserID <= 0 || (ownerID > 0 && event.UserID != ownerID) || event.ActionID <= 0 || event.ObjectTypeID <= 0 || !activity.IsDurableAction(event.ActionID) || event.ObjectRouteID < 0 || event.ObjectInternalID < 0 || event.MarkdownAddedBytes < 0 || event.MarkdownDeletedBytes < 0 || event.MarkdownAddedBytes > 1<<30 || event.MarkdownDeletedBytes > 1<<30 || event.OccurredAt.IsZero() || len(event.ObjectEntityType) > 64 || (event.ObjectPublicID != "" && !validCatalogPublicID(event.ObjectPublicID)) {
		return nil, errors.New("inconsistent multipart abort activity identity")
	}
	for _, char := range event.ObjectEntityType {
		if char != '_' && (char < 'a' || char > 'z') {
			return nil, errors.New("invalid multipart abort activity entity type")
		}
	}
	for _, char := range strings.TrimPrefix(intent.ID, "oss-multipart-abort:") {
		if char != '-' && (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return nil, errors.New("invalid multipart abort activity event id")
		}
	}
	return &intent, nil
}

// Both HTTP and lease recovery use this finalizer. An interrupted provider
// abort leaves the original intent on the leased session until a fenced winner
// can commit terminal state, quota/blueprint cleanup and activity together.
func (s *Server) finishOSSMultipartAbort(ctx context.Context, settlement ossMultipartSettlement) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var ownerID *int64
	var objectKey, status, token string
	var raw []byte
	var leaseValid bool
	if err = tx.QueryRow(ctx, `select owner_id,object_key,status,locked_by,abort_activity_intent,coalesce(lease_expires_at>clock_timestamp(),false) from oss_multipart_sessions where id=$1 for update`, settlement.ID).Scan(&ownerID, &objectKey, &status, &token, &raw, &leaseValid); err != nil {
		return err
	}
	if status == "aborted" {
		if !settlement.Already {
			return errOSSMultipartSessionLeaseLost
		}
		return tx.Commit(ctx)
	}
	if status != "aborting" || token != settlement.Token || !leaseValid {
		return errOSSMultipartSessionLeaseLost
	}
	actor := int64(0)
	if ownerID != nil {
		actor = *ownerID
	}
	intent, err := decodeOSSMultipartAbortIntent(raw, actor)
	if err != nil {
		return err
	}
	if ownerID != nil {
		// Association before quota matches upload registration and blueprint delete.
		rows, queryErr := tx.Query(ctx, `select id from blueprints where owner_id=$1 and original_object_key=$2 and status='uploading' order by id for update`, actor, objectKey)
		if queryErr != nil {
			return queryErr
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if err = deletePendingBlueprintUploadRowsTx(ctx, tx, ids); err != nil {
			return err
		}
		// Only actual reservations require a quota lock; administrative uploads have
		// no user reservation. Never create quota rows for an absent legacy actor.
		var reserved bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from oss_user_upload_quota_reservations where user_id=$1 and object_key=$2)`, actor, objectKey).Scan(&reserved); err != nil {
			return err
		}
		if reserved {
			if _, err = lockOSSUserQuotaTx(ctx, tx, actor); err != nil {
				return err
			}
			if _, err = releaseOSSUserQuotaReservationTx(ctx, tx, actor, objectKey); err != nil {
				return err
			}
		}
	}
	if intent != nil && !intent.Queued && !intent.ActorDeleted {
		if ownerID == nil {
			// FK deletion removed the account; preserve this explicit disposition and
			// clean its remote multipart upload without inventing a recipient/reward.
			var exists bool
			if err = tx.QueryRow(ctx, `select exists(select 1 from users where id=$1)`, intent.Event.UserID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return errors.New("multipart intent owner is missing while its actor still exists")
			}
			intent.ActorDeleted = true
		} else {
			if err = activity.EnqueueDurableTx(ctx, tx, intent.Event.activityEvent()); err != nil {
				return fmt.Errorf("persist multipart abort activity: %w", err)
			}
			intent.Queued = true
		}
		raw, err = json.Marshal(intent)
		if err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `update oss_multipart_sessions set status='aborted',aborted_at=now(),locked_by='',lease_expires_at=null,last_error='',failure_class='',abort_activity_intent=$3::jsonb,updated_at=now() where id=$1 and status='aborting' and locked_by=$2 and lease_expires_at>clock_timestamp()`, settlement.ID, settlement.Token, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errOSSMultipartSessionLeaseLost
	}
	return tx.Commit(ctx)
}

func (s *Server) ownsMultipartBlueprintAbort(ctx context.Context, ownerID int64, cfg ossConfigPayload, req ossCompleteUploadRequest) (bool, error) {
	var owns bool
	err := s.db.QueryRow(ctx, `select exists(select 1 from oss_multipart_sessions where owner_id=$1 and bucket=$2 and endpoint=$3 and object_key=$4 and upload_id=$5 and status in ('active','aborting','cleanup_pending','aborted','dead'))`, ownerID, cfg.Bucket, cfg.Endpoint, req.ObjectKey, req.MultipartUploadID).Scan(&owns)
	return owns, err
}
