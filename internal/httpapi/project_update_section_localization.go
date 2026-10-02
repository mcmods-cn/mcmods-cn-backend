package httpapi

import (
	"strings"
	"sync/atomic"
)

type projectUpdateSectionTranslation struct {
	zhCN string
	zhTW string
	enUS string
}

type projectUpdateNotificationMetrics struct {
	UnknownSectionFallbacks uint64 `json:"unknownSectionFallbacks"`
}

type projectUpdateNotificationCounters struct {
	unknownSectionFallbacks atomic.Uint64
}

var projectUpdateNotificationObservability projectUpdateNotificationCounters

var projectUpdateSectionAliases = map[string]string{
	"published_content":   "project_details",
	"description":         "description",
	"bodyMarkdown":        "description",
	"summary":             "summary",
	"download_files":      "downloads",
	"downloads":           "downloads",
	"minecraft_versions":  "minecraft_versions",
	"minecraftVersions":   "minecraft_versions",
	"project_version":     "project_version",
	"projectVersion":      "project_version",
	"changelog":           "changelog",
	"eventAt":             "release_date",
	"primaryName":         "project_name",
	"secondaryName":       "project_name",
	"abbreviation":        "project_name",
	"siteId":              "project_identity",
	"publicId":            "project_identity",
	"defaultLocale":       "localizations",
	"localizations":       "localizations",
	"modIds":              "identifiers",
	"environment":         "environment",
	"primaryCategory":     "categories",
	"categories":          "categories",
	"tags":                "categories",
	"compatibilities":     "compatibility",
	"loaders":             "compatibility",
	"searchKeywords":      "search_keywords",
	"authors":             "authors",
	"officialStatus":      "project_status",
	"sourceStatus":        "project_status",
	"license":             "license",
	"curseforgeProjectId": "external_sources",
	"modrinthProjectId":   "external_sources",
	"githubProjectPath":   "external_sources",
	"iconUrl":             "icon",
	"submissionMethod":    "submission",
	"links":               "links",
	"relationshipGroups":  "relationships",
	"parentProjects":      "relationships",
	"mods":                "relationships",
	"galleryImages":       "gallery",
	"packType":            "pack_configuration",
	"packagingMethod":     "pack_configuration",
	"features":            "features",
	"performance":         "performance",
	"resolution":          "resolution",
	"mapSize":             "map_size",
	"projectType":         "project_type",
	"categoryId":          "changelog_category",
	"newCategory":         "changelog_category",
	"kind":                "project_content",
	"operation":           "project_content",
	"modId":               "project_content",
	"modSiteId":           "project_content",
	"createdIdentity":     "project_content",
	"version":             "content_version",
	"template":            "content_template",
	"section":             "content_section",
	"layout":              "content_layout",
	"resource":            "content_resource",
}

var projectUpdateSectionTranslations = map[string]projectUpdateSectionTranslation{
	"project_details":    {zhCN: "项目资料", zhTW: "專案資料", enUS: "project details"},
	"description":        {zhCN: "详情介绍", zhTW: "詳細介紹", enUS: "description"},
	"summary":            {zhCN: "项目简介", zhTW: "專案簡介", enUS: "summary"},
	"downloads":          {zhCN: "下载文件", zhTW: "下載檔案", enUS: "download files"},
	"minecraft_versions": {zhCN: "Minecraft 版本", zhTW: "Minecraft 版本", enUS: "Minecraft versions"},
	"project_version":    {zhCN: "项目版本", zhTW: "專案版本", enUS: "project version"},
	"changelog":          {zhCN: "更新日志", zhTW: "更新日誌", enUS: "changelog"},
	"release_date":       {zhCN: "发布日期", zhTW: "發佈日期", enUS: "release date"},
	"project_name":       {zhCN: "项目名称", zhTW: "專案名稱", enUS: "project name"},
	"project_identity":   {zhCN: "项目标识", zhTW: "專案識別碼", enUS: "project identity"},
	"localizations":      {zhCN: "本地化内容", zhTW: "本地化內容", enUS: "translations"},
	"identifiers":        {zhCN: "模组标识", zhTW: "模組識別碼", enUS: "mod identifiers"},
	"environment":        {zhCN: "运行环境", zhTW: "執行環境", enUS: "environment"},
	"categories":         {zhCN: "分类与标签", zhTW: "分類與標籤", enUS: "categories and tags"},
	"compatibility":      {zhCN: "兼容版本", zhTW: "相容版本", enUS: "compatibility"},
	"search_keywords":    {zhCN: "搜索关键词", zhTW: "搜尋關鍵字", enUS: "search keywords"},
	"authors":            {zhCN: "作者信息", zhTW: "作者資訊", enUS: "authors"},
	"project_status":     {zhCN: "项目状态", zhTW: "專案狀態", enUS: "project status"},
	"license":            {zhCN: "许可证", zhTW: "授權條款", enUS: "license"},
	"external_sources":   {zhCN: "外部来源", zhTW: "外部來源", enUS: "external sources"},
	"icon":               {zhCN: "项目图标", zhTW: "專案圖示", enUS: "project icon"},
	"submission":         {zhCN: "提交信息", zhTW: "提交資訊", enUS: "submission information"},
	"links":              {zhCN: "相关链接", zhTW: "相關連結", enUS: "links"},
	"relationships":      {zhCN: "关联项目", zhTW: "關聯專案", enUS: "related projects"},
	"gallery":            {zhCN: "项目图库", zhTW: "專案圖庫", enUS: "gallery"},
	"pack_configuration": {zhCN: "整合包配置", zhTW: "整合包設定", enUS: "modpack configuration"},
	"features":           {zhCN: "项目特性", zhTW: "專案功能", enUS: "features"},
	"performance":        {zhCN: "性能信息", zhTW: "效能資訊", enUS: "performance"},
	"resolution":         {zhCN: "分辨率", zhTW: "解析度", enUS: "resolution"},
	"map_size":           {zhCN: "地图大小", zhTW: "地圖大小", enUS: "map size"},
	"project_type":       {zhCN: "项目类型", zhTW: "專案類型", enUS: "project type"},
	"changelog_category": {zhCN: "日志分类", zhTW: "日誌分類", enUS: "changelog category"},
	"project_content":    {zhCN: "项目内容", zhTW: "專案內容", enUS: "project content"},
	"content_version":    {zhCN: "内容版本", zhTW: "內容版本", enUS: "content version"},
	"content_template":   {zhCN: "内容模板", zhTW: "內容範本", enUS: "content template"},
	"content_section":    {zhCN: "内容章节", zhTW: "內容章節", enUS: "content section"},
	"content_layout":     {zhCN: "内容布局", zhTW: "內容版面", enUS: "content layout"},
	"content_resource":   {zhCN: "内容资源", zhTW: "內容資源", enUS: "content resource"},
}

func localizedProjectUpdateSectionText(locale string, sections []string) string {
	locale = normalizeContentLocale(locale)
	separator := ", "
	if locale == "zh-CN" || locale == "zh-TW" {
		separator = "、"
	}
	fallback := projectUpdateSectionTranslations["project_details"]
	labels := make([]string, 0, len(sections))
	seen := make(map[string]struct{}, len(sections))
	for _, section := range uniqueProjectUpdateSections(sections) {
		translation := fallback
		if alias, known := projectUpdateSectionAliases[section]; known {
			translation = projectUpdateSectionTranslations[alias]
		}
		label := translation.enUS
		switch locale {
		case "zh-CN":
			label = translation.zhCN
		case "zh-TW":
			label = translation.zhTW
		}
		if _, duplicate := seen[label]; duplicate {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	return strings.Join(labels, separator)
}

func unknownProjectUpdateSections(sections []string) []string {
	unknown := make([]string, 0)
	for _, section := range uniqueProjectUpdateSections(sections) {
		if _, known := projectUpdateSectionAliases[section]; !known {
			unknown = append(unknown, section)
		}
	}
	return unknown
}

func (c *projectUpdateNotificationCounters) snapshot() projectUpdateNotificationMetrics {
	if c == nil {
		return projectUpdateNotificationMetrics{}
	}
	return projectUpdateNotificationMetrics{UnknownSectionFallbacks: c.unknownSectionFallbacks.Load()}
}
