package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func normalizeAndValidateModRequest(req *createModRequest) error {
	req.SiteID = normalizeModSiteID(req.SiteID)
	req.PrimaryName = strings.TrimSpace(req.PrimaryName)
	req.SecondaryName = strings.TrimSpace(req.SecondaryName)
	req.Abbreviation = strings.TrimSpace(req.Abbreviation)
	req.Summary = strings.TrimSpace(req.Summary)
	defaultLocale, localizations, localizationErr := normalizeCatalogLocalizations(req.DefaultLocale, req.Localizations)
	if localizationErr != nil {
		return errors.New("localized mod content is invalid")
	}
	if len(localizations) == 0 {
		localizedName := req.SecondaryName
		if strings.TrimSpace(localizedName) == "" {
			localizedName = req.PrimaryName
		}
		localizations = []catalogLocalizationEdit{{Locale: defaultLocale, Name: localizedName, Summary: req.Summary, ContentMarkdown: req.BodyMarkdown}}
		defaultLocale, localizations, localizationErr = normalizeCatalogLocalizations(defaultLocale, localizations)
		if localizationErr != nil {
			return errors.New("localized mod content is invalid")
		}
	}
	if err := requireCatalogCreateDefaultLocalization(defaultLocale, localizations); err != nil {
		return errors.New("the default language must have a localized mod name")
	}
	req.DefaultLocale, req.Localizations = defaultLocale, localizations
	for _, localization := range localizations {
		if localization.Locale == defaultLocale {
			req.SecondaryName = localization.Name
			req.Summary = localization.Summary
			req.BodyMarkdown = localization.ContentMarkdown
			break
		}
	}
	req.Environment = strings.TrimSpace(req.Environment)
	req.PrimaryCategory = strings.TrimSpace(req.PrimaryCategory)
	req.OfficialStatus = strings.TrimSpace(req.OfficialStatus)
	req.SourceStatus = strings.TrimSpace(req.SourceStatus)
	req.License = strings.TrimSpace(req.License)
	req.CurseForgeProjectID = strings.TrimSpace(req.CurseForgeProjectID)
	req.ModrinthProjectID = strings.TrimSpace(req.ModrinthProjectID)
	req.GitHubProjectPath = normalizeGitHubProjectPath(req.GitHubProjectPath)
	req.IconURL = strings.TrimSpace(req.IconURL)
	req.BodyMarkdown = strings.TrimSpace(req.BodyMarkdown)
	req.SubmissionMethod = strings.TrimSpace(req.SubmissionMethod)
	if req.SubmissionMethod == "" {
		req.SubmissionMethod = "manual"
	}

	if req.PrimaryName == "" {
		return errors.New("主要名称不能为空")
	}
	if req.SiteID != "" && !validModSiteID(req.SiteID) {
		return errors.New("模组站内 ID 只能包含小写字母、数字、下划线或连字符，且必须以字母或数字开头和结尾")
	}
	if len(req.PrimaryName) > 160 || len(req.SecondaryName) > 160 {
		return errors.New("模组名称不能超过 160 个字符")
	}
	if !validModAbbreviation(req.Abbreviation) {
		return errors.New("简写名称只能包含 ASCII 字母、数字或符号，且不能超过 32 个字符")
	}
	if len(req.Summary) > 500 {
		return errors.New("简介不能超过 500 个字符")
	}
	if len(req.CurseForgeProjectID) > 128 || len(req.ModrinthProjectID) > 128 || len(req.GitHubProjectPath) > 201 {
		return errors.New("项目标识不能超过 128 个字符")
	}
	if req.GitHubProjectPath != "" && !validGitHubProjectPath(req.GitHubProjectPath) {
		return errors.New("GitHub 项目地址必须使用 owner/repository 格式")
	}
	identifiers, err := normalizeModIdentifiers(req.ModIDs)
	if err != nil {
		return err
	}
	req.ModIDs = identifiers
	if len(req.GalleryImages) > 32 {
		return errors.New("a mod gallery can contain at most 32 images")
	}
	seenGalleryFiles := map[string]bool{}
	seenGalleryIDs := map[string]bool{}
	cleanGallery := make([]modGalleryImagePayload, 0, len(req.GalleryImages))
	for _, image := range req.GalleryImages {
		image.PublicID = strings.ToLower(strings.TrimSpace(image.PublicID))
		image.FileID = strings.ToLower(strings.TrimSpace(image.FileID))
		if !validCatalogPublicID(image.FileID) || seenGalleryFiles[image.FileID] || (image.PublicID != "" && (!validCatalogPublicID(image.PublicID) || seenGalleryIDs[image.PublicID])) {
			return errors.New("mod gallery contains an invalid or duplicate image")
		}
		seenGalleryFiles[image.FileID] = true
		if image.PublicID != "" {
			seenGalleryIDs[image.PublicID] = true
		}
		cleanGallery = append(cleanGallery, modGalleryImagePayload{PublicID: image.PublicID, FileID: image.FileID})
	}
	req.GalleryImages = cleanGallery
	if len(req.BodyMarkdown) > 2*1024*1024 {
		return errors.New("正文内容过长")
	}
	if !allowedModEnvironments[req.Environment] || !allowedModCategories[req.PrimaryCategory] || !allowedModStatuses[req.OfficialStatus] || !allowedModSourceStatuses[req.SourceStatus] || !allowedModLicenses[req.License] || !allowedModSubmissionMethods[req.SubmissionMethod] {
		return errors.New("模组分类或状态字段无效")
	}
	if req.IconURL != "" && !validHTTPURL(req.IconURL) {
		return errors.New("模组图标链接无效")
	}

	req.Tags = uniqueTrimmed(req.Tags, 40)
	for _, tag := range req.Tags {
		if !allowedModTags[tag] {
			return fmt.Errorf("不支持的模组标签：%s", tag)
		}
	}
	req.SearchKeywords = uniqueTrimmed(req.SearchKeywords, 40)
	for _, keyword := range req.SearchKeywords {
		if len(keyword) > 80 {
			return errors.New("单个搜索关键词不能超过 80 个字符")
		}
	}
	cleanCompatibilities := make([]modLoaderCompatibilityPayload, 0, len(req.Compatibilities))
	seenCompatibility := map[string]bool{}
	for _, compatibility := range req.Compatibilities {
		compatibility.Loader = strings.TrimSpace(compatibility.Loader)
		compatibility.Versions = uniqueTrimmed(compatibility.Versions, 500)
		if compatibility.Loader == "" || seenCompatibility[compatibility.Loader] {
			continue
		}
		if len(compatibility.Loader) > 80 {
			return errors.New("模组加载器名称不能超过 80 个字符")
		}
		for _, version := range compatibility.Versions {
			if len(version) > 80 {
				return errors.New("关系条件的 Minecraft 版本必须来自该模组支持的版本")
			}
		}
		seenCompatibility[compatibility.Loader] = true
		cleanCompatibilities = append(cleanCompatibilities, compatibility)
	}
	req.Compatibilities = cleanCompatibilities
	supportedVersions := make([]string, 0)
	for _, compatibility := range req.Compatibilities {
		supportedVersions = append(supportedVersions, compatibility.Versions...)
	}
	supportedVersionSet := stringSet(supportedVersions...)
	for index := range req.ModIDs {
		for _, version := range req.ModIDs[index].MinecraftVersions {
			if len(version) > 80 || (len(supportedVersionSet) > 0 && !supportedVersionSet[version]) {
				return errors.New("mod ID 的适用版本必须来自该模组支持的 Minecraft 版本")
			}
		}
	}
	if len(req.Links) > 64 || len(req.Authors) > 64 || len(req.RelationshipGroups) > 50 {
		return errors.New("关联资料数量过多")
	}

	cleanLinks := make([]modLinkPayload, 0, len(req.Links))
	seenLinks := map[string]bool{}
	for _, item := range req.Links {
		item.Type = strings.TrimSpace(item.Type)
		item.URL = strings.TrimSpace(item.URL)
		item.Note = strings.TrimSpace(item.Note)
		if item.Type == "" && item.URL == "" {
			continue
		}
		if !allowedModLinkTypes[item.Type] || !validHTTPURL(item.URL) || len(item.Note) > 240 {
			return errors.New("相关链接的类型或地址无效")
		}
		key := item.Type + "\x00" + item.URL
		if !seenLinks[key] {
			seenLinks[key] = true
			cleanLinks = append(cleanLinks, item)
		}
	}
	req.GitHubProjectPath, req.Links = synchronizeGitHubProjectLink(req.GitHubProjectPath, cleanLinks)

	cleanAuthors := make([]modAuthorPayload, 0, len(req.Authors))
	for _, item := range req.Authors {
		item.CreatorID = strings.ToLower(strings.TrimSpace(item.CreatorID))
		item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
		item.Name = strings.TrimSpace(item.Name)
		item.AvatarURL = strings.TrimSpace(item.AvatarURL)
		item.Role = strings.TrimSpace(item.Role)
		if item.CreatorID == "" && item.Name == "" {
			continue
		}
		if item.CreatorID != "" && len(item.CreatorID) != 9 {
			return errors.New("作者或团队 ID 无效")
		}
		if item.Kind != "" && item.Kind != "author" && item.Kind != "team" {
			return errors.New("作者资料类型无效")
		}
		if item.AvatarURL != "" && !validHTTPURL(item.AvatarURL) {
			return errors.New("author avatar URL must use HTTP or HTTPS")
		}
		if len(item.Name) > 160 || len(item.Role) > 80 {
			return errors.New("作者或团队信息过长")
		}
		cleanAuthors = append(cleanAuthors, item)
	}
	req.Authors = cleanAuthors

	cleanGroups := make([]modRelationshipGroupPayload, 0, len(req.RelationshipGroups))
	relationshipCount := 0
	supportedLoaders := make([]string, 0, len(req.Compatibilities))
	for _, compatibility := range req.Compatibilities {
		supportedLoaders = append(supportedLoaders, compatibility.Loader)
	}
	supportedLoaderSet := stringSet(supportedLoaders...)
	for _, group := range req.RelationshipGroups {
		group.Direction = strings.ToLower(strings.TrimSpace(group.Direction))
		if group.Direction == "" {
			group.Direction = "outgoing"
		}
		if group.Direction != "outgoing" && group.Direction != "incoming" {
			return errors.New("invalid mod relationship direction")
		}
		if group.Direction == "incoming" {
			continue
		}
		group.Label = strings.TrimSpace(group.Label)
		group.Loader = strings.TrimSpace(group.Loader)
		group.MinecraftVersions = uniqueTrimmed(group.MinecraftVersions, 100)
		group.ModVersion = strings.TrimSpace(group.ModVersion)
		if len(group.Label) > 160 || len(group.Loader) > 80 || len(group.ModVersion) > 160 {
			return errors.New("模组关系条件字段过长")
		}
		if group.Loader != "" && len(supportedLoaderSet) > 0 && !supportedLoaderSet[group.Loader] {
			return errors.New("关系条件的加载器必须来自该模组支持的加载器")
		}
		for _, version := range group.MinecraftVersions {
			if len(version) > 80 || (len(supportedVersionSet) > 0 && !supportedVersionSet[version]) {
				return errors.New("minecraft 版本名称不能超过 80 个字符")
			}
		}
		cleanRelationships := make([]modRelationshipPayload, 0, len(group.Relationships))
		for _, item := range group.Relationships {
			item.Type = strings.TrimSpace(item.Type)
			item.RelatedModPublicID = strings.ToLower(strings.TrimSpace(item.RelatedModPublicID))
			item.RelatedModName = strings.TrimSpace(item.RelatedModName)
			item.RelatedModIdentifier = strings.TrimSpace(item.RelatedModIdentifier)
			if item.Type == "" && item.RelatedModName == "" && item.RelatedModPublicID == "" && item.RelatedModIdentifier == "" {
				continue
			}
			if !allowedModRelationshipTypes[item.Type] ||
				(item.RelatedModName == "" && item.RelatedModPublicID == "" && item.RelatedModIdentifier == "") {
				return errors.New("模组关系缺少有效的关系类型或关联模组")
			}
			if item.RelatedModIdentifier != "" && !validModIdentifier(item.RelatedModIdentifier) {
				return errors.New("uncollected related Mod ID is invalid")
			}
			if len(item.RelatedModName) > 160 {
				return errors.New("模组关系字段过长")
			}
			cleanRelationships = append(cleanRelationships, item)
			relationshipCount++
		}
		if relationshipCount > 200 {
			return errors.New("模组关系数量过多")
		}
		if len(cleanRelationships) == 0 {
			continue
		}
		group.Relationships = cleanRelationships
		cleanGroups = append(cleanGroups, group)
	}
	req.RelationshipGroups = cleanGroups
	return nil
}

func normalizeModIdentifiers(values []modIdentifierPayload) ([]modIdentifierPayload, error) {
	if len(values) > 32 {
		return nil, errors.New("a mod can have at most 32 Mod IDs")
	}
	result := make([]modIdentifierPayload, 0, len(values))
	seen := map[string]bool{}
	primaryCount := 0
	for _, value := range values {
		value.Identifier = strings.TrimSpace(value.Identifier)
		value.MinecraftVersionMin = strings.TrimSpace(value.MinecraftVersionMin)
		value.MinecraftVersionMax = strings.TrimSpace(value.MinecraftVersionMax)
		value.MinecraftVersions = uniqueTrimmed(value.MinecraftVersions, 500)
		if value.Identifier == "" {
			continue
		}
		if !validModIdentifier(value.Identifier) {
			return nil, errors.New("mod ID may only contain ASCII letters, numbers, underscores, dots and hyphens")
		}
		key := strings.ToLower(value.Identifier)
		if seen[key] {
			return nil, errors.New("duplicate Mod ID")
		}
		seen[key] = true
		if value.Primary {
			primaryCount++
		}
		result = append(result, value)
	}
	if len(result) > 0 && primaryCount == 0 {
		result[0].Primary = true
		primaryCount = 1
	}
	if primaryCount > 1 {
		return nil, errors.New("only one Mod ID can be primary")
	}
	return result, nil
}

func validModIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || (index > 0 && (character == '_' || character == '.' || character == '-')) {
			continue
		}
		return false
	}
	return true
}

func normalizeGitHubProjectPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && strings.EqualFold(parsed.Hostname(), "github.com") {
		value = parsed.Path
	}
	value = strings.Trim(strings.TrimSpace(value), "/")
	value = strings.TrimSuffix(value, ".git")
	parts := strings.Split(value, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return value
}

func validGitHubProjectPath(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(parts[0]) > 100 || len(parts[1]) > 100 {
		return false
	}
	for partIndex, part := range parts {
		for index, character := range part {
			valid := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-'
			if partIndex == 1 {
				valid = valid || character == '_' || character == '.'
			}
			if !valid || (partIndex == 0 && (index == 0 || index == len(part)-1) && character == '-') {
				return false
			}
		}
	}
	return true
}

func synchronizeGitHubProjectLink(projectPath string, links []modLinkPayload) (string, []modLinkPayload) {
	if projectPath == "" {
		for _, link := range links {
			if link.Type == "github" {
				candidate := normalizeGitHubProjectPath(link.URL)
				if validGitHubProjectPath(candidate) {
					projectPath = candidate
					break
				}
			}
		}
	}
	if projectPath == "" {
		return "", links
	}
	canonicalURL := "https://github.com/" + projectPath
	for index := range links {
		if links[index].Type == "github" {
			links[index].URL = canonicalURL
			return projectPath, links
		}
	}
	return projectPath, append(links, modLinkPayload{Type: "github", URL: canonicalURL})
}

func insertModIdentifiersTx(ctx context.Context, tx pgx.Tx, modID int64, values []modIdentifierPayload) error {
	for index, item := range values {
		if _, err := tx.Exec(ctx, `insert into mod_identifiers(
			mod_id,identifier,is_primary,minecraft_version_min,minecraft_version_max,minecraft_versions,display_order
		) values($1,$2,$3,$4,$5,$6,$7)`, modID, item.Identifier, item.Primary,
			item.MinecraftVersionMin, item.MinecraftVersionMax, item.MinecraftVersions, index); err != nil {
			return err
		}
	}
	return resolvePendingModReferencesTx(ctx, tx, modID)
}

func replaceModIdentifiersTx(ctx context.Context, tx pgx.Tx, modID int64, values []modIdentifierPayload) error {
	if _, err := tx.Exec(ctx, `delete from mod_identifiers where mod_id=$1`, modID); err != nil {
		return err
	}
	return insertModIdentifiersTx(ctx, tx, modID, values)
}

func validateModGalleryFilesTx(ctx context.Context, tx pgx.Tx, modID, actorID int64, images []modGalleryImagePayload) error {
	seenFileIDs := make(map[string]struct{}, len(images))
	for _, image := range images {
		if _, duplicate := seenFileIDs[image.FileID]; duplicate {
			return errors.New("mod gallery cannot contain the same uploaded file more than once")
		}
		seenFileIDs[image.FileID] = struct{}{}
		allowExistingBinding := false
		if image.PublicID != "" {
			var existingModID int64
			var existingFileID string
			err := tx.QueryRow(ctx, `select gallery.mod_id,file.public_id from mod_gallery_images gallery join oss_files file on file.id=gallery.oss_file_id where gallery.public_id=$1`, image.PublicID).
				Scan(&existingModID, &existingFileID)
			if err == nil && existingModID == modID && existingFileID == image.FileID {
				allowExistingBinding = true
			} else if err == nil || !errors.Is(err, pgx.ErrNoRows) {
				return errors.New("mod gallery contains an invalid image reference")
			}
		}
		file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, image.FileID, ossRasterBindingScope{
			UploaderID: actorID, AllowAnyUploader: allowExistingBinding,
		})
		if err != nil {
			return errors.New("a gallery image does not exist, is not an image, or is not owned by the editor")
		}
		var boundToAnotherMod bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from mod_gallery_images where oss_file_id=$1 and mod_id<>$2)`,
			file.ID, modID).Scan(&boundToAnotherMod); err != nil {
			return err
		}
		if boundToAnotherMod {
			return errors.New("a gallery image does not exist, is not an image, or is not owned by the editor")
		}
	}
	return nil
}

func replaceModGalleryImagesTx(ctx context.Context, tx pgx.Tx, modID, revisionID, actorID int64, images []modGalleryImagePayload) error {
	if _, err := tx.Exec(ctx, `delete from mod_gallery_images where mod_id=$1`, modID); err != nil {
		return err
	}
	for index, image := range images {
		if image.PublicID == "" {
			if _, err := tx.Exec(ctx, `insert into mod_gallery_images(mod_id,oss_file_id,display_order,created_by,published_revision_id)
				select $1,file.id,$3,$4,$5 from oss_files file where file.public_id=$2`,
				modID, image.FileID, index, nullableActorID(actorID), revisionID); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx, `insert into mod_gallery_images(public_id,mod_id,oss_file_id,display_order,created_by,published_revision_id)
			select $1,$2,file.id,$4,$5,$6 from oss_files file where file.public_id=$3`,
			image.PublicID, modID, image.FileID, index, nullableActorID(actorID), revisionID); err != nil {
			return err
		}
	}
	return nil
}

func insertModRelationshipGroups(ctx context.Context, tx pgx.Tx, modID int64, groups []modRelationshipGroupPayload) error {
	for groupIndex, group := range groups {
		if group.Direction == "incoming" {
			continue
		}

		var groupID int64
		if err := tx.QueryRow(ctx, `insert into mod_relationship_groups(
			mod_id,label,loader,minecraft_versions,mod_version,display_order
		) values($1,$2,$3,$4,$5,$6) returning id`,
			modID, group.Label, group.Loader, group.MinecraftVersions, group.ModVersion, groupIndex).Scan(&groupID); err != nil {
			return err
		}
		for relationshipIndex, relationship := range group.Relationships {
			var relatedModID *int64
			if relationship.RelatedModPublicID != "" {
				var resolvedID int64
				if err := tx.QueryRow(ctx, `select id,primary_name from mods
					where project_code=$1 and id<>$2 and review_status='approved'`,
					relationship.RelatedModPublicID, modID).Scan(&resolvedID, &relationship.RelatedModName); err != nil {
					return errors.New("selected related mod does not exist")
				}
				relatedModID = &resolvedID
			} else if relationship.RelatedModIdentifier != "" {
				var resolvedID int64
				err := tx.QueryRow(ctx, `select mod.id,mod.primary_name
					from mod_identifiers identifier join mods mod on mod.id=identifier.mod_id
					where lower(identifier.identifier)=lower($1) and mod.id<>$2 and mod.review_status='approved'
					order by identifier.is_primary desc,mod.id limit 1`,
					relationship.RelatedModIdentifier, modID).Scan(&resolvedID, &relationship.RelatedModName)
				if err == nil {
					relatedModID = &resolvedID
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
			}
			if relatedModID == nil && relationship.RelatedModIdentifier == "" {
				return errors.New("a related mod or an uncollected Mod ID is required")
			}
			relationshipID, err := insertModRelationship(ctx, tx, modID, groupID, relationship.Type,
				relatedModID, relationship.RelatedModName, relationship.RelatedModIdentifier, relationshipIndex)
			if err != nil {
				return err
			}
			if relatedModID == nil {
				if _, err := tx.Exec(ctx, `insert into unresolved_references(
					source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata
				) values('mod_relationship',$1,'relatedMod','mod',$2,lower($2),jsonb_build_object('sourceModId',$3))
				on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
				set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
					resolved_at=null,metadata=excluded.metadata,updated_at=now()`,
					relationshipID, relationship.RelatedModIdentifier, modID); err != nil {
					return err
				}
			}
		}
	}
	if _, err := tx.Exec(ctx, `delete from mod_relationship_groups relationship_group
		where relationship_group.mod_id=$1
		  and not exists(select 1 from mod_relationships relationship where relationship.group_id=relationship_group.id)`, modID); err != nil {
		return err
	}
	return nil
}

func insertModRelationship(
	ctx context.Context,
	tx pgx.Tx,
	sourceModID int64,
	groupID int64,
	relationshipType string,
	targetModID *int64,
	targetModName string,
	targetModIdentifier string,
	displayOrder int,
) (int64, error) {
	var relationshipID int64
	err := tx.QueryRow(ctx, `insert into mod_relationships(
		mod_id,group_id,relation_type,related_mod_id,related_mod_name,related_mod_identifier,display_order
	) values($1,$2,$3,$4,$5,$6,$7) returning id`,
		sourceModID, groupID, relationshipType, targetModID, targetModName,
		targetModIdentifier, displayOrder).Scan(&relationshipID)
	return relationshipID, err
}

func resolvePendingModReferencesTx(ctx context.Context, tx pgx.Tx, modID int64) error {
	var name, reviewStatus string
	if err := tx.QueryRow(ctx, `select primary_name,review_status from mods where id=$1`, modID).Scan(&name, &reviewStatus); err != nil {
		return err
	}
	if reviewStatus != "approved" {
		return nil
	}
	rows, err := tx.Query(ctx, `select unresolved.id,unresolved.source_id
		from unresolved_references unresolved
		join mod_relationships relationship
		  on unresolved.source_type='mod_relationship' and unresolved.source_id=relationship.id
		where unresolved.status='pending' and unresolved.reference_type='mod'
		  and relationship.mod_id<>$1
		  and exists(select 1 from mod_identifiers identifier
			where identifier.mod_id=$1 and lower(identifier.identifier)=unresolved.normalized_identifier)
		order by unresolved.id for update of unresolved`, modID)
	if err != nil {
		return err
	}
	type pendingReference struct {
		id             int64
		relationshipID int64
	}
	pending := make([]pendingReference, 0)
	for rows.Next() {
		var item pendingReference
		if err = rows.Scan(&item.id, &item.relationshipID); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range pending {
		if _, err = tx.Exec(ctx, `update mod_relationships
			set related_mod_id=$2,related_mod_name=$3 where id=$1 and related_mod_id is null`,
			item.relationshipID, modID, name); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update unresolved_references
			set status='resolved',resolved_type='mod',resolved_id=$2,resolved_at=now(),updated_at=now()
			where id=$1 and status='pending'`, item.id, modID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `update minecraft_server_mods server_mod
		set mod_id=$1
		from unresolved_references unresolved
		where unresolved.source_type='minecraft_server_mod'
		  and unresolved.source_id=server_mod.id
		  and unresolved.status='pending'
		  and unresolved.reference_type='mod'
		  and server_mod.mod_id is null
		  and exists(select 1 from mod_identifiers identifier
			where identifier.mod_id=$1
			  and lower(identifier.identifier)=unresolved.normalized_identifier)`, modID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update unresolved_references unresolved
		set status='resolved',resolved_type='mod',resolved_id=$1,resolved_at=now(),updated_at=now()
		where unresolved.source_type='minecraft_server_mod'
		  and unresolved.status='pending'
		  and unresolved.reference_type='mod'
		  and exists(select 1 from minecraft_server_mods server_mod
			join mod_identifiers identifier on identifier.mod_id=$1
			where server_mod.id=unresolved.source_id and server_mod.mod_id=$1
			  and lower(identifier.identifier)=unresolved.normalized_identifier)`, modID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update community_post_project_refs reference
		set target_id=$1,raw_identifier=''
		from unresolved_references unresolved
		where unresolved.source_type='community_post_project' and unresolved.source_id=reference.id
		  and unresolved.status='pending' and unresolved.reference_type='mod' and reference.target_id is null
		  and exists(select 1 from mod_identifiers identifier where identifier.mod_id=$1
		    and lower(identifier.identifier)=unresolved.normalized_identifier)`, modID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update unresolved_references unresolved
		set status='resolved',resolved_type='mod',resolved_id=$1,resolved_at=now(),updated_at=now()
		where unresolved.source_type='community_post_project' and unresolved.status='pending'
		  and unresolved.reference_type='mod'
		  and exists(select 1 from community_post_project_refs reference
		    join mod_identifiers identifier on identifier.mod_id=$1
		    where reference.id=unresolved.source_id and reference.target_id=$1
			  and lower(identifier.identifier)=unresolved.normalized_identifier)`, modID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update modpack_mods entry set mod_id=$1
		from unresolved_references unresolved,mods candidate
		where candidate.id=$1 and unresolved.source_type='modpack_mod' and unresolved.source_id=entry.id
		  and unresolved.status='pending' and unresolved.reference_type='mod' and entry.mod_id is null
		  and (entry.provider='modrinth' and entry.provider_project_id<>'' and candidate.modrinth_project_id=entry.provider_project_id
		    or entry.provider='curseforge' and entry.provider_project_id<>'' and candidate.curseforge_project_id=entry.provider_project_id
		    or entry.identifier<>'' and exists(select 1 from mod_identifiers identifier
		      where identifier.mod_id=$1 and lower(identifier.identifier)=lower(entry.identifier)))`, modID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update unresolved_references unresolved
		set status='resolved',resolved_type='mod',resolved_id=$1,resolved_at=now(),updated_at=now()
		where unresolved.source_type='modpack_mod' and unresolved.status='pending'
		  and unresolved.reference_type='mod' and exists(select 1 from modpack_mods entry
		    where entry.id=unresolved.source_id and entry.mod_id=$1)`, modID); err != nil {
		return err
	}
	return nil
}

func insertModCompatibilities(ctx context.Context, tx pgx.Tx, modID int64, compatibilities []modLoaderCompatibilityPayload) error {
	for _, compatibility := range compatibilities {
		for _, version := range compatibility.Versions {
			if _, err := tx.Exec(ctx, `insert into mod_loader_compatibilities (mod_id, loader, minecraft_version) values ($1,$2,$3) on conflict do nothing`, modID, compatibility.Loader, version); err != nil {
				return err
			}
		}
	}
	return nil
}

func validModAbbreviation(value string) bool {
	if len(value) > 32 {
		return false
	}
	for _, character := range value {
		if character < 33 || character > 126 {
			return false
		}
	}
	return true
}

func validHTTPURL(value string) bool {
	if len(value) > 2048 {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func uniqueTrimmed(values []string, maximum int) []string {
	result := make([]string, 0, min(len(values), maximum))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || len(result) >= maximum {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func resolveProjectAuthorForCreateTx(ctx context.Context, tx pgx.Tx, author modAuthorPayload, actorID int64, r *http.Request) (int64, string, string, error) {
	var creatorID int64
	var creatorName string
	if author.CreatorID != "" {
		err := tx.QueryRow(ctx, `select id,name from creators where public_id=$1`, author.CreatorID).Scan(&creatorID, &creatorName)
		if err != nil {
			return 0, "", "", errors.New("selected author or team does not exist")
		}
	} else {
		kind := author.Kind
		if kind == "" {
			kind = "author"
		}
		var err error
		creatorID, _, _, err = ensureNamedCreatorSnapshotTx(ctx, tx, creatorSnapshot{
			Kind: kind, Name: author.Name, AvatarURL: author.AvatarURL,
			AvatarFileID: author.AvatarFileID, AvatarInternalID: author.AvatarInternalID,
		}, actorID, "pending", r)
		if err != nil {
			return 0, "", "", err
		}
		creatorName, err = creatorNameTx(ctx, tx, creatorID)
		if err != nil {
			return 0, "", "", err
		}
	}
	roleName, err := creatorRoleNameTx(ctx, tx, author.RoleID)
	if err != nil {
		return 0, "", "", errors.New("selected creator role does not exist")
	}
	if roleName == "" {
		roleName = author.Role
	}
	return creatorID, creatorName, roleName, nil
}

type databaseQuery interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func availableModUniqueID(ctx context.Context, query databaseQuery) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		code, err := newModUniqueID()
		if err != nil {
			return "", err
		}
		var exists bool
		if err := query.QueryRow(ctx, `select exists(select 1 from public_routes where public_id = $1)`, code).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", errors.New("unable to allocate mod unique ID")
}

func newModUniqueID() (string, error) {
	const letters = "abcdefghjkmnpqrstuvwxyz"
	const digits = "23456789"
	const alphabet = letters + digits
	candidate := make([]byte, 9)
	for index, characters := range []string{letters, digits, alphabet, alphabet, alphabet, alphabet, alphabet, alphabet, alphabet} {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(characters))))
		if err != nil {
			return "", err
		}
		candidate[index] = characters[value.Int64()]
	}
	return string(candidate), nil
}

func availableModSiteID(ctx context.Context, query databaseQuery, name string) (string, error) {
	base := modSiteIDBase(name)
	for suffix := 0; suffix < 1000; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s_%d", base, suffix+1)
		}
		if err := ensureModSiteIDAvailable(ctx, query, candidate, 0); err == nil {
			return candidate, nil
		} else if !errors.Is(err, errModSiteIDTaken) {
			return "", err
		}
	}
	return "", errors.New("unable to allocate mod site ID")
}

func ensureModSiteIDAvailable(ctx context.Context, query databaseQuery, siteID string, excludeModID int64) error {
	var exists bool
	if err := query.QueryRow(ctx, `select exists(select 1 from mods where slug = $1 and id <> $2)`, siteID, excludeModID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errModSiteIDTaken
	}
	return nil
}

func normalizeModSiteID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validModSiteID(value string) bool {
	if len(value) == 0 || len(value) > 100 || !isASCIIAlphaNumeric(value[0]) || !isASCIIAlphaNumeric(value[len(value)-1]) {
		return false
	}
	for index := 1; index < len(value)-1; index++ {
		character := value[index]
		if !isASCIIAlphaNumeric(character) && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
}

func modSiteIDBase(value string) string {
	var builder strings.Builder
	previousSeparator := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		isASCIIAlphanumeric := (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
		if isASCIIAlphanumeric {
			builder.WriteRune(character)
			previousSeparator = false
			continue
		}
		if !previousSeparator && builder.Len() > 0 {
			builder.WriteByte('_')
			previousSeparator = true
		}
	}
	siteID := strings.Trim(builder.String(), "_")
	if siteID == "" {
		return "mod"
	}
	if len(siteID) > 100 {
		siteID = strings.Trim(siteID[:100], "_")
	}
	return siteID
}
