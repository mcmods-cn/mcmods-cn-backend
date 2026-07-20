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
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type catalogLocalizationPayload struct {
	Locale              string    `json:"locale"`
	Name                string    `json:"name"`
	Summary             string    `json:"summary"`
	ContentMarkdown     string    `json:"contentMarkdown"`
	Provenance          string    `json:"provenance"`
	SourceLocale        string    `json:"sourceLocale,omitempty"`
	Editable            bool      `json:"editable"`
	ReviewStatus        string    `json:"reviewStatus"`
	RevisionNo          int64     `json:"revisionNo"`
	PublishedRevisionID *int64    `json:"publishedRevisionId,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type contentTranslationState struct {
	Status                      string `json:"status"`
	TaskID                      *int64 `json:"taskId,omitempty"`
	Automatic                   bool   `json:"automatic"`
	CanRequest                  bool   `json:"canRequest"`
	CountsTowardDailyTokenQuota bool   `json:"countsTowardDailyTokenQuota"`
}

type contentResolutionResponse struct {
	PublicID        string                       `json:"publicId"`
	EntityType      string                       `json:"entityType"`
	RequestedLocale string                       `json:"requestedLocale"`
	ResolvedLocale  string                       `json:"resolvedLocale,omitempty"`
	DefaultLocale   string                       `json:"defaultLocale"`
	Resolution      string                       `json:"resolution"`
	Localization    *catalogLocalizationPayload  `json:"localization,omitempty"`
	Available       []catalogLocalizationPayload `json:"available"`
	EditableLocales []string                     `json:"editableLocales"`
	Translation     contentTranslationState      `json:"translation"`
}

type catalogEntityLocalizationSet struct {
	EntityID      string
	PublicID      string
	EntityType    string
	DefaultLocale string
	Localizations map[string]catalogLocalizationPayload
}

type requestContentTranslationPayload struct {
	TargetLocale string `json:"targetLocale"`
	SourceLocale string `json:"sourceLocale,omitempty"`
}

type updateContentLocalizationPayload struct {
	BaseRevisionID  *int64 `json:"baseRevisionId"`
	Locale          string `json:"locale"`
	Name            string `json:"name"`
	Summary         string `json:"summary"`
	ContentMarkdown string `json:"contentMarkdown"`
	Reason          string `json:"reason"`
}

type enqueuedContentTranslation struct {
	TaskID  int64
	TaskUID string
	Status  string
	Created bool
}

type contentVisibilityBypassKey struct{}

func (s *Server) catalogEntityContent(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "publicId must be a 9-character mcmods.cn public ID")
		return
	}
	entity, err := s.loadCatalogEntityLocalizations(r.Context(), publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "localized content subject does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load localized content")
		return
	}

	primary, secondary := s.requestContentLocales(r)
	if requested := normalizeContentLocale(r.URL.Query().Get("locale")); requested != "" {
		primary = requested
	}
	if requested := normalizeContentLocale(r.URL.Query().Get("secondaryLocale")); requested != "" {
		secondary = requested
	}
	availableLocales := make([]string, 0, len(entity.Localizations))
	available := make([]catalogLocalizationPayload, 0, len(entity.Localizations))
	for locale, localization := range entity.Localizations {
		availableLocales = append(availableLocales, locale)
		available = append(available, localization)
	}
	sort.Slice(available, func(left, right int) bool { return available[left].Locale < available[right].Locale })
	resolution := resolveContentLocale(primary, secondary, entity.DefaultLocale, availableLocales)
	response := contentResolutionResponse{
		PublicID:        entity.PublicID,
		EntityType:      entity.EntityType,
		RequestedLocale: resolution.RequestedLocale,
		ResolvedLocale:  resolution.ResolvedLocale,
		DefaultLocale:   entity.DefaultLocale,
		Resolution:      resolution.Reason,
		Available:       available,
		EditableLocales: supportedContentLocaleList(),
		Translation: contentTranslationState{
			Status:                      "not_required",
			Automatic:                   resolution.ShouldAutoTranslate,
			CanRequest:                  resolution.CanRequestTranslation,
			CountsTowardDailyTokenQuota: resolution.CanRequestTranslation,
		},
	}
	if localized, ok := entity.Localizations[resolution.ResolvedLocale]; ok {
		copy := localized
		response.Localization = &copy
	}
	if resolution.RequestedExists {
		response.Translation.Status = "ready"
		w.Header().Set("Content-Language", response.ResolvedLocale)
		writeJSON(w, http.StatusOK, response)
		return
	}

	if resolution.ShouldAutoTranslate && response.Localization != nil {
		task, queueErr := s.enqueueCatalogContentTranslation(
			r.Context(), entity, *response.Localization, resolution.RequestedLocale, 0, int64(maxPermissionValue), false,
		)
		if queueErr == nil {
			response.Translation.Status = task.Status
			response.Translation.TaskID = &task.TaskID
			if task.Created {
				s.publishContentTranslationTask(r.Context(), task)
			}
		} else {
			response.Translation.Status = "unavailable"
		}
	} else if resolution.CanRequestTranslation {
		response.Translation.Status = "request_required"
	} else {
		response.Translation.Status = "no_source"
	}
	w.Header().Set("Content-Language", response.ResolvedLocale)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) updateCatalogEntityContent(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "publicId must be a 9-character mcmods.cn public ID")
		return
	}
	var request updateContentLocalizationPayload
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "request body is invalid")
		return
	}
	_, localizations, err := normalizeCatalogLocalizations(request.Locale, []catalogLocalizationEdit{{
		Locale: request.Locale, Name: request.Name, Summary: request.Summary, ContentMarkdown: request.ContentMarkdown,
	}})
	if err != nil || len(localizations) != 1 || strings.TrimSpace(localizations[0].Name) == "" {
		writeError(w, http.StatusUnprocessableEntity, "an editable locale and non-empty localized name are required")
		return
	}
	localization := localizations[0]

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start localized content edit")
		return
	}
	defer tx.Rollback(r.Context())
	entity, revisionEntityID, err := loadEditableContentSubjectTx(r.Context(), tx, publicID)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errCatalogEditorNotFound) {
		writeError(w, http.StatusNotFound, "localized content subject does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load localized content subject")
		return
	}

	var currentRevisionID *int64
	var existingProvenance string
	err = tx.QueryRow(r.Context(), `select published_revision_id,provenance from content_localizations
		where subject_public_id=$1 and subject_type=$2 and locale=$3 for update`,
		publicID, entity.EntityType, localization.Locale).Scan(&currentRevisionID, &existingProvenance)
	if errors.Is(err, pgx.ErrNoRows) {
		currentRevisionID, existingProvenance, err = nil, "", nil
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load localized content revision")
		return
	}
	if !sameRevision(request.BaseRevisionID, currentRevisionID) {
		writeError(w, http.StatusConflict, "localized content changed after this edit was loaded")
		return
	}

	provenance := catalogHumanEditProvenance(existingProvenance)
	snapshot := catalogLocalizationSnapshot{
		SubjectPublicID: publicID, SubjectType: entity.EntityType, EntityID: entity.EntityID,
		Locale: localization.Locale, Name: localization.Name, Summary: localization.Summary,
		ContentMarkdown: localization.ContentMarkdown, Provenance: provenance, Editable: true,
	}
	rawSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode localized content revision")
		return
	}
	claims := currentClaims(r)
	reviewRequired := contentLocalizationReviewRequired(loadReviewConfig(r.Context(), s.db), entity.EntityType)
	if catalogMutationBypassesReview(claims.Permissions) {
		reviewRequired = false
	}
	reviewStatus := "approved"
	if reviewRequired {
		reviewStatus = "pending"
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "Localized content edit"
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityID: revisionEntityID, AggregateType: catalogAggregateLocalization,
		AggregateKey: contentLocalizationAggregateKey(publicID, entity.EntityType, localization.Locale),
		BaseRevision: currentRevisionID, Snapshot: rawSnapshot, Reason: reason, ActorID: claims.Subject,
		Source: "user", Status: reviewStatus,
		Metadata: map[string]any{"publicId": publicID, "entityType": entity.EntityType, "locale": localization.Locale, "operation": "edit"},
		Request:  r,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create localized content revision")
		return
	}
	if reviewStatus == "approved" {
		if err = publishCatalogLocalizationSnapshotTx(r.Context(), tx, created.RevisionID, rawSnapshot, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish localized content")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record localized content approval")
			return
		}
		if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, created.ChangeRequestID, claims.Subject, "automatic publication", r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record localized content publication")
			return
		}
	}
	activityID, err := insertCatalogActivityTx(r.Context(), tx, claims.Subject, catalogEditorSnapshot{
		Operation: "edit", Kind: entity.EntityType, EntityID: entity.EntityID, PublicID: publicID,
	}, created, reviewStatus)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record localized content activity")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit localized content edit")
		return
	}
	skipRequestActivity(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"publicId": publicID, "entityType": entity.EntityType, "locale": localization.Locale,
		"provenance": provenance, "revisionId": created.RevisionID, "changeRequestId": created.ChangeRequestID,
		"reviewStatus": reviewStatus, "activityEventId": activityID,
	})
}

func loadEditableContentSubjectTx(ctx context.Context, tx pgx.Tx, publicID string) (catalogEntityLocalizationSet, string, error) {
	var entity catalogEntityLocalizationSet
	if err := tx.QueryRow(ctx, `select route.entity_key,route.public_id,route.entity_type,subject.default_locale
		from public_routes route join content_subjects subject
		 on subject.public_id=route.public_id and subject.subject_type=route.entity_type
		where route.public_id=$1 for update of subject`, publicID).Scan(
		&entity.EntityID, &entity.PublicID, &entity.EntityType, &entity.DefaultLocale,
	); err != nil {
		return entity, "", err
	}
	revisionEntityID := ""
	switch entity.EntityType {
	case "mod":
		var exists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from mods where id::text=$1 and review_status='approved')`, entity.EntityID).Scan(&exists); err != nil || !exists {
			if err == nil {
				err = pgx.ErrNoRows
			}
			return entity, "", err
		}
	case "blueprint":
		var exists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from blueprints where id::text=$1 and status<>'deleted'
			and ($2 or review_status in ('not_required','approved')))`, entity.EntityID, contentVisibilityBypassed(ctx)).Scan(&exists); err != nil || !exists {
			if err == nil {
				err = pgx.ErrNoRows
			}
			return entity, "", err
		}
	case "skin":
		var exists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from skin_assets where id::text=$1 and status='active')`, entity.EntityID).Scan(&exists); err != nil || !exists {
			if err == nil {
				err = pgx.ErrNoRows
			}
			return entity, "", err
		}
	case "resource", "tag", "recipe_type", "recipe_template", "recipe":
		var exists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from catalog_entities where id=$1 and public_id=$2
			and entity_type=$3 and status='active' and archived_at is null)`, entity.EntityID, entity.PublicID, entity.EntityType).Scan(&exists); err != nil || !exists {
			if err == nil {
				err = pgx.ErrNoRows
			}
			return entity, "", err
		}
		revisionEntityID = entity.EntityID
	default:
		return entity, "", errCatalogEditorNotFound
	}
	return entity, revisionEntityID, nil
}

func contentLocalizationReviewRequired(config reviewConfig, entityType string) bool {
	switch entityType {
	case "mod":
		return config.ModEdit
	case "blueprint":
		return config.BlueprintEdit
	default:
		return config.CatalogEdit
	}
}

func (s *Server) requestCatalogContentTranslation(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "publicId must be a 9-character mcmods.cn public ID")
		return
	}
	var request requestContentTranslationPayload
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "request body is invalid")
		return
	}
	request.TargetLocale = normalizeContentLocale(request.TargetLocale)
	request.SourceLocale = normalizeContentLocale(request.SourceLocale)
	if request.TargetLocale == "" || !validContentLocaleTag(request.TargetLocale) {
		writeError(w, http.StatusBadRequest, "targetLocale is required")
		return
	}
	if isEditableContentLocale(request.TargetLocale) {
		writeError(w, http.StatusConflict, "supported content locales are translated automatically and can also be edited by users")
		return
	}
	entity, err := s.loadCatalogEntityLocalizations(r.Context(), publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "localized content subject does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load localized content")
		return
	}
	if existing, ok := entity.Localizations[request.TargetLocale]; ok {
		writeJSON(w, http.StatusOK, map[string]any{"cached": true, "localization": existing})
		return
	}
	primary, secondary := s.requestContentLocales(r)
	sourceLocale := request.SourceLocale
	if sourceLocale == "" {
		availableLocales := make([]string, 0, len(entity.Localizations))
		for locale := range entity.Localizations {
			availableLocales = append(availableLocales, locale)
		}
		sourceLocale = resolveContentLocale(primary, secondary, entity.DefaultLocale, availableLocales).ResolvedLocale
	}
	source, ok := entity.Localizations[sourceLocale]
	if !ok {
		writeError(w, http.StatusBadRequest, "sourceLocale is not available")
		return
	}
	claims := currentClaims(r)
	limit := int64(numericPermissionValue(claims.Permissions, "user.ai.daily_token_limit"))
	if limit <= 0 {
		writeError(w, http.StatusForbidden, "no daily AI token allowance is available")
		return
	}
	task, err := s.enqueueCatalogContentTranslation(r.Context(), entity, source, request.TargetLocale, claims.Subject, limit, true)
	if errors.Is(err, errAIQuotaExceeded) {
		writeError(w, http.StatusTooManyRequests, "daily AI token allowance is insufficient")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if task.Created && !s.publishContentTranslationTask(r.Context(), task) {
		writeError(w, http.StatusServiceUnavailable, "AI task queue is unavailable")
		return
	}
	annotateActivity(r, 0, catalogActivityObjectType(entity.EntityType), publicID, 0, map[string]any{
		"operation": "translate", "targetLocale": request.TargetLocale, "aiTaskId": task.TaskID,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"cached": false, "taskId": task.TaskID, "status": task.Status, "countsTowardDailyTokenQuota": true,
	})
}

func (s *Server) catalogContentTranslationResult(w http.ResponseWriter, r *http.Request) {
	taskID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("taskId")), 10, 64)
	if err != nil || taskID <= 0 {
		writeError(w, http.StatusBadRequest, "taskId is invalid")
		return
	}
	claims := currentClaims(r)
	var status, errorMessage string
	var resultRaw, payloadRaw []byte
	var createdBy *int64
	var inputTokens, outputTokens int64
	err = s.db.QueryRow(r.Context(), `
		select status,result,payload,error,created_by,input_tokens,output_tokens
		from ai_tasks where id=$1 and task_type=$2`, taskID, aiTaskContentTranslation).Scan(
		&status, &resultRaw, &payloadRaw, &errorMessage, &createdBy, &inputTokens, &outputTokens,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "translation task does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load translation task")
		return
	}
	if createdBy != nil && *createdBy != claims.Subject && !hasPermission(claims.Permissions, "content.review") {
		writeError(w, http.StatusForbidden, "translation task belongs to another user")
		return
	}
	response := map[string]any{
		"taskId": taskID, "status": status, "error": errorMessage,
		"inputTokens": inputTokens, "outputTokens": outputTokens,
	}
	if status == "completed" {
		var payload map[string]any
		var result map[string]any
		_ = json.Unmarshal(payloadRaw, &payload)
		_ = json.Unmarshal(resultRaw, &result)
		response["targetLocale"] = payload["targetLocale"]
		response["translation"] = translationItemsToMap(result)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) loadCatalogEntityLocalizations(ctx context.Context, publicID string) (catalogEntityLocalizationSet, error) {
	result := catalogEntityLocalizationSet{Localizations: map[string]catalogLocalizationPayload{}}
	err := s.db.QueryRow(ctx, `
		select route.entity_key,route.public_id,route.entity_type,subject.default_locale
		from public_routes route
		join content_subjects subject on subject.public_id=route.public_id and subject.subject_type=route.entity_type
		where route.public_id=$1
		  and (route.entity_type<>'mod' or exists(
		    select 1 from mods where id::text=route.entity_key and review_status='approved'
		  ))
		  and (route.entity_type<>'blueprint' or $2 or exists(
		    select 1 from blueprints where id::text=route.entity_key and status<>'deleted'
		      and review_status in ('not_required','approved')
		  ))
		  and (route.entity_type<>'skin' or $2 or exists(
		    select 1 from skin_assets where id::text=route.entity_key and status='active'
		      and review_status='approved' and visibility<>'private'
		  ))
		  and (not exists(select 1 from catalog_entities where public_id=route.public_id) or exists(
		    select 1 from catalog_entities where public_id=route.public_id and status='active' and archived_at is null
		  ))
		  and (route.entity_type<>'recipe_template' or exists(
		    select 1 from recipe_layout_templates template join catalog_entities parent on parent.id=template.recipe_type_id
		    where template.entity_id=route.entity_key and parent.status='active' and parent.archived_at is null
		  ))
		  and (route.entity_type<>'recipe' or exists(
		    select 1 from recipes recipe join catalog_entities parent on parent.id=recipe.recipe_type_id
		    where recipe.entity_id=route.entity_key and parent.status='active' and parent.archived_at is null
		  ))`, publicID, contentVisibilityBypassed(ctx)).Scan(
		&result.EntityID, &result.PublicID, &result.EntityType, &result.DefaultLocale,
	)
	if err != nil {
		return result, err
	}
	result.DefaultLocale = normalizeContentLocale(result.DefaultLocale)
	rows, err := s.db.Query(ctx, `
		select locale,name,summary,content_markdown,provenance,source_locale,editable,review_status,
		       revision_no,published_revision_id,updated_at
		from content_localizations
		where subject_public_id=$1 and subject_type=$2 and review_status='approved'
		order by locale`, result.PublicID, result.EntityType)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item catalogLocalizationPayload
		if err = rows.Scan(
			&item.Locale, &item.Name, &item.Summary, &item.ContentMarkdown, &item.Provenance, &item.SourceLocale,
			&item.Editable, &item.ReviewStatus, &item.RevisionNo, &item.PublishedRevisionID, &item.UpdatedAt,
		); err != nil {
			return result, err
		}
		item.Locale = normalizeContentLocale(item.Locale)
		item.SourceLocale = normalizeContentLocale(item.SourceLocale)
		result.Localizations[item.Locale] = item
	}
	return result, rows.Err()
}

func contentVisibilityBypassed(ctx context.Context) bool {
	value, _ := ctx.Value(contentVisibilityBypassKey{}).(bool)
	return value
}

func (s *Server) requestContentLocales(r *http.Request) (string, string) {
	primary := firstAcceptedContentLocale(r.Header.Get("Accept-Language"))
	secondary := "en"
	claims := currentClaims(r)
	if claims.Subject > 0 {
		_ = s.db.QueryRow(r.Context(), `
			select preferred_content_language,secondary_content_language from users where id=$1`, claims.Subject).
			Scan(&primary, &secondary)
	}
	primary = normalizeContentLocale(primary)
	secondary = normalizeContentLocale(secondary)
	if primary == "" {
		primary = "zh-CN"
	}
	if secondary == "" {
		secondary = "en"
	}
	return primary, secondary
}

var errAIQuotaExceeded = errors.New("AI token quota exceeded")

func (s *Server) enqueueCatalogContentTranslation(
	ctx context.Context,
	entity catalogEntityLocalizationSet,
	source catalogLocalizationPayload,
	targetLocale string,
	actorID int64,
	tokenLimit int64,
	quotaBacked bool,
) (enqueuedContentTranslation, error) {
	targetLocale = normalizeContentLocale(targetLocale)
	if targetLocale == "" || source.Locale == "" {
		return enqueuedContentTranslation{}, errors.New("translation locales are invalid")
	}
	cfg := s.aiConfigFromSettings(ctx)
	binding, ok := findAITaskModel(cfg.TaskModels, aiTaskContentTranslation)
	if !ok {
		return enqueuedContentTranslation{}, errors.New("content translation task has no configured AI model")
	}
	provider, model, ok := resolveAIModel(cfg, binding.ModelKey)
	if !ok {
		return enqueuedContentTranslation{}, errors.New("content translation AI model or provider is unavailable")
	}
	items := []map[string]string{
		{"key": "name", "text": source.Name},
		{"key": "summary", "text": source.Summary},
		{"key": "contentMarkdown", "text": source.ContentMarkdown},
	}
	payload := map[string]any{
		"entityId": entity.EntityID, "publicId": entity.PublicID, "entityType": entity.EntityType,
		"sourceLocale": source.Locale, "sourceRevisionNo": source.RevisionNo, "targetLocale": targetLocale, "items": items,
		"quotaBacked": quotaBacked,
	}
	rawPayload, _ := json.Marshal(payload)
	reserved := int64(utf8.RuneCountInString(source.Name+source.Summary+source.ContentMarkdown)*2 + 256)
	if reserved < 256 {
		reserved = 256
	}
	if !quotaBacked {
		reserved = 0
	}
	concurrencyKey := contentTranslationConcurrencyKey(entity.EntityID, source, targetLocale, actorID, quotaBacked)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return enqueuedContentTranslation{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, concurrencyKey); err != nil {
		return enqueuedContentTranslation{}, err
	}
	var existing enqueuedContentTranslation
	err = tx.QueryRow(ctx, `
		select task.id,task.task_uid,task.status from ai_tasks task
		where task.task_type=$1 and task.concurrency_key=$2 and (
		 task.status in ('queued','running','retrying') or (
		  task.status='completed' and exists(
		   select 1 from change_requests request
		   where request.aggregate_type=$3 and request.status='pending'
		     and request.metadata->>'aiTaskId'=task.id::text
		  )
		 ))
		order by task.created_at desc limit 1`, aiTaskContentTranslation, concurrencyKey, catalogAggregateLocalization).Scan(
		&existing.TaskID, &existing.TaskUID, &existing.Status,
	)
	if err == nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return enqueuedContentTranslation{}, commitErr
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return enqueuedContentTranslation{}, err
	}
	if quotaBacked {
		if actorID <= 0 || tokenLimit <= 0 {
			return enqueuedContentTranslation{}, errAIQuotaExceeded
		}
		if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, actorID); err != nil {
			return enqueuedContentTranslation{}, err
		}
		var used, pending int64
		if err = tx.QueryRow(ctx, `
			select
			 coalesce(sum(case when status='completed' then input_tokens+output_tokens else 0 end),0),
			 coalesce(sum(case when status in ('queued','running','retrying') then quota_reserved_tokens else 0 end),0)
			from ai_tasks where created_by=$1 and created_at>=date_trunc('day',now())`, actorID).Scan(&used, &pending); err != nil {
			return enqueuedContentTranslation{}, err
		}
		if tokenLimit != int64(maxPermissionValue) && used+pending+reserved > tokenLimit {
			return enqueuedContentTranslation{}, errAIQuotaExceeded
		}
	}
	taskUID := "ai_" + randomHex(16)
	createdBy := any(nil)
	if actorID > 0 {
		createdBy = actorID
	}
	var taskID int64
	err = tx.QueryRow(ctx, `
		insert into ai_tasks(
		 task_uid,task_type,provider,model,status,priority,concurrency_key,payload,created_by,queued_at,quota_reserved_tokens
		) values($1,$2,$3,$4,'queued',0,$5,$6::jsonb,$7,now(),$8) returning id`,
		taskUID, aiTaskContentTranslation, provider.Code, model.Model, concurrencyKey, string(rawPayload), createdBy, reserved,
	).Scan(&taskID)
	if err != nil {
		return enqueuedContentTranslation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return enqueuedContentTranslation{}, err
	}
	s.writeAITaskLog(ctx, taskID, "info", "task_queued", "Catalog content translation queued", payload)
	return enqueuedContentTranslation{TaskID: taskID, TaskUID: taskUID, Status: "queued", Created: true}, nil
}

func contentTranslationConcurrencyKey(entityID string, source catalogLocalizationPayload, targetLocale string, actorID int64, quotaBacked bool) string {
	parts := []string{
		"catalog-content",
		strings.TrimSpace(entityID),
		normalizeContentLocale(source.Locale),
		strconv.FormatInt(source.RevisionNo, 10),
		normalizeContentLocale(targetLocale),
	}
	if quotaBacked {
		parts = append(parts, "actor", strconv.FormatInt(actorID, 10))
	}
	return strings.Join(parts, ":")
}

func (s *Server) publishContentTranslationTask(ctx context.Context, task enqueuedContentTranslation) bool {
	if !task.Created {
		return true
	}
	if s.queue == nil || s.queue.PublishTask(ctx, "ai", aiTaskMessage{
		TaskID: task.TaskID, TaskUID: task.TaskUID, TaskType: aiTaskContentTranslation,
	}) != nil {
		_, _ = s.db.Exec(ctx, `
			update ai_tasks set status='failed',error='NATS unavailable',finished_at=now(),updated_at=now() where id=$1`, task.TaskID)
		return false
	}
	return true
}

func translationItemsToMap(result map[string]any) map[string]string {
	translated := map[string]string{}
	items, _ := result["items"].([]any)
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		key, _ := item["key"].(string)
		text, _ := item["text"].(string)
		switch key {
		case "name", "summary", "contentMarkdown":
			translated[key] = text
		}
	}
	return translated
}

func validCatalogPublicID(value string) bool {
	if len(value) != 9 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func contentLocalizationAggregateKey(publicID, subjectType, locale string) string {
	return fmt.Sprintf("%s:%s:%s", subjectType, publicID, normalizeContentLocale(locale))
}
