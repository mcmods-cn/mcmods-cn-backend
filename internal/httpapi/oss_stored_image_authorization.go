package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

var errInvalidStoredProjectIcon = errors.New("project icon must be an accessible, scanned raster image")

// An endpoint and object key identify storage, not permission. In particular,
// user-controlled icon URLs must never turn private uploads into signed URLs.
func storedOSSObjectKey(cfg ossConfigPayload, storedURL string) (string, bool) {
	for _, endpoint := range []string{cfg.PublicEndpoint, cfg.Endpoint} {
		if key, ok := ossObjectKeyUnderEndpoint(storedURL, endpoint); ok {
			return key, true
		}
	}
	return "", false
}

func storedRasterOSSFileAuthorized(ctx context.Context, query revisionQuery, cfg ossConfigPayload, objectKey string, actorID int64, lock bool) (bool, error) {
	statement := `select id,coalesce(uploader_id,0),content_type,endpoint from oss_files
		where bucket=$1 and object_key=$2 and status='active' and scan_status in ('clean','trusted_generated')`
	if lock {
		statement += ` for share`
	}
	var fileID, uploaderID int64
	var contentType, endpoint string
	err := query.QueryRow(ctx, statement, cfg.Bucket, objectKey).Scan(&fileID, &uploaderID, &contentType, &endpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !safeRasterContentType(contentType) {
		return false, nil
	}
	if actorID > 0 && uploaderID == actorID {
		return true, nil
	}
	aliases := make([]string, 0, 3)
	for _, base := range []string{cfg.PublicEndpoint, cfg.Endpoint, endpoint} {
		if base != "" {
			aliases = append(aliases, strings.TrimRight(base, "/")+"/"+objectKey)
		}
	}
	var public bool
	err = query.QueryRow(ctx, `select `+storedRasterPublicBindingSQL, fileID, uploaderID, aliases).Scan(&public)
	return public, err
}

const storedRasterPublicBindingSQL = `		exists(select 1 from users where status<>'deleted' and (avatar_file_id=$1 or profile_background_file_id=$1))
		or exists(select 1 from creators where review_status='approved' and avatar_file_id=$1)
		or exists(select 1 from blueprints where cover_file_id=$1 and status in ('ready','partial') and review_status in ('not_required','approved'))
		or exists(select 1 from skin_texture_blobs blob join skin_assets asset on asset.blob_hash=blob.hash
			where blob.oss_file_id=$1 and asset.status='active' and asset.review_status='approved' and asset.visibility in ('public','unlisted'))
		or exists(
			select 1 from (
				select 'mod'::text entity_type,id,icon_url from mods where review_status='approved'
				union all select 'modpack',id,icon_url from modpacks where review_status='approved'
				union all select project_type,id,icon_url from simple_projects where review_status='approved'
			) project
			join content_revisions revision on revision.entity_type=project.entity_type and revision.entity_id=project.id
			join change_requests request on request.proposed_revision_id=revision.id and request.status='approved'
			where revision.created_by=$2 and $2>0
			  and split_part(split_part(project.icon_url,'?',1),'#',1)=any($3::text[])
			  and split_part(split_part(revision.snapshot->>'iconUrl','?',1),'#',1)=any($3::text[])
		)`

// Lists collect their URLs after closing the original rows. Authorization is
// loaded once for the whole bounded page; presigning itself needs no network.
func (s *Server) resolveStoredOSSImageURLsWithConfig(ctx context.Context, cfg ossConfigPayload, urls []string) ([]string, error) {
	resolved := make([]string, len(urls))
	keys := make([]string, 0, len(urls))
	byIndex := make(map[int]string)
	seen := make(map[string]bool)
	for i, raw := range urls {
		raw = strings.TrimSpace(raw)
		key, internal := storedOSSObjectKey(cfg, raw)
		if !internal {
			resolved[i] = raw
			continue
		}
		byIndex[i] = key
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return resolved, nil
	}
	claims, _ := ctx.Value(claimsContextKey).(security.Claims)
	// Substitutions apply only to our static SQL, never to a URL or user input.
	publicBinding := strings.NewReplacer(
		"$1", "file.id", "$2", "coalesce(file.uploader_id,0)",
		"$3", `array[rtrim($3::text,'/')||'/'||file.object_key,rtrim($4::text,'/')||'/'||file.object_key,rtrim(file.endpoint,'/')||'/'||file.object_key]`,
	).Replace(storedRasterPublicBindingSQL)
	rows, err := s.db.Query(ctx, `select file.object_key from oss_files file
		where file.bucket=$1 and file.object_key=any($2::text[]) and file.status='active'
		  and file.scan_status in ('clean','trusted_generated') and lower(btrim(split_part(file.content_type,';',1)))=any($6::text[])
		  and ((file.uploader_id=$5 and $5>0) or (`+publicBinding+`))`,
		cfg.Bucket, keys, cfg.PublicEndpoint, cfg.Endpoint, claims.Subject, safeRasterContentTypes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allowed := make(map[string]bool)
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, err
		}
		allowed[key] = true
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i, key := range byIndex {
		if !allowed[key] {
			continue
		}
		access, err := s.resolveOSSObjectAccessWithConfig(ctx, cfg, key, ossObjectAccessOptions{})
		if err != nil {
			return nil, err
		}
		resolved[i] = access.URL
	}
	return resolved, nil
}

// Pending revision previews need a resource-scoped proof, not a general
// administrator exception to file authorization. The immutable snapshot must
// have been authored by the uploader and the viewer must edit/review its target.
type storedProjectRevisionIcon struct {
	EntityType       string
	EntityID         int64
	RevisionPublicID string
	URL              string
}

func (s *Server) resolveStoredProjectRevisionIconURL(ctx context.Context, cfg ossConfigPayload, entityType string, entityID int64, revisionPublicID, storedURL string) (string, error) {
	resolved, err := s.resolveStoredProjectRevisionIconURLs(ctx, cfg, []storedProjectRevisionIcon{{entityType, entityID, revisionPublicID, storedURL}})
	if err != nil {
		return "", err
	}
	return resolved[0], nil
}

func (s *Server) resolveStoredProjectRevisionIconURLs(ctx context.Context, cfg ossConfigPayload, icons []storedProjectRevisionIcon) ([]string, error) {
	urls := make([]string, len(icons))
	for i := range icons {
		urls[i] = icons[i].URL
	}
	resolved, err := s.resolveStoredOSSImageURLsWithConfig(ctx, cfg, urls)
	if err != nil {
		return nil, err
	}
	claims, _ := ctx.Value(claimsContextKey).(security.Claims)
	if claims.Subject <= 0 {
		return resolved, nil
	}
	indices, entityIDs := make([]int64, 0), make([]int64, 0)
	entityTypes, revisionIDs, keys := make([]string, 0), make([]string, 0), make([]string, 0)
	for i, icon := range icons {
		key, internal := storedOSSObjectKey(cfg, icon.URL)
		if resolved[i] != "" || !internal {
			continue
		}
		indices = append(indices, int64(i))
		entityIDs = append(entityIDs, icon.EntityID)
		entityTypes = append(entityTypes, icon.EntityType)
		revisionIDs = append(revisionIDs, icon.RevisionPublicID)
		keys = append(keys, key)
	}
	if len(indices) == 0 {
		return resolved, nil
	}
	rows, err := s.db.Query(ctx, `select input.ordinal,route.public_id,revision.created_by from
		unnest($1::bigint[],$2::text[],$3::bigint[],$4::text[],$5::text[]) input(ordinal,entity_type,entity_id,revision_id,object_key)
		join content_revisions revision on revision.entity_type=input.entity_type and revision.entity_id=input.entity_id and revision.public_id=input.revision_id
		join public_routes route on route.entity_type=revision.entity_type and route.internal_id=revision.entity_id
		join oss_files file on file.uploader_id=revision.created_by and file.bucket=$6 and file.object_key=input.object_key
		  and file.status='active' and file.scan_status in ('clean','trusted_generated')
		  and lower(btrim(split_part(file.content_type,';',1)))=any($9::text[])
		where revision.created_by>0 and revision.entity_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon')
		  and (exists(select 1 from mods where revision.entity_type='mod' and id=revision.entity_id)
		    or exists(select 1 from modpacks where revision.entity_type='modpack' and id=revision.entity_id)
		    or exists(select 1 from simple_projects where project_type=revision.entity_type and id=revision.entity_id))
		  and split_part(split_part(revision.snapshot->>'iconUrl','?',1),'#',1)=any(array[
		    rtrim($7::text,'/')||'/'||file.object_key,rtrim($8::text,'/')||'/'||file.object_key,rtrim(file.endpoint,'/')||'/'||file.object_key])`,
		indices, entityTypes, entityIDs, revisionIDs, keys, cfg.Bucket, cfg.PublicEndpoint, cfg.Endpoint, safeRasterContentTypes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allowed := make([]int, 0)
	for rows.Next() {
		var index int
		var projectPublicID string
		var uploaderID int64
		if err = rows.Scan(&index, &projectPublicID, &uploaderID); err != nil {
			return nil, err
		}
		if canReviewProjectSubmission(claims, projectPublicID, uploaderID) || claimsAllow(claims, "project.edit") || claimsAllow(claims, "project.edit."+projectPublicID) {
			allowed = append(allowed, index)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for _, index := range allowed {
		key, _ := storedOSSObjectKey(cfg, icons[index].URL)
		access, err := s.resolveOSSObjectAccessWithConfig(ctx, cfg, key, ossObjectAccessOptions{})
		if err != nil {
			return nil, err
		}
		resolved[index] = access.URL
	}
	return resolved, nil
}

// Call with the mutation transaction so the scanned file cannot be deleted
// concurrently with accepting the snapshot. A previously stored URL grants no
// permission by itself; accessible public bindings allow editors to retain it.
func validateStoredProjectIconURL(ctx context.Context, query revisionQuery, cfg ossConfigPayload, proposedURL, _ string, actorID int64) (string, error) {
	proposedURL = strings.TrimSpace(proposedURL)
	if proposedURL == "" {
		return "", nil
	}
	if !validHTTPURL(proposedURL) {
		return "", errInvalidStoredProjectIcon
	}
	key, internal := storedOSSObjectKey(cfg, proposedURL)
	if !internal {
		return proposedURL, nil
	}
	allowed, err := storedRasterOSSFileAuthorized(ctx, query, cfg, key, actorID, true)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", errInvalidStoredProjectIcon
	}
	return ossStoredObjectURL(cfg, key), nil
}
