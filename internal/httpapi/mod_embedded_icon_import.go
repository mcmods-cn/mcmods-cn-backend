package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"mcmods-cn-backend/internal/activity"
)

const (
	embeddedIconImportMaxBytes = int64(128 << 20)
	embeddedIconImportBatch    = 250
	iconRendererImportSource   = "iconrenderer"
	letMeSeeSeeImportSource    = "letmeseesee"
	irrImportSource            = "irr"
)

var embeddedIconResourceIDPattern = regexp.MustCompile(`^[a-z0-9_.-]+:[a-z0-9_./-]+$`)
var minecraftFormattingCodePattern = regexp.MustCompile(`(?i)[§搂][0-9a-fk-or]`)

type createEmbeddedIconImportJobRequest struct {
	OSSFileID                   string `json:"ossFileId"`
	TargetVersionPublicID       string `json:"targetVersionPublicId"`
	OverwriteExistingImportData bool   `json:"overwriteExistingImportData"`
	Source                      string `json:"source"`
}

type embeddedIconCatalogEntry struct {
	Name            string                   `json:"name"`
	EnglishName     string                   `json:"englishName"`
	IRREnglishName  string                   `json:"Englishname"`
	RegisterName    string                   `json:"registerName"`
	Mod             string                   `json:"mod"`
	Metadata        int                      `json:"metadata"`
	CreativeTabName string                   `json:"CreativeTabName"`
	Type            string                   `json:"type"`
	OreDictList     string                   `json:"OredictList"`
	MaxStackSize    int                      `json:"maxStackSize"`
	MaxDurability   int                      `json:"maxDurability"`
	SmallIcon       string                   `json:"smallIcon"`
	LargeIcon       string                   `json:"largeIcon"`
	IRRIcon         string                   `json:"Icon"`
	IRRVariants     []embeddedIconIRRVariant `json:"-"`
}

type embeddedIconIRRVariant struct {
	Name        string `json:"name,omitempty"`
	EnglishName string `json:"englishName,omitempty"`
	Metadata    int    `json:"metadata"`
}

type embeddedIconImportBuild struct {
	resource catalogResourceImportRow
	media    []modExportPNGMedia
	data     map[string][]byte
}

func normalizeEmbeddedIconImportSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "iconrenderer", "icon-renderer":
		return iconRendererImportSource
	case "letmeseesee", "letmeseesee(yourcode)", "letmeseesee-yourcode", "yourcode":
		return letMeSeeSeeImportSource
	case "irr", "item-render-resource", "itemrenderresource":
		return irrImportSource
	default:
		return ""
	}
}

func embeddedIconImporterVersion(source string) string {
	if source = normalizeEmbeddedIconImportSource(source); source != "" {
		return source + "-json/v1"
	}
	return ""
}

func embeddedIconSourceFromImporterVersion(version string) string {
	switch strings.ToLower(strings.TrimSpace(version)) {
	case iconRendererImportSource + "-json/v1":
		return iconRendererImportSource
	case letMeSeeSeeImportSource + "-json/v1":
		return letMeSeeSeeImportSource
	case irrImportSource + "-json/v1":
		return irrImportSource
	default:
		return ""
	}
}

func (s *Server) createEmbeddedIconImportUpload(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	s.createOSSDirectUploadWithScope(w, r, ossModCatalogScopePrefix+identity.UniqueID)
}

func (s *Server) completeEmbeddedIconImportUpload(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	s.completeOSSDirectUploadWithScope(w, r, ossModCatalogScopePrefix+identity.UniqueID)
}

func (s *Server) createEmbeddedIconImportJob(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	var request createEmbeddedIconImportJobRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid catalog import request")
		return
	}
	request.Source = normalizeEmbeddedIconImportSource(request.Source)
	request.TargetVersionPublicID = strings.ToLower(strings.TrimSpace(request.TargetVersionPublicID))
	request.OSSFileID = strings.ToLower(strings.TrimSpace(request.OSSFileID))
	importerVersion := embeddedIconImporterVersion(request.Source)
	if !validCatalogPublicID(request.OSSFileID) || request.TargetVersionPublicID == "" || importerVersion == "" {
		writeError(w, http.StatusBadRequest, "invalid catalog import request")
		return
	}
	var targetVersionID int64
	if err := s.db.QueryRow(r.Context(), `select id from mod_content_versions where mod_id=$1 and public_id=$2 and status='active'`,
		identity.ID, request.TargetVersionPublicID).Scan(&targetVersionID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "select an active mod data version")
		return
	}

	claims := currentClaims(r)
	var originalName, objectKey, digest, source, category string
	var archiveFileID, size int64
	err := s.db.QueryRow(r.Context(), `select id,original_name,object_key,sha256,size_bytes,source,category
		from oss_files where public_id=$1 and uploader_id=$2 and status='active'`, request.OSSFileID, claims.Subject).
		Scan(&archiveFileID, &originalName, &objectKey, &digest, &size, &source, &category)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "catalog import file is unavailable")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read catalog import file")
		return
	}
	expectedCategory := ossModImportCategory(identity.UniqueID, request.Source, "catalog")
	if source != request.Source || category != expectedCategory ||
		strings.ToLower(filepath.Ext(originalName)) != ".json" || size <= 0 || size > embeddedIconImportMaxBytes {
		writeError(w, http.StatusBadRequest, "file does not match the selected catalog importer")
		return
	}

	packageID, jobID := newExportID(), newExportID()
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create catalog import job")
		return
	}
	defer tx.Rollback(r.Context())
	manifest, _ := json.Marshal(map[string]any{"source": request.Source, "format": "jsonl", "originalName": originalName})
	err = tx.QueryRow(r.Context(), `insert into catalog_import_packages
		(id,sha256,archive_file_id,archive_name,schema_version,exporter_version,minecraft_version,loader,manifest,namespaces,profile,uploaded_by)
		values($1,$2,$3,$4,$5,$6,'','unknown',$7::jsonb,'{}'::text[],'icons',$8)
		on conflict(sha256) do update set archive_file_id=excluded.archive_file_id,archive_name=excluded.archive_name,
			schema_version=excluded.schema_version,exporter_version=excluded.exporter_version,manifest=excluded.manifest,uploaded_by=excluded.uploaded_by
		returning id`, packageID, digest, archiveFileID, originalName, importerVersion, request.Source, string(manifest), claims.Subject).Scan(&packageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save catalog import package")
		return
	}

	deduplicated, shouldPublish, cleanupDuplicate := false, false, false
	var existingStatus string
	var existingStalled bool
	err = tx.QueryRow(r.Context(), `select id,status,status in ('validating','importing') and coalesce(heartbeat_at,updated_at)<now()-$6::interval
		from catalog_import_jobs where mod_id=$1 and package_id=$2 and importer_version=$3 and target_version_id=$4 and overwrite_existing=$5 for update`,
		identity.ID, packageID, importerVersion, targetVersionID, request.OverwriteExistingImportData, pgInterval(modExportStaleAfter)).
		Scan(&jobID, &existingStatus, &existingStalled)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		jobID = newExportID()
		_, err = tx.Exec(r.Context(), `insert into catalog_import_jobs
			(id,mod_id,package_id,importer_version,target_version_id,overwrite_existing,created_by)
			values($1,$2,$3,$4,$5,$6,$7)`, jobID, identity.ID, packageID, importerVersion,
			targetVersionID, request.OverwriteExistingImportData, claims.Subject)
		shouldPublish = err == nil
	case err == nil && (shouldRetryModExportStatus(existingStatus) || existingStalled):
		_, err = tx.Exec(r.Context(), `update catalog_import_jobs set status='queued',progress=0,current_stage='recovery',
			error_code='',error_detail='{}'::jsonb,created_by=$2,started_at=null,finished_at=null,heartbeat_at=null,
			run_token='',updated_at=now() where id=$1`, jobID, claims.Subject)
		shouldPublish = err == nil
	case err == nil:
		deduplicated = true
		cleanupDuplicate = existingStatus == "ready" || existingStatus == "partial"
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save catalog import job")
		return
	}
	if shouldPublish {
		eventID := newExportID()
		payload, _ := json.Marshal(modExportJobMessage{JobID: jobID})
		if _, err = tx.Exec(r.Context(), `insert into nats_outbox(event_id,subject,aggregate_type,aggregate_id,payload)
			values($1,$2,'mod_export_job',$3,$4::jsonb)`, eventID, modExportTaskCode, jobID, string(payload)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to queue catalog import")
			return
		}
		if err = insertEmbeddedIconImportActivityTx(r.Context(), tx, claims.Subject, jobID, request.TargetVersionPublicID,
			request.OverwriteExistingImportData, request.Source); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record catalog import activity")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit catalog import job")
		return
	}
	if shouldPublish {
		s.dispatchModExportJob(r.Context(), jobID)
	} else if cleanupDuplicate {
		if client, cfg, clientErr := s.ossClient(r.Context()); clientErr == nil {
			_, _ = client.DeleteObject(r.Context(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
		}
		_, _ = s.db.Exec(r.Context(), `update oss_files set status='deleted',updated_at=now() where id=$1`, archiveFileID)
		_, _ = s.db.Exec(r.Context(), `update catalog_import_packages set archive_file_id=null where id=$1 and archive_file_id=$2`, packageID, archiveFileID)
	}
	response, _ := s.modExportJobByID(r.Context(), jobID, identity.ID)
	response.Deduplicated = deduplicated
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) importModExportJob(ctx context.Context, jobID string) error {
	var importerVersion string
	if err := s.db.QueryRow(ctx, `select importer_version from catalog_import_jobs where id=$1`, jobID).Scan(&importerVersion); err != nil {
		return err
	}
	if embeddedIconSourceFromImporterVersion(importerVersion) != "" {
		return s.importEmbeddedIconCatalogJob(ctx, jobID, importerVersion)
	}
	return s.importMCModsExportJob(ctx, jobID)
}

func (s *Server) importEmbeddedIconCatalogJob(ctx context.Context, jobID, importerVersion string) (resultErr error) {
	source := embeddedIconSourceFromImporterVersion(importerVersion)
	if source == "" {
		return errors.New("unsupported embedded icon catalog importer")
	}
	runToken, err := s.claimModExportJob(ctx, jobID)
	if errors.Is(err, errModExportLeaseLost) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, stopHeartbeat := s.startModExportHeartbeat(ctx, jobID, runToken)
	var packageID string
	var modID int64
	defer func() {
		stopHeartbeat()
		if resultErr == nil || errors.Is(resultErr, errModExportLeaseLost) || errors.Is(context.Cause(ctx), errModExportLeaseLost) {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = s.cleanupModExportStaging(cleanupContext, packageID, modID, runToken)
		cancel()
		s.failModExportJob(jobID, runToken, "import_failed", resultErr)
	}()

	var createdBy, sourceFileID, targetVersionID int64
	var targetVersionPublicID, expectedHash, objectKey, uniqueID string
	var overwrite bool
	var minecraftVersions, loaders []string
	err = s.db.QueryRow(ctx, `select job.package_id,job.mod_id,coalesce(job.created_by,0),job.target_version_id,version.public_id,
		job.overwrite_existing,package.sha256,coalesce(file.id,0),coalesce(file.object_key,''),mod.project_code,
		version.minecraft_versions,version.loaders
		from catalog_import_jobs job join catalog_import_packages package on package.id=job.package_id
		join mods mod on mod.id=job.mod_id join mod_content_versions version on version.id=job.target_version_id and version.mod_id=job.mod_id
		left join oss_files file on file.id=package.archive_file_id where job.id=$1`, jobID).
		Scan(&packageID, &modID, &createdBy, &targetVersionID, &targetVersionPublicID, &overwrite, &expectedHash, &sourceFileID,
			&objectKey, &uniqueID, &minecraftVersions, &loaders)
	if err != nil {
		return err
	}
	if objectKey == "" {
		return errors.New("catalog import source file is unavailable")
	}
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return err
	}
	raw, err := downloadEmbeddedIconCatalog(ctx, client, cfg.Bucket, objectKey, expectedHash)
	if err != nil {
		return err
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "validating", 12, "parsing"); err != nil {
		return err
	}
	entries, err := decodeEmbeddedIconCatalogForSource(raw, source)
	if err != nil {
		return err
	}
	namespaces := embeddedIconCatalogNamespaces(entries)
	if len(namespaces) == 0 {
		return errors.New("catalog contains no importable resources")
	}
	manifestJSON, _ := json.Marshal(map[string]any{
		"source": source, "format": "jsonl", "entryCount": len(entries), "namespaces": namespaces,
		"iconSizes": []int{32, 128, 256},
	})
	minecraftVersion, loader := strings.Join(minecraftVersions, ", "), strings.Join(loaders, ", ")
	if loader == "" {
		loader = "unknown"
	}
	if _, err = s.db.Exec(ctx, `update catalog_import_packages set schema_version=$2,exporter_version=$3,
		minecraft_version=$4,loader=$5,manifest=$6::jsonb,namespaces=$7,profile='icons' where id=$1`,
		packageID, importerVersion, source, minecraftVersion, loader, string(manifestJSON), namespaces); err != nil {
		return err
	}
	if _, err = s.db.Exec(ctx, `delete from catalog_import_revisions where job_id=$1 and mod_id=$2 and status='staging'`, jobID, modID); err != nil {
		return err
	}

	revisions := make(map[string]string, len(namespaces))
	if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		for _, namespace := range namespaces {
			lockKey := fmt.Sprintf("catalog-json:%d:%s:%s:%s", modID, targetVersionPublicID, source, namespace)
			if _, lockErr := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); lockErr != nil {
				return lockErr
			}
			revisionID := newExportID()
			var revisionNo int64
			if queryErr := tx.QueryRow(ctx, `select coalesce(max(revision_no),0)+1 from catalog_import_revisions
				where mod_id=$1 and target_version_id=$2 and source_kind=$3 and source_namespace=$4`,
				modID, targetVersionID, source, namespace).Scan(&revisionNo); queryErr != nil {
				return queryErr
			}
			if _, insertErr := tx.Exec(ctx, `insert into catalog_import_revisions
				(id,mod_id,package_id,job_id,target_version_id,revision_no,minecraft_version,loader,exporter_version,
				 source_namespace,source_kind,source_metadata,import_run_token)
				values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13)`,
				revisionID, modID, packageID, jobID, targetVersionID, revisionNo, minecraftVersion, loader,
				importerVersion, namespace, source, string(manifestJSON), runToken); insertErr != nil {
				return insertErr
			}
			revisions[namespace] = revisionID
		}
		return nil
	}); err != nil {
		return err
	}
	if err = s.updateModExportJob(ctx, jobID, runToken, "importing", 25, "icons"); err != nil {
		return err
	}
	resolver, err := loadCatalogResourceIdentityResolver(ctx, s.db)
	if err != nil {
		return err
	}
	uploadedObjects := make(map[string]struct{})
	pngAssetCount := 0
	for start := 0; start < len(entries); start += embeddedIconImportBatch {
		end := min(start+embeddedIconImportBatch, len(entries))
		builds, buildErr := buildEmbeddedIconImports(ctx, cfg, uniqueID, source, resolver, revisions, entries[start:end])
		if buildErr != nil {
			return buildErr
		}
		media := make([]modExportPNGMedia, 0, len(builds)*3)
		resources := make([]catalogResourceImportRow, 0, len(builds))
		pngByPath := make(map[string][]byte, len(builds)*3)
		for _, build := range builds {
			resources = append(resources, build.resource)
			media = append(media, build.media...)
			for assetPath, data := range build.data {
				pngByPath[assetPath] = data
			}
		}
		uploadContext, cancelUploads := context.WithCancel(ctx)
		pool := newModExportPNGUploadPool(uploadContext, client, cfg.Bucket)
		for _, item := range media {
			if _, exists := uploadedObjects[item.ObjectKey]; exists {
				continue
			}
			uploadedObjects[item.ObjectKey] = struct{}{}
			pool.submit(item.ObjectKey, item.AssetPath, item.Digest, pngByPath[item.AssetPath])
		}
		if uploadErr := pool.wait(); uploadErr != nil {
			cancelUploads()
			return uploadErr
		}
		cancelUploads()
		if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
			if persistErr := persistExportPNGMedia(ctx, tx, cfg, uniqueID, createdBy, source, media); persistErr != nil {
				return persistErr
			}
			return persistCatalogResources(ctx, tx, resources)
		}); err != nil {
			return err
		}
		pngAssetCount += len(media)
		progress := 25 + int(float64(end)/float64(len(entries))*60)
		if err = s.updateModExportJob(ctx, jobID, runToken, "importing", progress, "database"); err != nil {
			return err
		}
	}

	revisionIDs := make([]string, 0, len(revisions))
	for _, revisionID := range revisions {
		revisionIDs = append(revisionIDs, revisionID)
	}
	sort.Strings(revisionIDs)
	canActivate := createdBy > 0 && s.userHasPermission(ctx, createdBy, "project.no-review."+uniqueID)
	if err = s.runModExportTransaction(ctx, func(tx pgx.Tx) error {
		for _, revisionID := range revisionIDs {
			if canActivate {
				var namespace, revisionSource string
				if queryErr := tx.QueryRow(ctx, `select source_namespace,source_kind from catalog_import_revisions
					where id=$1 and import_run_token=$2`, revisionID, runToken).Scan(&namespace, &revisionSource); queryErr != nil {
					return queryErr
				}
				if _, updateErr := tx.Exec(ctx, `update catalog_import_revisions set is_active=false,status='superseded'
					where mod_id=$1 and target_version_id=$2 and source_namespace=$3 and source_kind=$4 and id<>$5 and is_active`,
					modID, targetVersionID, namespace, revisionSource, revisionID); updateErr != nil {
					return updateErr
				}
			}
			if _, updateErr := tx.Exec(ctx, `update catalog_import_revisions set status='ready',is_active=$2,
				activated_at=case when $2 then now() else null end where id=$1 and import_run_token=$3`,
				revisionID, canActivate, runToken); updateErr != nil {
				return updateErr
			}
		}
		if canActivate {
			if syncErr := syncImportedResourcesToContentVersionTx(ctx, tx, revisionIDs, targetVersionID, overwrite, createdBy); syncErr != nil {
				return syncErr
			}
		}
		if statsErr := refreshModExportRevisionStats(ctx, tx, revisionIDs); statsErr != nil {
			return statsErr
		}
		detail, _ := json.Marshal(map[string]any{"entryCount": len(entries), "pngAssetCount": pngAssetCount, "source": source})
		tag, updateErr := tx.Exec(ctx, `update catalog_import_jobs set status='ready',progress=100,
			current_stage=case when $4 then 'complete' else 'review' end,error_detail=$3::jsonb,finished_at=now(),
			heartbeat_at=now(),updated_at=now(),run_token='' where id=$1 and run_token=$2`, jobID, runToken, string(detail), canActivate)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() == 0 {
			return errModExportLeaseLost
		}
		_, updateErr = tx.Exec(ctx, `update catalog_import_packages set imported_at=now() where id=$1`, packageID)
		return updateErr
	}); err != nil {
		return err
	}
	_, _ = client.DeleteObject(context.Background(), &aliyunoss.DeleteObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	_, _ = s.db.Exec(context.Background(), `update oss_files set status='deleted',updated_at=now() where id=$1`, sourceFileID)
	s.notifyModExportResult(context.Background(), jobID, "ready", nil)
	return nil
}

func downloadEmbeddedIconCatalog(ctx context.Context, client *aliyunoss.Client, bucket, objectKey, expectedHash string) ([]byte, error) {
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		return nil, fmt.Errorf("download catalog import: %w", err)
	}
	defer result.Body.Close()
	var buffer bytes.Buffer
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(&buffer, hasher), io.LimitReader(result.Body, embeddedIconImportMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if written > embeddedIconImportMaxBytes {
		return nil, errors.New("catalog import exceeds size limit")
	}
	if hex.EncodeToString(hasher.Sum(nil)) != expectedHash {
		return nil, errors.New("catalog import SHA-256 mismatch")
	}
	return buffer.Bytes(), nil
}

func decodeEmbeddedIconCatalog(raw []byte) ([]embeddedIconCatalogEntry, error) {
	return decodeEmbeddedIconCatalogForSource(raw, iconRendererImportSource)
}

func decodeEmbeddedIconCatalogForSource(raw []byte, source string) ([]embeddedIconCatalogEntry, error) {
	source = normalizeEmbeddedIconImportSource(source)
	if source == "" {
		return nil, errors.New("unsupported embedded icon catalog importer")
	}
	raw = bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}))
	if len(raw) == 0 {
		return nil, errors.New("catalog import is empty")
	}
	entries := make([]embeddedIconCatalogEntry, 0)
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("decode catalog JSON array: %w", err)
		}
	} else {
		for lineIndex, line := range bytes.Split(raw, []byte{'\n'}) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var entry embeddedIconCatalogEntry
			if err := json.Unmarshal(line, &entry); err != nil {
				return nil, fmt.Errorf("decode catalog JSON line %d: %w", lineIndex+1, err)
			}
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, errors.New("catalog import contains no entries")
	}
	if len(entries) > maxExportFileCount {
		return nil, errors.New("catalog import contains too many entries")
	}
	seen := make(map[string]int, len(entries))
	validated := make([]embeddedIconCatalogEntry, 0, len(entries))
	for index := range entries {
		entry := &entries[index]
		if source == irrImportSource {
			normalizeIRRCatalogEntry(entry)
		}
		entry.RegisterName = strings.ToLower(strings.TrimSpace(entry.RegisterName))
		entry.Type = strings.ToLower(strings.TrimSpace(entry.Type))
		if !embeddedIconResourceIDPattern.MatchString(entry.RegisterName) {
			return nil, fmt.Errorf("entry %d has invalid registerName %q", index+1, entry.RegisterName)
		}
		if entry.Type != "item" && entry.Type != "block" && entry.Type != "entity" {
			return nil, fmt.Errorf("entry %d has unsupported type %q", index+1, entry.Type)
		}
		if entry.Type == "entity" && source != irrImportSource {
			return nil, fmt.Errorf("entry %d has unsupported type %q", index+1, entry.Type)
		}
		key := entry.Type + "\x00" + entry.RegisterName
		if existingIndex, duplicate := seen[key]; duplicate {
			if source == irrImportSource {
				validated[existingIndex].IRRVariants = append(validated[existingIndex].IRRVariants, irrCatalogVariant(*entry))
				continue
			}
			return nil, fmt.Errorf("entry %d duplicates %s", index+1, entry.RegisterName)
		}
		seen[key] = len(validated)
		if entry.Type != "entity" && strings.TrimSpace(entry.SmallIcon) == "" && strings.TrimSpace(entry.LargeIcon) == "" {
			return nil, fmt.Errorf("entry %d has no icon", index+1)
		}
		if source == irrImportSource {
			entry.IRRVariants = []embeddedIconIRRVariant{irrCatalogVariant(*entry)}
		}
		validated = append(validated, *entry)
	}
	return validated, nil
}

func irrCatalogVariant(entry embeddedIconCatalogEntry) embeddedIconIRRVariant {
	return embeddedIconIRRVariant{
		Name: cleanMinecraftDisplayName(entry.Name), EnglishName: cleanMinecraftDisplayName(entry.EnglishName),
		Metadata: entry.Metadata,
	}
}

func normalizeIRRCatalogEntry(entry *embeddedIconCatalogEntry) {
	entry.Name = cleanMinecraftDisplayName(entry.Name)
	entry.EnglishName = cleanMinecraftDisplayName(defaultString(entry.EnglishName, entry.IRREnglishName))
	entry.CreativeTabName = cleanMinecraftDisplayName(entry.CreativeTabName)
	entry.SmallIcon = defaultString(strings.TrimSpace(entry.SmallIcon), strings.TrimSpace(entry.IRRIcon))
	entry.LargeIcon = defaultString(strings.TrimSpace(entry.LargeIcon), strings.TrimSpace(entry.IRRIcon))
	if strings.TrimSpace(entry.Type) == "" && strings.TrimSpace(entry.Mod) != "" {
		entry.Type = "entity"
	}
}

func cleanMinecraftDisplayName(value string) string {
	return strings.TrimSpace(minecraftFormattingCodePattern.ReplaceAllString(strings.TrimSpace(value), ""))
}

func isLikelyMinecraftTranslationKey(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, ".") &&
		(strings.HasPrefix(value, "entity.") || strings.HasPrefix(value, "item.") || strings.HasPrefix(value, "block.")) &&
		strings.HasSuffix(value, ".name")
}

func embeddedIconCatalogNamespaces(entries []embeddedIconCatalogEntry) []string {
	seen := make(map[string]struct{})
	for _, entry := range entries {
		namespace, _ := resourceParts(entry.RegisterName)
		if namespace != "" {
			seen[namespace] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for namespace := range seen {
		result = append(result, namespace)
	}
	sort.Strings(result)
	return result
}

func buildEmbeddedIconImports(ctx context.Context, cfg ossConfigPayload, uniqueID, source string,
	resolver catalogResourceIdentityResolver, revisions map[string]string, entries []embeddedIconCatalogEntry,
) ([]embeddedIconImportBuild, error) {
	result := make([]embeddedIconImportBuild, len(entries))
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(maxExportPNGConcurrency)
	for index := range entries {
		index := index
		group.Go(func() error {
			select {
			case <-groupContext.Done():
				return groupContext.Err()
			default:
			}
			build, err := buildEmbeddedIconImport(cfg, uniqueID, source, resolver, revisions, entries[index])
			if err != nil {
				return fmt.Errorf("%s: %w", entries[index].RegisterName, err)
			}
			result[index] = build
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}

func buildEmbeddedIconImport(cfg ossConfigPayload, uniqueID, source string, resolver catalogResourceIdentityResolver,
	revisions map[string]string, entry embeddedIconCatalogEntry,
) (embeddedIconImportBuild, error) {
	kindCode, registry := "minecraft.item", "items"
	if entry.Type == "block" {
		kindCode, registry = "minecraft.block", "blocks"
	} else if entry.Type == "entity" {
		kindCode, registry = "minecraft.entity_type", "entities"
	}
	resolved := resolver.resolve(kindCode, entry.RegisterName)
	rawNamespace, _ := resourceParts(entry.RegisterName)
	revisionID := revisions[rawNamespace]
	if revisionID == "" {
		return embeddedIconImportBuild{}, errors.New("resource namespace has no import revision")
	}
	assetPaths := map[int]string{}
	media := make([]modExportPNGMedia, 0, 3)
	iconBytes := make(map[int][]byte, 3)
	if strings.TrimSpace(entry.SmallIcon) != "" || strings.TrimSpace(entry.LargeIcon) != "" {
		small, smallErr := decodeEmbeddedIconPNG(entry.SmallIcon)
		large, largeErr := decodeEmbeddedIconPNG(entry.LargeIcon)
		if smallErr != nil && largeErr != nil {
			return embeddedIconImportBuild{}, fmt.Errorf("decode icons: small=%v, large=%v", smallErr, largeErr)
		}
		for _, size := range []int{32, 128, 256} {
			sourceImage := large
			if size == 32 && smallErr == nil {
				sourceImage = small
			} else if largeErr != nil {
				sourceImage = small
			}
			rendered, err := renderEmbeddedIconPNG(sourceImage, size)
			if err != nil {
				return embeddedIconImportBuild{}, err
			}
			iconBytes[size] = rendered
			assetPath := path.Join("icons", registry, fmt.Sprint(size), resolved.Namespace, resolved.ResourcePath+".png")
			assetPaths[size] = assetPath
			config, _, err := image.DecodeConfig(bytes.NewReader(rendered))
			if err != nil {
				return embeddedIconImportBuild{}, err
			}
			digest := sha256Hex(rendered)
			iconCategory := ossProjectCategory("mod", uniqueID, "icons", registry, fmt.Sprint(size), revisionID)
			media = append(media, modExportPNGMedia{
				RevisionID: revisionID, AssetPath: assetPath,
				ObjectKey: path.Join(ossObjectPrefix(cfg.Prefix, iconCategory), resolved.PublicID+".png"),
				Digest:    digest, ByteLength: int64(len(rendered)), Width: config.Width, Height: config.Height,
				Original: path.Base(assetPath),
			})
		}
	}
	names := map[string]string{}
	if value := cleanMinecraftDisplayName(entry.Name); value != "" && !isLikelyMinecraftTranslationKey(value) {
		names["zh-CN"] = value
	}
	if value := cleanMinecraftDisplayName(entry.EnglishName); value != "" && !isLikelyMinecraftTranslationKey(value) {
		names["en-US"] = value
	}
	translationKey := ""
	if isLikelyMinecraftTranslationKey(entry.Name) {
		translationKey = cleanMinecraftDisplayName(entry.Name)
	} else if isLikelyMinecraftTranslationKey(entry.EnglishName) {
		translationKey = cleanMinecraftDisplayName(entry.EnglishName)
	}
	iconVariants := map[string]string{}
	for _, size := range []int{32, 128, 256} {
		if assetPaths[size] != "" {
			iconVariants[fmt.Sprint(size)] = assetPaths[size]
		}
	}
	namesJSON, _ := json.Marshal(names)
	dataJSON, _ := json.Marshal(map[string]any{
		"id": entry.RegisterName, "registry": registry, "source": source,
		"sourceModId":     strings.TrimSpace(entry.Mod),
		"translationKey":  translationKey,
		"sourceVariants":  entry.IRRVariants,
		"creativeTabName": strings.TrimSpace(entry.CreativeTabName),
		"oreDictionary":   parseEmbeddedIconOreDictionary(entry.OreDictList),
		"maxStackSize":    entry.MaxStackSize, "maxDurability": entry.MaxDurability,
		"iconVariants": iconVariants,
	})
	resource := catalogResourceImportRow{
		EntityID: resolved.ID, PublicID: resolved.PublicID, KindCode: kindCode,
		CanonicalID: resolved.CanonicalID, RawID: resolved.RawID, Namespace: resolved.Namespace, ResourcePath: resolved.ResourcePath,
		RevisionID: revisionID, SnapshotID: catalogSnapshotID("resource", revisionID, resolved.ID, ""),
		Registry: registry, Names: string(namesJSON), Data: string(dataJSON),
		IconPath: assetPaths[32], PreviewPath: assetPaths[256],
	}
	dataByPath := make(map[string][]byte, len(iconBytes))
	for size, data := range iconBytes {
		dataByPath[assetPaths[size]] = data
	}
	return embeddedIconImportBuild{resource: resource, media: media, data: dataByPath}, nil
}

func decodeEmbeddedIconPNG(encoded string) (image.Image, error) {
	encoded = strings.TrimSpace(encoded)
	if separator := strings.Index(encoded, ","); strings.HasPrefix(strings.ToLower(encoded), "data:image/png") && separator >= 0 {
		encoded = encoded[separator+1:]
	}
	if encoded == "" {
		return nil, errors.New("icon is empty")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, errors.New("icon exceeds size limit")
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "png" {
		return nil, errors.New("icon is not a valid PNG")
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || int64(bounds.Dx())*int64(bounds.Dy()) > 100_000_000 {
		return nil, errors.New("icon dimensions are invalid")
	}
	return decoded, nil
}

func renderEmbeddedIconPNG(source image.Image, target int) ([]byte, error) {
	if source == nil || target <= 0 {
		return nil, errors.New("invalid icon resize request")
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := min(float64(target)/float64(width), float64(target)/float64(height))
	renderWidth, renderHeight := max(1, int(float64(width)*scale)), max(1, int(float64(height)*scale))
	offsetX, offsetY := (target-renderWidth)/2, (target-renderHeight)/2
	output := image.NewNRGBA(image.Rect(0, 0, target, target))
	for y := 0; y < renderHeight; y++ {
		sourceY := bounds.Min.Y + min(height-1, y*height/renderHeight)
		for x := 0; x < renderWidth; x++ {
			sourceX := bounds.Min.X + min(width-1, x*width/renderWidth)
			output.SetNRGBA(offsetX+x, offsetY+y, color.NRGBAModel.Convert(source.At(sourceX, sourceY)).(color.NRGBA))
		}
	}
	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&buffer, output); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func parseEmbeddedIconOreDictionary(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || value == "[]" {
		return []string{}
	}
	var parsed []string
	if json.Unmarshal([]byte(value), &parsed) == nil {
		return normalizeEmbeddedIconStrings(parsed)
	}
	value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	return normalizeEmbeddedIconStrings(strings.Split(value, ","))
}

func normalizeEmbeddedIconStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func insertEmbeddedIconImportActivityTx(ctx context.Context, tx pgx.Tx, actorID int64, jobID, versionPublicID string, overwrite bool, source string) error {
	metadata, err := json.Marshal(map[string]any{
		"jobId": jobID, "targetVersionPublicId": versionPublicID,
		"overwriteExistingImportData": overwrite, "source": source,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,object_public_id,metadata,occurred_at)
		values(nullif($1,0),$2,$3,$4,$5::jsonb,$6)`, actorID, activity.ActionUpload, activity.ObjectMod,
		versionPublicID, string(metadata), time.Now().UTC())
	return err
}
