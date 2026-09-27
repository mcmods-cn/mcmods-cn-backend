package searchindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	searchQueueBatchSize         = 200
	searchRebuildPageSize        = 500
	searchImportByteBudget       = 8 << 20
	searchDocumentArrayItemLimit = 64
	searchDocumentArrayByteLimit = 8 << 10
	searchPollInterval           = 2 * time.Second
	searchProjectionRebuildLock  = "mcmods-search-projection-rebuild"
)

var errSearchProjectionVersionSuperseded = errors.New("search projection schema version is superseded")

type searchDatabase interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Begin(context.Context) (pgx.Tx, error)
}

type Worker struct {
	pool              *pgxpool.Pool
	db                searchDatabase
	client            *Client
	projectionVersion int
}

type queueJob struct {
	DocumentType string
	DocumentID   int64
	Operation    string
	Token        time.Time
}

func NewWorker(db *pgxpool.Pool, client *Client) *Worker {
	return &Worker{pool: db, db: db, client: client, projectionVersion: projectionSchemaVersion}
}

func (worker *Worker) Start(ctx context.Context) {
	if worker == nil || worker.pool == nil || worker.client == nil || !worker.client.Enabled() {
		return
	}
	go worker.run(ctx)
}

func (worker *Worker) run(ctx context.Context) {
	for ctx.Err() == nil {
		worker.client.SetReady(false)
		if err := worker.rebuild(ctx); err != nil {
			log.Printf("Typesense initial rebuild failed; PostgreSQL search fallback remains active: %v", err)
			if !waitSearchWorker(ctx, 15*time.Second) {
				return
			}
			continue
		}
		worker.client.SetReady(true)
		log.Print("Typesense search projections are ready")
		break
	}

	ticker := time.NewTicker(searchPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !worker.client.Ready() {
				if err := worker.rebuild(ctx); err != nil {
					continue
				}
				worker.client.SetReady(true)
				continue
			}
			if err := worker.drain(ctx); err != nil {
				log.Printf("Typesense queue sync failed; queued changes will be retried: %v", err)
			}
		}
	}
}

func waitSearchWorker(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// withSearchProjectionLease coordinates every backend instance through one
// PostgreSQL advisory lock. Rebuilds take the exclusive form while queue
// drains take the shared form, so events committed after the snapshot starts
// remain durable until the replacement aliases are active.
func (worker *Worker) withSearchProjectionLease(
	ctx context.Context, shared bool, operation func(*Worker) error,
) (operationErr error) {
	if worker == nil || worker.pool == nil {
		return errors.New("search projection lease requires a database pool")
	}
	connection, err := worker.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire search projection lease connection: %w", err)
	}
	lockSQL := `select pg_advisory_lock(hashtext($1))`
	if shared {
		lockSQL = `select pg_advisory_lock_shared(hashtext($1))`
	}
	if _, err = connection.Exec(ctx, lockSQL, searchProjectionRebuildLock); err != nil {
		closeSearchProjectionLeaseConnection(connection)
		return fmt.Errorf("acquire search projection lease: %w", err)
	}
	defer func() {
		if releaseErr := releaseSearchProjectionLease(connection, shared); operationErr == nil && releaseErr != nil {
			operationErr = releaseErr
		}
	}()
	leasedWorker := *worker
	leasedWorker.db = connection
	return operation(&leasedWorker)
}

func releaseSearchProjectionLease(connection *pgxpool.Conn, shared bool) error {
	unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlockSQL := `select pg_advisory_unlock(hashtext($1))`
	if shared {
		unlockSQL = `select pg_advisory_unlock_shared(hashtext($1))`
	}
	var unlocked bool
	err := connection.QueryRow(unlockCtx, unlockSQL, searchProjectionRebuildLock).Scan(&unlocked)
	if err == nil && unlocked {
		connection.Release()
		return nil
	}
	closeSearchProjectionLeaseConnection(connection)
	if err != nil {
		return fmt.Errorf("release search projection lease: %w", err)
	}
	return errors.New("release search projection lease: advisory lock was not held")
}

func closeSearchProjectionLeaseConnection(connection *pgxpool.Conn) {
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = connection.Hijack().Close(closeCtx)
}

func (worker *Worker) rebuild(ctx context.Context) error {
	if err := worker.client.Health(ctx); err != nil {
		return err
	}
	current, err := worker.projectionsCurrent(ctx)
	if err != nil {
		return err
	}
	if current {
		return worker.drain(ctx)
	}
	if err = worker.withSearchProjectionLease(ctx, false, func(leasedWorker *Worker) error {
		current, currentErr := leasedWorker.projectionsCurrent(ctx)
		if currentErr != nil {
			return currentErr
		}
		if current {
			return nil
		}
		return leasedWorker.rebuildProjections(ctx)
	}); err != nil {
		return err
	}
	return worker.drain(ctx)
}

func (worker *Worker) rebuildProjections(ctx context.Context) error {
	if err := worker.ensureProjectionVersionNotSuperseded(ctx); err != nil {
		return err
	}
	for _, registration := range searchRegistry {
		kind := registration.kind
		current, currentErr := worker.projectionCurrent(ctx, registration)
		if currentErr != nil {
			return currentErr
		}
		if current {
			continue
		}
		template := registration.schema
		oldCollection, err := worker.client.AliasTarget(ctx, worker.client.Alias(kind))
		if err != nil && !isStatus(err, 404) {
			return fmt.Errorf("read %s alias: %w", kind, err)
		}
		collection := worker.client.VersionedCollection(kind, worker.projectionVersion)
		template.Name = collection
		if err = worker.beginSearchRebuildProgress(ctx, kind, collection); err != nil {
			return fmt.Errorf("begin %s search rebuild progress: %w", kind, err)
		}
		if err = worker.client.CreateCollection(ctx, template); err != nil {
			worker.failSearchRebuildProgress(ctx, kind, err)
			return fmt.Errorf("create %s collection: %w", kind, err)
		}
		if err = worker.rebuildCollectionDocuments(ctx, registration, collection); err != nil {
			_ = worker.client.DeleteCollection(context.Background(), collection)
			worker.failSearchRebuildProgress(ctx, kind, err)
			return fmt.Errorf("stream %s search documents: %w", kind, err)
		}
		if err = worker.client.UpsertAlias(ctx, worker.client.Alias(kind), collection); err != nil {
			_ = worker.client.DeleteCollection(context.Background(), collection)
			worker.failSearchRebuildProgress(ctx, kind, err)
			return fmt.Errorf("activate %s search collection: %w", kind, err)
		}
		var stateTag pgconn.CommandTag
		if stateTag, err = worker.db.Exec(ctx, `insert into search_index_state(collection_kind,schema_version,collection_name,rebuilt_at)
			values($1,$2,$3,now()) on conflict(collection_kind) do update set
			schema_version=excluded.schema_version,collection_name=excluded.collection_name,rebuilt_at=excluded.rebuilt_at
			where search_index_state.schema_version<=excluded.schema_version`,
			kind, worker.projectionVersion, collection); err != nil {
			worker.failSearchRebuildProgress(ctx, kind, err)
			return fmt.Errorf("record %s search projection: %w", kind, err)
		}
		if stateTag.RowsAffected() != 1 {
			err = fmt.Errorf("%w: cannot record version %d for %s", errSearchProjectionVersionSuperseded,
				worker.projectionVersion, kind)
			worker.failSearchRebuildProgress(ctx, kind, err)
			return err
		}
		if progressErr := worker.completeSearchRebuildProgress(ctx, kind); progressErr != nil {
			log.Printf("complete %s search rebuild progress: %v", kind, progressErr)
		}
		if oldCollection != "" && oldCollection != collection {
			if err = worker.client.DeleteCollection(ctx, oldCollection); err != nil {
				log.Printf("delete superseded Typesense collection %s: %v", oldCollection, err)
			}
		}
	}
	return nil
}

func (worker *Worker) rebuildCollectionDocuments(
	ctx context.Context, registration searchCollectionRegistration, collection string,
) error {
	for _, document := range registration.documents {
		documentType := document.documentType
		var afterID int64
		for {
			ids, loadErr := worker.loadDocumentIDPage(ctx, documentType, afterID, searchRebuildPageSize)
			if loadErr != nil {
				return fmt.Errorf("load %s search ID page: %w", documentType, loadErr)
			}
			if len(ids) == 0 {
				break
			}
			documents, loadErr := document.load(ctx, worker, ids)
			if loadErr != nil {
				return fmt.Errorf("load %s search document page: %w", documentType, loadErr)
			}
			indexedBytes, importErr := worker.importSearchDocumentBatches(ctx, collection, documents)
			if importErr != nil {
				return fmt.Errorf("import %s search document page: %w", documentType, importErr)
			}
			afterID = ids[len(ids)-1]
			if progressErr := worker.recordSearchRebuildProgress(
				ctx, registration.kind, documentType, afterID, int64(len(documents)), indexedBytes,
			); progressErr != nil {
				return fmt.Errorf("record %s search rebuild progress: %w", documentType, progressErr)
			}
			if len(ids) < searchRebuildPageSize {
				break
			}
		}
	}
	return nil
}

func (worker *Worker) loadDocumentIDPage(
	ctx context.Context, documentType string, afterID int64, limit int,
) ([]int64, error) {
	_, document, err := registeredDocument(documentType)
	if err != nil {
		return nil, err
	}
	rows, err := worker.db.Query(ctx, document.idPageQuery, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func searchDocumentIDPageQuery(documentType string) (string, error) {
	_, document, err := registeredDocument(documentType)
	return document.idPageQuery, err
}

func (worker *Worker) importSearchDocumentBatches(
	ctx context.Context, collection string, documents []map[string]any,
) (int64, error) {
	batch := make([]map[string]any, 0, len(documents))
	batchBytes := 0
	var indexedBytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := worker.client.ImportDocuments(ctx, collection, batch); err != nil {
			return err
		}
		batch = batch[:0]
		batchBytes = 0
		return nil
	}
	for _, document := range documents {
		encoded, err := json.Marshal(document)
		if err != nil {
			return indexedBytes, fmt.Errorf("measure search document: %w", err)
		}
		documentBytes := len(encoded) + 1
		if documentBytes > searchImportByteBudget {
			return indexedBytes, fmt.Errorf("search document exceeds %d-byte import budget", searchImportByteBudget)
		}
		if batchBytes+documentBytes > searchImportByteBudget {
			if err = flush(); err != nil {
				return indexedBytes, err
			}
		}
		batch = append(batch, document)
		batchBytes += documentBytes
		indexedBytes += int64(documentBytes)
	}
	return indexedBytes, flush()
}

func (worker *Worker) beginSearchRebuildProgress(ctx context.Context, kind, collection string) error {
	_, err := worker.db.Exec(ctx, `insert into search_index_rebuild_progress(
		collection_kind,collection_name,status,started_at,updated_at
	) values($1,$2,'building',now(),now()) on conflict(collection_kind) do update set
		collection_name=excluded.collection_name,document_type='',last_document_id=0,
		indexed_document_count=0,indexed_byte_count=0,batch_count=0,status='building',
		last_error='',started_at=now(),updated_at=now()`, kind, collection)
	return err
}

func (worker *Worker) recordSearchRebuildProgress(
	ctx context.Context, kind, documentType string, lastID, documentCount, byteCount int64,
) error {
	_, err := worker.db.Exec(ctx, `update search_index_rebuild_progress set
		document_type=$2,last_document_id=$3,indexed_document_count=indexed_document_count+$4,
		indexed_byte_count=indexed_byte_count+$5,batch_count=batch_count+1,updated_at=now()
		where collection_kind=$1 and status='building'`, kind, documentType, lastID, documentCount, byteCount)
	return err
}

func (worker *Worker) failSearchRebuildProgress(ctx context.Context, kind string, cause error) {
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, _ = worker.db.Exec(ctx, `update search_index_rebuild_progress set status='failed',last_error=$2,updated_at=now()
		where collection_kind=$1`, kind, message)
}

func (worker *Worker) completeSearchRebuildProgress(ctx context.Context, kind string) error {
	_, err := worker.db.Exec(ctx, `update search_index_rebuild_progress set status='complete',last_error='',updated_at=now()
		where collection_kind=$1`, kind)
	return err
}

func (worker *Worker) projectionsCurrent(ctx context.Context) (bool, error) {
	if err := worker.ensureProjectionVersionNotSuperseded(ctx); err != nil {
		return false, err
	}
	for _, registration := range searchRegistry {
		current, err := worker.projectionCurrent(ctx, registration)
		if err != nil {
			return false, err
		}
		if !current {
			return false, nil
		}
	}
	return true, nil
}

func (worker *Worker) ensureProjectionVersionNotSuperseded(ctx context.Context) error {
	var newestVersion int
	if err := worker.db.QueryRow(ctx, `select coalesce(max(schema_version),0) from search_index_state`).
		Scan(&newestVersion); err != nil {
		return fmt.Errorf("read newest search projection version: %w", err)
	}
	if newestVersion > worker.projectionVersion {
		return fmt.Errorf("%w: database version %d is newer than worker version %d",
			errSearchProjectionVersionSuperseded, newestVersion, worker.projectionVersion)
	}
	return nil
}

func (worker *Worker) projectionCurrent(
	ctx context.Context, registration searchCollectionRegistration,
) (bool, error) {
	kind := registration.kind
	collection, err := worker.client.AliasTarget(ctx, worker.client.Alias(kind))
	if isStatus(err, 404) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s alias: %w", kind, err)
	}
	var storedCollection string
	var storedVersion int
	err = worker.db.QueryRow(ctx, `select collection_name,schema_version from search_index_state where collection_kind=$1`, kind).
		Scan(&storedCollection, &storedVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s search projection state: %w", kind, err)
	}
	if storedVersion > worker.projectionVersion {
		return false, fmt.Errorf("%w: %s database version %d is newer than worker version %d",
			errSearchProjectionVersionSuperseded, kind, storedVersion, worker.projectionVersion)
	}
	if storedVersion != worker.projectionVersion || storedCollection != collection {
		return false, nil
	}
	exists, err := worker.client.CollectionExists(ctx, collection)
	if err != nil {
		return false, fmt.Errorf("inspect %s search collection: %w", kind, err)
	}
	if !exists {
		return false, nil
	}
	return true, nil
}

func (worker *Worker) drain(ctx context.Context) error {
	return worker.withSearchProjectionLease(ctx, true, func(leasedWorker *Worker) error {
		return leasedWorker.drainWithLease(ctx)
	})
}

func (worker *Worker) drainWithLease(ctx context.Context) error {
	if err := worker.ensureProjectionVersionNotSuperseded(ctx); err != nil {
		return err
	}
	jobs, err := worker.claim(ctx)
	if err != nil || len(jobs) == 0 {
		return err
	}
	byType := make(map[string][]queueJob)
	for _, job := range jobs {
		byType[job.DocumentType] = append(byType[job.DocumentType], job)
	}
	batchErrors := make([]error, 0)
	for documentType, typedJobs := range byType {
		collection, documentRegistration, registrationErr := registeredDocument(documentType)
		if registrationErr != nil {
			batchErrors = append(batchErrors, registrationErr)
			if retryErr := worker.retry(ctx, typedJobs, registrationErr); retryErr != nil {
				batchErrors = append(batchErrors, retryErr)
			}
			continue
		}
		ids := make([]int64, 0, len(typedJobs))
		for _, job := range typedJobs {
			if job.Operation == "upsert" {
				ids = append(ids, job.DocumentID)
			}
		}
		documents, loadErr := documentRegistration.load(ctx, worker, ids)
		if loadErr != nil {
			batchErrors = append(batchErrors, fmt.Errorf("load %s search queue documents: %w", documentType, loadErr))
			if retryErr := worker.retry(ctx, typedJobs, loadErr); retryErr != nil {
				batchErrors = append(batchErrors, retryErr)
			}
			continue
		}
		found := make(map[int64]bool, len(documents))
		for _, document := range documents {
			if id, ok := document["internal_id"].(int64); ok {
				found[id] = true
			}
		}
		if loadErr = worker.client.ImportDocuments(ctx, worker.client.Alias(collection.kind), documents); loadErr != nil {
			batchErrors = append(batchErrors, fmt.Errorf("import %s search queue documents: %w", documentType, loadErr))
			if retryErr := worker.retry(ctx, typedJobs, loadErr); retryErr != nil {
				batchErrors = append(batchErrors, retryErr)
			}
			continue
		}
		for _, job := range typedJobs {
			if job.Operation == "delete" || !found[job.DocumentID] {
				if deleteErr := worker.client.DeleteDocument(ctx, worker.client.Alias(collection.kind), documentKey(documentType, job.DocumentID)); deleteErr != nil {
					batchErrors = append(batchErrors, fmt.Errorf("delete %s search queue document %d: %w",
						documentType, job.DocumentID, deleteErr))
					if retryErr := worker.retry(ctx, []queueJob{job}, deleteErr); retryErr != nil {
						batchErrors = append(batchErrors, retryErr)
					}
					continue
				}
			}
			if completeErr := worker.complete(ctx, job); completeErr != nil {
				batchErrors = append(batchErrors, fmt.Errorf("complete %s search queue document %d: %w",
					documentType, job.DocumentID, completeErr))
			}
		}
	}
	return errors.Join(batchErrors...)
}

func (worker *Worker) claim(ctx context.Context) ([]queueJob, error) {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `with picked as (
		select document_type,document_id from search_index_queue where available_at<=now()
		order by available_at,updated_at limit $1 for update skip locked
	) update search_index_queue queue set attempts=queue.attempts+1,available_at=now()+interval '1 minute',updated_at=clock_timestamp()
	from picked where queue.document_type=picked.document_type and queue.document_id=picked.document_id
	returning queue.document_type,queue.document_id,queue.operation,queue.updated_at`, searchQueueBatchSize)
	if err != nil {
		return nil, err
	}
	jobs := make([]queueJob, 0, searchQueueBatchSize)
	for rows.Next() {
		var job queueJob
		if err = rows.Scan(&job.DocumentType, &job.DocumentID, &job.Operation, &job.Token); err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, job)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (worker *Worker) complete(ctx context.Context, job queueJob) error {
	_, err := worker.db.Exec(ctx, `delete from search_index_queue where document_type=$1 and document_id=$2 and updated_at=$3`,
		job.DocumentType, job.DocumentID, job.Token)
	return err
}

func (worker *Worker) retry(ctx context.Context, jobs []queueJob, cause error) error {
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	retryErrors := make([]error, 0)
	for _, job := range jobs {
		if _, err := worker.db.Exec(ctx, `update search_index_queue set available_at=now()+
			interval '1 second'*least(900,5*power(2,least(attempts,8))::integer),last_error=$4,updated_at=clock_timestamp()
			where document_type=$1 and document_id=$2 and updated_at=$3`,
			job.DocumentType, job.DocumentID, job.Token, message); err != nil {
			retryErrors = append(retryErrors, fmt.Errorf("persist %s search retry for document %d: %w",
				job.DocumentType, job.DocumentID, err))
		}
	}
	return errors.Join(retryErrors...)
}

func documentKey(documentType string, id int64) string {
	return documentType + "_" + strconv.FormatInt(id, 10)
}

func (worker *Worker) loadTypedDocuments(ctx context.Context, documentType string, ids []int64) ([]map[string]any, error) {
	_, registration, err := registeredDocument(documentType)
	if err != nil {
		return nil, err
	}
	return registration.load(ctx, worker, ids)
}

func (worker *Worker) loadProjectDocuments(ctx context.Context, ids []int64, onlyType string) ([]map[string]any, error) {
	documents := make([]map[string]any, 0)
	queries := []struct {
		documentType string
		sql          string
	}{
		{"mod", `with selected_mods as (
			select * from mods mod where mod.id=any($1::bigint[])
		), localizations as (
			select localization.subject_id,
				(array_agg(left(localization.name,512) order by localization.locale)
					filter(where localization.name<>''))[1:16] names,
				(array_agg(left(localization.summary,256)||' '||left(localization.content_markdown,512)
					order by localization.locale))[1:16] text
			from content_localizations localization
			where localization.subject_type='mod' and localization.subject_id=any($1::bigint[])
			group by localization.subject_id
		), identifiers as (
			select identifier.mod_id,(array_agg(left(identifier.identifier,128)
				order by identifier.display_order,identifier.id))[1:64] items
			from mod_identifiers identifier where identifier.mod_id=any($1::bigint[]) group by identifier.mod_id
		), creator_names as (
			select binding.subject_id,(array_agg(distinct left(creator.name,512)
				order by left(creator.name,512)))[1:16] items
			from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
			where binding.subject_type='mod' and binding.subject_id=any($1::bigint[]) group by binding.subject_id
		), tags as (
			select tag.mod_id,(array_agg(left(tag.tag,128) order by tag.tag))[1:64] items from mod_tags tag
			where tag.mod_id=any($1::bigint[]) group by tag.mod_id
		), compatibilities as (
			select compatibility.mod_id,
				(array_agg(distinct left(compatibility.minecraft_version,64)
					order by left(compatibility.minecraft_version,64)))[1:64] versions,
				(array_agg(distinct left(compatibility.loader,64)
					order by left(compatibility.loader,64)))[1:64] loaders
			from mod_loader_compatibilities compatibility
			where compatibility.mod_id=any($1::bigint[]) group by compatibility.mod_id
		)
		select mod.id,'mod'::text,mod.project_code,mod.slug,
			array[left(mod.primary_name,2048),left(mod.secondary_name,2048),left(mod.abbreviation,512)]||
				coalesce(localizations.names,'{}'::text[]),
			array[left(mod.summary,2048),left(mod.body_markdown,8192)]||coalesce(localizations.text,'{}'::text[]),
			coalesce(identifiers.items,'{}'::text[]),coalesce(creator_names.items,'{}'::text[]),
			coalesce((select array_agg(left(keyword.value,128) order by keyword.ordinal)
				from unnest(mod.search_keywords) with ordinality keyword(value,ordinal) where keyword.ordinal<=64),'{}'::text[]),
			coalesce(tags.items,'{}'::text[]),coalesce(compatibilities.versions,'{}'::text[]),
			coalesce(compatibilities.loaders,'{}'::text[]),mod.review_status,coalesce(mod.submitted_by,0),mod.updated_at
		from selected_mods mod left join localizations on localizations.subject_id=mod.id
		left join identifiers on identifiers.mod_id=mod.id left join creator_names on creator_names.subject_id=mod.id
		left join tags on tags.mod_id=mod.id left join compatibilities on compatibilities.mod_id=mod.id`},
		{"modpack", `with selected_modpacks as (
			select * from modpacks pack where pack.id=any($1::bigint[])
		), localizations as (
			select localization.subject_id,
				(array_agg(left(localization.name,512) order by localization.locale)
					filter(where localization.name<>''))[1:16] names,
				(array_agg(left(localization.summary,256)||' '||left(localization.content_markdown,512)
					order by localization.locale))[1:16] text
			from content_localizations localization
			where localization.subject_type='modpack' and localization.subject_id=any($1::bigint[])
			group by localization.subject_id
		), creator_names as (
			select binding.subject_id,(array_agg(distinct left(creator.name,512)
				order by left(creator.name,512)))[1:16] items
			from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
			where binding.subject_type='modpack' and binding.subject_id=any($1::bigint[]) group by binding.subject_id
		), tags as (
			select tag.modpack_id,(array_agg(left(tag.tag,128) order by tag.tag))[1:64] items from modpack_tags tag
			where tag.modpack_id=any($1::bigint[]) group by tag.modpack_id
		), compatibilities as (
			select compatibility.modpack_id,
				(array_agg(distinct left(compatibility.minecraft_version,64)
					order by left(compatibility.minecraft_version,64)))[1:64] versions,
				(array_agg(distinct left(compatibility.loader,64)
					order by left(compatibility.loader,64)))[1:64] loaders
			from modpack_loader_compatibilities compatibility
			where compatibility.modpack_id=any($1::bigint[]) group by compatibility.modpack_id
		)
		select pack.id,'modpack'::text,pack.public_id,pack.slug,
			array[left(pack.primary_name,2048),left(pack.secondary_name,2048),left(pack.abbreviation,512)]||
				coalesce(localizations.names,'{}'::text[]),
			array[left(pack.summary,2048),left(pack.body_markdown,8192)]||coalesce(localizations.text,'{}'::text[]),'{}'::text[],
			coalesce(creator_names.items,'{}'::text[]),
			coalesce((select array_agg(left(keyword.value,128) order by keyword.ordinal)
				from unnest(pack.search_keywords) with ordinality keyword(value,ordinal) where keyword.ordinal<=64),'{}'::text[]),
			coalesce(tags.items,'{}'::text[]),
			coalesce(compatibilities.versions,'{}'::text[]),coalesce(compatibilities.loaders,'{}'::text[]),
			pack.review_status,coalesce(pack.submitted_by,0),pack.updated_at
		from selected_modpacks pack left join localizations on localizations.subject_id=pack.id
		left join creator_names on creator_names.subject_id=pack.id left join tags on tags.modpack_id=pack.id
		left join compatibilities on compatibilities.modpack_id=pack.id`},
		{"simple_project", `with selected_projects as (
			select * from simple_projects project where project.id=any($1::bigint[])
		), localizations as (
			select localization.project_id,
				(array_agg(left(localization.name,512) order by localization.locale)
					filter(where localization.name<>''))[1:16] names,
				(array_agg(left(localization.summary,256)||' '||left(localization.body_markdown,512)
					order by localization.locale))[1:16] text
			from simple_project_localizations localization
			where localization.project_id=any($1::bigint[]) group by localization.project_id
		), creator_names as (
			select binding.subject_type,binding.subject_id,(array_agg(distinct left(creator.name,512)
				order by left(creator.name,512)))[1:16] items
			from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
			where binding.subject_type in ('plugin','map','resource_pack','shader_pack','datapack','addon')
				and binding.subject_id=any($1::bigint[]) group by binding.subject_type,binding.subject_id
		)
		select project.id,project.project_type,project.public_id,project.slug,
			array_prepend(left(project.primary_name,2048),coalesce(localizations.names,'{}'::text[])),
			array_prepend(left(project.summary,2048),array_prepend(left(project.body_markdown,8192),
				coalesce(localizations.text,'{}'::text[]))),
			'{}'::text[],coalesce(creator_names.items,'{}'::text[]),
			coalesce((select array_agg(left(keyword.value,128) order by keyword.ordinal)
				from unnest(project.search_keywords) with ordinality keyword(value,ordinal) where keyword.ordinal<=64),'{}'::text[]),
			coalesce((select array_agg(left(category.value,128) order by category.ordinal)
				from unnest(project.categories) with ordinality category(value,ordinal) where category.ordinal<=64),'{}'::text[]),
			coalesce((select array_agg(left(version.value,64) order by version.ordinal)
				from unnest(project.minecraft_versions) with ordinality version(value,ordinal) where version.ordinal<=64),'{}'::text[]),
			coalesce((select array_agg(left(loader.value,64) order by loader.ordinal)
				from unnest(project.loaders) with ordinality loader(value,ordinal) where loader.ordinal<=64),'{}'::text[]),
			project.review_status,coalesce(project.submitted_by,0),project.updated_at
		from selected_projects project left join localizations on localizations.project_id=project.id
		left join creator_names on creator_names.subject_type=project.project_type and creator_names.subject_id=project.id`},
	}
	for _, query := range queries {
		if onlyType != "" && onlyType != query.documentType {
			continue
		}
		rows, err := worker.db.Query(ctx, query.sql, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, submittedBy int64
			var entityType, publicID, slug, reviewStatus string
			var names, text, identifiers, creators, keywords, categories, versions, loaders []string
			var updated time.Time
			if err = rows.Scan(&id, &entityType, &publicID, &slug, &names, &text, &identifiers, &creators, &keywords, &categories,
				&versions, &loaders, &reviewStatus, &submittedBy, &updated); err != nil {
				rows.Close()
				return nil, err
			}
			documents = append(documents, map[string]any{
				"id": documentKey(query.documentType, id), "internal_id": id, "entity_type": entityType,
				"public_id": publicID, "slug": slug, "names": compactStrings(names), "text": compactStrings(text),
				"identifiers": compactStrings(identifiers), "creators": compactStrings(creators), "keywords": compactStrings(keywords),
				"categories": compactStrings(categories), "minecraft_versions": compactStrings(versions), "loaders": compactStrings(loaders),
				"review_status": reviewStatus, "submitted_by": submittedBy, "updated_at": updated.Unix(),
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return documents, nil
}

func (worker *Worker) loadCommunityDocuments(ctx context.Context, ids []int64) ([]map[string]any, error) {
	rows, err := worker.db.Query(ctx, `select post.id,post.kind,
		array_prepend(post.title,coalesce((select array_agg(translation.title) from community_post_translations translation where translation.post_id=post.id),'{}'::text[])),
		array_prepend(post.body_markdown,coalesce((select array_agg(translation.body_markdown) from community_post_translations translation where translation.post_id=post.id),'{}'::text[])),post.minecraft_versions,
		post.review_status,post.status,post.author_id,coalesce(post.published_at,post.created_at),
		coalesce((select array_agg(route.public_id) from community_post_project_refs ref join public_routes route on route.entity_type=ref.target_type and route.internal_id=ref.target_id where ref.post_id=post.id),'{}'::text[]),
		coalesce((select array_agg(entity.public_id) from community_post_resource_refs ref join catalog_entities entity on entity.id=ref.resource_id where ref.post_id=post.id),'{}'::text[])
		from community_posts post where post.id=any($1::bigint[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := make([]map[string]any, 0)
	for rows.Next() {
		var id, authorID int64
		var kind, reviewStatus, status string
		var titles, bodies, versions, projectIDs, resourceIDs []string
		var published time.Time
		if err = rows.Scan(&id, &kind, &titles, &bodies, &versions, &reviewStatus, &status, &authorID, &published, &projectIDs, &resourceIDs); err != nil {
			return nil, err
		}
		documents = append(documents, map[string]any{"id": documentKey("community_post", id), "internal_id": id,
			"kind": kind, "title": compactStrings(titles), "body": compactStrings(bodies), "minecraft_versions": compactStrings(versions),
			"project_ids": compactStrings(projectIDs), "resource_ids": compactStrings(resourceIDs), "review_status": reviewStatus,
			"status": status, "author_id": authorID, "published_at": published.Unix()})
	}
	return documents, rows.Err()
}

func (worker *Worker) loadCreatorDocuments(ctx context.Context, ids []int64) ([]map[string]any, error) {
	rows, err := worker.db.Query(ctx, `select creator.id,creator.kind,creator.name,creator.description_markdown,
		coalesce((select array_agg(localization.content_markdown) from content_localizations localization where localization.subject_type='creator' and localization.subject_id=creator.id),'{}'::text[]),
		creator.review_status,coalesce(creator.created_by,0),coalesce((select claim.user_id from creator_claims claim where claim.creator_id=creator.id and claim.status='approved' limit 1),0),creator.updated_at
		from creators creator where creator.id=any($1::bigint[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := make([]map[string]any, 0)
	for rows.Next() {
		var id, createdBy, claimedBy int64
		var kind, name, description, reviewStatus string
		var localized []string
		var updated time.Time
		if err = rows.Scan(&id, &kind, &name, &description, &localized, &reviewStatus, &createdBy, &claimedBy, &updated); err != nil {
			return nil, err
		}
		documents = append(documents, map[string]any{"id": documentKey("creator", id), "internal_id": id,
			"kind": kind, "name": name, "text": compactStrings(append([]string{description}, localized...)),
			"review_status": reviewStatus, "created_by": createdBy, "claimed_user_id": claimedBy, "updated_at": updated.Unix()})
	}
	return documents, rows.Err()
}

func (worker *Worker) loadResourceDocuments(ctx context.Context, ids []int64) ([]map[string]any, error) {
	rows, err := worker.db.Query(ctx, `select entity.id,entity.public_id,resource.kind_code,resource.namespace,resource.canonical_id,entity.status,
		greatest(entity.updated_at,resource.updated_at),
		coalesce((select array_agg(distinct value) from (
			select localization.name value from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name<>''
			union all select imported_name.value from resource_import_snapshots snapshot cross join lateral jsonb_each_text(snapshot.names) imported_name where snapshot.resource_id=entity.id and imported_name.value<>''
		) names),'{}'::text[])
		from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
		where entity.id=any($1::bigint[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var publicID, kindCode, namespace, canonicalID, status string
		var updated time.Time
		var names []string
		if err = rows.Scan(&id, &publicID, &kindCode, &namespace, &canonicalID, &status, &updated, &names); err != nil {
			return nil, err
		}
		documents = append(documents, map[string]any{"id": documentKey("resource", id), "internal_id": id,
			"public_id": publicID, "kind_code": kindCode, "namespace": namespace, "canonical_id": canonicalID,
			"names": compactStrings(names), "status": status, "updated_at": updated.Unix()})
	}
	return documents, rows.Err()
}

func (worker *Worker) loadServerDocuments(ctx context.Context, ids []int64) ([]map[string]any, error) {
	rows, err := worker.db.Query(ctx, `select server.id,server.public_id,server.name,
		array[server.short_description,server.body_markdown,server.last_motd],server.primary_tag,
		server.languages,server.minecraft_versions,server.loader,server.modded,server.last_online,
		server.has_whitelist,server.online_mode,server.review_status,server.created_at,server.updated_at,
		coalesce((popularity.heat_score*1000000)::bigint,0),coalesce(popularity.download_count,0),
		coalesce(popularity.favorite_count,0),coalesce((popularity.bayesian_rating*10000)::bigint,0),
		coalesce(popularity.rating_count,0),coalesce(popularity.view_count,0),coalesce(popularity.comment_count,0),
		coalesce((select array_agg(distinct source.value) from (
			select term.value from minecraft_server_mods server_mod left join mods mod on mod.id=server_mod.mod_id
			cross join lateral unnest(array[server_mod.raw_mod_id,coalesce(mod.project_code,''),coalesce(mod.slug,''),
				coalesce(mod.primary_name,''),coalesce(mod.secondary_name,'')]) term(value)
			where server_mod.server_id=server.id
			union all
			select identifier.identifier from minecraft_server_mods server_mod
			join mod_identifiers identifier on identifier.mod_id=server_mod.mod_id where server_mod.server_id=server.id
		) source where source.value<>''),'{}'::text[])
		from minecraft_servers server
		left join public_routes popularity_route
			on popularity_route.entity_type='minecraft_server' and popularity_route.internal_id=server.id
		left join content_popularity_stats popularity on popularity.object_route_id=popularity_route.id
		where server.id=any($1::bigint[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var publicID, name, primaryTag, loader, reviewStatus string
		var text, languages, versions, mods []string
		var modded, online, whitelist, onlineMode bool
		var created, updated time.Time
		var heat, downloads, favorites, rating, ratingCount, views, comments int64
		if err = rows.Scan(&id, &publicID, &name, &text, &primaryTag, &languages, &versions, &loader, &modded,
			&online, &whitelist, &onlineMode, &reviewStatus, &created, &updated, &heat, &downloads,
			&favorites, &rating, &ratingCount, &views, &comments, &mods); err != nil {
			return nil, err
		}
		heatSortAsc := heat * 2
		heatSortDesc := heat * 2
		if online {
			heatSortDesc++
		} else {
			heatSortAsc++
		}
		documents = append(documents, map[string]any{
			"id": documentKey("server", id), "internal_id": id, "public_id": publicID, "name": name,
			"text": compactStrings(text), "mods": compactStrings(mods), "primary_tag": primaryTag,
			"languages": compactStrings(languages), "minecraft_versions": compactStrings(versions), "loader": loader,
			"modded": modded, "online": online, "whitelist": whitelist, "online_mode": onlineMode,
			"review_status": reviewStatus, "created_at": created.Unix(), "updated_at": updated.Unix(),
			"heat_sort_asc": heatSortAsc, "heat_sort_desc": heatSortDesc, "download_count": downloads,
			"favorite_count": favorites, "rating_score": rating, "rating_count": ratingCount,
			"view_count": views, "comment_count": comments,
		})
	}
	return documents, rows.Err()
}

func compactStrings(values []string) []string {
	result := make([]string, 0, min(len(values), searchDocumentArrayItemLimit))
	seen := make(map[string]bool, len(values))
	remainingBytes := searchDocumentArrayByteLimit
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || remainingBytes == 0 || len(result) == searchDocumentArrayItemLimit {
			break
		}
		value = truncateSearchString(value, remainingBytes)
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		remainingBytes -= len(value)
	}
	return result
}

func truncateSearchString(value string, maximumBytes int) string {
	if len(value) <= maximumBytes {
		return value
	}
	value = value[:maximumBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
