package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/security"

	"github.com/jackc/pgx/v5"
)

type catalogEditorEntity struct {
	ID                  int64
	IdentityKey         string
	PublicID            string
	EntityType          string
	Status              string
	DefaultLocale       string
	PublishedRevisionID *int64
}

func (s *Server) catalogEditorEntityByPublicID(ctx context.Context, publicID, entityType string) (catalogEditorEntity, error) {
	var entity catalogEditorEntity
	err := s.db.QueryRow(ctx, `select id,identity_key,public_id,entity_type,status,default_locale,published_revision_id
		from catalog_entities where public_id=$1 and entity_type=$2`, strings.ToLower(strings.TrimSpace(publicID)), entityType).
		Scan(&entity.ID, &entity.IdentityKey, &entity.PublicID, &entity.EntityType, &entity.Status, &entity.DefaultLocale, &entity.PublishedRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity, errCatalogEditorNotFound
	}
	return entity, err
}

func (s *Server) submitCatalogEditorMutation(r *http.Request, snapshot catalogEditorSnapshot, requestedBase *string) (catalogEditResult, error) {
	var result catalogEditResult
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return result, err
	}
	defer tx.Rollback(r.Context())

	if snapshot.Operation == "create" {
		if strings.TrimSpace(snapshot.IdentityKey) == "" {
			return result, errCatalogEditorInvalid
		}
		_, insertErr := tx.Exec(r.Context(), `insert into catalog_entities(identity_key,public_id,entity_type,status)
			values($1,$2,$3,'placeholder') on conflict(identity_key) do nothing`, snapshot.IdentityKey, snapshot.PublicID, snapshot.Kind)
		if insertErr != nil {
			return result, insertErr
		}
		if err = tx.QueryRow(r.Context(), `select id from catalog_entities where identity_key=$1`, snapshot.IdentityKey).Scan(&snapshot.EntityID); err != nil {
			return result, err
		}
	}
	if snapshot.EntityID <= 0 {
		return result, errCatalogEditorInvalid
	}
	var publishedRevisionID *int64
	var status, publicID, entityType string
	if err = tx.QueryRow(r.Context(), `select status,published_revision_id,public_id,entity_type from catalog_entities where id=$1 for update`, snapshot.EntityID).
		Scan(&status, &publishedRevisionID, &publicID, &entityType); errors.Is(err, pgx.ErrNoRows) {
		return result, errCatalogEditorNotFound
	} else if err != nil {
		return result, err
	}
	if entityType != snapshot.Kind {
		return result, errCatalogEditorConflict
	}
	snapshot.PublicID = publicID
	if snapshot.Operation != "create" && snapshot.Operation != "edit" && snapshot.Operation != "delete" {
		return result, errCatalogEditorInvalid
	}
	requestedBaseID, err := resolveRevisionPublicID(r.Context(), tx, requestedBase)
	if err != nil {
		return result, errCatalogEditorConflict
	}
	if !catalogMutationBaseMatches(snapshot.Operation, status, requestedBaseID, publishedRevisionID) {
		return result, errCatalogEditorConflict
	}
	if snapshot.Operation == "create" {
		var pending bool
		if err = tx.QueryRow(r.Context(), `select exists(select 1 from change_requests
			where aggregate_type=$1 and aggregate_key=$2 and status='pending')`,
			catalogAggregateForKind(snapshot.Kind), snapshot.PublicID).Scan(&pending); err != nil {
			return result, err
		}
		// A rejected or withdrawn first submission leaves the deterministic
		// identity placeholder in place so its immutable review history remains
		// referentially valid. It may be resubmitted only after the previous
		// request is no longer pending and before anything has been published.
		if pending {
			return result, errCatalogEditorConflict
		}
	}
	claims := currentClaims(r)
	allowForeignFiles := claimsAllow(claims, "content.review") || claimsAllow(claims, "admin.*")
	snapshot.AllowForeignFiles = allowForeignFiles
	if err = validateCatalogSnapshotReferencesTx(r.Context(), tx, snapshot, claims.Subject, allowForeignFiles); err != nil {
		return result, err
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		return result, err
	}
	reviewConfig := loadReviewConfig(r.Context(), s.db)
	reviewRequired := catalogMutationReviewRequired(reviewConfig, snapshot.Operation)
	if catalogMutationBypassesReview(claims) {
		reviewRequired = false
	}
	reviewStatus := "approved"
	if reviewRequired {
		reviewStatus = "pending"
	}
	reason := strings.TrimSpace(catalogSnapshotReason(snapshot))
	if reason == "" {
		reason = "Catalog " + snapshot.Operation
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: snapshot.Kind, EntityID: snapshot.EntityID, AggregateType: catalogAggregateForKind(snapshot.Kind), AggregateKey: snapshot.PublicID,
		BaseRevision: publishedRevisionID, Snapshot: raw, Reason: reason, ActorID: claims.Subject, Source: "user",
		Status: reviewStatus, Metadata: map[string]any{"kind": snapshot.Kind, "operation": snapshot.Operation, "publicId": snapshot.PublicID}, Request: r,
	})
	if err != nil {
		return result, err
	}
	if reviewStatus == "approved" {
		if err = publishCatalogEditorSnapshotTx(r.Context(), tx, created.RevisionID, raw, claims.Subject); err != nil {
			return result, err
		}
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			return result, err
		}
		if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, created.ChangeRequestID, claims.Subject, "automatic publication", r); err != nil {
			return result, err
		}
	}
	activityEventID, err := insertCatalogActivityTx(r.Context(), tx, claims.Subject, snapshot)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return result, err
	}
	skipRequestActivity(r)
	result = catalogEditResult{ObjectPublicID: snapshot.PublicID, Operation: snapshot.Operation, RevisionID: created.RevisionPublicID,
		ChangeRequestID: created.ChangeRequestPublicID, ReviewStatus: reviewStatus, ActivityEventID: activityEventID}
	return result, nil
}

func catalogMutationBypassesReview(claims security.Claims) bool {
	return claimsAllow(claims, "content.no-review") || claimsAllow(claims, "admin.*")
}

func catalogMutationBaseMatches(operation, status string, requested, published *int64) bool {
	if operation == "create" {
		return status == "placeholder" && requested == nil && published == nil
	}
	// Imported observations can predate the canonical editor and therefore
	// legitimately have no published canonical revision. Their first manual
	// edit uses a nil base; after publication every edit requires the exact
	// canonical revision id as usual.
	return (operation == "edit" || operation == "delete") && status == "active" && sameRevision(requested, published)
}

func catalogMutationReviewRequired(config reviewConfig, operation string) bool {
	switch operation {
	case "create":
		return config.CatalogCreate
	case "delete":
		return config.CatalogDelete
	default:
		return config.CatalogEdit
	}
}

func catalogSnapshotReason(snapshot catalogEditorSnapshot) string {
	if strings.TrimSpace(snapshot.Reason) != "" {
		return snapshot.Reason
	}
	switch {
	case snapshot.Resource != nil:
		return snapshot.Resource.Reason
	case snapshot.Tag != nil:
		return snapshot.Tag.Reason
	case snapshot.RecipeType != nil:
		return snapshot.RecipeType.Reason
	case snapshot.Template != nil:
		return snapshot.Template.Reason
	case snapshot.Recipe != nil:
		return snapshot.Recipe.Reason
	default:
		return ""
	}
}

func catalogAggregateForKind(kind string) string {
	switch kind {
	case "resource":
		return catalogAggregateResource
	case "tag":
		return catalogAggregateTag
	case "recipe_type":
		return catalogAggregateRecipeType
	case "recipe_template":
		return catalogAggregateRecipeTemplate
	case "recipe":
		return catalogAggregateRecipe
	default:
		return "catalog_editor_unknown"
	}
}

func catalogActivityObjectType(kind string) int16 {
	switch kind {
	case "resource":
		return activity.ObjectResource
	case "tag":
		return activity.ObjectTag
	case "mod":
		return activity.ObjectMod
	case "blueprint":
		return activity.ObjectBlueprint
	default:
		return activity.ObjectRecipe
	}
}

func insertCatalogActivityTx(ctx context.Context, tx pgx.Tx, actorID int64, snapshot catalogEditorSnapshot) (string, error) {
	actionID := activity.ActionEdit
	if snapshot.Operation == "create" {
		actionID = activity.ActionCreate
	} else if snapshot.Operation == "delete" {
		actionID = activity.ActionDelete
	}
	var eventID string
	err := tx.QueryRow(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,object_route_id,markdown_added_bytes,occurred_at)
		select $1,$2,$3,route.id,0,$5 from public_routes route where route.public_id=$4
		returning id::text`, actorID, actionID, catalogActivityObjectType(snapshot.Kind), snapshot.PublicID, time.Now().UTC()).Scan(&eventID)
	return eventID, err
}

func appendCatalogPublishedReviewEventTx(ctx context.Context, tx pgx.Tx, requestID, actorID int64, note string, r *http.Request) error {
	actorSnapshot, err := actorSnapshotTx(ctx, tx, actorID)
	if err != nil {
		return err
	}
	ip, userAgent, _ := auditRequestValues(r)
	_, err = tx.Exec(ctx, `insert into review_events(change_request_id,event_type,actor_id,actor_snapshot,note,ip,user_agent)
		values($1,'published',$2,$3,$4,$5,$6)`, requestID, nullableActorID(actorID), actorSnapshot, note, ip, userAgent)
	if err != nil {
		return err
	}
	return appendReviewedProjectUpdateEventTx(ctx, tx, requestID, actorID)
}

func publishCatalogEditorSnapshotTx(ctx context.Context, tx pgx.Tx, revisionID int64, raw []byte, actorID int64) error {
	var snapshot catalogEditorSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	if err := hydrateCatalogEditorSnapshotTx(ctx, tx, &snapshot); err != nil {
		return err
	}
	var rasterScope ossRasterBindingScope
	if snapshot.Kind == "resource" || snapshot.Kind == "recipe_template" {
		var submittedBy *int64
		if err := tx.QueryRow(ctx, `select created_by from content_revisions where id=$1`, revisionID).Scan(&submittedBy); err != nil {
			return err
		}
		if submittedBy != nil {
			rasterScope.UploaderID = *submittedBy
		}
		rasterScope.AllowAnyUploader = snapshot.AllowForeignFiles
	}
	if snapshot.Operation == "delete" {
		result, err := tx.Exec(ctx, `update catalog_entities set status='archived',archived_at=now(),published_revision_id=$2,updated_at=now()
			where id=$1 and status='active'`, snapshot.EntityID, revisionID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return errCatalogEditorConflict
		}
		return nil
	}

	if _, err := tx.Exec(ctx, `update catalog_entities set status='active',archived_at=null,default_locale=$2,published_revision_id=$3,updated_at=now() where id=$1`,
		snapshot.EntityID, snapshot.DefaultLocale, revisionID); err != nil {
		return err
	}
	if err := materializeCatalogImportLocalizationsTx(ctx, tx, snapshot, revisionID); err != nil {
		return err
	}
	if err := publishCatalogLocalizationsTx(ctx, tx, snapshot.EntityID, snapshot.PublicID, snapshot.Kind, snapshot.Localizations, revisionID, actorID); err != nil {
		return err
	}
	switch snapshot.Kind {
	case "resource":
		return publishCatalogResourceTx(ctx, tx, snapshot, revisionID, actorID, rasterScope)
	case "tag":
		return publishCatalogTagTx(ctx, tx, snapshot, revisionID)
	case "recipe_type":
		return publishCatalogRecipeTypeTx(ctx, tx, snapshot, revisionID, actorID)
	case "recipe_template":
		return publishCatalogRecipeTemplateTx(ctx, tx, snapshot, revisionID, actorID, rasterScope)
	case "recipe":
		return publishCatalogRecipeTx(ctx, tx, snapshot, revisionID, actorID)
	default:
		return errCatalogEditorInvalid
	}
}

// Revision snapshots contain public identities only. Internal keys are resolved
// again inside the publishing transaction so review payloads never expose or
// depend on database primary keys.
func hydrateCatalogEditorSnapshotTx(ctx context.Context, tx pgx.Tx, snapshot *catalogEditorSnapshot) error {
	if snapshot == nil || strings.TrimSpace(snapshot.PublicID) == "" || strings.TrimSpace(snapshot.Kind) == "" {
		return errCatalogEditorInvalid
	}
	if err := tx.QueryRow(ctx, `select id from catalog_entities where public_id=$1 and entity_type=$2`,
		snapshot.PublicID, snapshot.Kind).Scan(&snapshot.EntityID); err != nil {
		return errCatalogEditorReference
	}
	if snapshot.Operation != "delete" && (snapshot.Kind == "recipe_template" || snapshot.Kind == "recipe") {
		if strings.TrimSpace(snapshot.ParentPublicID) == "" {
			return errCatalogEditorInvalid
		}
		if err := tx.QueryRow(ctx, `select id from catalog_entities
			where public_id=$1 and entity_type='recipe_type' and status='active'`,
			snapshot.ParentPublicID).Scan(&snapshot.ParentEntityID); err != nil {
			return errCatalogEditorReference
		}
	}
	if snapshot.OwnerModPublicID != "" {
		var ownerModID int64
		if err := tx.QueryRow(ctx, `select id from mods where project_code=$1`, snapshot.OwnerModPublicID).Scan(&ownerModID); err != nil {
			return errCatalogEditorReference
		}
		snapshot.OwnerModID = &ownerModID
	}
	return nil
}

// materializeCatalogImportLocalizationsTx preserves the imported starting
// language when the first human revision only changes invariant fields. It is
// intentionally insert-only: existing AI or human content wins, and the dirty
// locales in the same editor snapshot are applied immediately afterwards by
// publishCatalogLocalizationsTx.
func materializeCatalogImportLocalizationsTx(ctx context.Context, tx pgx.Tx, snapshot catalogEditorSnapshot, revisionID int64) error {
	if snapshot.Operation != "edit" {
		return nil
	}
	var raw []byte
	var fallbackName string
	var err error
	switch snapshot.Kind {
	case "resource":
		err = tx.QueryRow(ctx, `select imported.names,resource.canonical_id from resource_import_snapshots imported
			join catalog_import_revisions revision on revision.id=imported.revision_id
			join game_resources resource on resource.entity_id=imported.resource_id
			where imported.resource_id=$1 and revision.is_active and revision.status in ('ready','partial')
			order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1`, snapshot.EntityID).
			Scan(&raw, &fallbackName)
	case "recipe_type":
		err = tx.QueryRow(ctx, `select imported.title_names,type.canonical_id from recipe_type_import_snapshots imported
			join catalog_import_revisions revision on revision.id=imported.revision_id
			join recipe_types type on type.entity_id=imported.recipe_type_id
			where imported.recipe_type_id=$1 and revision.is_active and revision.status in ('ready','partial')
			order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1`, snapshot.EntityID).
			Scan(&raw, &fallbackName)
	case "tag":
		err = tx.QueryRow(ctx, `select '{}'::jsonb,tag.canonical_id from tag_import_snapshots imported
			join catalog_import_revisions revision on revision.id=imported.revision_id
			join catalog_tags tag on tag.entity_id=imported.tag_id
			where imported.tag_id=$1 and revision.is_active and revision.status in ('ready','partial')
			order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1`, snapshot.EntityID).
			Scan(&raw, &fallbackName)
	case "recipe_template":
		err = tx.QueryRow(ctx, `select '{}'::jsonb,imported.source_template_id from recipe_template_import_snapshots imported
			join catalog_import_revisions revision on revision.id=imported.revision_id
			where imported.canonical_template_id=$1 and revision.is_active and revision.status in ('ready','partial')
			order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1`, snapshot.EntityID).
			Scan(&raw, &fallbackName)
	case "recipe":
		err = tx.QueryRow(ctx, `select '{}'::jsonb,coalesce(nullif(recipe.canonical_source_id,''),imported.source_recipe_id,imported.source_recipe_key)
			from recipe_import_snapshots imported join catalog_import_revisions revision on revision.id=imported.revision_id
			join recipes recipe on recipe.entity_id=imported.recipe_id
			where imported.recipe_id=$1 and revision.is_active and revision.status in ('ready','partial')
			order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1`, snapshot.EntityID).
			Scan(&raw, &fallbackName)
	default:
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	localizations, _ := catalogImportedEditorLocalizations(raw, snapshot.DefaultLocale, fallbackName)
	for _, localization := range localizations {
		locale, _ := localization["locale"].(string)
		name, _ := localization["name"].(string)
		if locale == "" || strings.TrimSpace(name) == "" {
			continue
		}
		if _, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,summary,content_markdown,
			provenance,source_locale,ai_task_id,revision_no,editable,review_status,published_revision_id,updated_by)
			values($1,$2,$2,$3,$4,'','','import','',null,1,true,'approved',$5,null)
			on conflict(subject_type,subject_id,locale) do nothing`, snapshot.Kind, snapshot.EntityID, locale, name, revisionID); err != nil {
			return err
		}
	}
	return nil
}

func publishCatalogLocalizationsTx(ctx context.Context, tx pgx.Tx, entityID int64, _ string, subjectType string, localizations []catalogLocalizationEdit, revisionID, actorID int64) error {
	for _, localization := range localizations {
		var existingProvenance string
		var existingRevision int64
		err := tx.QueryRow(ctx, `select provenance,revision_no from content_localizations where subject_type=$1 and subject_id=$2 and locale=$3 for update`, subjectType, entityID, localization.Locale).
			Scan(&existingProvenance, &existingRevision)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		provenance := catalogHumanEditProvenance(existingProvenance)
		if err = invalidateAIDerivedLocalizationsTx(ctx, tx, entityID, subjectType, localization.Locale); err != nil {
			return err
		}
		if existingRevision == 0 {
			existingRevision = 1
		} else {
			existingRevision++
		}
		if _, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,summary,content_markdown,provenance,source_locale,ai_task_id,revision_no,editable,review_status,published_revision_id,updated_by)
			values($1,$2,(select id from catalog_entities where id=$2 and entity_type=$1),$3,$4,$5,$6,$7,'',null,$8,true,'approved',$9,$10)
			on conflict(subject_type,subject_id,locale) do update set catalog_entity_id=excluded.catalog_entity_id,name=excluded.name,summary=excluded.summary,
			content_markdown=excluded.content_markdown,provenance=excluded.provenance,source_locale='',ai_task_id=null,
			revision_no=excluded.revision_no,editable=true,review_status='approved',published_revision_id=excluded.published_revision_id,
			updated_by=excluded.updated_by,updated_at=now()`, subjectType, entityID, localization.Locale, localization.Name, localization.Summary,
			localization.ContentMarkdown, provenance, existingRevision, revisionID, nullableActorID(actorID)); err != nil {
			return err
		}
	}
	return nil
}

func catalogHumanEditProvenance(existing string) string {
	if existing == "ai" || existing == "human_corrected" {
		return "human_corrected"
	}
	return "human"
}

func publishCatalogLocalizationSnapshotTx(ctx context.Context, tx pgx.Tx, revisionID int64, raw []byte, actorID int64) error {
	var snapshot catalogLocalizationSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	locale, err := normalizeCatalogLocale(snapshot.Locale)
	if err != nil {
		return errCatalogEditorInvalid
	}
	var revisionNo int64
	publicID := strings.ToLower(strings.TrimSpace(snapshot.SubjectPublicID))
	subjectType := strings.TrimSpace(snapshot.SubjectType)
	if publicID == "" || subjectType == "" {
		if snapshot.EntityID <= 0 {
			return errCatalogEditorInvalid
		}
		if err = tx.QueryRow(ctx, `select public_id,entity_type from catalog_entities where id=$1`, snapshot.EntityID).Scan(&publicID, &subjectType); err != nil {
			return err
		}
	}
	var subjectID int64
	if err = tx.QueryRow(ctx, `select internal_id from public_routes where public_id=$1 and entity_type=$2`, publicID, subjectType).Scan(&subjectID); err != nil {
		return errCatalogEditorReference
	}
	if snapshot.EntityID > 0 && snapshot.EntityID != subjectID {
		return errCatalogEditorConflict
	}
	snapshot.EntityID = subjectID
	if snapshot.AITaskPublicID != nil {
		var aiTaskID int64
		if err = tx.QueryRow(ctx, `select id from ai_tasks where task_uid=$1`, *snapshot.AITaskPublicID).Scan(&aiTaskID); err != nil {
			return errCatalogEditorReference
		}
		snapshot.AITaskID = &aiTaskID
	}
	var catalogEntityID *int64
	_ = tx.QueryRow(ctx, `select id from catalog_entities where id=$1 and public_id=$2 and entity_type=$3 and status<>'archived'`,
		subjectID, publicID, subjectType).Scan(&catalogEntityID)
	_ = tx.QueryRow(ctx, `select revision_no+1 from content_localizations where subject_type=$1 and subject_id=$2 and locale=$3`, subjectType, subjectID, locale).Scan(&revisionNo)
	if revisionNo == 0 {
		revisionNo = 1
	}
	provenance := snapshot.Provenance
	if provenance != "ai" && provenance != "human" && provenance != "human_corrected" && provenance != "import" {
		return errCatalogEditorInvalid
	}
	if provenance == "human" || provenance == "human_corrected" {
		if err = invalidateAIDerivedLocalizationsTx(ctx, tx, subjectID, subjectType, locale); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,summary,content_markdown,provenance,source_locale,ai_task_id,revision_no,editable,review_status,published_revision_id,updated_by)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'approved',$13,$14)
		on conflict(subject_type,subject_id,locale) do update set catalog_entity_id=excluded.catalog_entity_id,name=excluded.name,summary=excluded.summary,content_markdown=excluded.content_markdown,
		provenance=excluded.provenance,source_locale=excluded.source_locale,ai_task_id=excluded.ai_task_id,revision_no=excluded.revision_no,
		editable=excluded.editable,review_status='approved',published_revision_id=excluded.published_revision_id,updated_by=excluded.updated_by,updated_at=now()`,
		subjectType, subjectID, catalogEntityID, locale, snapshot.Name, snapshot.Summary, snapshot.ContentMarkdown, provenance, snapshot.SourceLocale,
		snapshot.AITaskID, revisionNo, snapshot.Editable, revisionID, nullableActorID(actorID)); err != nil {
		return err
	}
	return nil
}

func invalidateAIDerivedLocalizationsTx(ctx context.Context, tx pgx.Tx, subjectID int64, subjectType, sourceLocale string) error {
	_, err := tx.Exec(ctx, `delete from content_localizations
		where subject_id=$1 and subject_type=$2 and source_locale=$3 and locale<>$3 and provenance='ai'`,
		subjectID, subjectType, normalizeContentLocale(sourceLocale))
	return err
}

func publishCatalogResourceTx(
	ctx context.Context,
	tx pgx.Tx,
	snapshot catalogEditorSnapshot,
	revisionID, actorID int64,
	rasterScope ossRasterBindingScope,
) error {
	edit := snapshot.Resource
	if edit == nil {
		return errCatalogEditorInvalid
	}
	kindCode, canonicalID := strings.TrimSpace(edit.KindCode), strings.TrimSpace(edit.CanonicalID)
	if kindCode == "" || canonicalID == "" {
		return errCatalogEditorInvalid
	}
	canonicalDefinition, err := canonicalizeGlobalCatalogResourceDefinition(ctx, tx, kindCode, canonicalID, nonNilJSONObject(edit.Definition))
	if err != nil {
		return err
	}
	edit.Definition = canonicalDefinition
	if err = validateCatalogResourceDefinition(kindCode, canonicalDefinition); err != nil {
		return err
	}
	iconFileID, err := resolveOptionalTrustedRasterOSSFilePublicID(ctx, tx, edit.IconFileID, rasterScope)
	if err != nil {
		return errCatalogEditorReference
	}
	renderFileID, err := resolveOptionalTrustedRasterOSSFilePublicID(ctx, tx, edit.RenderFileID, rasterScope)
	if err != nil {
		return errCatalogEditorReference
	}
	if _, err := tx.Exec(ctx, `insert into resource_kinds(code,family,user_visible) values($1,split_part($1,'.',1),true) on conflict(code) do nothing`, kindCode); err != nil {
		return err
	}
	namespace, path := resourceParts(canonicalID)
	if _, err := tx.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
		values($1,$2,$3,$4,$5,$6,true) on conflict(entity_id) do update set kind_code=excluded.kind_code,canonical_id=excluded.canonical_id,
		namespace=excluded.namespace,resource_path=excluded.resource_path,owner_mod_id=coalesce(excluded.owner_mod_id,game_resources.owner_mod_id),resolved=true,updated_at=now()`,
		snapshot.EntityID, kindCode, canonicalID, namespace, path, snapshot.OwnerModID); err != nil {
		return err
	}
	for _, alias := range uniqueTrimmed([]string{edit.RawCanonicalID, canonicalID}, 2) {
		if _, err := tx.Exec(ctx, `insert into game_resource_aliases(kind_code,alias_id,resource_id,source)
			values($1,$2,$3,'manual') on conflict(kind_code,alias_id) do update set
			resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`, kindCode, alias, snapshot.EntityID); err != nil {
			return err
		}
	}
	definition := string(catalogJSON(nonNilJSONObject(edit.Definition)))
	_, err = tx.Exec(ctx, `insert into catalog_resource_definitions(resource_id,definition_schema_version,definition,icon_file_id,render_file_id,published_revision_id,updated_by)
		values($1,1,$2::jsonb,$3,$4,$5,$6) on conflict(resource_id) do update set definition_schema_version=excluded.definition_schema_version,definition=excluded.definition,
		icon_file_id=excluded.icon_file_id,render_file_id=excluded.render_file_id,published_revision_id=excluded.published_revision_id,
		updated_by=excluded.updated_by,updated_at=now()`, snapshot.EntityID, definition, iconFileID, renderFileID, revisionID, nullableActorID(actorID))
	return err
}

func publishCatalogTagTx(ctx context.Context, tx pgx.Tx, snapshot catalogEditorSnapshot, revisionID int64) error {
	edit := snapshot.Tag
	if edit == nil || strings.TrimSpace(edit.Registry) == "" || strings.TrimSpace(edit.CanonicalID) == "" {
		return errCatalogEditorInvalid
	}
	if _, err := tx.Exec(ctx, `insert into catalog_tags(entity_id,registry,canonical_id) values($1,$2,$3)
		on conflict(entity_id) do update set registry=excluded.registry,canonical_id=excluded.canonical_id`, snapshot.EntityID,
		strings.TrimSpace(edit.Registry), strings.TrimSpace(edit.CanonicalID)); err != nil {
		return err
	}
	resources, err := resolveActiveResourcePublicIDsTx(ctx, tx, edit.MemberResourcePublicIDs)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `delete from catalog_tag_members where tag_id=$1`, snapshot.EntityID); err != nil {
		return err
	}
	for ordinal, resourceID := range resources {
		if _, err = tx.Exec(ctx, `insert into catalog_tag_members(tag_id,resource_id,ordinal,published_revision_id) values($1,$2,$3,$4)`,
			snapshot.EntityID, resourceID, ordinal, revisionID); err != nil {
			return err
		}
	}
	return nil
}

func publishCatalogRecipeTypeTx(ctx context.Context, tx pgx.Tx, snapshot catalogEditorSnapshot, revisionID, actorID int64) error {
	edit := snapshot.RecipeType
	if edit == nil || strings.TrimSpace(edit.CanonicalID) == "" {
		return errCatalogEditorInvalid
	}
	if _, err := tx.Exec(ctx, `insert into recipe_types(entity_id,canonical_id) values($1,$2)
		on conflict(entity_id) do update set canonical_id=excluded.canonical_id`, snapshot.EntityID, strings.TrimSpace(edit.CanonicalID)); err != nil {
		return err
	}
	definition := string(catalogJSON(nonNilJSONObject(edit.Definition)))
	if _, err := tx.Exec(ctx, `insert into recipe_type_definitions(recipe_type_id,definition,published_revision_id,updated_by)
		values($1,$2::jsonb,$3,$4) on conflict(recipe_type_id) do update set definition=excluded.definition,
		published_revision_id=excluded.published_revision_id,updated_by=excluded.updated_by,updated_at=now()`, snapshot.EntityID,
		definition, revisionID, nullableActorID(actorID)); err != nil {
		return err
	}
	resources, err := resolveActiveResourcePublicIDsTx(ctx, tx, edit.CatalystResourcePublicIDs)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `delete from recipe_type_catalysts where recipe_type_id=$1`, snapshot.EntityID); err != nil {
		return err
	}
	for ordinal, resourceID := range resources {
		if _, err = tx.Exec(ctx, `insert into recipe_type_catalysts(recipe_type_id,resource_id,ordinal,published_revision_id) values($1,$2,$3,$4)`,
			snapshot.EntityID, resourceID, ordinal, revisionID); err != nil {
			return err
		}
	}
	return nil
}

func publishCatalogRecipeTemplateTx(
	ctx context.Context,
	tx pgx.Tx,
	snapshot catalogEditorSnapshot,
	revisionID, actorID int64,
	rasterScope ossRasterBindingScope,
) error {
	edit := snapshot.Template
	if edit == nil || snapshot.ParentEntityID <= 0 {
		return errCatalogEditorInvalid
	}
	if err := validateCatalogTemplate(edit); err != nil {
		return err
	}
	backgroundFileID, err := resolveOptionalTrustedRasterOSSFilePublicID(ctx, tx, edit.BackgroundFileID, rasterScope)
	if err != nil {
		return errCatalogEditorReference
	}
	definition := string(catalogJSON(nonNilJSONObject(edit.Definition)))
	if _, err := tx.Exec(ctx, `insert into recipe_layout_templates(entity_id,recipe_type_id,template_key,import_snapshot_id,background_file_id,
		canvas_width,canvas_height,image_scale,definition,published_revision_id,updated_by)
		values($1,$2,$3,null,$4,$5,$6,$7,$8::jsonb,$9,$10)
		on conflict(entity_id) do update set recipe_type_id=excluded.recipe_type_id,template_key=excluded.template_key,
		import_snapshot_id=case when excluded.background_file_id is null then recipe_layout_templates.import_snapshot_id else null end,
		background_file_id=excluded.background_file_id,canvas_width=excluded.canvas_width,canvas_height=excluded.canvas_height,
		image_scale=excluded.image_scale,definition=excluded.definition,published_revision_id=excluded.published_revision_id,
		updated_by=excluded.updated_by,updated_at=now()`, snapshot.EntityID, snapshot.ParentEntityID, strings.TrimSpace(edit.TemplateKey),
		backgroundFileID, edit.Canvas.Width, edit.Canvas.Height, edit.Canvas.ImageScale, definition, revisionID, nullableActorID(actorID)); err != nil {
		return err
	}
	slotKeys := make([]string, len(edit.Slots))
	for index := range edit.Slots {
		slotKeys[index] = edit.Slots[index].SlotKey
	}
	var referencedRemoved bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from recipe_template_slots slot
		join recipe_bindings binding on binding.template_slot_id=slot.id
		where slot.template_id=$1 and not(slot.slot_key=any($2::text[])))`, snapshot.EntityID, slotKeys).Scan(&referencedRemoved); err != nil {
		return err
	}
	if referencedRemoved {
		return errCatalogEditorConflict
	}
	// Move existing ordinals out of the requested range so slot reordering can
	// be applied with the unique(template_id, ordinal) constraint in place.
	if _, err := tx.Exec(ctx, `update recipe_template_slots set ordinal=ordinal+1000000 where template_id=$1`, snapshot.EntityID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from recipe_template_slots where template_id=$1 and not(slot_key=any($2::text[]))`, snapshot.EntityID, slotKeys); err != nil {
		return err
	}
	for _, slot := range edit.Slots {
		var referencedRoleChange bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from recipe_template_slots slot
			join recipe_bindings binding on binding.template_slot_id=slot.id
			where slot.template_id=$1 and slot.slot_key=$2 and slot.role<>$3)`, snapshot.EntityID, slot.SlotKey, slot.Role).Scan(&referencedRoleChange); err != nil {
			return err
		}
		if referencedRoleChange {
			return errCatalogEditorConflict
		}
		slotID := catalogSnapshotID("canonical-template-slot", "stable", strconv.FormatInt(snapshot.EntityID, 10), slot.SlotKey)
		if _, err := tx.Exec(ctx, `insert into recipe_template_slots(identity_key,template_id,slot_key,role,output_index,ordinal,x,y,width,height,definition)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)
			on conflict(template_id,slot_key) do update set role=excluded.role,output_index=excluded.output_index,
			ordinal=excluded.ordinal,x=excluded.x,y=excluded.y,width=excluded.width,height=excluded.height,definition=excluded.definition`, slotID, snapshot.EntityID, slot.SlotKey, slot.Role,
			slot.OutputIndex, slot.Ordinal, slot.Rect.X, slot.Rect.Y, slot.Rect.Width, slot.Rect.Height,
			string(catalogJSON(nonNilJSONObject(slot.Definition)))); err != nil {
			return err
		}
	}
	return nil
}

func publishCatalogRecipeTx(ctx context.Context, tx pgx.Tx, snapshot catalogEditorSnapshot, revisionID, actorID int64) error {
	edit := snapshot.Recipe
	if edit == nil || snapshot.ParentEntityID <= 0 {
		return errCatalogEditorInvalid
	}
	var templateID int64
	if err := tx.QueryRow(ctx, `select template.entity_id from recipe_layout_templates template
		join catalog_entities entity on entity.id=template.entity_id
		where entity.public_id=$1 and entity.status='active' and template.recipe_type_id=$2`, edit.TemplatePublicID, snapshot.ParentEntityID).Scan(&templateID); err != nil {
		return errCatalogEditorReference
	}
	slotRows, err := tx.Query(ctx, `select id,slot_key,role from recipe_template_slots where template_id=$1`, templateID)
	if err != nil {
		return err
	}
	slotIDs := map[string]int64{}
	slotRoles := map[string]string{}
	for slotRows.Next() {
		var id int64
		var key, role string
		if err = slotRows.Scan(&id, &key, &role); err != nil {
			slotRows.Close()
			return err
		}
		slotIDs[key], slotRoles[key] = id, role
	}
	if err = slotRows.Err(); err != nil {
		slotRows.Close()
		return err
	}
	slotRows.Close()
	if err = validateCatalogRecipeBindings(edit, slotRoles); err != nil {
		return err
	}
	canonicalSourceID := strings.TrimSpace(edit.CanonicalSourceID)
	var canonical any
	if canonicalSourceID != "" {
		canonical = canonicalSourceID
	}
	var sourceVersionID, sourceModID any
	if edit.SourceVersionPublicID != "" {
		var versionID, modID int64
		if err = tx.QueryRow(ctx, `select id,mod_id from mod_content_versions where public_id=$1 and status='active'`,
			edit.SourceVersionPublicID).Scan(&versionID, &modID); err != nil {
			return errCatalogEditorReference
		}
		sourceVersionID, sourceModID = versionID, modID
	}
	// Applicable versions are relations, not part of the recipe's semantic
	// content. Excluding them keeps identical recipes reusable across versions.
	fingerprintEdit := *edit
	fingerprintEdit.ApplicableVersionIDs = nil
	fingerprint := sha256Hex(catalogJSON(fingerprintEdit))
	if _, err = tx.Exec(ctx, `insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,owner_mod_id,identity_source)
		values($1,$2,$3,$4,$5,'manual') on conflict(entity_id) do update set recipe_type_id=excluded.recipe_type_id,
		canonical_source_id=excluded.canonical_source_id,semantic_fingerprint=excluded.semantic_fingerprint,
		owner_mod_id=excluded.owner_mod_id,identity_source='manual'`, snapshot.EntityID,
		snapshot.ParentEntityID, canonical, fingerprint, sourceModID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into recipe_definitions(recipe_id,template_id,source_mod_content_version_id,definition_schema_version,definition,published_revision_id,updated_by)
		values($1,$2,$3,1,$4::jsonb,$5,$6) on conflict(recipe_id) do update set template_id=excluded.template_id,
		source_mod_content_version_id=excluded.source_mod_content_version_id,definition=excluded.definition,
		definition_schema_version=excluded.definition_schema_version,
		published_revision_id=excluded.published_revision_id,updated_by=excluded.updated_by,updated_at=now()`,
		snapshot.EntityID, templateID, sourceVersionID, string(catalogJSON(nonNilJSONObject(edit.Definition))), revisionID, nullableActorID(actorID)); err != nil {
		return err
	}
	if edit.ApplicableVersionIDs != nil {
		versions := *edit.ApplicableVersionIDs
		if len(versions) == 0 {
			return errCatalogEditorInvalid
		}
		if _, err = tx.Exec(ctx, `delete from recipe_version_bindings where recipe_id=$1 and not(version_code=any($2::text[]))`, snapshot.EntityID, versions); err != nil {
			return err
		}
		for _, version := range versions {
			if _, err = tx.Exec(ctx, `insert into recipe_version_bindings(recipe_id,version_code,created_by,source)
				values($1,$2,$3,'editor') on conflict(recipe_id,version_code) do nothing`, snapshot.EntityID, version, nullableActorID(actorID)); err != nil {
				return err
			}
		}
	}
	var versionBindingCount int
	if err = tx.QueryRow(ctx, `select count(*)::int from recipe_version_bindings where recipe_id=$1`, snapshot.EntityID).Scan(&versionBindingCount); err != nil {
		return err
	}
	if versionBindingCount == 0 {
		return errCatalogEditorInvalid
	}
	if _, err = tx.Exec(ctx, `delete from recipe_bindings where recipe_id=$1`, snapshot.EntityID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `delete from unresolved_resource_references
		where source_entity_id=$1 and source_revision_id is null and field_path like 'bindings.%'`, snapshot.EntityID); err != nil {
		return err
	}
	slotKeys := make([]string, 0, len(edit.Bindings))
	for slotKey := range edit.Bindings {
		slotKeys = append(slotKeys, slotKey)
	}
	sort.Strings(slotKeys)
	for ordinal, slotKey := range slotKeys {
		binding := edit.Bindings[slotKey]
		bindingID := catalogSnapshotID("canonical-recipe-binding", strconv.FormatInt(revisionID, 10), strconv.FormatInt(snapshot.EntityID, 10), slotKey)
		var bindingInternalID int64
		if err = tx.QueryRow(ctx, `insert into recipe_bindings(identity_key,recipe_id,template_slot_id,ordinal,definition)
			values($1,$2,$3,$4,$5::jsonb) returning id`, bindingID, snapshot.EntityID, slotIDs[slotKey], ordinal,
			string(catalogJSON(nonNilJSONObject(binding.Definition)))).Scan(&bindingInternalID); err != nil {
			return err
		}
		for index, candidate := range binding.Candidates {
			var resourceID int64
			if candidate.RawResourceID != "" {
				resourceID, err = ensurePlaceholderGameResourceTx(ctx, tx, candidate.KindCode, candidate.RawResourceID)
			} else {
				var resourceIDs []int64
				resourceIDs, err = resolveActiveResourcePublicIDsTx(ctx, tx, []string{candidate.ResourcePublicID})
				if err == nil {
					resourceID = resourceIDs[0]
				}
			}
			if err != nil {
				return err
			}
			candidateID := catalogSnapshotID("canonical-recipe-candidate", strconv.FormatInt(revisionID, 10), bindingID, strconv.Itoa(index))
			if _, err = tx.Exec(ctx, `insert into recipe_binding_candidates(identity_key,binding_id,candidate_index,resource_id,amount,probability,byproduct,definition)
				values($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, candidateID, bindingInternalID, index, resourceID, candidate.Amount,
				candidate.Probability, candidate.Byproduct, string(catalogJSON(nonNilJSONObject(candidate.Definition)))); err != nil {
				return err
			}
			if candidate.RawResourceID != "" {
				if _, err = tx.Exec(ctx, `insert into unresolved_resource_references(
					source_entity_id,field_path,kind_code,raw_resource_id,resolved_resource_id,status
				) values($1,$2,$3,$4,$5,'pending')`,
					snapshot.EntityID, fmt.Sprintf("bindings.%s.candidates.%d", slotKey, index),
					candidate.KindCode, candidate.RawResourceID, resourceID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateCatalogSnapshotReferencesTx(ctx context.Context, tx pgx.Tx, snapshot catalogEditorSnapshot, actorID int64, allowForeignFiles bool) error {
	if snapshot.Operation == "delete" {
		return nil
	}
	switch snapshot.Kind {
	case "resource":
		if snapshot.Resource == nil {
			return errCatalogEditorInvalid
		}
		return validateCatalogImageFileReferences(ctx, tx, actorID, allowForeignFiles, snapshot.Resource.IconFileID, snapshot.Resource.RenderFileID)
	case "tag":
		if snapshot.Tag == nil {
			return errCatalogEditorInvalid
		}
		_, err := resolveActiveResourcePublicIDsTx(ctx, tx, append([]string(nil), snapshot.Tag.MemberResourcePublicIDs...))
		return err
	case "recipe_type":
		if snapshot.RecipeType == nil {
			return errCatalogEditorInvalid
		}
		_, err := resolveActiveResourcePublicIDsTx(ctx, tx, append([]string(nil), snapshot.RecipeType.CatalystResourcePublicIDs...))
		return err
	case "recipe_template":
		if snapshot.Template == nil || snapshot.ParentEntityID <= 0 {
			return errCatalogEditorInvalid
		}
		var active bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from recipe_types recipe_type
			join catalog_entities entity on entity.id=recipe_type.entity_id
			where recipe_type.entity_id=$1 and entity.status='active')`, snapshot.ParentEntityID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return errCatalogEditorReference
		}
		return validateCatalogImageFileReferences(ctx, tx, actorID, allowForeignFiles, snapshot.Template.BackgroundFileID)
	case "recipe":
		if snapshot.Recipe == nil || snapshot.ParentEntityID <= 0 {
			return errCatalogEditorInvalid
		}
		var templateID int64
		if err := tx.QueryRow(ctx, `select template.entity_id from recipe_layout_templates template
			join catalog_entities entity on entity.id=template.entity_id
			where entity.public_id=$1 and entity.status='active' and template.recipe_type_id=$2`,
			snapshot.Recipe.TemplatePublicID, snapshot.ParentEntityID).Scan(&templateID); err != nil {
			return errCatalogEditorReference
		}
		rows, err := tx.Query(ctx, `select slot_key,role from recipe_template_slots where template_id=$1`, templateID)
		if err != nil {
			return err
		}
		roles := map[string]string{}
		for rows.Next() {
			var key, role string
			if err = rows.Scan(&key, &role); err != nil {
				rows.Close()
				return err
			}
			roles[key] = role
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if err = validateCatalogRecipeBindings(snapshot.Recipe, roles); err != nil {
			return err
		}
		if snapshot.Recipe.SourceVersionPublicID != "" {
			var valid bool
			if err = tx.QueryRow(ctx, `select exists(select 1 from mod_content_versions version
				join mods mod on mod.id=version.mod_id
				where version.public_id=$1 and version.status='active' and (mod.review_status='approved' or mod.submitted_by=$2))`,
				snapshot.Recipe.SourceVersionPublicID, actorID).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return errCatalogEditorReference
			}
		}
		for _, binding := range snapshot.Recipe.Bindings {
			publicIDs := make([]string, 0, len(binding.Candidates))
			for index := range binding.Candidates {
				if binding.Candidates[index].ResourcePublicID != "" {
					publicIDs = append(publicIDs, binding.Candidates[index].ResourcePublicID)
				}
			}
			if _, err = resolveActiveResourcePublicIDsTx(ctx, tx, publicIDs); err != nil {
				return err
			}
		}
		return nil
	default:
		return errCatalogEditorInvalid
	}
}

func ensurePlaceholderGameResourceTx(ctx context.Context, tx pgx.Tx, kindCode, rawResourceID string) (int64, error) {
	kindCode = strings.TrimSpace(kindCode)
	rawResourceID = strings.TrimSpace(rawResourceID)
	if kindCode == "" || rawResourceID == "" || len(rawResourceID) > 255 {
		return 0, errCatalogEditorReference
	}
	var existingID int64
	err := tx.QueryRow(ctx, `select entity_id from game_resources
		where kind_code=$1 and canonical_id=$2`, kindCode, rawResourceID).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	family := kindCode
	if separator := strings.LastIndex(kindCode, "."); separator >= 0 && separator+1 < len(kindCode) {
		family = kindCode[separator+1:]
	}
	if _, err = tx.Exec(ctx, `insert into resource_kinds(code,family,user_visible)
		values($1,$2,true) on conflict(code) do nothing`, kindCode, family); err != nil {
		return 0, err
	}
	identityKey := "placeholder:resource:" + sha256Hex([]byte(kindCode+"\x00"+strings.ToLower(rawResourceID)))
	var entityID int64
	if err = tx.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type,status)
		values($1,'resource','placeholder')
		on conflict(identity_key) do update set updated_at=now()
		returning id`, identityKey).Scan(&entityID); err != nil {
		return 0, err
	}
	namespace, resourcePath := "", rawResourceID
	if separator := strings.Index(rawResourceID, ":"); separator > 0 {
		namespace, resourcePath = rawResourceID[:separator], rawResourceID[separator+1:]
	}
	if _, err = tx.Exec(ctx, `insert into game_resources(
		entity_id,kind_code,canonical_id,namespace,resource_path,resolved
	) values($1,$2,$3,$4,$5,false)
	on conflict(kind_code,canonical_id) do nothing`,
		entityID, kindCode, rawResourceID, namespace, resourcePath); err != nil {
		return 0, err
	}
	if err = tx.QueryRow(ctx, `select entity_id from game_resources
		where kind_code=$1 and canonical_id=$2`, kindCode, rawResourceID).Scan(&entityID); err != nil {
		return 0, err
	}
	return entityID, nil
}

func resolveActiveResourcePublicIDsTx(ctx context.Context, tx pgx.Tx, publicIDs []string) ([]int64, error) {
	if len(publicIDs) == 0 {
		return []int64{}, nil
	}
	seen := make(map[string]struct{}, len(publicIDs))
	for index := range publicIDs {
		publicIDs[index] = strings.ToLower(strings.TrimSpace(publicIDs[index]))
		if publicIDs[index] == "" {
			return nil, errCatalogEditorReference
		}
		if _, duplicate := seen[publicIDs[index]]; duplicate {
			return nil, errCatalogEditorReference
		}
		seen[publicIDs[index]] = struct{}{}
	}
	rows, err := tx.Query(ctx, `select entity.id from unnest($1::text[]) with ordinality requested(public_id,ordinal)
		join catalog_entities entity on entity.public_id=requested.public_id and entity.entity_type='resource' and entity.status='active'
		join game_resources resource on resource.entity_id=entity.id order by requested.ordinal`, publicIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]int64, 0, len(publicIDs))
	for rows.Next() {
		var entityID int64
		if err = rows.Scan(&entityID); err != nil {
			return nil, err
		}
		result = append(result, entityID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(result) != len(publicIDs) {
		return nil, errCatalogEditorReference
	}
	return result, nil
}

func catalogEditorHTTPStatus(err error) int {
	switch {
	case errors.Is(err, errCatalogEditorNotFound):
		return http.StatusNotFound
	case errors.Is(err, errCatalogEditorConflict):
		return http.StatusConflict
	case errors.Is(err, errReviewInProgress):
		return http.StatusConflict
	case errors.Is(err, errCatalogEditorReference), errors.Is(err, errCatalogEditorInvalid):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func catalogEditorErrorMessage(err error) string {
	switch catalogEditorHTTPStatus(err) {
	case http.StatusNotFound:
		return "catalog entity not found"
	case http.StatusConflict:
		if errors.Is(err, errReviewInProgress) {
			return errReviewInProgress.Error()
		}
		return "catalog entity changed after this edit was loaded"
	case http.StatusUnprocessableEntity:
		return err.Error()
	default:
		return "catalog edit failed"
	}
}

func catalogEditorAggregate(aggregateType string) bool {
	switch aggregateType {
	case catalogAggregateResource, catalogAggregateTag, catalogAggregateRecipeType, catalogAggregateRecipeTemplate, catalogAggregateRecipe:
		return true
	default:
		return false
	}
}

func publishedCatalogEditorRevisionTx(ctx context.Context, tx pgx.Tx, entityID string) (*int64, error) {
	var revisionID *int64
	err := tx.QueryRow(ctx, `select published_revision_id from catalog_entities where id=$1 for update`, entityID).Scan(&revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errCatalogEditorNotFound
	}
	return revisionID, err
}

func catalogEditorIdentityForRecipe(recipeTypeID, canonicalSourceID string) catalogIdentity {
	key := recipeTypeID + "\x00manual\x00" + strings.TrimSpace(canonicalSourceID)
	if strings.TrimSpace(canonicalSourceID) == "" {
		key += randomHex(16)
	}
	return newCatalogIdentity("recipe", key)
}

func catalogEditorIdentityForTemplate(recipeTypeID, templateKey string) catalogIdentity {
	return newCatalogIdentity("recipe_template", recipeTypeID+"\x00"+strings.TrimSpace(templateKey))
}

func validateCatalogImageFileReferences(ctx context.Context, tx pgx.Tx, actorID int64, allowForeign bool, fileIDs ...*string) error {
	scope := ossRasterBindingScope{UploaderID: actorID, AllowAnyUploader: allowForeign}
	for _, fileID := range fileIDs {
		if fileID == nil {
			continue
		}
		if _, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, *fileID, scope); err != nil {
			return errCatalogEditorReference
		}
	}
	return nil
}

func canonicalCatalogString(value string, maximum int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum {
		return "", errCatalogEditorInvalid
	}
	return value, nil
}
