package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
)

func (s *Server) createCreatorRole(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code         string         `json:"code"`
		Name         string         `json:"name"`
		Description  string         `json:"description"`
		Translations map[string]any `json:"translations"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid creator role")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		writeError(w, http.StatusBadRequest, "role name is required")
		return
	}
	request.Code = normalizeCode(request.Code)
	if request.Code == "" {
		request.Code = "custom." + strings.ReplaceAll(normalizeCreatorName(request.Name), " ", "_")
	}
	raw, _ := json.Marshal(request.Translations)
	var id string
	err := s.db.QueryRow(r.Context(), `insert into creator_role_definitions(
		code,name,description,translations,is_custom,created_by
	) values($1,$2,$3,$4,true,$5) returning public_id`,
		request.Code, request.Name, strings.TrimSpace(request.Description), raw, currentClaims(r).Subject,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "role code already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create creator role")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "code": request.Code, "name": request.Name})
}

func createCreatorTx(ctx context.Context, tx pgx.Tx, snapshot creatorSnapshot, actorID int64, status string,
	canManageTeamMembers bool, r *http.Request) (int64, string, createdContentRevision, error) {
	var id int64
	var publicID string
	err := tx.QueryRow(ctx, `insert into creators(
		kind,name,normalized_name,description_markdown,avatar_url,avatar_file_id,created_by,review_status
	) values($1,$2,$3,$4,$5,$6,$7,$8) returning id,public_id`,
		snapshot.Kind, snapshot.Name, normalizeCreatorName(snapshot.Name), snapshot.DescriptionMarkdown,
		snapshot.AvatarURL, snapshot.AvatarInternalID, actorID, status,
	).Scan(&id, &publicID)
	if err != nil {
		return 0, "", createdContentRevision{}, err
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType: snapshot.Kind, EntityID: id, AggregateType: "creator", AggregateKey: publicID, Snapshot: raw,
		Reason: "Create creator profile", ActorID: actorID, Source: "user", Status: status,
		Metadata: map[string]any{"creatorId": publicID, "kind": snapshot.Kind}, Request: r,
	})
	if err != nil {
		return 0, "", createdContentRevision{}, err
	}
	if err = applyCreatorRelationsTx(ctx, tx, id, snapshot, status, canManageTeamMembers, actorID); err != nil {
		return 0, "", createdContentRevision{}, err
	}
	if status == "approved" {
		if err = publishCreatorLocalizationsTx(ctx, tx, id, snapshot.Kind, snapshot.Name, snapshot.DefaultLocale, snapshot.Localizations, created.RevisionID, actorID); err != nil {
			return 0, "", createdContentRevision{}, err
		}
		if _, err = tx.Exec(ctx, `update creators set published_revision_id=$2,review_status='approved' where id=$1`, id, created.RevisionID); err != nil {
			return 0, "", createdContentRevision{}, err
		}
		if err = appendReviewResolutionTx(ctx, tx, created.ChangeRequestID, "approved", actorID, "automatic approval", r); err != nil {
			return 0, "", createdContentRevision{}, err
		}
	}
	return id, publicID, created, nil
}

func ensureNamedCreatorSnapshotTx(ctx context.Context, tx pgx.Tx, snapshot creatorSnapshot, actorID int64, status string, r *http.Request) (int64, string, bool, error) {
	if snapshot.Kind != "author" && snapshot.Kind != "team" {
		snapshot.Kind = "author"
	}
	if err := normalizeCreatorSnapshot(&snapshot); err != nil {
		return 0, "", false, err
	}
	var id int64
	var publicID string
	identityType, identityURL := creatorIdentityLink(snapshot.Links)
	err := tx.QueryRow(ctx, `select id,public_id from creators
		where kind=$1 and (
			normalized_name=$2 or ($3<>'' and exists(
				select 1 from creator_links link where link.creator_id=creators.id and link.link_type=$3 and link.url=$4
			)))
		order by ($3<>'' and exists(
			select 1 from creator_links link where link.creator_id=creators.id and link.link_type=$3 and link.url=$4
		)) desc,review_status='approved' desc,id limit 1`,
		snapshot.Kind, normalizeCreatorName(snapshot.Name), identityType, identityURL).Scan(&id, &publicID)
	if err == nil {
		return id, publicID, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, err
	}
	id, publicID, _, err = createCreatorTx(ctx, tx, snapshot, actorID, status, false, r)
	if err != nil {
		return 0, "", false, fmt.Errorf("create imported creator: %w", err)
	}
	return id, publicID, true, nil
}

func creatorIdentityLink(links []creatorLinkPayload) (string, string) {
	for _, link := range links {
		switch link.Type {
		case "modrinth", "curseforge":
			if link.URL != "" {
				return link.Type, link.URL
			}
		}
	}
	return "", ""
}

func (s *Server) applyCreatorSnapshotTx(
	ctx context.Context,
	tx pgx.Tx,
	creatorID, revisionID, actorID, uploaderID int64,
	canManageTeamMembers bool,
	snapshot creatorSnapshot,
) error {
	if err := normalizeCreatorSnapshot(&snapshot); err != nil {
		return err
	}
	var avatarFileID *int64
	if snapshot.AvatarFileID != nil {
		file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, *snapshot.AvatarFileID, ossRasterBindingScope{UploaderID: uploaderID})
		if err != nil {
			return err
		}
		avatarFileID = &file.ID
		snapshot.AvatarURL = ossStoredObjectURL(s.ossConfigFromSettings(ctx), file.ObjectKey)
	}
	if _, err := tx.Exec(ctx, `update creators set
		name=$2,normalized_name=$3,description_markdown=$4,avatar_url=$5,avatar_file_id=$6,
		review_status='approved',published_revision_id=$7,updated_at=now()
		where id=$1`, creatorID, snapshot.Name, normalizeCreatorName(snapshot.Name),
		snapshot.DescriptionMarkdown, snapshot.AvatarURL, avatarFileID, revisionID); err != nil {
		return err
	}
	if err := publishCreatorLocalizationsTx(ctx, tx, creatorID, snapshot.Kind, snapshot.Name, snapshot.DefaultLocale, snapshot.Localizations, revisionID, actorID); err != nil {
		return err
	}
	return applyCreatorRelationsTx(ctx, tx, creatorID, snapshot, "approved", canManageTeamMembers, actorID)
}

func (s *Server) resolveCreatorAvatarTx(
	ctx context.Context,
	tx pgx.Tx,
	actorID int64,
	snapshot *creatorSnapshot,
) error {
	if snapshot.AvatarFileID == nil {
		// A provider/import URL is presentation-only until the user explicitly
		// uploads a trusted OSS file. Never persist a client-supplied hotlink.
		snapshot.AvatarInternalID = nil
		snapshot.AvatarURL = ""
		return nil
	}
	publicID := strings.ToLower(strings.TrimSpace(*snapshot.AvatarFileID))
	if publicID == "" {
		snapshot.AvatarFileID = nil
		snapshot.AvatarInternalID = nil
		snapshot.AvatarURL = ""
		return nil
	}
	file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, publicID, ossRasterBindingScope{UploaderID: actorID})
	if err != nil {
		return errors.New("creator avatar file was not found")
	}
	snapshot.AvatarFileID = &publicID
	snapshot.AvatarInternalID = &file.ID
	snapshot.AvatarURL = ossStoredObjectURL(s.ossConfigFromSettings(ctx), file.ObjectKey)
	return nil
}

func applyCreatorRelationsTx(ctx context.Context, tx pgx.Tx, creatorID int64, snapshot creatorSnapshot,
	creatorStatus string, canManageTeamMembers bool, actorID int64) error {
	if _, err := tx.Exec(ctx, `delete from creator_links where creator_id=$1`, creatorID); err != nil {
		return err
	}
	for index, link := range snapshot.Links {
		if _, err := tx.Exec(ctx, `insert into creator_links(creator_id,link_type,url,label,display_order)
			values($1,$2,$3,$4,$5)`, creatorID, link.Type, link.URL, link.Label, index); err != nil {
			return err
		}
	}
	if snapshot.Kind != "team" || snapshot.Members == nil {
		return nil
	}
	return replaceCreatorTeamMembersTx(ctx, tx, creatorID, snapshot.Members, creatorStatus, canManageTeamMembers, actorID)
}

func replaceCreatorTeamMembersTx(ctx context.Context, tx pgx.Tx, creatorID int64, members []creatorMemberPayload,
	creatorStatus string, canManageTeamMembers bool, actorID int64) error {
	type existingTeamMember struct {
		MemberID, RoleID int64
		Title, Status    string
		DisplayOrder     int
	}
	existing := map[string]existingTeamMember{}
	rows, err := tx.Query(ctx, `select member_creator_id,role_id,title,status,display_order
		from creator_team_members where team_id=$1
		order by member_creator_id,role_id for update`, creatorID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item existingTeamMember
		if err = rows.Scan(&item.MemberID, &item.RoleID, &item.Title, &item.Status, &item.DisplayOrder); err != nil {
			rows.Close()
			return err
		}
		existing[fmt.Sprintf("%d:%d", item.MemberID, item.RoleID)] = item
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	requested := make(map[string]struct{}, len(members))
	for index, member := range members {
		var memberID int64
		if err := tx.QueryRow(ctx, `select id from creators where public_id=$1 and kind='author'`, member.CreatorID).Scan(&memberID); err != nil {
			return err
		}
		var roleID int64
		if err := tx.QueryRow(ctx, `select id from creator_role_definitions where public_id=$1`, member.RoleID).Scan(&roleID); err != nil {
			return err
		}
		key := fmt.Sprintf("%d:%d", memberID, roleID)
		if _, duplicate := requested[key]; duplicate {
			return errors.New("duplicate team author membership")
		}
		requested[key] = struct{}{}
		previous, found := existing[key]
		status := "pending"
		if found && previous.Status == "approved" {
			status = "approved"
		} else if creatorStatus == "approved" && canManageTeamMembers {
			status = "approved"
		}
		if found {
			if _, err = tx.Exec(ctx, `update creator_team_members set
				title=$4,status=$5,display_order=$6,
				approved_by=case when $5='approved' and status<>'approved' then $7 else approved_by end,
				approved_at=case when $5='approved' and status<>'approved' then now() else approved_at end,
				updated_at=now()
				where team_id=$1 and member_creator_id=$2 and role_id=$3`, creatorID, memberID, roleID,
				member.Title, status, index, actorID); err != nil {
				return err
			}
			continue
		}
		if _, err = tx.Exec(ctx, `insert into creator_team_members(
			team_id,member_creator_id,role_id,title,status,created_by,approved_by,approved_at,display_order
		) values($1,$2,$3,$4,$5,$6::bigint,case when $5='approved' then $6::bigint else null end,
			case when $5='approved' then now() else null end,$7)`, creatorID, memberID, roleID,
			member.Title, status, actorID, index); err != nil {
			return err
		}
	}
	for key, previous := range existing {
		if _, retained := requested[key]; retained {
			continue
		}
		if canManageTeamMembers && previous.Status != "revoked" {
			if _, err = tx.Exec(ctx, `update creator_team_members set status='revoked',updated_at=now()
				where team_id=$1 and member_creator_id=$2 and role_id=$3`, creatorID, previous.MemberID, previous.RoleID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) creatorLinks(ctx context.Context, creatorID int64) ([]creatorLinkPayload, error) {
	rows, err := s.db.Query(ctx, `select link_type,url,label from creator_links where creator_id=$1 order by display_order,id`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]creatorLinkPayload, 0)
	for rows.Next() {
		var item creatorLinkPayload
		if err = rows.Scan(&item.Type, &item.URL, &item.Label); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Server) creatorCollaborators(ctx context.Context, creatorID int64) ([]creatorSummary, error) {
	rows, err := s.db.Query(ctx, `
		select distinct collaborator.public_id,collaborator.kind,collaborator.name,collaborator.avatar_url,
		       collaborator.review_status,exists(select 1 from creator_claims claim
		         where claim.creator_id=collaborator.id and claim.status='approved'),
		       (select count(distinct mod.id) from content_creator_bindings binding
		        join mods mod on mod.id=binding.subject_id
		        where binding.creator_id=collaborator.id and binding.subject_type='mod' and binding.status='approved'
		          and mod.review_status='approved')
		from content_creator_bindings own_binding
		join content_creator_bindings collaborator_binding
		  on collaborator_binding.subject_type=own_binding.subject_type
		 and collaborator_binding.subject_id=own_binding.subject_id
		 and collaborator_binding.creator_id<>own_binding.creator_id
		join creators collaborator on collaborator.id=collaborator_binding.creator_id
		where own_binding.creator_id=$1 and own_binding.status='approved' and collaborator_binding.status='approved'
		  and (own_binding.subject_type<>'mod' or exists(
		    select 1 from mods shared_mod where shared_mod.id=own_binding.subject_id and shared_mod.review_status='approved'
		  ))
		  and collaborator.review_status='approved'
		order by collaborator.name`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]creatorSummary, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var item creatorSummary
		if err = rows.Scan(&item.PublicID, &item.Kind, &item.Name, &item.AvatarURL, &item.ReviewStatus, &item.Claimed, &item.WorkCount); err != nil {
			return nil, err
		}
		item.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, item.AvatarURL)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Server) creatorMembers(ctx context.Context, creatorID, viewerID int64, admin bool) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		select member.public_id,member.name,member.avatar_url,role.public_id,role.code,role.name,relation.title
		from creator_team_members relation
		join creators member on member.id=relation.member_creator_id
		join creator_role_definitions role on role.id=relation.role_id
		where relation.team_id=$1 and relation.status='approved'
		  and (member.review_status='approved' or member.created_by=$2 or $3)
		order by relation.display_order,relation.created_at`, creatorID, viewerID, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var publicID, name, avatarURL, roleID, roleCode, roleName, title string
		if err = rows.Scan(&publicID, &name, &avatarURL, &roleID, &roleCode, &roleName, &title); err != nil {
			return nil, err
		}
		avatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, avatarURL)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"creatorId": publicID, "name": name, "avatarUrl": avatarURL,
			"role": map[string]any{"id": roleID, "code": roleCode, "name": roleName}, "title": title,
		})
	}
	return result, rows.Err()
}

func (s *Server) creatorTeams(ctx context.Context, creatorID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		select team.public_id,team.kind,team.name,team.avatar_url,team.review_status,
		       false,
		       (select count(distinct mod.id) from content_creator_bindings binding
		        join mods mod on mod.id=binding.subject_id
		        where binding.creator_id=team.id and binding.subject_type='mod' and binding.status='approved'
		          and mod.review_status='approved'),
		       role.public_id,role.code,role.name,relation.title
		from creator_team_members relation
		join creators team on team.id=relation.team_id
		join creator_role_definitions role on role.id=relation.role_id
		where relation.member_creator_id=$1 and relation.status='approved' and team.review_status='approved'
		order by lower(team.name),team.id,relation.display_order`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var team creatorSummary
		var roleID, roleCode, roleName, title string
		if err = rows.Scan(
			&team.PublicID, &team.Kind, &team.Name, &team.AvatarURL, &team.ReviewStatus,
			&team.Claimed, &team.WorkCount, &roleID, &roleCode, &roleName, &title,
		); err != nil {
			return nil, err
		}
		team.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, team.AvatarURL)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"team":  team,
			"role":  map[string]any{"id": roleID, "code": roleCode, "name": roleName},
			"title": title,
		})
	}
	return result, rows.Err()
}

func (s *Server) creatorWorks(ctx context.Context, creatorID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		select distinct mod.project_code,mod.slug,mod.primary_name,mod.secondary_name,mod.summary,mod.icon_url
		from content_creator_bindings binding join mods mod on mod.id=binding.subject_id
		where binding.creator_id=$1 and binding.subject_type='mod' and binding.status='approved' and mod.review_status='approved'
		order by mod.primary_name`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var uniqueID, siteID, primaryName, secondaryName, summary, iconURL string
		if err = rows.Scan(&uniqueID, &siteID, &primaryName, &secondaryName, &summary, &iconURL); err != nil {
			return nil, err
		}
		iconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, iconURL)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"uniqueId": uniqueID, "siteId": siteID, "primaryName": primaryName,
			"secondaryName": secondaryName, "summary": summary, "iconUrl": iconURL,
		})
	}
	return result, rows.Err()
}

func normalizeCreatorSnapshot(snapshot *creatorSnapshot) error {
	snapshot.Kind = strings.ToLower(strings.TrimSpace(snapshot.Kind))
	if snapshot.Kind != "author" && snapshot.Kind != "team" {
		return errors.New("creator kind must be author or team")
	}
	snapshot.Name = strings.TrimSpace(snapshot.Name)
	snapshot.DescriptionMarkdown = strings.TrimSpace(snapshot.DescriptionMarkdown)
	if snapshot.Name == "" || len([]byte(snapshot.Name)) > 160 {
		return errors.New("creator name is required and must not exceed 160 bytes")
	}
	if strings.TrimSpace(snapshot.DefaultLocale) == "" {
		snapshot.DefaultLocale = defaultCreatorLocale(snapshot.DescriptionMarkdown, snapshot.Name)
	}
	if len(snapshot.Localizations) == 0 {
		snapshot.Localizations = []creatorLocalizationEdit{{
			Locale: snapshot.DefaultLocale, ContentMarkdown: snapshot.DescriptionMarkdown,
		}}
	}
	defaultLocale, localizations, localizationErr := normalizeCreatorLocalizations(snapshot.DefaultLocale, snapshot.Localizations)
	if localizationErr != nil {
		return errors.New("localized creator content is invalid")
	}
	snapshot.DefaultLocale, snapshot.Localizations = defaultLocale, localizations
	for _, localization := range localizations {
		if localization.Locale != defaultLocale {
			continue
		}
		snapshot.DescriptionMarkdown = localization.ContentMarkdown
		break
	}
	snapshot.AvatarURL = strings.TrimSpace(snapshot.AvatarURL)
	if snapshot.AvatarURL != "" && !validHTTPURL(snapshot.AvatarURL) {
		return errors.New("avatar URL must use HTTP or HTTPS")
	}
	seenLinks := map[string]struct{}{}
	cleanLinks := make([]creatorLinkPayload, 0, len(snapshot.Links))
	for _, link := range snapshot.Links {
		link.Type = normalizeCode(link.Type)
		link.URL = strings.TrimSpace(link.URL)
		link.Label = strings.TrimSpace(link.Label)
		if link.Type == "" || !validHTTPURL(link.URL) || len(link.Label) > 160 {
			return errors.New("creator links must have a type and HTTP or HTTPS URL")
		}
		key := link.Type + "\x00" + link.URL
		if _, exists := seenLinks[key]; exists {
			continue
		}
		seenLinks[key] = struct{}{}
		cleanLinks = append(cleanLinks, link)
	}
	snapshot.Links = cleanLinks
	if snapshot.Kind != "team" {
		snapshot.Members = nil
	}
	return nil
}

func (s *Server) creatorLocalizations(ctx context.Context, creatorID int64, kind, name, description string) (string, []creatorLocalizationEdit, error) {
	defaultLocale := ""
	if err := s.db.QueryRow(ctx, `select default_locale from content_subjects where subject_type=$1 and subject_id=$2`, kind, creatorID).Scan(&defaultLocale); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	rows, err := s.db.Query(ctx, `select locale,content_markdown from content_localizations
		where subject_type=$1 and subject_id=$2 and review_status='approved' order by locale`, kind, creatorID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	localizations := make([]creatorLocalizationEdit, 0)
	for rows.Next() {
		var item creatorLocalizationEdit
		if err = rows.Scan(&item.Locale, &item.ContentMarkdown); err != nil {
			return "", nil, err
		}
		localizations = append(localizations, item)
	}
	if err = rows.Err(); err != nil {
		return "", nil, err
	}
	if len(localizations) == 0 {
		defaultLocale = defaultCreatorLocale(name, description)
		localizations = append(localizations, creatorLocalizationEdit{Locale: defaultLocale, ContentMarkdown: description})
	}
	if defaultLocale == "" {
		defaultLocale = localizations[0].Locale
	}
	return defaultLocale, localizations, nil
}

func publishCreatorLocalizationsTx(ctx context.Context, tx pgx.Tx, creatorID int64, kind, name, defaultLocale string, localizations []creatorLocalizationEdit, revisionID, actorID int64) error {
	if _, err := tx.Exec(ctx, `insert into content_subjects(subject_type,subject_id,default_locale)
		values($1,$2,$3) on conflict(subject_type,subject_id) do update
		set default_locale=excluded.default_locale,updated_at=now()`, kind, creatorID, defaultLocale); err != nil {
		return err
	}
	catalogLocalizations := make([]catalogLocalizationEdit, 0, len(localizations))
	for _, localization := range localizations {
		catalogLocalizations = append(catalogLocalizations, catalogLocalizationEdit{
			Locale: localization.Locale, Name: name, ContentMarkdown: localization.ContentMarkdown,
		})
	}
	if err := publishCatalogLocalizationsTx(ctx, tx, creatorID, "", kind, catalogLocalizations, revisionID, actorID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update content_localizations set name=$3
		where subject_type=$1 and subject_id=$2 and name<>$3`, kind, creatorID, name)
	return err
}

func normalizeCreatorLocalizations(defaultLocale string, localizations []creatorLocalizationEdit) (string, []creatorLocalizationEdit, error) {
	seen := make(map[string]struct{}, len(localizations))
	for index := range localizations {
		locale, err := normalizeCatalogLocale(localizations[index].Locale)
		if err != nil || !isEditableContentLocale(locale) {
			return "", nil, errCatalogEditorInvalid
		}
		if _, exists := seen[locale]; exists {
			return "", nil, errCatalogEditorInvalid
		}
		if len(localizations[index].ContentMarkdown) > maxModExportEntryMarkdownBytes {
			return "", nil, errCatalogEditorInvalid
		}
		seen[locale] = struct{}{}
		localizations[index].Locale = locale
	}
	if strings.TrimSpace(defaultLocale) == "" {
		defaultLocale = "en-US"
	}
	normalizedDefault, err := normalizeCatalogLocale(defaultLocale)
	if err != nil || !isEditableContentLocale(normalizedDefault) {
		return "", nil, errCatalogEditorInvalid
	}
	if _, exists := seen[normalizedDefault]; !exists {
		return "", nil, errCatalogEditorInvalid
	}
	return normalizedDefault, localizations, nil
}

func defaultCreatorLocale(values ...string) string {
	for _, value := range values {
		for _, char := range value {
			if unicode.Is(unicode.Han, char) {
				return "zh-CN"
			}
		}
	}
	return "en-US"
}

func resolveCreatorClaimProofFiles(ctx context.Context, tx pgx.Tx, userID int64, publicIDs []string) ([]reviewAttachmentFile, error) {
	if len(publicIDs) > creatorClaimMaxFiles {
		return nil, errors.New("claim attachments cannot exceed 5 files")
	}
	files := make([]reviewAttachmentFile, 0, len(publicIDs))
	var total int64
	for _, publicID := range publicIDs {
		file, err := lookupReviewAttachment(ctx, tx, reviewAttachmentLookup{
			Kind: reviewAttachmentByUploader, UploaderID: userID,
		}, publicID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("claim attachment was not found or does not belong to the current user")
		}
		if err != nil {
			return nil, errors.New("failed to load claim attachment")
		}
		total += file.SizeBytes
		files = append(files, file)
	}
	if total > creatorClaimMaxTotalBytes {
		return nil, errors.New("claim attachments cannot exceed 10 MB in total")
	}
	return files, nil
}

func (s *Server) creatorClaimAttachments(ctx context.Context, claimIDs []int64) (map[int64][]creatorClaimAttachment, error) {
	result := make(map[int64][]creatorClaimAttachment, len(claimIDs))
	if len(claimIDs) == 0 {
		return result, nil
	}
	rows, err := s.db.Query(ctx, `select attachment.claim_id,file.public_id,file.original_name,
		greatest(file.size_bytes,file.source_size_bytes)
		from creator_claim_attachments attachment
		join oss_files file on file.id=attachment.oss_file_id
		where attachment.claim_id=any($1::bigint[]) and `+safeReviewAttachmentPredicate+`
		order by attachment.claim_id,attachment.display_order,file.id`, claimIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var claimID int64
		var item creatorClaimAttachment
		if err = rows.Scan(&claimID, &item.ID, &item.Name, &item.SizeBytes); err != nil {
			return nil, err
		}
		result[claimID] = append(result[claimID], item)
	}
	return result, rows.Err()
}

func normalizeCreatorName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

func uniquePublicIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) != 9 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func creatorObjectType(kind string) int16 {
	if kind == "team" {
		return activity.ObjectTeam
	}
	return activity.ObjectAuthor
}

func creatorPath(kind, publicID string) string {
	if kind == "team" {
		return "/teams/" + publicID
	}
	return "/authors/" + publicID
}

func boundedInt(value string, fallback, minimum, maximum int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	if parsed < minimum {
		return minimum
	}
	if parsed > maximum {
		return maximum
	}
	return parsed
}

func creatorNameTx(ctx context.Context, tx pgx.Tx, creatorID int64) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `select name from creators where id=$1`, creatorID).Scan(&name)
	return name, err
}

func creatorRoleNameTx(ctx context.Context, tx pgx.Tx, roleID *string) (string, error) {
	if roleID == nil {
		return "", nil
	}
	var name string
	err := tx.QueryRow(ctx, `select name from creator_role_definitions where public_id=$1`, *roleID).Scan(&name)
	return name, err
}

func creatorRoleInternalIDTx(ctx context.Context, tx pgx.Tx, roleID *string) (*int64, error) {
	if roleID == nil {
		return nil, nil
	}
	var internalID int64
	if err := tx.QueryRow(ctx, `select id from creator_role_definitions where public_id=$1`, *roleID).Scan(&internalID); err != nil {
		return nil, err
	}
	return &internalID, nil
}

func creatorRolePermissionGrantingTx(ctx context.Context, tx pgx.Tx, roleID *int64) (bool, error) {
	if roleID == nil {
		return false, nil
	}
	var permissionGranting bool
	if err := tx.QueryRow(ctx, `select permission_granting from creator_role_definitions where id=$1`, *roleID).Scan(&permissionGranting); err != nil {
		return false, err
	}
	return permissionGranting, nil
}
