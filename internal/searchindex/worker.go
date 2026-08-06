package searchindex

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	searchQueueBatchSize  = 200
	searchImportBatchSize = 500
	searchPollInterval    = 2 * time.Second
)

type Worker struct {
	db     *pgxpool.Pool
	client *Client
}

type queueJob struct {
	DocumentType string
	DocumentID   int64
	Operation    string
	Token        time.Time
}

func NewWorker(db *pgxpool.Pool, client *Client) *Worker {
	return &Worker{db: db, client: client}
}

func (worker *Worker) Start(ctx context.Context) {
	if worker == nil || worker.db == nil || worker.client == nil || !worker.client.Enabled() {
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
	for kind, template := range collectionSchemas() {
		oldCollection, err := worker.client.AliasTarget(ctx, worker.client.Alias(kind))
		if err != nil && !isStatus(err, 404) {
			return fmt.Errorf("read %s alias: %w", kind, err)
		}
		collection := worker.client.VersionedCollection(kind)
		template.Name = collection
		if err = worker.client.CreateCollection(ctx, template); err != nil {
			return fmt.Errorf("create %s collection: %w", kind, err)
		}
		documents, loadErr := worker.loadDocuments(ctx, kind, nil)
		if loadErr != nil {
			_ = worker.client.DeleteCollection(context.Background(), collection)
			return fmt.Errorf("load %s search documents: %w", kind, loadErr)
		}
		for start := 0; start < len(documents); start += searchImportBatchSize {
			end := min(start+searchImportBatchSize, len(documents))
			if err = worker.client.ImportDocuments(ctx, collection, documents[start:end]); err != nil {
				_ = worker.client.DeleteCollection(context.Background(), collection)
				return fmt.Errorf("import %s search documents: %w", kind, err)
			}
		}
		if err = worker.client.UpsertAlias(ctx, worker.client.Alias(kind), collection); err != nil {
			_ = worker.client.DeleteCollection(context.Background(), collection)
			return fmt.Errorf("activate %s search collection: %w", kind, err)
		}
		if _, err = worker.db.Exec(ctx, `insert into search_index_state(collection_kind,schema_version,collection_name,rebuilt_at)
			values($1,$2,$3,now()) on conflict(collection_kind) do update set
			schema_version=excluded.schema_version,collection_name=excluded.collection_name,rebuilt_at=excluded.rebuilt_at`,
			kind, projectionSchemaVersion, collection); err != nil {
			return fmt.Errorf("record %s search projection: %w", kind, err)
		}
		if oldCollection != "" && oldCollection != collection {
			if err = worker.client.DeleteCollection(ctx, oldCollection); err != nil {
				log.Printf("delete superseded Typesense collection %s: %v", oldCollection, err)
			}
		}
	}
	return worker.drain(ctx)
}

func (worker *Worker) projectionsCurrent(ctx context.Context) (bool, error) {
	for kind := range collectionSchemas() {
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
		if storedVersion != projectionSchemaVersion || storedCollection != collection {
			return false, nil
		}
		exists, err := worker.client.CollectionExists(ctx, collection)
		if err != nil {
			return false, fmt.Errorf("inspect %s search collection: %w", kind, err)
		}
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

func (worker *Worker) drain(ctx context.Context) error {
	jobs, err := worker.claim(ctx)
	if err != nil || len(jobs) == 0 {
		return err
	}
	byType := make(map[string][]queueJob)
	for _, job := range jobs {
		byType[job.DocumentType] = append(byType[job.DocumentType], job)
	}
	for documentType, typedJobs := range byType {
		kind := collectionKind(documentType)
		ids := make([]int64, 0, len(typedJobs))
		for _, job := range typedJobs {
			if job.Operation == "upsert" {
				ids = append(ids, job.DocumentID)
			}
		}
		documents, loadErr := worker.loadTypedDocuments(ctx, documentType, ids)
		if loadErr != nil {
			worker.retry(ctx, typedJobs, loadErr)
			continue
		}
		found := make(map[int64]bool, len(documents))
		for _, document := range documents {
			if id, ok := document["internal_id"].(int64); ok {
				found[id] = true
			}
		}
		if loadErr = worker.client.ImportDocuments(ctx, worker.client.Alias(kind), documents); loadErr != nil {
			worker.retry(ctx, typedJobs, loadErr)
			continue
		}
		failed := false
		for _, job := range typedJobs {
			if job.Operation == "delete" || !found[job.DocumentID] {
				if deleteErr := worker.client.DeleteDocument(ctx, worker.client.Alias(kind), documentKey(documentType, job.DocumentID)); deleteErr != nil {
					worker.retry(ctx, []queueJob{job}, deleteErr)
					failed = true
					continue
				}
			}
			worker.complete(ctx, job)
		}
		if failed {
			continue
		}
	}
	return nil
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

func (worker *Worker) complete(ctx context.Context, job queueJob) {
	_, _ = worker.db.Exec(ctx, `delete from search_index_queue where document_type=$1 and document_id=$2 and updated_at=$3`,
		job.DocumentType, job.DocumentID, job.Token)
}

func (worker *Worker) retry(ctx context.Context, jobs []queueJob, cause error) {
	message := cause.Error()
	if len(message) > 2000 {
		message = message[:2000]
	}
	for _, job := range jobs {
		_, _ = worker.db.Exec(ctx, `update search_index_queue set available_at=now()+
			interval '1 second'*least(900,5*power(2,least(attempts,8))::integer),last_error=$4,updated_at=clock_timestamp()
			where document_type=$1 and document_id=$2 and updated_at=$3`, job.DocumentType, job.DocumentID, job.Token, message)
	}
}

func collectionKind(documentType string) string {
	switch documentType {
	case "mod", "modpack", "simple_project":
		return "projects"
	case "community_post":
		return "community"
	case "creator":
		return "creators"
	case "server":
		return "servers"
	default:
		return "resources"
	}
}

func documentKey(documentType string, id int64) string {
	return documentType + "_" + strconv.FormatInt(id, 10)
}

func (worker *Worker) loadDocuments(ctx context.Context, kind string, ids []int64) ([]map[string]any, error) {
	switch kind {
	case "projects":
		return worker.loadProjectDocuments(ctx, ids, "")
	case "community":
		return worker.loadCommunityDocuments(ctx, ids)
	case "creators":
		return worker.loadCreatorDocuments(ctx, ids)
	case "resources":
		return worker.loadResourceDocuments(ctx, ids)
	case "servers":
		return worker.loadServerDocuments(ctx, ids)
	default:
		return nil, fmt.Errorf("unknown search collection %q", kind)
	}
}

func (worker *Worker) loadTypedDocuments(ctx context.Context, documentType string, ids []int64) ([]map[string]any, error) {
	switch documentType {
	case "mod", "modpack", "simple_project":
		return worker.loadProjectDocuments(ctx, ids, documentType)
	case "community_post":
		return worker.loadCommunityDocuments(ctx, ids)
	case "creator":
		return worker.loadCreatorDocuments(ctx, ids)
	case "resource":
		return worker.loadResourceDocuments(ctx, ids)
	case "server":
		return worker.loadServerDocuments(ctx, ids)
	default:
		return nil, fmt.Errorf("unknown search document type %q", documentType)
	}
}

func selected(ids []int64) (bool, []int64) {
	// nil is used by a full rebuild; a non-nil empty slice means that an
	// incremental queue batch contains only deletions and must not load every
	// document in the collection.
	return ids == nil, ids
}

func (worker *Worker) loadProjectDocuments(ctx context.Context, ids []int64, onlyType string) ([]map[string]any, error) {
	all, selectedIDs := selected(ids)
	documents := make([]map[string]any, 0)
	queries := []struct {
		documentType string
		sql          string
	}{
		{"mod", `select mod.id,'mod'::text,mod.project_code,mod.slug,array[mod.primary_name,mod.secondary_name,mod.abbreviation] ||
			coalesce((select array_agg(localization.name) from content_localizations localization where localization.subject_type='mod' and localization.subject_id=mod.id and localization.name<>''),'{}'::text[]),
			array[mod.summary,mod.body_markdown] || coalesce((select array_agg(localization.summary||' '||localization.content_markdown) from content_localizations localization where localization.subject_type='mod' and localization.subject_id=mod.id),'{}'::text[]),
			coalesce((select array_agg(identifier.identifier order by identifier.display_order,identifier.id) from mod_identifiers identifier where identifier.mod_id=mod.id),'{}'::text[]),
			coalesce((select array_agg(distinct creator.name) from content_creator_bindings binding join creators creator on creator.id=binding.creator_id where binding.subject_type='mod' and binding.subject_id=mod.id),'{}'::text[]),
			mod.search_keywords,coalesce((select array_agg(tag order by tag) from mod_tags where mod_id=mod.id),'{}'::text[]),
			coalesce((select array_agg(distinct minecraft_version) from mod_loader_compatibilities where mod_id=mod.id),'{}'::text[]),
			coalesce((select array_agg(distinct loader) from mod_loader_compatibilities where mod_id=mod.id),'{}'::text[]),
			mod.review_status,coalesce(mod.created_by,0),mod.updated_at from mods mod where ($1 or mod.id=any($2::bigint[]))`},
		{"modpack", `select pack.id,'modpack'::text,pack.public_id,pack.slug,array[pack.primary_name,pack.secondary_name,pack.abbreviation] ||
			coalesce((select array_agg(localization.name) from content_localizations localization where localization.subject_type='modpack' and localization.subject_id=pack.id and localization.name<>''),'{}'::text[]),
			array[pack.summary,pack.body_markdown] || coalesce((select array_agg(localization.summary||' '||localization.content_markdown) from content_localizations localization where localization.subject_type='modpack' and localization.subject_id=pack.id),'{}'::text[]),'{}'::text[],
			coalesce((select array_agg(distinct creator.name) from content_creator_bindings binding join creators creator on creator.id=binding.creator_id where binding.subject_type='modpack' and binding.subject_id=pack.id),'{}'::text[]),
			pack.search_keywords,coalesce((select array_agg(tag order by tag) from modpack_tags where modpack_id=pack.id),'{}'::text[]),
			coalesce((select array_agg(distinct minecraft_version) from modpack_loader_compatibilities where modpack_id=pack.id),'{}'::text[]),
			coalesce((select array_agg(distinct loader) from modpack_loader_compatibilities where modpack_id=pack.id),'{}'::text[]),
			pack.review_status,coalesce(pack.created_by,0),pack.updated_at from modpacks pack where ($1 or pack.id=any($2::bigint[]))`},
		{"simple_project", `select project.id,project.project_type,project.public_id,project.slug,
			array_prepend(project.primary_name,coalesce((select array_agg(localization.name) from simple_project_localizations localization where localization.project_id=project.id and localization.name<>''),'{}'::text[])),
			array_prepend(project.summary,array_prepend(project.body_markdown,coalesce((select array_agg(localization.summary||' '||localization.body_markdown) from simple_project_localizations localization where localization.project_id=project.id),'{}'::text[]))),
			'{}'::text[],coalesce((select array_agg(distinct creator.name) from content_creator_bindings binding join creators creator on creator.id=binding.creator_id where binding.subject_type=project.project_type and binding.subject_id=project.id),'{}'::text[]),
			project.search_keywords,project.categories,project.minecraft_versions,project.loaders,
			project.review_status,coalesce(project.created_by,0),project.updated_at from simple_projects project where ($1 or project.id=any($2::bigint[]))`},
	}
	for _, query := range queries {
		if onlyType != "" && onlyType != query.documentType {
			continue
		}
		rows, err := worker.db.Query(ctx, query.sql, all, selectedIDs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, createdBy int64
			var entityType, publicID, slug, reviewStatus string
			var names, text, identifiers, creators, keywords, categories, versions, loaders []string
			var updated time.Time
			if err = rows.Scan(&id, &entityType, &publicID, &slug, &names, &text, &identifiers, &creators, &keywords, &categories,
				&versions, &loaders, &reviewStatus, &createdBy, &updated); err != nil {
				rows.Close()
				return nil, err
			}
			documents = append(documents, map[string]any{
				"id": documentKey(query.documentType, id), "internal_id": id, "entity_type": entityType,
				"public_id": publicID, "slug": slug, "names": compactStrings(names), "text": compactStrings(text),
				"identifiers": compactStrings(identifiers), "creators": compactStrings(creators), "keywords": compactStrings(keywords),
				"categories": compactStrings(categories), "minecraft_versions": compactStrings(versions), "loaders": compactStrings(loaders),
				"review_status": reviewStatus, "created_by": createdBy, "updated_at": updated.Unix(),
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
	all, selectedIDs := selected(ids)
	rows, err := worker.db.Query(ctx, `select post.id,post.kind,
		array_prepend(post.title,coalesce((select array_agg(translation.title) from community_post_translations translation where translation.post_id=post.id),'{}'::text[])),
		array_prepend(post.body_markdown,coalesce((select array_agg(translation.body_markdown) from community_post_translations translation where translation.post_id=post.id),'{}'::text[])),post.minecraft_versions,
		post.review_status,post.status,post.author_id,coalesce(post.published_at,post.created_at),
		coalesce((select array_agg(route.public_id) from community_post_project_refs ref join public_routes route on route.entity_type=ref.target_type and route.internal_id=ref.target_id where ref.post_id=post.id),'{}'::text[]),
		coalesce((select array_agg(entity.public_id) from community_post_resource_refs ref join catalog_entities entity on entity.id=ref.resource_id where ref.post_id=post.id),'{}'::text[])
		from community_posts post where ($1 or post.id=any($2::bigint[]))`, all, selectedIDs)
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
	all, selectedIDs := selected(ids)
	rows, err := worker.db.Query(ctx, `select creator.id,creator.kind,creator.name,creator.description_markdown,
		coalesce((select array_agg(localization.content_markdown) from content_localizations localization where localization.subject_type='creator' and localization.subject_id=creator.id),'{}'::text[]),
		creator.review_status,coalesce(creator.created_by,0),coalesce(creator.claimed_by,0),creator.updated_at
		from creators creator where ($1 or creator.id=any($2::bigint[]))`, all, selectedIDs)
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
			"review_status": reviewStatus, "created_by": createdBy, "claimed_by": claimedBy, "updated_at": updated.Unix()})
	}
	return documents, rows.Err()
}

func (worker *Worker) loadResourceDocuments(ctx context.Context, ids []int64) ([]map[string]any, error) {
	all, selectedIDs := selected(ids)
	rows, err := worker.db.Query(ctx, `select entity.id,entity.public_id,resource.kind_code,resource.namespace,resource.canonical_id,entity.status,
		greatest(entity.updated_at,resource.updated_at),
		coalesce((select array_agg(distinct value) from (
			select localization.name value from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name<>''
			union all select imported_name.value from resource_import_snapshots snapshot cross join lateral jsonb_each_text(snapshot.names) imported_name where snapshot.resource_id=entity.id and imported_name.value<>''
		) names),'{}'::text[])
		from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
		where ($1 or entity.id=any($2::bigint[]))`, all, selectedIDs)
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
	all, selectedIDs := selected(ids)
	rows, err := worker.db.Query(ctx, `select server.id,server.public_id,server.name,
		array[server.short_description,server.body_markdown,server.last_motd],server.primary_tag,
		server.languages,server.minecraft_versions,server.loader,server.modded,server.last_online,
		server.has_whitelist,server.online_mode,server.review_status,server.updated_at,
		coalesce((select array_agg(distinct source.value) from (
			select term.value from minecraft_server_mods server_mod left join mods mod on mod.id=server_mod.mod_id
			cross join lateral unnest(array[server_mod.raw_mod_id,coalesce(mod.public_id,''),coalesce(mod.project_code,''),
				coalesce(mod.primary_name,''),coalesce(mod.secondary_name,'')]) term(value)
			where server_mod.server_id=server.id
			union all
			select identifier.identifier from minecraft_server_mods server_mod
			join mod_identifiers identifier on identifier.mod_id=server_mod.mod_id where server_mod.server_id=server.id
		) source where source.value<>''),'{}'::text[])
		from minecraft_servers server where ($1 or server.id=any($2::bigint[]))`, all, selectedIDs)
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
		var updated time.Time
		if err = rows.Scan(&id, &publicID, &name, &text, &primaryTag, &languages, &versions, &loader, &modded,
			&online, &whitelist, &onlineMode, &reviewStatus, &updated, &mods); err != nil {
			return nil, err
		}
		documents = append(documents, map[string]any{
			"id": documentKey("server", id), "internal_id": id, "public_id": publicID, "name": name,
			"text": compactStrings(text), "mods": compactStrings(mods), "primary_tag": primaryTag,
			"languages": compactStrings(languages), "minecraft_versions": compactStrings(versions), "loader": loader,
			"modded": modded, "online": online, "whitelist": whitelist, "online_mode": onlineMode,
			"review_status": reviewStatus, "updated_at": updated.Unix(),
		})
	}
	return documents, rows.Err()
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
