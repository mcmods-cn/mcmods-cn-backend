package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mcmods-cn-backend/internal/queue"
)

var blueprintFormats = map[string]bool{"nbt": true, "schem": true, "schematic": true, "litematic": true, "json": true}

var (
	errBlueprintNotFound       = errors.New("blueprint not found")
	errBlueprintNotRetryable   = errors.New("blueprint is not retryable")
	errBlueprintSourceTooLarge = errors.New("blueprint source exceeds processing size limit")
	errBlueprintUserJobLimit   = errors.New("blueprint active job limit reached")
	errBlueprintGlobalJobLimit = errors.New("blueprint global active conversion limit reached")
)

const (
	maxBlueprintActiveJobsPerUser       = 4
	maxBlueprintActiveConversionsGlobal = 16
)

const blueprintActiveJobCountSQL = `select count(*) from (
	select 1 from blueprint_jobs where created_by=$1 and status in ('queued','processing')
	order by id limit $2
) active`

const blueprintGlobalActiveConversionCountSQL = `select count(*) from (
	select 1 from blueprint_jobs where operation='convert' and status in ('queued','processing')
	limit $1
) active`

const blueprintMaterialRevisionSQL = `select distinct on(source_namespace) source_namespace,id::text
	from catalog_import_revisions
	where source_namespace=any($1::text[]) and is_active and status in ('ready','partial')
	order by source_namespace,coalesce(activated_at,created_at) desc,id desc`

func logBlueprintReadFailure(publicID, stage string, err error) {
	slog.Error("load blueprint data", "module", "blueprint", "public_id", publicID, "stage", stage, "error", err)
}

func appendBlueprintMaterialNamespace(namespaces []string, seen map[string]struct{}, blockID string) []string {
	namespace := strings.SplitN(blockID, ":", 2)[0]
	if _, exists := seen[namespace]; namespace == "" || exists {
		return namespaces
	}
	seen[namespace] = struct{}{}
	return append(namespaces, namespace)
}

type blueprintContentSnapshot struct {
	PublicID             string                    `json:"publicId"`
	Title                string                    `json:"title"`
	Description          string                    `json:"description"`
	CoverFileID          string                    `json:"coverFileId,omitempty"`
	CoverKey             string                    `json:"coverObjectKey,omitempty"`
	DefaultLocale        string                    `json:"defaultLocale,omitempty"`
	Localizations        []catalogLocalizationEdit `json:"localizations,omitempty"`
	ReplaceLocalizations bool                      `json:"replaceLocalizations,omitempty"`
}

type blueprintRequiredMod struct {
	ProjectCode   string   `json:"projectCode"`
	SiteID        string   `json:"siteId"`
	PrimaryName   string   `json:"primaryName"`
	SecondaryName string   `json:"secondaryName"`
	ModID         string   `json:"modId"`
	IconURL       string   `json:"iconUrl"`
	Namespaces    []string `json:"namespaces"`
}

func isBlueprintExtension(extension string) bool {
	format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extension)), ".")
	return format != "json" && blueprintFormats[format]
}

func blueprintFormatFromName(name string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(name))), ".")
}

func (s *Server) createPendingBlueprint(ctx context.Context, ownerID int64, originalName string, uploadExpiresAt time.Time) (int64, string, error) {
	format := blueprintFormatFromName(originalName)
	if !blueprintFormats[format] || format == "json" {
		return 0, "", fmt.Errorf("unsupported blueprint format %q", format)
	}
	if !uploadExpiresAt.After(time.Now()) {
		return 0, "", errors.New("blueprint upload expiry must be in the future")
	}
	title := strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))
	var id int64
	var publicID string
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format,upload_expires_at) values($1,$2,$3,$4) returning id,public_id`, ownerID, title, format, uploadExpiresAt).Scan(&id, &publicID); err != nil {
		return 0, "", err
	}
	if _, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,editable,review_status,updated_by)
		values('blueprint',$1,'en-US',$2,'human',true,'approved',$3)`, id, title, ownerID); err != nil {
		return 0, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, "", err
	}
	return id, publicID, nil
}

func deletePendingBlueprintUploadRowsTx(ctx context.Context, tx pgx.Tx, blueprintIDs []int64) error {
	if len(blueprintIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `delete from content_localizations where subject_type='blueprint' and subject_id=any($1::bigint[])`, blueprintIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from content_subjects where subject_type='blueprint' and subject_id=any($1::bigint[])`, blueprintIDs); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `delete from blueprints where id=any($1::bigint[]) and status='uploading'`, blueprintIDs)
	if err != nil {
		return err
	}
	if command.RowsAffected() != int64(len(blueprintIDs)) {
		return errors.New("pending blueprint upload changed during cleanup")
	}
	return nil
}

func (s *Server) discardPendingBlueprintUpload(ctx context.Context, ownerID, blueprintID int64, objectKey string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var selectedID int64
	query := `select id from blueprints where owner_id=$1 and status='uploading' and id=$2 for update`
	argument := any(blueprintID)
	if blueprintID <= 0 {
		query = `select id from blueprints where owner_id=$1 and status='uploading' and original_object_key=$2 for update`
		argument = objectKey
	}
	if err = tx.QueryRow(ctx, query, ownerID, argument).Scan(&selectedID); errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	} else if err != nil {
		return err
	}
	if err = deletePendingBlueprintUploadRowsTx(ctx, tx, []int64{selectedID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func enqueueBlueprintJobTx(ctx context.Context, tx pgx.Tx, blueprintID, createdBy int64, operation, targetFormat string) (string, error) {
	if operation == "convert" {
		if err := reserveBlueprintGlobalConversionBudgetTx(ctx, tx); err != nil {
			return "", err
		}
	}
	if err := reserveBlueprintUserJobBudgetTx(ctx, tx, createdBy); err != nil {
		return "", err
	}
	var jobID int64
	var jobPublicID string
	if err := tx.QueryRow(ctx, `insert into blueprint_jobs(blueprint_id,operation,target_format,created_by) values($1,$2,$3,$4) returning id,public_id`, blueprintID, operation, targetFormat, createdBy).Scan(&jobID, &jobPublicID); err != nil {
		return "", err
	}
	if _, err := queue.EnqueueTx(ctx, tx, "blueprint_convert", "blueprint.conversion.requested", "blueprint_job", jobPublicID, "", blueprintJobMessage{JobID: jobID}); err != nil {
		return "", err
	}
	return jobPublicID, nil
}

func reserveBlueprintGlobalConversionBudgetTx(ctx context.Context, tx pgx.Tx) error {
	// All public conversion producers use this transaction helper. The global
	// lock remains held through the subsequent user admission, task insert,
	// Outbox insert, and caller commit, so different accounts cannot race the
	// site-wide count across application instances.
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(
		hashtextextended('blueprint-conversion-global',0))`); err != nil {
		return err
	}
	var active int
	if err := tx.QueryRow(ctx, blueprintGlobalActiveConversionCountSQL, maxBlueprintActiveConversionsGlobal).Scan(&active); err != nil {
		return err
	}
	if active >= maxBlueprintActiveConversionsGlobal {
		return errBlueprintGlobalJobLimit
	}
	return nil
}

func reserveBlueprintUserJobBudgetTx(ctx context.Context, tx pgx.Tx, createdBy int64) error {
	if createdBy <= 0 {
		return errors.New("blueprint job creator is required")
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("blueprint-job-user:%d", createdBy)); err != nil {
		return err
	}
	var active int
	if err := tx.QueryRow(ctx, blueprintActiveJobCountSQL, createdBy, maxBlueprintActiveJobsPerUser).Scan(&active); err != nil {
		return err
	}
	if active >= maxBlueprintActiveJobsPerUser {
		return errBlueprintUserJobLimit
	}
	return nil
}

func (s *Server) enqueueBlueprintJob(ctx context.Context, blueprintID, createdBy int64, operation, targetFormat string) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	jobPublicID, err := enqueueBlueprintJobTx(ctx, tx, blueprintID, createdBy, operation, targetFormat)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return jobPublicID, nil
}

func (s *Server) retryBlueprintJob(ctx context.Context, publicID string, ownerID, actorID int64) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var blueprintID int64
	var status string
	if err = tx.QueryRow(ctx, `select id,status from blueprints where public_id=$1 and owner_id=$2 for update`, publicID, ownerID).Scan(&blueprintID, &status); errors.Is(err, pgx.ErrNoRows) {
		return "", errBlueprintNotFound
	} else if err != nil {
		return "", err
	}
	if status != "failed" {
		return "", errBlueprintNotRetryable
	}
	var active bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from blueprint_jobs
		where blueprint_id=$1 and operation='normalize' and target_format='' and status in ('queued','processing'))`, blueprintID).Scan(&active); err != nil {
		return "", err
	}
	if active {
		return "", errBlueprintNotRetryable
	}
	tag, err := tx.Exec(ctx, `update blueprints set status='queued',last_error='',updated_at=now() where id=$1 and status='failed'`, blueprintID)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", errBlueprintNotRetryable
	}
	jobPublicID, err := enqueueBlueprintJobTx(ctx, tx, blueprintID, actorID, "normalize", "")
	if err != nil {
		if isUniqueViolation(err) {
			return "", errBlueprintNotRetryable
		}
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return jobPublicID, nil
}

func (s *Server) userOwnsPendingBlueprintObject(ctx context.Context, ownerID int64, objectKey string) bool {
	var exists bool
	_ = s.db.QueryRow(ctx, `select exists(select 1 from blueprints where owner_id=$1 and original_object_key=$2 and status='uploading')`, ownerID, objectKey).Scan(&exists)
	return exists
}

func (s *Server) userOwnsBlueprintObject(ctx context.Context, ownerID int64, objectKey string) bool {
	var exists bool
	blueprintRoot := path.Join(ossRoot(s.ossConfigFromSettings(ctx).Prefix), ossProjectDirectory, normalizeOSSProjectKind("blueprint")) + "/"
	_ = s.db.QueryRow(ctx, `select exists(select 1 from blueprints where owner_id=$1 and status<>'deleted'
		and $2 like $3 || public_id || '/%')`, ownerID, objectKey, blueprintRoot).Scan(&exists)
	return exists
}

func (s *Server) blueprintForExistingFile(ctx context.Context, fileID, ownerID int64) (map[string]any, error) {
	if fileID <= 0 {
		return nil, nil
	}
	var publicID, status string
	if s.db.QueryRow(ctx, `select public_id,status from blueprints where owner_id=$1 and original_file_id=$2 and status<>'deleted' order by created_at limit 1`, ownerID, fileID).Scan(&publicID, &status) == nil {
		return map[string]any{"id": publicID, "status": status}, nil
	}
	var objectKey, originalName, contentType, sha string
	var size int64
	if s.db.QueryRow(ctx, `select object_key,source_original_name,content_type,source_size_bytes,sha256 from oss_files where id=$1 and uploader_id=$2 and status='active'`, fileID, ownerID).
		Scan(&objectKey, &originalName, &contentType, &size, &sha) != nil || !isBlueprintExtension(filepath.Ext(originalName)) {
		return nil, nil
	}
	blueprintID, publicID, err := s.createPendingBlueprint(ctx, ownerID, originalName, time.Now().Add(time.Hour))
	if err != nil {
		return nil, err
	}
	_, _ = s.db.Exec(ctx, `update blueprints set original_object_key=$2 where id=$1`, blueprintID, objectKey)
	blueprint, err := s.completeBlueprintUpload(ctx, fileID, ownerID, objectKey, originalName, contentType, size, sha)
	if err != nil {
		_, cleanupErr := s.db.Exec(ctx, `delete from blueprints where id=$1 and status='uploading'`, blueprintID)
		return nil, errors.Join(err, cleanupErr)
	}
	return blueprint, nil
}

func (s *Server) reusableBlueprintByHash(ctx context.Context, sha string, size, requesterID int64) (map[string]any, error) {
	if sha == "" || size <= 0 {
		return nil, nil
	}
	var publicID, status string
	err := s.db.QueryRow(ctx, `select blueprint.public_id,blueprint.status
		from blueprints blueprint
		join oss_files file on file.id=blueprint.original_file_id and file.status='active'
		where file.sha256=$1 and coalesce(nullif(file.source_size_bytes,0),file.size_bytes)=$2
		  and blueprint.status<>'deleted'
		  and (blueprint.owner_id=$3 or (blueprint.status='ready' and blueprint.review_status in ('not_required','approved')))
		order by (blueprint.owner_id=$3) desc,blueprint.created_at asc limit 1`, sha, size, requesterID).Scan(&publicID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find reusable blueprint by hash: %w", err)
	}
	return map[string]any{"id": publicID, "status": status}, nil
}

func (s *Server) completeBlueprintUpload(ctx context.Context, fileID, ownerID int64, objectKey, originalName, contentType string, size int64, sha string) (map[string]any, error) {
	if size <= 0 || size > maxBlueprintSourceBytes {
		return nil, errBlueprintSourceTooLarge
	}
	var blueprintID int64
	var publicID, status, format string
	err := s.db.QueryRow(ctx, `select id,public_id,status,source_format from blueprints where owner_id=$1 and original_object_key=$2 and status in ('uploading','queued','failed') order by created_at desc limit 1`, ownerID, objectKey).
		Scan(&blueprintID, &publicID, &status, &format)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if status == "queued" {
		return map[string]any{"id": publicID, "status": status}, nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	lockKey := fmt.Sprintf("blueprint:%d:%d", ownerID, fileID)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return nil, err
	}
	var existingPublicID, existingStatus string
	existingErr := tx.QueryRow(ctx, `select public_id,status from blueprints
		where owner_id=$1 and original_file_id=$2 and status<>'deleted' and id<>$3
		order by created_at limit 1 for update`, ownerID, fileID, blueprintID).Scan(&existingPublicID, &existingStatus)
	if existingErr == nil {
		if _, err = tx.Exec(ctx, `delete from blueprints where id=$1`, blueprintID); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return map[string]any{"id": existingPublicID, "status": existingStatus}, nil
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return nil, existingErr
	}
	if _, err = tx.Exec(ctx, `update blueprints set original_file_id=$2,status='queued',upload_expires_at=null,last_error='',updated_at=now() where id=$1`, blueprintID, fileID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `insert into blueprint_variants(blueprint_id,format,file_id,object_key,original,recommended,status,original_name,content_type,size_bytes,sha256,created_by)
		values($1,$2,$3,$4,true,true,'ready',$5,$6,$7,$8,$9) on conflict(blueprint_id,object_key) do update set file_id=excluded.file_id,status='ready'`,
		blueprintID, format, fileID, objectKey, originalName, contentType, size, sha, ownerID); err != nil {
		return nil, err
	}
	jobPublicID, err := enqueueBlueprintJobTx(ctx, tx, blueprintID, ownerID, "normalize", "")
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"id": publicID, "status": "queued", "jobId": jobPublicID}, nil
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}

func (s *Server) blueprints(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	admin := claimsAllow(claims, "admin.*")
	request, err := parseBlueprintPageRequest(r.URL.Query(), claims.Subject, admin)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := []any{claims.Subject, admin}
	where := []string{"b.status <> 'deleted'", "(b.review_status in ('not_required','approved') or b.owner_id=$1 or $2)"}
	if request.Query != "" {
		args = append(args, "%"+request.Query+"%")
		queryArg := len(args)
		where = append(where, fmt.Sprintf(`(b.title ilike $%[1]d or b.description_markdown ilike $%[1]d or b.public_id ilike $%[1]d
			or exists (
				select 1 from blueprint_mods blueprint_mod join mods mod on mod.id=blueprint_mod.mod_id
				where blueprint_mod.blueprint_id=b.id and (
					blueprint_mod.source_namespace ilike $%[1]d or mod.slug ilike $%[1]d or mod.project_code ilike $%[1]d or mod.primary_name ilike $%[1]d
					or mod.secondary_name ilike $%[1]d or mod.abbreviation ilike $%[1]d
					or exists (select 1 from mod_identifiers identifier where identifier.mod_id=mod.id and identifier.identifier ilike $%[1]d)
				)
			))`, queryArg))
	}
	orderSQL := catalogOrderSQL(request.Sort, request.Direction, false, 0,
		"b.created_at", "b.updated_at", "b.id", "b.title")
	cursorPredicate, cursorArguments := blueprintCursorPredicateSQL(request.Cursor, len(args)+1)
	args = append(args, cursorArguments...)
	args = append(args, request.Limit+1, request.Offset)
	rows, err := s.db.Query(r.Context(), `select b.id,b.public_id,b.title,b.description_markdown,b.source_format,b.status,b.size_x,b.size_y,b.size_z,
		b.block_count,b.palette_count,b.created_at,b.updated_at,u.id,u.username,u.avatar_url,lower(b.title),
		coalesce(popularity.heat_score,0)::text,coalesce(popularity.download_count,0)::text,
		coalesce(popularity.favorite_count,0)::text,coalesce(popularity.bayesian_rating,0)::text,
		coalesce(popularity.rating_count,0),coalesce(popularity.view_count,0)::text,
		coalesce(popularity.comment_count,0)::text
		from blueprints b join users u on u.id=b.owner_id
		left join public_routes popularity_route on popularity_route.entity_type='blueprint' and popularity_route.internal_id=b.id
		left join content_popularity_stats popularity on popularity.object_route_id=popularity_route.id
		where `+strings.Join(where, " and ")+` `+cursorPredicate+`
		order by `+orderSQL+` limit $`+strconv.Itoa(len(args)-1)+` offset $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取蓝图库失败")
		return
	}
	defer rows.Close()
	listRows := make([]blueprintPageRow, 0, request.Limit+1)
	blueprintIDs := make([]int64, 0)
	for rows.Next() {
		var item blueprintPageRow
		if err = rows.Scan(&item.InternalID, &item.PublicID, &item.Title, &item.Description, &item.Format, &item.Status,
			&item.SizeX, &item.SizeY, &item.SizeZ, &item.BlockCount, &item.PaletteCount, &item.CreatedAt, &item.UpdatedAt,
			&item.UploaderID, &item.Username, &item.Avatar, &item.SortName, &item.Heat, &item.Downloads,
			&item.Favorites, &item.Rating, &item.RatingCount, &item.Views, &item.Comments); err != nil {
			writeError(w, http.StatusInternalServerError, "解析蓝图库失败")
			return
		}
		listRows = append(listRows, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取蓝图库失败")
		return
	}
	rows.Close()
	hasMore := len(listRows) > request.Limit
	nextCursor := ""
	if hasMore {
		listRows = listRows[:request.Limit]
		nextCursor = blueprintSQLPageCursor(request, listRows[len(listRows)-1])
	}
	for _, item := range listRows {
		blueprintIDs = append(blueprintIDs, item.InternalID)
	}
	requiredMods, err := s.blueprintRequiredModsByID(r.Context(), blueprintIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取蓝图所需模组失败")
		return
	}
	items := make([]map[string]any, 0, len(listRows))
	ossCfg := s.ossConfigFromSettings(r.Context())
	for _, item := range listRows {
		item.Avatar, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.Avatar)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate blueprint uploader avatar URL")
			return
		}
		items = append(items, map[string]any{"id": item.PublicID, "title": item.Title, "description": item.Description, "sourceFormat": item.Format, "status": item.Status,
			"size": []int{item.SizeX, item.SizeY, item.SizeZ}, "blockCount": item.BlockCount, "paletteCount": item.PaletteCount, "createdAt": item.CreatedAt, "updatedAt": item.UpdatedAt,
			"coverUrl": "/api/v1/blueprints/" + item.PublicID + "/cover?v=" + strconv.FormatInt(item.UpdatedAt.Unix(), 10), "requiredMods": requiredMods[item.InternalID],
			"uploader": map[string]any{"id": item.UploaderID, "username": item.Username, "avatarUrl": item.Avatar}})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "limit": request.Limit, "offset": request.Offset,
		"hasMore": hasMore, "nextCursor": nextCursor,
	})
}

func (s *Server) blueprintRequiredModsByID(ctx context.Context, blueprintIDs []int64) (map[int64][]blueprintRequiredMod, error) {
	result := make(map[int64][]blueprintRequiredMod, len(blueprintIDs))
	for _, blueprintID := range blueprintIDs {
		result[blueprintID] = []blueprintRequiredMod{}
	}
	if len(blueprintIDs) == 0 {
		return result, nil
	}
	ossCfg := s.ossConfigFromSettings(ctx)
	rows, err := s.db.Query(ctx, `select blueprint_mod.blueprint_id,mod.project_code,mod.slug,mod.primary_name,mod.secondary_name,
		primary_identifier.identifier,mod.icon_url,array_agg(distinct blueprint_mod.source_namespace order by blueprint_mod.source_namespace)
		from blueprint_mods blueprint_mod join mods mod on mod.id=blueprint_mod.mod_id
		join lateral (select identifier.identifier from mod_identifiers identifier where identifier.mod_id=mod.id
			order by identifier.is_primary desc,identifier.display_order,identifier.id limit 1) primary_identifier on true
		where blueprint_mod.blueprint_id=any($1::bigint[])
		group by blueprint_mod.blueprint_id,mod.id,mod.project_code,mod.slug,mod.primary_name,mod.secondary_name,primary_identifier.identifier,mod.icon_url
		order by blueprint_mod.blueprint_id,lower(mod.primary_name),mod.slug`, blueprintIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var blueprintID int64
		var mod blueprintRequiredMod
		if err = rows.Scan(&blueprintID, &mod.ProjectCode, &mod.SiteID, &mod.PrimaryName, &mod.SecondaryName,
			&mod.ModID, &mod.IconURL, &mod.Namespaces); err != nil {
			return nil, err
		}
		mod.IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, mod.IconURL)
		if err != nil {
			return nil, err
		}
		result[blueprintID] = append(result[blueprintID], mod)
	}
	return result, rows.Err()
}

func (s *Server) blueprintDetail(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	var blueprintID, ownerID, originalFileID int64
	var title, description, format, status, reviewStatus, originalKey, normalizedKey, coverKey, lastError, username, avatar string
	var sizeX, sizeY, sizeZ, paletteCount, entityCount, dataVersion int
	var blockCount int64
	var createdAt, updatedAt time.Time
	err := s.db.QueryRow(r.Context(), `select b.id,b.owner_id,coalesce(b.original_file_id,0),b.title,b.description_markdown,b.source_format,b.status,b.review_status,b.original_object_key,
		b.normalized_object_key,b.cover_object_key,b.size_x,b.size_y,b.size_z,b.block_count,b.palette_count,b.entity_count,b.data_version,b.last_error,b.created_at,b.updated_at,
		u.username,u.avatar_url from blueprints b join users u on u.id=b.owner_id where b.public_id=$1 and b.status<>'deleted'`, publicID).
		Scan(&blueprintID, &ownerID, &originalFileID, &title, &description, &format, &status, &reviewStatus, &originalKey, &normalizedKey, &coverKey, &sizeX, &sizeY, &sizeZ, &blockCount, &paletteCount, &entityCount, &dataVersion, &lastError, &createdAt, &updatedAt, &username, &avatar)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "蓝图不存在")
		return
	}
	if err != nil {
		logBlueprintReadFailure(publicID, "detail", err)
		writeError(w, http.StatusInternalServerError, "读取蓝图失败")
		return
	}
	avatar, err = s.resolveStoredOSSObjectAccessURL(r.Context(), avatar)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate blueprint uploader avatar URL")
		return
	}
	claims := currentClaims(r)
	if reviewStatus != "not_required" && reviewStatus != "approved" && claims.Subject != ownerID && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusNotFound, "蓝图不存在")
		return
	}
	variants, err := s.blueprintVariants(r.Context(), blueprintID)
	if err != nil {
		logBlueprintReadFailure(publicID, "variants", err)
		writeError(w, http.StatusInternalServerError, "读取蓝图格式失败")
		return
	}
	materials, err := s.blueprintMaterialRows(r.Context(), blueprintID, normalizeExportContentLocale(r.URL.Query().Get("locale")))
	if err != nil {
		logBlueprintReadFailure(publicID, "materials", err)
		writeError(w, http.StatusInternalServerError, "读取蓝图方块统计失败")
		return
	}
	assetRevisions, err := s.blueprintAssetRevisions(r.Context(), blueprintID)
	if err != nil {
		logBlueprintReadFailure(publicID, "asset_revisions", err)
		writeError(w, http.StatusInternalServerError, "读取蓝图资源版本失败")
		return
	}
	requiredModsByID, err := s.blueprintRequiredModsByID(r.Context(), []int64{blueprintID})
	if err != nil {
		logBlueprintReadFailure(publicID, "required_mods", err)
		writeError(w, http.StatusInternalServerError, "读取蓝图所需模组失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": publicID, "title": title, "description": description, "sourceFormat": format, "status": status, "reviewStatus": reviewStatus, "lastError": lastError,
		"size": []int{sizeX, sizeY, sizeZ}, "blockCount": blockCount, "paletteCount": paletteCount, "entityCount": entityCount, "dataVersion": dataVersion,
		"createdAt": createdAt, "updatedAt": updatedAt, "canEdit": claims.Subject == ownerID || claimsAllow(claims, "admin.*"),
		"uploader": map[string]any{"id": ownerID, "username": username, "avatarUrl": avatar},
		"variants": variants, "materials": materials, "requiredMods": requiredModsByID[blueprintID], "assetRevisions": assetRevisions, "renderAvailable": normalizedKey != "", "originalFileId": originalFileID, "originalObjectKey": originalKey,
		"coverUrl": "/api/v1/blueprints/" + publicID + "/cover?v=" + strconv.FormatInt(updatedAt.Unix(), 10), "coverGenerated": strings.Contains(coverKey, "/cover/generated-"),
	})
}

func (s *Server) blueprintAssetRevisions(ctx context.Context, blueprintID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `with namespaces as (
		select distinct split_part(block_id,':',1) namespace from blueprint_materials where blueprint_id=$1
	), paths as (
		select revision.id revision_id,asset.asset_path from catalog_import_revisions revision join namespaces on namespaces.namespace=revision.source_namespace
		join catalog_import_text_assets asset on asset.revision_id=revision.id where revision.is_active
		union select revision.id,asset.asset_path from catalog_import_revisions revision join namespaces on namespaces.namespace=revision.source_namespace
		join catalog_import_binary_assets asset on asset.revision_id=revision.id where revision.is_active
		union select revision.id,asset.asset_path from catalog_import_revisions revision join namespaces on namespaces.namespace=revision.source_namespace
		join catalog_import_media asset on asset.revision_id=revision.id where revision.is_active
	)
	select paths.revision_id,mods.slug,array_agg(paths.asset_path order by paths.asset_path)
	from paths join catalog_import_revisions revision on revision.id=paths.revision_id
	join mods on mods.id=revision.mod_id
	group by paths.revision_id,mods.slug order by paths.revision_id`, blueprintID)
	if err != nil {
		return nil, fmt.Errorf("query blueprint asset revisions: %w", err)
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var revisionID, siteID string
		var paths []string
		if err = rows.Scan(&revisionID, &siteID, &paths); err != nil {
			return nil, fmt.Errorf("scan blueprint asset revision: %w", err)
		}
		result = append(result, map[string]any{"id": revisionID, "siteId": siteID, "paths": paths})
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate blueprint asset revisions: %w", err)
	}
	return result, nil
}

func (s *Server) blueprintVariants(ctx context.Context, blueprintID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select public_id,format,original,recommended,status,original_name,content_type,size_bytes,sha256,created_at from blueprint_variants where blueprint_id=$1 order by original desc,recommended desc,created_at`, blueprintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var size int64
		var id, format, status, name, contentType, sha string
		var original, recommended bool
		var createdAt time.Time
		if err = rows.Scan(&id, &format, &original, &recommended, &status, &name, &contentType, &size, &sha, &createdAt); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"id": id, "format": format, "original": original, "recommended": recommended, "status": status, "originalName": name, "contentType": contentType, "sizeBytes": size, "sha256": sha, "createdAt": createdAt})
	}
	return result, rows.Err()
}

func (s *Server) blueprintMaterialRows(ctx context.Context, blueprintID int64, locale string) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select block_state,block_id,properties,block_count from blueprint_materials where blueprint_id=$1 order by block_count desc,block_state`, blueprintID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type materialRow struct {
		state, blockID string
		properties     map[string]string
		count          int64
	}
	materialRows := make([]materialRow, 0)
	for rows.Next() {
		var state, blockID string
		var properties []byte
		var count int64
		if err = rows.Scan(&state, &blockID, &properties, &count); err != nil {
			return nil, err
		}
		var propertyMap map[string]string
		if err = json.Unmarshal(properties, &propertyMap); err != nil {
			return nil, fmt.Errorf("decode blueprint material properties %s: %w", blockID, err)
		}
		if propertyMap == nil {
			propertyMap = map[string]string{}
		}
		materialRows = append(materialRows, materialRow{state: state, blockID: blockID, properties: propertyMap, count: count})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	namespaceSet := make(map[string]struct{}, len(materialRows))
	namespaces := make([]string, 0, len(materialRows))
	for _, row := range materialRows {
		namespaces = appendBlueprintMaterialNamespace(namespaces, namespaceSet, row.blockID)
	}
	revisions := make(map[string]string, len(namespaces))
	if len(namespaces) > 0 {
		revisionRows, revisionErr := s.db.Query(ctx, blueprintMaterialRevisionSQL, namespaces)
		if revisionErr != nil {
			return nil, fmt.Errorf("query blueprint material revisions: %w", revisionErr)
		}
		defer revisionRows.Close()
		for revisionRows.Next() {
			var namespace, revisionID string
			if err = revisionRows.Scan(&namespace, &revisionID); err != nil {
				return nil, fmt.Errorf("scan blueprint material revision: %w", err)
			}
			revisions[namespace] = revisionID
		}
		if err = revisionRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate blueprint material revisions: %w", err)
		}
		revisionRows.Close()
	}
	keys := make([]exportResourceKey, 0, len(materialRows))
	for _, row := range materialRows {
		namespace := strings.SplitN(row.blockID, ":", 2)[0]
		if revisionID := revisions[namespace]; revisionID != "" {
			keys = append(keys, exportResourceKey{RevisionID: revisionID, ResourceID: row.blockID, Kind: "block"})
		}
	}
	resolved, err := s.resolveExportResources(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("resolve blueprint material resources: %w", err)
	}
	result := make([]map[string]any, 0, len(materialRows))
	for _, row := range materialRows {
		namespace := strings.SplitN(row.blockID, ":", 2)[0]
		revisionID := revisions[namespace]
		source := resolved[exportResourceKey{RevisionID: revisionID, ResourceID: row.blockID, Kind: "block"}]
		name := source.Names[locale]
		if name == "" {
			name = source.Names["zh_cn"]
		}
		if name == "" {
			name = source.Names["en_us"]
		}
		if name == "" {
			name = row.blockID
		}
		result = append(result, map[string]any{"state": row.state, "blockId": row.blockID, "properties": row.properties, "count": row.count,
			"name": name, "names": source.Names, "iconPath": source.IconPath, "previewPath": source.PreviewPath,
			"sourceRevisionId": source.RevisionID, "sourceModSiteId": source.ModSiteID,
			"sourceVersionPublicId": source.VersionPublicID, "detailUrl": canonicalResourceDetailURL(source),
			"publicId": source.PublicID})
	}
	return result, nil
}

func (s *Server) blueprintCover(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	claims := currentClaims(r)
	var objectKey, contentType, reviewStatus string
	err := s.db.QueryRow(r.Context(), `select file.object_key,coalesce(file.content_type,''),blueprint.review_status
		from blueprints blueprint join oss_files file on file.id=blueprint.cover_file_id
		where blueprint.public_id=$1 and blueprint.status<>'deleted'
		and file.status='active' and file.scan_status in ('clean','trusted_generated')
		and lower(split_part(file.content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')
		and (blueprint.review_status in ('not_required','approved') or blueprint.owner_id=$2 or $3)`, publicID, claims.Subject, claimsAllow(claims, "admin.*")).Scan(&objectKey, &contentType, &reviewStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		logBlueprintReadFailure(publicID, "cover", err)
		writeError(w, http.StatusInternalServerError, "读取蓝图封面信息失败")
		return
	}
	if objectKey == "" {
		err = errors.New("blueprint cover object key is empty")
		logBlueprintReadFailure(publicID, "cover", err)
		writeError(w, http.StatusInternalServerError, "读取蓝图封面信息失败")
		return
	}
	client, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	result, err := client.GetObject(r.Context(), &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取蓝图封面失败")
		return
	}
	defer result.Body.Close()
	if contentType == "" && result.ContentType != nil {
		contentType = *result.ContentType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	setBlueprintPreviewCacheControl(w, reviewStatus, "public, max-age=3600, stale-while-revalidate=86400")
	_, _ = io.Copy(w, result.Body)
}

func (s *Server) blueprintRenderData(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	claims := currentClaims(r)
	var objectKey, reviewStatus string
	err := s.db.QueryRow(r.Context(), `select normalized_object_key,review_status from blueprints where public_id=$1 and status in ('ready','partial')
		and (review_status in ('not_required','approved') or owner_id=$2 or $3)`, publicID, claims.Subject, claimsAllow(claims, "admin.*")).Scan(&objectKey, &reviewStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "蓝图渲染数据尚未就绪")
		return
	}
	if err != nil {
		logBlueprintReadFailure(publicID, "render_data", err)
		writeError(w, http.StatusInternalServerError, "读取蓝图渲染数据失败")
		return
	}
	if objectKey == "" {
		writeError(w, http.StatusNotFound, "蓝图渲染数据尚未就绪")
		return
	}
	client, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	result, err := client.GetObject(r.Context(), &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取蓝图渲染数据失败")
		return
	}
	defer result.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	setBlueprintPreviewCacheControl(w, reviewStatus, "public, max-age=300")
	_, _ = io.Copy(w, result.Body)
}

func setBlueprintPreviewCacheControl(w http.ResponseWriter, reviewStatus, publicValue string) {
	switch strings.ToLower(strings.TrimSpace(reviewStatus)) {
	case "approved", "not_required":
		w.Header().Set("Cache-Control", publicValue)
	default:
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Add("Vary", "Authorization")
		w.Header().Add("Vary", "Cookie")
	}
}

type blueprintUpdateRequest struct {
	Title         string                    `json:"title"`
	Description   string                    `json:"description"`
	Reason        string                    `json:"reason,omitempty"`
	DefaultLocale string                    `json:"defaultLocale"`
	Localizations []catalogLocalizationEdit `json:"localizations"`
}

func (s *Server) updateBlueprint(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	var request blueprintUpdateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Title = strings.TrimSpace(request.Title)
	if request.Title == "" || len([]rune(request.Title)) > 120 {
		writeError(w, http.StatusBadRequest, "蓝图名称不能为空且不能超过 120 个字符")
		return
	}
	replaceLocalizations := request.Localizations != nil
	if replaceLocalizations {
		var localizationErr error
		request.DefaultLocale, request.Localizations, localizationErr = normalizeCatalogLocalizations(request.DefaultLocale, request.Localizations)
		if localizationErr != nil || requireCatalogCreateDefaultLocalization(request.DefaultLocale, request.Localizations) != nil {
			writeError(w, http.StatusBadRequest, "the default language must have a localized blueprint name")
			return
		}
		for _, localization := range request.Localizations {
			if localization.Locale == request.DefaultLocale {
				request.Title = localization.Name
				request.Description = localization.ContentMarkdown
				break
			}
		}
	}
	ownerID, allowed := s.blueprintOwnerAccess(r, publicID)
	if !allowed {
		writeError(w, http.StatusForbidden, "没有权限编辑该蓝图")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存蓝图失败")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, "blueprint:"+publicID); err != nil {
		writeError(w, http.StatusInternalServerError, "Lock blueprint revision failed")
		return
	}
	var blueprintID int64
	var coverFileID, coverKey string
	var baseRevisionID *int64
	if err = tx.QueryRow(r.Context(), `select id,coalesce((select public_id from oss_files where id=blueprints.cover_file_id),''),cover_object_key,published_revision_id from blueprints where public_id=$1 and owner_id=$2 for update`, publicID, ownerID).
		Scan(&blueprintID, &coverFileID, &coverKey, &baseRevisionID); err != nil {
		writeError(w, http.StatusInternalServerError, "读取蓝图版本失败")
		return
	}
	snapshotValue := blueprintContentSnapshot{PublicID: publicID, Title: request.Title, Description: request.Description, CoverFileID: coverFileID, CoverKey: coverKey,
		DefaultLocale: request.DefaultLocale, Localizations: request.Localizations, ReplaceLocalizations: replaceLocalizations}
	snapshot, _ := json.Marshal(snapshotValue)
	var latestRevisionSource string
	latestRevisionErr := tx.QueryRow(r.Context(), `select source from content_revisions where aggregate_type='blueprint' and aggregate_key=$1 order by revision_no desc limit 1`, publicID).Scan(&latestRevisionSource)
	if latestRevisionErr != nil && !errors.Is(latestRevisionErr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "Read blueprint revision failed")
		return
	}
	initialSubmission := baseRevisionID == nil || errors.Is(latestRevisionErr, pgx.ErrNoRows) || latestRevisionSource == "blueprint_upload"
	reviewConfig := loadReviewConfig(r.Context(), s.db)
	reviewRequired := reviewConfig.BlueprintEdit
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "Blueprint information update"
	}
	operation := "edit"
	if initialSubmission {
		reviewRequired = reviewConfig.BlueprintCreate
		if strings.TrimSpace(request.Reason) == "" {
			reason = "New blueprint"
		}
		operation = "create"
		if err = withdrawPendingContentRequestsTx(r.Context(), tx, "blueprint", publicID, currentClaims(r).Subject, r); err != nil {
			writeError(w, http.StatusInternalServerError, "Replace blueprint draft failed")
			return
		}
	}
	reviewRequired = reviewRequired && !claimsAllow(currentClaims(r), "admin.*")
	if antiAbuseModerationRequired(r) {
		reviewRequired = true
	}
	status := "approved"
	if reviewRequired {
		status = "pending"
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: "blueprint", EntityID: blueprintID, AggregateType: "blueprint", AggregateKey: publicID, BaseRevision: baseRevisionID, Snapshot: snapshot,
		Reason: reason, ActorID: currentClaims(r).Subject, Status: status, Source: "blueprint_metadata",
		Metadata: map[string]any{"blueprintId": publicID, "title": request.Title, "operation": operation}, Request: r,
	})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "创建蓝图修订失败")
		return
	}
	if reviewRequired {
		if _, err = tx.Exec(r.Context(), `update blueprints set review_status=case when published_revision_id is null then 'pending' else review_status end,updated_at=now() where id=$1`, blueprintID); err != nil {
			writeError(w, http.StatusInternalServerError, "提交蓝图审核失败")
			return
		}
	} else if err = applyBlueprintContentSnapshotTx(r.Context(), tx, blueprintID, created.RevisionID, currentClaims(r).Subject, snapshotValue); err != nil {
		writeError(w, http.StatusInternalServerError, "保存蓝图失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存蓝图失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": !reviewRequired, "reviewRequired": reviewRequired, "revisionId": created.RevisionPublicID})
}

func applyBlueprintContentSnapshotTx(ctx context.Context, tx pgx.Tx, blueprintID, revisionID, actorID int64, snapshot blueprintContentSnapshot) error {
	var coverFileID *int64
	if snapshot.CoverFileID != "" {
		var existingID int64
		var existingKey string
		err := tx.QueryRow(ctx, `select file.id,file.object_key
			from blueprints blueprint join oss_files file on file.id=blueprint.cover_file_id
			where blueprint.id=$1 and file.public_id=$2 and file.status='active'
			  and file.scan_status in ('clean','trusted_generated')
			  and lower(trim(split_part(file.content_type,';',1)))=any($3::text[])
			for update of file`, blueprintID, snapshot.CoverFileID, safeRasterContentTypes).Scan(&existingID, &existingKey)
		if err == nil {
			coverFileID = &existingID
			snapshot.CoverKey = existingKey
		} else {
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			file, resolveErr := resolveTrustedRasterOSSFilePublicID(ctx, tx, snapshot.CoverFileID, ossRasterBindingScope{UploaderID: actorID})
			if resolveErr != nil {
				return resolveErr
			}
			coverFileID = &file.ID
			snapshot.CoverKey = file.ObjectKey
		}
	}
	command, err := tx.Exec(ctx, `update blueprints set title=$2,description_markdown=$3,cover_file_id=$4,cover_object_key=$5,
		published_revision_id=$6,review_status='approved',updated_at=now() where id=$1`, blueprintID, snapshot.Title,
		snapshot.Description, coverFileID, snapshot.CoverKey, revisionID)
	if err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return errors.New("blueprint was not updated")
	}
	if !snapshot.ReplaceLocalizations {
		return nil
	}
	return replaceOwnedAssetLocalizationsTx(ctx, tx, "blueprint", blueprintID, revisionID, actorID, snapshot.DefaultLocale, snapshot.Localizations)
}

func (s *Server) convertBlueprint(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	var request struct {
		Format string `json:"format"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(request.Format)), ".")
	if format != "nbt" && format != "schem" && format != "litematic" {
		writeError(w, http.StatusBadRequest, "目前支持转换为 nbt 或 schem")
		return
	}
	claims := currentClaims(r)
	var blueprintID int64
	if err := s.db.QueryRow(r.Context(), `select id from blueprints where public_id=$1 and status in ('ready','partial')
		and (review_status in ('not_required','approved') or owner_id=$2 or $3)`, publicID, claims.Subject, claimsAllow(claims, "admin.*")).Scan(&blueprintID); err != nil {
		writeError(w, http.StatusConflict, "蓝图尚未准备完成")
		return
	}
	var existingID string
	if s.db.QueryRow(r.Context(), `select public_id from blueprint_variants where blueprint_id=$1 and format=$2 and status='ready' order by original desc,created_at limit 1`, blueprintID, format).Scan(&existingID) == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "variantId": existingID, "alreadyExists": true})
		return
	}
	jobID, err := s.enqueueBlueprintJob(r.Context(), blueprintID, claims.Subject, "convert", format)
	if errors.Is(err, errBlueprintUserJobLimit) {
		writeAPIError(w, http.StatusTooManyRequests, "BLUEPRINT_JOB_CONCURRENCY_LIMIT", "too many blueprint jobs are already active", 60, nil)
		return
	}
	if errors.Is(err, errBlueprintGlobalJobLimit) {
		writeAPIError(w, http.StatusTooManyRequests, "BLUEPRINT_GLOBAL_CONCURRENCY_LIMIT", "the public blueprint conversion queue is at capacity", 60, nil)
		return
	}
	if isBlueprintActiveJobConflict(err) {
		writeAPIError(w, http.StatusConflict, "BLUEPRINT_CONVERSION_ALREADY_QUEUED", "the requested blueprint conversion is already active", 30, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建转换任务失败")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": jobID, "status": "queued"})
}

func isBlueprintActiveJobConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_blueprint_jobs_active_operation"
}

func (s *Server) retryBlueprint(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	ownerID, allowed := s.blueprintOwnerAccess(r, publicID)
	if !allowed {
		writeError(w, http.StatusForbidden, "没有权限重试该蓝图")
		return
	}
	jobID, err := s.retryBlueprintJob(r.Context(), publicID, ownerID, currentClaims(r).Subject)
	if errors.Is(err, errBlueprintNotFound) {
		writeError(w, http.StatusNotFound, "蓝图不存在")
		return
	}
	if errors.Is(err, errBlueprintNotRetryable) {
		writeError(w, http.StatusConflict, "蓝图不处于可重试状态或已有处理任务")
		return
	}
	if errors.Is(err, errBlueprintUserJobLimit) {
		writeAPIError(w, http.StatusTooManyRequests, "BLUEPRINT_JOB_CONCURRENCY_LIMIT", "too many blueprint jobs are already active", 60, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建重试任务失败")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": jobID, "status": "queued"})
}

func (s *Server) downloadBlueprintVariant(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	variantID := strings.ToLower(strings.TrimSpace(r.PathValue("variantId")))
	if !validCatalogPublicID(variantID) {
		writeError(w, http.StatusBadRequest, "格式文件 ID 不正确")
		return
	}
	var objectKey string
	var ownerID int64
	claims := currentClaims(r)
	err := s.db.QueryRow(r.Context(), `select v.object_key,b.owner_id from blueprint_variants v join blueprints b on b.id=v.blueprint_id
		where b.public_id=$1 and v.public_id=$2 and v.status='ready' and (b.review_status in ('not_required','approved') or b.owner_id=$3 or $4)`,
		publicID, variantID, claims.Subject, claimsAllow(claims, "admin.*")).Scan(&objectKey, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "蓝图格式文件不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取蓝图格式文件失败")
		return
	}
	if s.presignOSSFileWithRequest(w, r, ossPresignRequest{ObjectKey: objectKey}) {
		if rewardErr := s.recordOwnedContentDownload(r.Context(), "blueprint", publicID, ownerID, claims.Subject); rewardErr != nil {
			log.Printf("blueprint download reward: public_id=%s owner=%d downloader=%d: %v",
				publicID, ownerID, claims.Subject, rewardErr)
		}
	}
}

func (s *Server) blueprintOwnerAccess(r *http.Request, publicID string) (int64, bool) {
	var ownerID int64
	if s.db.QueryRow(r.Context(), `select owner_id from blueprints where public_id=$1 and status<>'deleted'`, publicID).Scan(&ownerID) != nil {
		return 0, false
	}
	claims := currentClaims(r)
	return ownerID, claims.Subject == ownerID || claimsAllow(claims, "admin.*")
}

func (s *Server) resolvePublicLink(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if len(publicID) != 9 {
		writeError(w, http.StatusNotFound, "短链接不存在")
		return
	}
	var entityType, target string
	var internalID int64
	err := s.db.QueryRow(r.Context(), `select entity_type,internal_id,canonical_path from public_routes where public_id=$1`, publicID).Scan(&entityType, &internalID, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "短链接不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "解析短链接失败")
		return
	}
	if target == "" {
		target = s.catalogPublicTarget(r.Context(), entityType, internalID, publicID)
	}
	if target == "" {
		writeError(w, http.StatusNotFound, "该内容暂时没有可访问页面")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID, "entityType": entityType, "target": target})
}

func (s *Server) catalogPublicTarget(ctx context.Context, entityType string, internalID int64, publicID string) string {
	switch entityType {
	case "tag":
		return "/mods-tag?publicId=" + url.QueryEscape(publicID)
	case "recipe_type":
		return "/recipe-types?publicId=" + url.QueryEscape(publicID)
	case "recipe":
		return "/recipe-types?recipePublicId=" + url.QueryEscape(publicID)
	case "resource", "structure", "document":
		var revisionID, siteID, registry, kind string
		err := s.db.QueryRow(ctx, `select snapshot.revision_id,mod.slug,snapshot.registry,resource.kind_code
			from game_resources resource
			join resource_import_snapshots snapshot on snapshot.resource_id=resource.entity_id
			join catalog_import_revisions revision on revision.id=snapshot.revision_id and revision.is_active
			join mods mod on mod.id=revision.mod_id where resource.entity_id=$1 order by revision.activated_at desc nulls last limit 1`, internalID).
			Scan(&revisionID, &siteID, &registry, &kind)
		if err != nil {
			return ""
		}
		category := categoryForResourceKind(kind)
		return "/mods/" + url.PathEscape(siteID) + "/data/" + url.PathEscape(revisionID) + "/" + url.PathEscape(category) + "/entry?publicId=" + url.QueryEscape(publicID) + "&registry=" + url.QueryEscape(registry)
	}
	return ""
}

func categoryForResourceKind(kind string) string {
	switch kind {
	case "minecraft.item", "minecraft.block":
		return "items"
	case "minecraft.fluid", "mekanism.gas", "mekanism.infusion", "mekanism.pigment", "mekanism.slurry":
		return "ingredients"
	case "minecraft.entity_type":
		return "entities"
	case "minecraft.mob_effect":
		return "effects"
	case "minecraft.enchantment":
		return "enchantments"
	case "minecraft.key_mapping":
		return "keybinds"
	case "minecraft.advancement":
		return "advancements"
	case "minecraft.loot_table":
		return "loot_tables"
	case "minecraft.structure":
		return "structures"
	default:
		return "documents"
	}
}
