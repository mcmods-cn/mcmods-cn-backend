package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

const maxBlueprintSourceBytes = 512 << 20

type BlueprintWorker struct {
	cfg           config.Config
	db            *pgxpool.Pool
	queue         *queue.Client
	api           *Server
	fallbackSlots chan struct{}
}

type blueprintJobMessage struct {
	JobID int64 `json:"jobId"`
}

func NewBlueprintWorker(cfg config.Config, db *pgxpool.Pool, queueClient *queue.Client) *BlueprintWorker {
	return &BlueprintWorker{cfg: cfg, db: db, queue: queueClient, api: &Server{cfg: cfg, db: db, queue: queueClient}, fallbackSlots: make(chan struct{}, 4)}
}

func (worker *BlueprintWorker) Start(ctx context.Context) error {
	if worker.queue == nil {
		return queue.ErrUnavailable
	}
	subscribeErr := worker.queue.SubscribeTask("blueprint_convert", worker.handleMessage)
	go worker.recoverQueuedJobs(ctx)
	return subscribeErr
}

func (worker *BlueprintWorker) handleMessage(ctx context.Context, raw []byte) error {
	var message blueprintJobMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return err
	}
	if message.JobID <= 0 {
		return errors.New("blueprint job ID is missing")
	}
	return worker.processJob(ctx, message.JobID)
}

func (worker *BlueprintWorker) recoverQueuedJobs(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	worker.dispatchQueuedJobs(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.dispatchQueuedJobs(ctx)
		}
	}
}

func (worker *BlueprintWorker) dispatchQueuedJobs(ctx context.Context) {
	if err := worker.recoverStaleBlueprintJobs(ctx); err != nil {
		log.Printf("recover blueprint jobs: %v", err)
		return
	}
	rows, err := worker.db.Query(ctx, `select id from blueprint_jobs where status = 'queued' order by created_at limit 32`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var jobID int64
		if rows.Scan(&jobID) != nil {
			continue
		}
		if err := worker.queue.PublishTask(ctx, "blueprint_convert", blueprintJobMessage{JobID: jobID}); err != nil && !errors.Is(err, queue.ErrTaskDisabled) {
			select {
			case worker.fallbackSlots <- struct{}{}:
			case <-ctx.Done():
				return
			default:
				continue
			}
			go func(id int64) {
				defer func() { <-worker.fallbackSlots }()
				jobContext, cancel := context.WithTimeout(ctx, 30*time.Minute)
				defer cancel()
				if processErr := worker.processJob(jobContext, id); processErr != nil {
					log.Printf("blueprint fallback worker job %d: %v", id, processErr)
				}
			}(jobID)
		}
	}
}

func (worker *BlueprintWorker) processJob(parent context.Context, jobID int64) (resultErr error) {
	claim, err := worker.claimBlueprintJob(parent, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, stopHeartbeat := worker.startBlueprintJobHeartbeat(parent, jobID, claim.RunToken)
	defer stopHeartbeat()
	defer func() {
		if resultErr == nil {
			resultErr = context.Cause(ctx)
		}
		finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if finishErr := worker.finishBlueprintJob(finishCtx, jobID, claim.RunToken, resultErr); finishErr != nil {
			resultErr = fmt.Errorf("finalize blueprint job: %w", finishErr)
			return
		}
		if resultErr != nil {
			worker.notifyTemplate(claim.CreatedBy, "blueprint_conversion_failure", map[string]string{"error": truncateRunes(resultErr.Error(), 1000)}, claim.BlueprintID)
		}
	}()
	switch claim.Operation {
	case "normalize":
		resultErr = worker.normalizeBlueprint(ctx, jobID, claim.RunToken, claim.BlueprintID, claim.CreatedBy)
	case "convert":
		resultErr = worker.convertBlueprint(ctx, jobID, claim.RunToken, claim.BlueprintID, claim.CreatedBy, claim.TargetFormat)
	default:
		resultErr = fmt.Errorf("unsupported blueprint operation %q", claim.Operation)
	}
	return resultErr
}

func (worker *BlueprintWorker) normalizeBlueprint(ctx context.Context, jobID int64, runToken string, blueprintID, createdBy int64) error {
	var publicID, objectKey, sourceFormat, title, description, coverKey string
	var coverFileID int64
	startTx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer startTx.Rollback(ctx)
	if err = lockBlueprintJobLeaseTx(ctx, startTx, jobID, runToken); err != nil {
		return err
	}
	if err = startTx.QueryRow(ctx, `update blueprints set status='processing',last_error='',updated_at=now() where id=$1 and status<>'deleted'
		returning public_id,original_object_key,source_format,title,description_markdown,coalesce(cover_file_id,0),cover_object_key`, blueprintID).
		Scan(&publicID, &objectKey, &sourceFormat, &title, &description, &coverFileID, &coverKey); err != nil {
		return err
	}
	if err = startTx.Commit(ctx); err != nil {
		return err
	}
	raw, _, err := worker.readOSSObject(ctx, objectKey)
	if err != nil {
		return err
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, runToken, 25); err != nil {
		return err
	}
	document, err := decodeBlueprint(raw, sourceFormat, title)
	if err != nil {
		return fmt.Errorf("decode %s blueprint: %w", sourceFormat, err)
	}
	materials := blueprintMaterials(document)
	normalized, _, err := encodeBlueprint(document, "json")
	if err != nil {
		return err
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, runToken, 55); err != nil {
		return err
	}
	normalizedConfig := worker.api.ossConfigFromSettings(ctx)
	normalizedKey := path.Join(ossObjectPrefix(normalizedConfig.Prefix, ossBlueprintReleaseCategory(publicID, "normalized")), "blueprint-"+runToken+".json")
	fileID, err := worker.api.writeGeneratedOSSObject(ctx, normalizedKey, "blueprint.json", "application/json", normalized, createdBy, "blueprint_normalized")
	if err != nil {
		return err
	}
	_ = fileID
	if coverFileID <= 0 && coverKey == "" {
		cover, coverErr := renderBlueprintCover(document)
		if coverErr != nil {
			log.Printf("render generated cover for blueprint %s: %v", publicID, coverErr)
		} else {
			generatedConfig := worker.api.ossConfigFromSettings(ctx)
			generatedKey := path.Join(ossObjectPrefix(generatedConfig.Prefix, ossBlueprintTextCategory(publicID, "cover")), "generated-"+randomObjectName()+".png")
			generatedFileID, writeErr := worker.api.writeGeneratedOSSObject(ctx, generatedKey, "blueprint-cover.png", "image/png", cover, createdBy, "blueprint_generated_cover")
			if writeErr != nil {
				log.Printf("store generated cover for blueprint %s: %v", publicID, writeErr)
			} else {
				coverFileID, coverKey = generatedFileID, generatedKey
			}
		}
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockBlueprintJobLeaseTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, "blueprint:"+publicID); err != nil {
		return fmt.Errorf("lock blueprint revision: %w", err)
	}
	var existingReviewStatus string
	var existingPublishedRevisionID *int64
	var currentCoverFileID int64
	var currentCoverKey string
	if err = tx.QueryRow(ctx, `select review_status,published_revision_id,coalesce(cover_file_id,0),cover_object_key from blueprints where id=$1 and status<>'deleted' for update`, blueprintID).
		Scan(&existingReviewStatus, &existingPublishedRevisionID, &currentCoverFileID, &currentCoverKey); err != nil {
		return err
	}
	if currentCoverFileID > 0 || currentCoverKey != "" {
		coverFileID, coverKey = currentCoverFileID, currentCoverKey
	}
	if _, err = tx.Exec(ctx, `delete from blueprint_materials where blueprint_id=$1`, blueprintID); err != nil {
		return err
	}
	for _, material := range materials {
		propertyMap := material.Properties
		if propertyMap == nil {
			propertyMap = map[string]string{}
		}
		properties, _ := json.Marshal(propertyMap)
		if _, err = tx.Exec(ctx, `insert into blueprint_materials(blueprint_id,block_state,block_id,properties,block_count) values($1,$2,$3,$4::jsonb,$5)`,
			blueprintID, material.State, material.BlockID, string(properties), material.Count); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `delete from blueprint_mods where blueprint_id=$1`, blueprintID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into blueprint_mods(blueprint_id,source_namespace,mod_id)
		select $1,namespace.source_namespace,source.mod_id
		from (
			select distinct split_part(block_id,':',1) source_namespace
			from blueprint_materials where blueprint_id=$1 and position(':' in block_id) > 1
		) namespace
		join lateral (
			select candidate.mod_id from (
				select revision.mod_id,0 priority,coalesce(revision.activated_at,revision.created_at) matched_at
				from catalog_import_revisions revision
				where revision.source_namespace=namespace.source_namespace
				  and revision.is_active and revision.status in ('ready','partial')
				union all
				select mod.id,1 priority,mod.updated_at matched_at from mods mod
				where mod.review_status='approved' and exists (
					select 1 from mod_identifiers identifier
					where identifier.mod_id=mod.id and lower(identifier.identifier)=lower(namespace.source_namespace)
				)
			) candidate order by candidate.priority,candidate.matched_at desc limit 1
		) source on true`, blueprintID); err != nil {
		return err
	}
	status := "ready"
	if len(document.Warnings) > 0 {
		status = "partial"
	}
	var revisionCount int
	if err = tx.QueryRow(ctx, `select count(*) from content_revisions where aggregate_type='blueprint' and aggregate_key=$1`, publicID).Scan(&revisionCount); err != nil {
		return err
	}
	reviewStatus := existingReviewStatus
	var publishedRevisionID any
	if existingPublishedRevisionID != nil {
		publishedRevisionID = *existingPublishedRevisionID
	}
	if revisionCount == 0 {
		reviewRequired := loadReviewConfig(ctx, tx).BlueprintCreate
		revisionStatus := "approved"
		reviewStatus = "approved"
		if reviewRequired {
			revisionStatus = "pending"
			reviewStatus = "pending"
		}
		var coverFilePublicID string
		if coverFileID > 0 {
			value, resolveErr := ossFilePublicIDForInternal(ctx, tx, &coverFileID)
			if resolveErr != nil {
				return resolveErr
			}
			if value != nil {
				coverFilePublicID = *value
			}
		}
		snapshot, _ := json.Marshal(blueprintContentSnapshot{PublicID: publicID, Title: title, Description: description, CoverFileID: coverFilePublicID, CoverKey: coverKey})
		created, createErr := createContentRevisionTx(ctx, tx, createContentRevisionParams{
			EntityType: "blueprint", EntityID: blueprintID,
			AggregateType: "blueprint", AggregateKey: publicID, Snapshot: snapshot, Reason: "New blueprint",
			ActorID: createdBy, Status: revisionStatus, Source: "blueprint_upload",
			Metadata: map[string]any{"blueprintId": publicID, "title": title, "operation": "create"},
		})
		if createErr != nil {
			return createErr
		}
		if !reviewRequired {
			publishedRevisionID = created.RevisionID
		} else {
			publishedRevisionID = nil
		}
	}
	if _, err = tx.Exec(ctx, `update blueprints set status=$2,normalized_object_key=$3,size_x=$4,size_y=$5,size_z=$6,
		block_count=$7,palette_count=$8,entity_count=$9,data_version=$10,review_status=$11,published_revision_id=$12,
		cover_file_id=nullif($13,0),cover_object_key=$14,last_error='',updated_at=now() where id=$1`,
		blueprintID, status, normalizedKey, document.Size[0], document.Size[1], document.Size[2], len(document.Blocks), len(materials), len(document.Entities), document.DataVersion, reviewStatus, publishedRevisionID, coverFileID, coverKey); err != nil {
		return err
	}
	if err = refreshBlueprintJobLeaseTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, runToken, 90); err != nil {
		return err
	}
	worker.notifyTemplate(createdBy, "blueprint_conversion_success", map[string]string{"name": title}, blueprintID)
	return nil
}

func (worker *BlueprintWorker) convertBlueprint(ctx context.Context, jobID int64, runToken string, blueprintID, createdBy int64, targetFormat string) error {
	var publicID, title, normalizedKey string
	if err := worker.db.QueryRow(ctx, `select public_id,title,normalized_object_key from blueprints where id=$1 and status<>'deleted'`, blueprintID).Scan(&publicID, &title, &normalizedKey); err != nil {
		return err
	}
	if normalizedKey == "" {
		return errors.New("blueprint has not been normalized")
	}
	raw, _, err := worker.readOSSObject(ctx, normalizedKey)
	if err != nil {
		return err
	}
	var document blueprintDocument
	if err = json.Unmarshal(raw, &document); err != nil {
		return err
	}
	if err = worker.updateBlueprintJobProgress(ctx, jobID, runToken, 40); err != nil {
		return err
	}
	encoded, contentType, err := encodeBlueprint(document, targetFormat)
	if err != nil {
		return err
	}
	extension := strings.TrimPrefix(strings.ToLower(targetFormat), ".")
	conversionConfig := worker.api.ossConfigFromSettings(ctx)
	objectKey := path.Join(ossObjectPrefix(conversionConfig.Prefix, ossBlueprintReleaseCategory(publicID, extension)), randomObjectName()+"."+extension)
	filename := strings.TrimSuffix(title, "."+document.SourceFormat) + "." + extension
	fileID, err := worker.api.writeGeneratedOSSObject(ctx, objectKey, filename, contentType, encoded, createdBy, "blueprint_conversion")
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockBlueprintJobLeaseTx(ctx, tx, jobID, runToken); err != nil {
		return err
	}
	var activeID int64
	if err = tx.QueryRow(ctx, `select id from blueprints where id=$1 and status<>'deleted' for update`, blueprintID).Scan(&activeID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into blueprint_variants(blueprint_id,format,file_id,object_key,original,recommended,status,original_name,content_type,size_bytes,sha256,created_by)
		values($1,$2,$3,$4,false,false,'ready',$5,$6,$7,$8,$9)`, blueprintID, extension, fileID, objectKey, filename, contentType, len(encoded), hex.EncodeToString(digest[:]), createdBy)
	if err == nil {
		err = refreshBlueprintJobLeaseTx(ctx, tx, jobID, runToken)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err == nil {
		worker.notifyTemplate(createdBy, "blueprint_format_success", map[string]string{"name": title, "format": strings.ToUpper(extension)}, blueprintID)
	}
	return err
}

func (worker *BlueprintWorker) readOSSObject(ctx context.Context, objectKey string) ([]byte, string, error) {
	client, cfg, err := worker.api.ossClient(ctx)
	if err != nil {
		return nil, "", err
	}
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		return nil, "", err
	}
	defer result.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(result.Body, maxBlueprintSourceBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > maxBlueprintSourceBytes {
		return nil, "", errors.New("blueprint exceeds processing size limit")
	}
	contentType := "application/octet-stream"
	if result.ContentType != nil {
		contentType = *result.ContentType
	}
	return raw, contentType, nil
}

func (worker *BlueprintWorker) notifyTemplate(userID int64, code string, values map[string]string, blueprintID int64) {
	if userID <= 0 {
		return
	}
	var publicID, blueprintName string
	_ = worker.db.QueryRow(context.Background(), `select public_id,title from blueprints where id=$1`, blueprintID).Scan(&publicID, &blueprintName)
	if values == nil {
		values = map[string]string{}
	}
	if values["name"] == "" {
		values["name"] = blueprintName
	}
	targetData := map[string]any{"blueprintId": publicID, "targetLabel": blueprintName, "url": "/blueprints/" + publicID}
	if worker.queue != nil && worker.queue.PublishTask(context.Background(), notificationTaskCode, notificationEvent{Action: "direct", RecipientID: userID, Kind: "system", TemplateKey: code, TemplateValues: values, Data: targetData}) == nil {
		return
	}
	rendered, err := renderNotificationTemplate(context.Background(), worker.db, userID, code, values)
	if err != nil {
		return
	}
	data, _ := json.Marshal(targetData)
	params, _ := json.Marshal(rendered.Values)
	_, _ = worker.db.Exec(context.Background(), `insert into notifications(recipient_id,kind,title,body,source_locale,data,template_key,template_version,template_params) values($1,'system',$2,$3,$4,$5::jsonb,$6,$7,$8::jsonb)`, userID, rendered.Title, rendered.Body, rendered.Locale, string(data), rendered.Key, rendered.Version, string(params))
}
