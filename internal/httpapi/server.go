package httpapi

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/antiabuse"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/mailer"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/searchindex"
)

type Server struct {
	cfg            config.Config
	db             *pgxpool.Pool
	mailer         mailer.Mailer
	queue          *queue.Client
	cache          *querycache.Cache
	activity       *activity.Monitor
	antiAbuse      *antiabuse.Service
	search         *searchindex.Client
	mux            *http.ServeMux
	ygg            *yggdrasilService
	yggMu          sync.RWMutex
	trustedProxies []*net.IPNet
	realtime       *realtimeHub
	liveRequests   atomic.Uint64
	readyRequests  atomic.Uint64
	databasePings  atomic.Uint64
}

const corsAllowedHeaders = "Authorization, Content-Type, Idempotency-Key, X-Request-ID, X-Client-ID, X-Anti-Abuse-Form, X-Anti-Abuse-Trap, X-Anti-Abuse-Challenge, X-MCMods-Bot-Token"
const corsExposedHeaders = "Retry-After, X-MCMods-API-Response"

func NewServer(cfg config.Config, db *pgxpool.Pool, queueClient *queue.Client, sharedCache *querycache.Cache, activityMonitor *activity.Monitor, searchClient *searchindex.Client) http.Handler {
	if sharedCache == nil {
		sharedCache = querycache.New(cfg.Redis)
	}
	server := &Server{
		cfg:            cfg,
		db:             db,
		mailer:         mailer.New(cfg.SMTP),
		queue:          queueClient,
		cache:          sharedCache,
		activity:       activityMonitor,
		search:         searchClient,
		mux:            http.NewServeMux(),
		trustedProxies: parseTrustedProxyNetworks(cfg.TrustedProxyCIDRs),
		realtime:       newRealtimeHub(),
	}
	server.antiAbuse = antiabuse.New(cfg.AntiAbuse, db, server.cache)
	yggdrasilConfig, err := server.loadYggdrasilConfig(context.Background())
	if err != nil {
		log.Printf("load Yggdrasil settings: %v; using environment configuration", err)
	} else {
		server.cfg.Yggdrasil = yggdrasilConfig
	}
	server.ygg = newYggdrasilService(server.cfg)
	server.routes()
	server.subscribeRealtimeBroadcast()
	return server.securityHeaders(server.cors(server.cookieRequestOrigin(server.yggdrasilALI(server.compression(server.logAccess(server.botTraffic(server.mux)))))))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /live", s.live)
	s.mux.HandleFunc("GET /ready", s.ready)
	s.mux.HandleFunc("GET /api/v1/reports/reasons", reportReasons)
	s.mux.HandleFunc("POST /api/v1/reports", s.requirePermission("report.create", s.createUnifiedReport))
	s.mux.HandleFunc("POST /api/v1/reports/evidence/uploads", s.requirePermission("report.create", s.createReportEvidenceUpload))
	s.mux.HandleFunc("POST /api/v1/reports/evidence/uploads/complete", s.requirePermission("report.create", s.completeReportEvidenceUpload))
	s.mux.HandleFunc("POST /api/v1/reports/evidence/{id}/access", s.requireAuth(s.reportEvidenceAccess))
	s.mux.HandleFunc("GET /api/v1/reports/me", s.requirePermission("report.view_own", s.ownReports))
	s.mux.HandleFunc("GET /api/v1/site-affairs/blackroom", s.publicBlackroom)
	s.mux.HandleFunc("GET /api/v1/site-affairs/blackroom/{id}", s.blackroomDetail)
	s.mux.HandleFunc("POST /api/v1/log-shares/paste", s.optionalAuth(s.createPasteLogShare))
	s.mux.HandleFunc("POST /api/v1/log-shares/files", s.requireAuth(s.createFileLogShares))
	s.mux.HandleFunc("GET /api/v1/log-shares/me", s.requireAuth(s.myLogShares))
	s.mux.HandleFunc("DELETE /api/v1/log-shares/{code}", s.requireAuth(s.deleteLogShare))
	s.mux.HandleFunc("GET /api/v1/log-shares/s/{code}", s.optionalAuth(s.publicLogShare))
	s.mux.HandleFunc("GET /api/v1/log-shares/s/{code}/download", s.optionalAuth(s.downloadLogShare))
	s.yggdrasilRoutes()
	s.mux.HandleFunc("POST /api/v1/auth/register", s.register)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.HandleFunc("POST /api/v1/auth/email-code", s.requestEmailCode)
	s.mux.HandleFunc("POST /api/v1/auth/email-login", s.emailLogin)
	s.mux.HandleFunc("GET /api/v1/auth/oauth/{provider}/start", s.oauthStart)
	s.mux.HandleFunc("GET /api/v1/auth/oauth/{provider}/callback", s.oauthCallback)
	s.mux.HandleFunc("GET /api/v1/auth/permissions/evaluate", s.requireAuth(s.evaluatePermission))
	s.mux.HandleFunc("GET /api/v1/permissions/compare/options", s.requireAuth(s.permissionComparisonOptions))
	s.mux.HandleFunc("POST /api/v1/permissions/compare", s.requireAuth(s.comparePermissions))
	s.mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.me))
	s.mux.HandleFunc("GET /api/v1/realtime/events", s.requireAuth(s.realtimeEvents))
	s.mux.HandleFunc("GET /api/v1/anti-abuse/form-token", s.requireAuth(s.antiAbuseFormToken))
	s.mux.HandleFunc("GET /api/v1/review-locks/{entityType}/{publicId}", s.requireAuth(s.reviewLock))
	s.mux.HandleFunc("POST /api/v1/review-locks/{entityType}/{publicId}/subscribe", s.requireAuth(s.subscribeReviewCompletion))
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.logout))
	s.mux.HandleFunc("GET /api/v1/location", s.visitorLocation)
	s.mux.HandleFunc("GET /api/v1/markdown/config", s.markdownConfig)
	s.mux.HandleFunc("GET /api/v1/site/config", s.publicSiteGeneralConfig)
	s.mux.HandleFunc("GET /api/v1/site-affairs/about", s.publicAboutPage)
	s.mux.HandleFunc("GET /api/v1/site-affairs/changelogs", s.publicSiteChangelogs)
	s.mux.HandleFunc("GET /api/v1/site-affairs/changelogs/{id}", s.publicSiteChangelogDetail)
	s.mux.HandleFunc("POST /api/v1/site/presence", s.optionalAuth(s.touchSitePresence))
	s.mux.HandleFunc("GET /api/v1/ratings/{targetType}/{publicId}", s.optionalAuth(s.ratingSummary))
	s.mux.HandleFunc("PUT /api/v1/ratings/{targetType}/{publicId}", s.requirePermission("rating.create", s.ratingItem))
	s.mux.HandleFunc("DELETE /api/v1/ratings/{targetType}/{publicId}", s.requirePermission("rating.create", s.ratingItem))
	s.mux.HandleFunc("GET /api/v1/ratings/{targetType}/{publicId}/reviews", s.requirePermission("rating.read", s.ratingReviews))
	s.mux.HandleFunc("GET /api/v1/content-metrics/{publicId}", s.optionalAuth(s.contentMetrics))
	s.mux.HandleFunc("POST /api/v1/content-metrics/{publicId}/view", s.optionalAuth(s.recordMetricView))
	s.mux.HandleFunc("GET /api/v1/oss/files/{publicId}/content", s.publicInlineOSSFile)
	s.mux.HandleFunc("GET /api/v1/minecraft/versions", s.publicMinecraftVersions)
	s.mux.HandleFunc("GET /api/v1/servers/settings", s.optionalAuth(s.publicServerSettings))
	s.mux.HandleFunc("GET /api/v1/servers", s.optionalAuth(s.publicMinecraftServers))
	s.mux.HandleFunc("POST /api/v1/servers/probe", s.requirePermission("server.create", s.probeMinecraftServer))
	s.mux.HandleFunc("POST /api/v1/servers", s.requirePermission("server.create", s.createMinecraftServer))
	s.mux.HandleFunc("GET /api/v1/servers/{serverId}", s.optionalAuth(s.publicMinecraftServerDetail))
	s.mux.HandleFunc("PATCH /api/v1/servers/{serverId}", s.requireAuth(s.updateMinecraftServer))
	s.mux.HandleFunc("GET /api/v1/servers/{serverId}/history", s.optionalAuth(s.minecraftServerHistory))
	s.mux.HandleFunc("GET /api/v1/mod-imports/providers", s.publicModImportProviders)
	s.mux.HandleFunc("GET /api/v1/mods", s.optionalAuth(s.publicMods))
	s.mux.HandleFunc("GET /api/v1/modpacks", s.optionalAuth(s.modpacks))
	s.mux.HandleFunc("GET /api/v1/content-projects/{projectType}", s.optionalAuth(s.simpleProjects))
	s.mux.HandleFunc("POST /api/v1/content-projects/{projectType}", s.requireAuth(s.simpleProjects))
	s.mux.HandleFunc("GET /api/v1/catalog/resource-presentation", s.catalogResourcePresentation)
	s.mux.HandleFunc("GET /api/v1/catalog/resources", s.requirePermission("global_resource.list", s.catalogResources))
	s.mux.HandleFunc("POST /api/v1/catalog/resources", s.requirePermission("content.write", s.createCatalogResource))
	s.mux.HandleFunc("GET /api/v1/catalog/resources/{publicId}", s.requirePermission("global_resource.view", s.catalogResourceDetail))
	s.mux.HandleFunc("GET /api/v1/admin/global-resources", s.requirePermission("global_resource.list", s.catalogResources))
	s.mux.HandleFunc("GET /api/v1/admin/global-resources/{publicId}", s.requirePermission("global_resource.view", s.catalogResourceDetail))
	s.mux.HandleFunc("GET /api/v1/admin/global-resources/{publicId}/bindings", s.requirePermission("global_resource.binding.view", s.adminGlobalResourceBindings))
	s.mux.HandleFunc("GET /api/v1/catalog/resources/{publicId}/{assetKind}", s.optionalAuth(s.catalogResourceAsset))
	s.mux.HandleFunc("GET /api/v1/content/{publicId}", s.optionalAuth(s.catalogEntityContent))
	s.mux.HandleFunc("PUT /api/v1/content/{publicId}", s.requirePermission("content.write", s.updateCatalogEntityContent))
	s.mux.HandleFunc("POST /api/v1/content/{publicId}/translations", s.requirePermission("content.translate", s.requestCatalogContentTranslation))
	s.mux.HandleFunc("GET /api/v1/content/translations/{taskId}", s.requirePermission("content.translate", s.catalogContentTranslationResult))
	s.mux.HandleFunc("GET /api/v1/users/me/content-languages", s.requireAuth(s.contentLanguageSettings))
	s.mux.HandleFunc("PUT /api/v1/users/me/content-languages", s.requireAuth(s.contentLanguageSettings))
	s.mux.HandleFunc("GET /api/v1/users/me/drafts", s.requireAuth(s.userDrafts))
	s.mux.HandleFunc("POST /api/v1/users/me/drafts", s.requireAuth(s.userDrafts))
	s.mux.HandleFunc("POST /api/v1/users/me/drafts/complete", s.requireAuth(s.completeUserDraft))
	s.mux.HandleFunc("GET /api/v1/users/me/drafts/{draftId}", s.requireAuth(s.userDraftItem))
	s.mux.HandleFunc("DELETE /api/v1/users/me/drafts/{draftId}", s.requireAuth(s.userDraftItem))
	s.mux.HandleFunc("GET /api/v1/tags", s.optionalAuth(s.catalogTags))
	s.mux.HandleFunc("POST /api/v1/tags", s.requirePermission("content.write", s.catalogTags))
	s.mux.HandleFunc("GET /api/v1/tags/{publicId}", s.optionalAuth(s.catalogTagDetail))
	s.mux.HandleFunc("PUT /api/v1/tags/{publicId}", s.requirePermission("content.write", s.catalogTagDetail))
	s.mux.HandleFunc("DELETE /api/v1/tags/{publicId}", s.requirePermission("content.write", s.catalogTagDetail))
	s.mux.HandleFunc("GET /api/v1/recipe-types", s.optionalAuth(s.globalRecipeTypes))
	s.mux.HandleFunc("POST /api/v1/recipe-types", s.requirePermission("content.write", s.createCatalogRecipeType))
	s.mux.HandleFunc("GET /api/v1/recipe-types/{publicId}", s.optionalAuth(s.catalogRecipeTypeDetail))
	s.mux.HandleFunc("GET /api/v1/recipe-types/{publicId}/catalog", s.optionalAuth(s.globalRecipeTypeCatalog))
	s.mux.HandleFunc("PUT /api/v1/recipe-types/{publicId}", s.requirePermission("content.write", s.catalogRecipeTypeDetail))
	s.mux.HandleFunc("DELETE /api/v1/recipe-types/{publicId}", s.requirePermission("content.write", s.catalogRecipeTypeDetail))
	s.mux.HandleFunc("GET /api/v1/recipe-types/{typePublicId}/templates", s.optionalAuth(s.catalogRecipeTemplates))
	s.mux.HandleFunc("POST /api/v1/recipe-types/{typePublicId}/templates", s.requirePermission("content.write", s.catalogRecipeTemplates))
	s.mux.HandleFunc("POST /api/v1/recipe-types/{typePublicId}/recipes", s.requirePermission("content.write", s.createCatalogRecipe))
	s.mux.HandleFunc("GET /api/v1/recipe-types/{typePublicId}/recipes", s.optionalAuth(s.catalogRecipes))
	s.mux.HandleFunc("POST /api/v1/catalog/recipes", s.requirePermission("content.write", s.createCatalogRecipe))
	s.mux.HandleFunc("GET /api/v1/catalog/recipe-source-versions", s.optionalAuth(s.catalogRecipeSourceVersions))
	s.mux.HandleFunc("GET /api/v1/recipe-templates/{publicId}", s.optionalAuth(s.catalogRecipeTemplateDetail))
	s.mux.HandleFunc("PUT /api/v1/recipe-templates/{publicId}", s.requirePermission("content.write", s.catalogRecipeTemplateDetail))
	s.mux.HandleFunc("DELETE /api/v1/recipe-templates/{publicId}", s.requirePermission("content.write", s.catalogRecipeTemplateDetail))
	s.mux.HandleFunc("GET /api/v1/recipe-templates/{publicId}/background", s.optionalAuth(s.catalogRecipeTemplateBackground))
	s.mux.HandleFunc("GET /api/v1/catalog/recipes/{publicId}", s.optionalAuth(s.catalogRecipeDetail))
	s.mux.HandleFunc("GET /api/v1/catalog/recipes/{publicId}/render", s.optionalAuth(s.globalRecipeRender))
	s.mux.HandleFunc("PUT /api/v1/catalog/recipes/{publicId}", s.requirePermission("content.write", s.catalogRecipeDetail))
	s.mux.HandleFunc("DELETE /api/v1/catalog/recipes/{publicId}", s.requirePermission("content.write", s.catalogRecipeDetail))
	s.mux.HandleFunc("GET /api/v1/public-links/{publicId}", s.resolvePublicLink)
	s.mux.HandleFunc("GET /api/v1/skin-service", s.optionalAuth(s.skinService))
	s.mux.HandleFunc("GET /api/v1/skins", s.optionalAuth(s.skins))
	s.mux.HandleFunc("POST /api/v1/skins", s.requirePermission("skin.library.upload", s.skins))
	s.mux.HandleFunc("GET /api/v1/skins/{publicId}", s.optionalAuth(s.skinDetail))
	s.mux.HandleFunc("PUT /api/v1/skins/{publicId}", s.requireAuth(s.skinDetail))
	s.mux.HandleFunc("GET /api/v1/skins/{publicId}/content", s.requireAuth(s.ownedSkinContent))
	s.mux.HandleFunc("PUT /api/v1/skins/{publicId}/content", s.requireAuth(s.ownedSkinContent))
	s.mux.HandleFunc("DELETE /api/v1/skins/{publicId}", s.requireAuth(s.skinDetail))
	s.mux.HandleFunc("GET /api/v1/users/me/skin-wardrobe", s.requireAuth(s.skinWardrobe))
	s.mux.HandleFunc("PUT /api/v1/users/me/skin-wardrobe/{publicId}", s.requireAuth(s.skinWardrobeItem))
	s.mux.HandleFunc("DELETE /api/v1/users/me/skin-wardrobe/{publicId}", s.requireAuth(s.skinWardrobeItem))
	s.mux.HandleFunc("GET /api/v1/users/me/player-profiles", s.requireAuth(s.myPlayerProfiles))
	s.mux.HandleFunc("POST /api/v1/users/me/player-profiles", s.requirePermission("skin.profile.create", s.myPlayerProfiles))
	s.mux.HandleFunc("PUT /api/v1/users/me/player-profiles/{publicId}", s.requirePermission("skin.profile.write", s.myPlayerProfile))
	s.mux.HandleFunc("DELETE /api/v1/users/me/player-profiles/{publicId}", s.requirePermission("skin.profile.write", s.myPlayerProfile))
	s.mux.HandleFunc("PUT /api/v1/users/me/player-profiles/{publicId}/textures", s.requirePermission("skin.profile.write", s.setPlayerProfileTexture))
	s.mux.HandleFunc("GET /api/v1/users/{id}/player-profiles", s.optionalAuth(s.publicUserPlayerProfiles))
	s.mux.HandleFunc("GET /api/v1/player-profiles/{publicId}", s.optionalAuth(s.playerProfileDetail))
	s.mux.HandleFunc("PUT /api/v1/users/me/launcher-credential", s.requirePermission("skin.launcher.login", s.launcherCredential))
	s.mux.HandleFunc("DELETE /api/v1/users/me/launcher-credential", s.requireAuth(s.launcherCredential))
	s.mux.HandleFunc("GET /api/v1/users/me/launcher-sessions", s.requireAuth(s.launcherSessions))
	s.mux.HandleFunc("DELETE /api/v1/users/me/launcher-sessions/{sessionId}", s.requireAuth(s.launcherSession))
	s.mux.HandleFunc("GET /api/v1/creators", s.optionalAuth(s.creators))
	s.mux.HandleFunc("POST /api/v1/creators", s.requirePermission("creator.create", s.createCreator))
	s.mux.HandleFunc("POST /api/v1/creator-imports", s.requirePermission("creator.create", s.importCreator))
	s.mux.HandleFunc("GET /api/v1/creators/{publicId}", s.optionalAuth(s.creatorDetail))
	s.mux.HandleFunc("PUT /api/v1/creators/{publicId}", s.requireAuth(s.updateCreator))
	s.mux.HandleFunc("POST /api/v1/creators/{publicId}/claims", s.requirePermission("creator.claim", s.claimCreator))
	s.mux.HandleFunc("GET /api/v1/creator-roles", s.optionalAuth(s.creatorRoles))
	s.mux.HandleFunc("POST /api/v1/creator-roles", s.requirePermission("creator.role.write", s.createCreatorRole))
	s.mux.HandleFunc("GET /api/v1/economy/currencies", s.optionalAuth(s.publicCurrencies))
	s.mux.HandleFunc("GET /api/v1/shop/items", s.optionalAuth(s.publicShopItems))
	s.mux.HandleFunc("GET /api/v1/blueprints", s.optionalAuth(s.blueprints))
	s.mux.HandleFunc("GET /api/v1/blueprints/{publicId}", s.optionalAuth(s.blueprintDetail))
	s.mux.HandleFunc("GET /api/v1/blueprints/{publicId}/render", s.optionalAuth(s.blueprintRenderData))
	s.mux.HandleFunc("GET /api/v1/blueprints/{publicId}/cover", s.optionalAuth(s.blueprintCover))
	s.mux.HandleFunc("PUT /api/v1/blueprints/{publicId}", s.requireAuth(s.updateBlueprint))
	s.mux.HandleFunc("GET /api/v1/blueprints/{publicId}/content", s.requireAuth(s.ownedBlueprintContent))
	s.mux.HandleFunc("PUT /api/v1/blueprints/{publicId}/content", s.requireAuth(s.ownedBlueprintContent))
	s.mux.HandleFunc("POST /api/v1/blueprints/{publicId}/convert", s.requireAuth(s.convertBlueprint))
	s.mux.HandleFunc("POST /api/v1/blueprints/{publicId}/retry", s.requireAuth(s.retryBlueprint))
	s.mux.HandleFunc("POST /api/v1/blueprints/{publicId}/variants/{variantId}/download", s.optionalAuth(s.downloadBlueprintVariant))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}", s.optionalAuth(s.publicModDetail))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/icon", s.optionalAuth(s.publicModIcon))
	s.mux.HandleFunc("GET /api/v1/modpacks/{siteId}", s.optionalAuth(s.modpackItem))
	s.mux.HandleFunc("GET /api/v1/modpacks/{siteId}/icon", s.optionalAuth(s.modpackIcon))
	s.mux.HandleFunc("GET /api/v1/modpacks/{siteId}/editor", s.requireAuth(s.modpackEditor))
	s.mux.HandleFunc("PUT /api/v1/modpacks/{siteId}", s.requireAuth(s.modpackItem))
	s.mux.HandleFunc("GET /api/v1/modpacks/{siteId}/history", s.optionalAuth(s.modpackHistory))
	s.mux.HandleFunc("GET /api/v1/modpacks/{siteId}/gallery/{publicId}", s.optionalAuth(s.modpackGalleryImage))
	s.mux.HandleFunc("GET /api/v1/content-projects/{projectType}/{siteId}", s.optionalAuth(s.simpleProjectItem))
	s.mux.HandleFunc("GET /api/v1/content-projects/{projectType}/{siteId}/icon", s.optionalAuth(s.simpleProjectIcon))
	s.mux.HandleFunc("PUT /api/v1/content-projects/{projectType}/{siteId}", s.requireAuth(s.simpleProjectItem))
	s.mux.HandleFunc("GET /api/v1/content-projects/{projectType}/{siteId}/editor", s.requireAuth(s.simpleProjectEditor))
	s.mux.HandleFunc("GET /api/v1/content-projects/{projectType}/{siteId}/history", s.optionalAuth(s.simpleProjectHistory))
	s.mux.HandleFunc("GET /api/v1/content-projects/{projectType}/{siteId}/gallery/{publicId}", s.optionalAuth(s.simpleProjectGalleryImage))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/editor", s.requireAuth(s.modEditorDetail))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/content-versions", s.optionalAuth(s.modContentVersions))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/content-versions", s.requireAuth(s.modContentVersions))
	s.mux.HandleFunc("PUT /api/v1/mods/{siteId}/content-versions/{versionId}", s.requireAuth(s.modContentVersion))
	s.mux.HandleFunc("DELETE /api/v1/mods/{siteId}/content-versions/{versionId}", s.requireAuth(s.modContentVersion))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/content-templates", s.optionalAuth(s.modContentTemplates))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/content-templates", s.requireAuth(s.modContentTemplates))
	s.mux.HandleFunc("PUT /api/v1/mods/{siteId}/content-templates/{templateId}", s.requireAuth(s.modContentTemplate))
	s.mux.HandleFunc("DELETE /api/v1/mods/{siteId}/content-templates/{templateId}", s.requireAuth(s.modContentTemplate))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/content-sections", s.optionalAuth(s.modContentSections))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/content-sections", s.requireAuth(s.modContentSections))
	s.mux.HandleFunc("PUT /api/v1/mods/{siteId}/content-sections/{sectionId}", s.requireAuth(s.modContentSection))
	s.mux.HandleFunc("DELETE /api/v1/mods/{siteId}/content-sections/{sectionId}", s.requireAuth(s.modContentSection))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/content-sections/{sectionId}/resources", s.optionalAuth(s.modContentSectionResources))
	s.mux.HandleFunc("PUT /api/v1/mods/{siteId}/content-sections/{sectionId}/layout", s.requireAuth(s.modContentSectionLayout))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/content-resources", s.optionalAuth(s.modContentResources))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/content-resources", s.requireAuth(s.modContentResources))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/content-resources/{resourceId}/similar", s.optionalAuth(s.modContentSimilarResources))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/content-resources/{resourceId}/history", s.optionalAuth(s.modContentResourceHistory))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/content-resources/{resourceId}", s.optionalAuth(s.modContentResource))
	s.mux.HandleFunc("PUT /api/v1/mods/{siteId}/content-resources/{resourceId}", s.requireAuth(s.modContentResource))
	s.mux.HandleFunc("DELETE /api/v1/mods/{siteId}/content-resources/{resourceId}", s.requireAuth(s.modContentResource))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/gallery/{publicId}", s.optionalAuth(s.modGalleryImage))
	s.mux.HandleFunc("GET /api/v1/projects/{projectType}/{projectId}/files", s.optionalAuth(s.projectFiles))
	s.mux.HandleFunc("POST /api/v1/projects/{projectType}/{projectId}/files", s.requireAuth(s.projectFiles))
	s.mux.HandleFunc("DELETE /api/v1/projects/{projectType}/{projectId}/files/{fileId}", s.requireAuth(s.projectFile))
	s.mux.HandleFunc("POST /api/v1/projects/{projectType}/{projectId}/files/{source}/{fileId}/download", s.optionalAuth(s.downloadProjectFile))
	s.mux.HandleFunc("POST /api/v1/projects/{projectType}/{projectId}/files/uploads/presign", s.requireAuth(s.createProjectFileUpload))
	s.mux.HandleFunc("POST /api/v1/projects/{projectType}/{projectId}/files/uploads/complete", s.requireAuth(s.completeProjectFileUpload))
	s.mux.HandleFunc("GET /api/v1/projects/{projectType}/{projectId}/automation", s.requireAuth(s.projectAutomation))
	s.mux.HandleFunc("PUT /api/v1/projects/{projectType}/{projectId}/automation", s.requireAuth(s.projectAutomation))
	s.mux.HandleFunc("POST /api/v1/projects/{projectType}/{projectId}/automation/sources", s.requireAuth(s.bindProjectAutomationSource))
	s.mux.HandleFunc("POST /api/v1/projects/{projectType}/{projectId}/automation/runs", s.requireAuth(s.runProjectAutomation))
	s.mux.HandleFunc("GET /api/v1/projects/{projectType}/{projectId}/automation/runs", s.requireAuth(s.projectAutomationRuns))
	s.mux.HandleFunc("POST /api/v1/mods", s.requirePermission("project.create", s.createMod))
	s.mux.HandleFunc("POST /api/v1/modpacks", s.requirePermission("modpack.create", s.createModpack))
	s.mux.HandleFunc("POST /api/v1/mod-imports", s.requirePermission("project.create", s.createModMetadataImport))
	s.mux.HandleFunc("GET /api/v1/mod-imports/{jobId}", s.requirePermission("project.create", s.getModMetadataImport))
	s.mux.HandleFunc("POST /api/v1/modpack-imports", s.requirePermission("modpack.create", s.createModpackMetadataImport))
	s.mux.HandleFunc("GET /api/v1/modpack-imports/{jobId}", s.requirePermission("modpack.create", s.getModMetadataImport))
	s.mux.HandleFunc("POST /api/v1/content-project-imports/{projectType}", s.requireAuth(s.createSimpleProjectMetadataImport))
	s.mux.HandleFunc("GET /api/v1/content-project-imports/{projectType}/{jobId}", s.requireAuth(s.getSimpleProjectMetadataImport))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/revisions", s.optionalAuth(s.modRevisionHistory))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/revisions", s.requireAuth(s.submitModRevision))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/revisions/compare", s.optionalAuth(s.compareModRevisions))
	s.mux.HandleFunc("PATCH /api/v1/mods/{siteId}/revisions/{revisionId}", s.requireAuth(s.reviewModRevision))
	s.mux.HandleFunc("PATCH /api/v1/content-revisions/{revisionId}", s.requireAuth(s.reviewContentRevision))
	s.mux.HandleFunc("GET /api/v1/reviews/content", s.requireAuth(s.adminModContentReviews))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/export-imports/uploads/presign", s.requireAuth(s.createModExportUpload))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/export-imports/uploads/resume", s.requireAuth(s.resumeModExportUpload))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/export-imports/uploads/complete", s.requireAuth(s.completeModExportUpload))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/export-imports", s.requireAuth(s.createModExportJob))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/export-imports/active", s.requireAuth(s.getActiveModExportJob))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/export-imports/{jobId}", s.requireAuth(s.getModExportJob))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/export-imports/{jobId}/retry", s.requireAuth(s.retryModExportJob))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/export-imports/{jobId}/cancel", s.requireAuth(s.cancelModExportJob))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/catalog-imports/uploads/presign", s.requireAuth(s.createEmbeddedIconImportUpload))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/catalog-imports/uploads/complete", s.requireAuth(s.completeEmbeddedIconImportUpload))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/catalog-imports", s.requireAuth(s.createEmbeddedIconImportJob))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/catalog-imports/{jobId}", s.requireAuth(s.getModExportJob))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/catalog-imports/{jobId}/retry", s.requireAuth(s.retryModExportJob))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/catalog-imports/{jobId}/cancel", s.requireAuth(s.cancelModExportJob))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/export-data", s.optionalAuth(s.modExportDataSummary))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/registries/{registry}", s.optionalAuth(s.modExportRegistry))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/registry-entries", s.optionalAuth(s.modExportRegistryEntries))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/document-entries", s.optionalAuth(s.modExportDocumentEntries))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/tags", s.optionalAuth(s.modExportTags))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/tag-detail", s.optionalAuth(s.modExportTagDetail))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/entry-detail", s.optionalAuth(s.modExportEntryDetail))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/assets", s.optionalAuth(s.modExportAssetIndex))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/assets/content", s.optionalAuth(s.modExportAssetContent))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/structures", s.optionalAuth(s.modExportStructures))
	s.mux.HandleFunc("GET /api/v1/export-revisions/{revisionId}/structures/{structureId}/template", s.optionalAuth(s.modExportStructureTemplate))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/applications", s.requireAuth(s.modApplications))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/applications", s.requireAuth(s.modApplications))
	s.mux.HandleFunc("GET /api/v1/comment-targets/{targetType}/{targetKey}/comments", s.optionalAuth(s.commentsForTarget))
	s.mux.HandleFunc("GET /api/v1/comment-targets/{targetType}/{targetKey}/comments/floors/{floor}", s.optionalAuth(s.commentFloor))
	s.mux.HandleFunc("POST /api/v1/comment-targets/{targetType}/{targetKey}/comments", s.requireAuth(s.commentsForTarget))
	s.mux.HandleFunc("GET /api/v1/comments/{commentId}/replies", s.optionalAuth(s.commentReplies))
	s.mux.HandleFunc("GET /api/v1/comments/{commentId}/thread", s.optionalAuth(s.commentThread))
	s.mux.HandleFunc("PATCH /api/v1/comments/{commentId}", s.requireAuth(s.commentItem))
	s.mux.HandleFunc("DELETE /api/v1/comments/{commentId}", s.requireAuth(s.commentItem))
	s.mux.HandleFunc("PUT /api/v1/comments/{commentId}/pin", s.requireAuth(s.commentPin))
	s.mux.HandleFunc("DELETE /api/v1/comments/{commentId}/pin", s.requireAuth(s.commentPin))
	s.mux.HandleFunc("PUT /api/v1/comments/{commentId}/reaction", s.requireAuth(s.commentReaction))
	s.mux.HandleFunc("DELETE /api/v1/comments/{commentId}/reaction", s.requireAuth(s.commentReaction))
	s.mux.HandleFunc("POST /api/v1/comments/{commentId}/reports", s.requireAuth(s.reportComment))
	s.mux.HandleFunc("GET /api/v1/comments/{commentId}/watch", s.requireAuth(s.commentWatch))
	s.mux.HandleFunc("PUT /api/v1/comments/{commentId}/watch", s.requireAuth(s.commentWatch))
	s.mux.HandleFunc("DELETE /api/v1/comments/{commentId}/watch", s.requireAuth(s.commentWatch))
	s.mux.HandleFunc("GET /api/v1/community/posts", s.optionalAuth(s.communityPosts))
	s.mux.HandleFunc("GET /api/v1/community/post-categories", s.optionalAuth(s.communityPostCategoryOptions))
	s.mux.HandleFunc("POST /api/v1/community/posts", s.requireAuth(s.communityPosts))
	s.mux.HandleFunc("GET /api/v1/community/posts/{id}", s.optionalAuth(s.communityPostItem))
	s.mux.HandleFunc("PUT /api/v1/community/posts/{id}", s.requireAuth(s.communityPostItem))
	s.mux.HandleFunc("GET /api/v1/community/posts/{id}/history", s.optionalAuth(s.communityPostHistory))
	s.mux.HandleFunc("POST /api/v1/community/posts/{id}/answers/{commentId}", s.requireAuth(s.acceptCommunityPostAnswer))
	s.mux.HandleFunc("POST /api/v1/community/posts/{id}/self-solved", s.requireAuth(s.selfSolveCommunityPost))
	s.mux.HandleFunc("POST /api/v1/community/posts/{id}/translations", s.requirePermission("content.translate", s.requestCommunityPostTranslation))
	s.mux.HandleFunc("GET /api/v1/changelogs", s.optionalAuth(s.projectChangelogs))
	s.mux.HandleFunc("POST /api/v1/changelogs", s.requireAuth(s.projectChangelogs))
	s.mux.HandleFunc("GET /api/v1/changelogs/{id}", s.optionalAuth(s.projectChangelogItem))
	s.mux.HandleFunc("PUT /api/v1/changelogs/{id}", s.requireAuth(s.projectChangelogItem))
	s.mux.HandleFunc("GET /api/v1/changelogs/{id}/history", s.optionalAuth(s.projectChangelogHistory))
	s.mux.HandleFunc("GET /api/v1/community/translations/{taskId}", s.requirePermission("content.translate", s.communityPostTranslationResult))
	s.mux.HandleFunc("GET /api/v1/users/me/markdown-playground", s.requireAuth(s.getMarkdownPlaygroundDraft))
	s.mux.HandleFunc("PUT /api/v1/users/me/markdown-playground", s.requireAuth(s.saveMarkdownPlaygroundDraft))
	s.mux.HandleFunc("POST /api/v1/users/me/oss/uploads/presign", s.requireAuth(s.createUserOSSDirectUpload))
	s.mux.HandleFunc("POST /api/v1/users/me/oss/uploads/complete", s.requireAuth(s.completeUserOSSDirectUpload))
	s.mux.HandleFunc("GET /api/v1/users/me/files", s.requireAuth(s.userOSSFiles))
	s.mux.HandleFunc("GET /api/v1/users/me/files/quota", s.requireAuth(s.userOSSFileQuota))
	s.mux.HandleFunc("POST /api/v1/users/me/files/presign", s.requireAuth(s.presignUserOSSFile))
	s.mux.HandleFunc("DELETE /api/v1/users/me/files", s.requireAuth(s.deleteUserOSSFile))
	s.mux.HandleFunc("GET /api/v1/users/me/notification-settings", s.requireAuth(s.getNotificationSettings))
	s.mux.HandleFunc("PUT /api/v1/users/me/notification-settings", s.requireAuth(s.updateNotificationSettings))
	s.mux.HandleFunc("GET /api/v1/users/me/profile-settings", s.requireAuth(s.getUserProfileSettings))
	s.mux.HandleFunc("PUT /api/v1/users/me/profile-settings", s.requireAuth(s.updateUserProfileSettings))
	s.mux.HandleFunc("GET /api/v1/users/me/overview", s.requireAuth(s.userOverview))
	s.mux.HandleFunc("GET /api/v1/users/me/comment-watches", s.requireAuth(s.myCommentWatches))
	s.mux.HandleFunc("POST /api/v1/comment-watches/{watchId}/read", s.requireAuth(s.commentWatchItem))
	s.mux.HandleFunc("PATCH /api/v1/comment-watches/{watchId}", s.requireAuth(s.commentWatchItem))
	s.mux.HandleFunc("GET /api/v1/users/me/economy", s.requireAuth(s.userEconomyOverview))
	s.mux.HandleFunc("GET /api/v1/users/me/economy/transactions", s.requireAuth(s.userCurrencyTransactions))
	s.mux.HandleFunc("GET /api/v1/users/me/experience/transactions", s.requireAuth(s.userExperienceTransactions))
	s.mux.HandleFunc("POST /api/v1/users/me/economy/checkin", s.requirePermission("economy.checkin", s.checkIn))
	s.mux.HandleFunc("POST /api/v1/users/me/economy/transfer", s.requirePermission("economy.transfer", s.transferCurrency))
	s.mux.HandleFunc("POST /api/v1/users/me/shop/purchase", s.requirePermission("shop.purchase", s.purchaseShopItem))
	s.mux.HandleFunc("POST /api/v1/users/me/shop/use", s.requirePermission("shop.use", s.useShopItem))
	s.mux.HandleFunc("GET /api/v1/users/me/tasks", s.requireAuth(s.userTasks))
	s.mux.HandleFunc("GET /api/v1/users/me/favorite-collections", s.requireAuth(s.favoriteCollections))
	s.mux.HandleFunc("POST /api/v1/users/me/favorite-collections", s.requireAuth(s.createFavoriteCollection))
	s.mux.HandleFunc("PUT /api/v1/users/me/favorite-collections/{id}", s.requireAuth(s.updateFavoriteCollection))
	s.mux.HandleFunc("DELETE /api/v1/users/me/favorite-collections/{id}", s.requireAuth(s.deleteFavoriteCollection))
	s.mux.HandleFunc("GET /api/v1/users/me/favorite-collections/{id}/items", s.requireAuth(s.favoriteCollectionItems))
	s.mux.HandleFunc("GET /api/v1/users/me/favorites", s.requireAuth(s.favoriteMembership))
	s.mux.HandleFunc("PUT /api/v1/users/me/favorites", s.requireAuth(s.setFavoriteMembership))
	s.mux.HandleFunc("GET /api/v1/users/{id}/profile", s.optionalAuth(s.userProfile))
	s.mux.HandleFunc("GET /api/v1/users/{id}/card", s.optionalAuth(s.userCard))
	s.mux.HandleFunc("GET /api/v1/users/me/statistics", s.requireAuth(s.myUserStatistics))
	s.mux.HandleFunc("GET /api/v1/users/{id}/statistics", s.optionalAuth(s.publicUserStatistics))
	s.mux.HandleFunc("GET /api/v1/users/{id}/followers", s.optionalAuth(s.userFollowers))
	s.mux.HandleFunc("GET /api/v1/users/{id}/following", s.optionalAuth(s.userFollowing))
	s.mux.HandleFunc("GET /api/v1/users/me/blocks", s.requireAuth(s.userBlockList))
	s.mux.HandleFunc("GET /api/v1/users/{id}/showcase", s.optionalAuth(s.userShowcase))
	s.mux.HandleFunc("GET /api/v1/users/{id}/contributions", s.optionalAuth(s.userContributions))
	s.mux.HandleFunc("GET /api/v1/users/{id}/favorite-collections", s.optionalAuth(s.publicFavoriteCollections))
	s.mux.HandleFunc("GET /api/v1/users/{id}/favorite-collections/{collectionId}/items", s.optionalAuth(s.publicFavoriteCollectionItems))
	s.mux.HandleFunc("POST /api/v1/users/{id}/follow", s.requirePermission("user.follow.create", s.followUser))
	s.mux.HandleFunc("DELETE /api/v1/users/{id}/follow", s.requirePermission("user.follow.create", s.unfollowUser))
	s.mux.HandleFunc("PUT /api/v1/users/{id}/block", s.requireAuth(s.userBlock))
	s.mux.HandleFunc("DELETE /api/v1/users/{id}/block", s.requireAuth(s.userBlock))
	s.mux.HandleFunc("GET /api/v1/messages/conversations", s.requireAuth(s.conversations))
	s.mux.HandleFunc("POST /api/v1/messages/conversations", s.requirePermission("user.message.send", s.startConversation))
	s.mux.HandleFunc("GET /api/v1/messages/conversations/{id}", s.requireAuth(s.conversationMessages))
	s.mux.HandleFunc("POST /api/v1/messages/conversations/{id}", s.requirePermission("user.message.send", s.sendConversationMessage))
	s.mux.HandleFunc("PUT /api/v1/messages/conversations/{id}/presence", s.requireAuth(s.updateConversationPresence))
	s.mux.HandleFunc("GET /api/v1/notifications", s.requireAuth(s.notifications))
	s.mux.HandleFunc("GET /api/v1/notifications/unread", s.requireAuth(s.unreadSummary))
	s.mux.HandleFunc("GET /api/v1/me/unread-summary", s.requireAuth(s.unreadSummary))
	s.mux.HandleFunc("POST /api/v1/notifications/read-all", s.requireAuth(s.markAllNotificationsRead))
	s.mux.HandleFunc("POST /api/v1/notifications/{id}/read", s.requireAuth(s.markNotificationRead))
	s.mux.HandleFunc("GET /api/v1/notifications/ai-balance", s.requireAuth(s.aiDailyBalance))
	s.mux.HandleFunc("POST /api/v1/notifications/{id}/translate", s.requirePermission("notification.translate", s.translateNotification))
	s.mux.HandleFunc("GET /api/v1/notifications/translations/{id}", s.requirePermission("notification.translate", s.notificationTranslationResult))

	s.mux.HandleFunc("GET /api/v1/admin/dashboard", s.requirePermission("admin.access", s.adminDashboard))
	s.mux.HandleFunc("GET /api/v1/admin/dashboard/projects", s.requirePermission("admin.access", s.adminDashboardProjects))
	s.mux.HandleFunc("GET /api/v1/admin/dashboard/projects/{publicId}", s.requirePermission("admin.access", s.adminDashboardProject))
	s.mux.HandleFunc("GET /api/v1/admin/nav", s.requirePermission("admin.access", s.adminNav))
	s.mux.HandleFunc("GET /api/v1/admin/config", s.requirePermission("admin.config.read", s.adminConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/general/logo-upload-access", s.requirePermission("admin.config.write", s.siteLogoUploadAccess))
	s.mux.HandleFunc("PUT /api/v1/admin/config/general", s.requirePermission("admin.config.write", s.updateSiteGeneralConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/yggdrasil", s.requirePermission("admin.config.read", s.getYggdrasilConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/yggdrasil", s.requirePermission("admin.config.write", s.updateYggdrasilConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/markdown", s.requirePermission("admin.config.read", s.markdownConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/markdown", s.requirePermission("admin.config.write", s.updateMarkdownConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/profile", s.requirePermission("admin.config.write", s.updateProfileConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/minecraft-versions", s.requirePermission("admin.config.write", s.updateMinecraftVersions))
	s.mux.HandleFunc("POST /api/v1/admin/config/minecraft-versions/sync", s.requirePermission("admin.config.write", s.syncMinecraftVersions))
	s.mux.HandleFunc("PUT /api/v1/admin/config/mail", s.requirePermission("mail.write", s.updateMailConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/oauth", s.requirePermission("admin.config.write", s.updateOAuthConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/nats", s.requirePermission("admin.config.read", s.getNATSConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/nats", s.requirePermission("admin.config.write", s.updateNATSConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/mod-imports", s.requirePermission("admin.config.read", s.getModImportConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/mod-imports", s.requirePermission("admin.config.write", s.updateModImportConfig))
	s.mux.HandleFunc("GET /api/v1/admin/mod-content-attribute-templates", s.requirePermission("admin.config.read", s.adminModContentAttributeTemplates))
	s.mux.HandleFunc("PUT /api/v1/admin/mod-content-attribute-templates/{templateId}", s.requirePermission("admin.config.write", s.adminModContentAttributeTemplate))
	s.mux.HandleFunc("GET /api/v1/admin/config/notifications", s.requirePermission("admin.config.read", s.getNotificationTemplates))
	s.mux.HandleFunc("PUT /api/v1/admin/config/notifications", s.requirePermission("admin.config.write", s.updateNotificationTemplates))
	s.mux.HandleFunc("GET /api/v1/admin/config/reviews", s.requirePermission("admin.config.read", s.getReviewConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/reviews", s.requirePermission("admin.config.write", s.updateReviewConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/oss", s.requirePermission("oss.read", s.getOSSConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/oss", s.requirePermission("oss.write", s.updateOSSConfig))
	s.mux.HandleFunc("POST /api/v1/admin/mail/test", s.requirePermission("mail.write", s.sendTestMail))
	s.mux.HandleFunc("GET /api/v1/admin/permissions", s.requirePermission("permission.read", s.permissionCatalog))
	s.mux.HandleFunc("POST /api/v1/admin/permissions", s.requirePermission("permission.write", s.createPermission))
	s.mux.HandleFunc("POST /api/v1/admin/roles", s.requirePermission("permission.write", s.createRole))
	s.mux.HandleFunc("PUT /api/v1/admin/roles/{code}", s.requirePermission("permission.write", s.updateRole))
	s.mux.HandleFunc("DELETE /api/v1/admin/roles/{code}", s.requirePermission("permission.write", s.deleteRole))
	s.mux.HandleFunc("GET /api/v1/admin/permission-defaults", s.requirePermission("permission.read", s.getPermissionDefaults))
	s.mux.HandleFunc("PUT /api/v1/admin/permission-defaults", s.requirePermission("permission.write", s.updatePermissionDefaults))
	s.mux.HandleFunc("GET /api/v1/admin/role-tracks", s.requirePermission("permission.read", s.roleTracks))
	s.mux.HandleFunc("POST /api/v1/admin/role-tracks", s.requirePermission("permission.write", s.createRoleTrack))
	s.mux.HandleFunc("PUT /api/v1/admin/role-tracks/{code}", s.requirePermission("permission.write", s.updateRoleTrack))
	s.mux.HandleFunc("DELETE /api/v1/admin/role-tracks/{code}", s.requirePermission("permission.write", s.deleteRoleTrack))
	s.mux.HandleFunc("GET /api/v1/admin/users", s.requirePermission("user.read", s.adminUsers))
	s.mux.HandleFunc("POST /api/v1/admin/users", s.requirePermission("user.write", s.createAdminUser))
	s.mux.HandleFunc("GET /api/v1/admin/users/{id}", s.requirePermission("user.read", s.adminUserDetails))
	s.mux.HandleFunc("PUT /api/v1/admin/users/{id}/status", s.requirePermission("user.write", s.updateUserStatus))
	s.mux.HandleFunc("GET /api/v1/admin/users/{id}/balances", s.requirePermission("economy.read", s.adminUserBalances))
	s.mux.HandleFunc("POST /api/v1/admin/users/{id}/balances", s.requirePermission("economy.balance.write", s.adjustAdminUserBalance))
	s.mux.HandleFunc("POST /api/v1/admin/users/{id}/role-tracks/{code}/upgrade", s.requirePermission("permission.write", s.upgradeUserRoleTrack))
	s.mux.HandleFunc("POST /api/v1/admin/users/{id}/role-tracks/{code}/downgrade", s.requirePermission("permission.write", s.downgradeUserRoleTrack))
	s.mux.HandleFunc("PUT /api/v1/admin/users/{id}/roles", s.requirePermission("permission.write", s.updateUserRoles))
	s.mux.HandleFunc("GET /api/v1/admin/users/{id}/permissions", s.requirePermission("permission.read", s.userPermissionDetails))
	s.mux.HandleFunc("PUT /api/v1/admin/users/{id}/permissions", s.requirePermission("permission.write", s.updateUserPermissions))
	s.mux.HandleFunc("GET /api/v1/admin/mod-applications", s.requirePermission("project.review", s.adminModApplications))
	s.mux.HandleFunc("PATCH /api/v1/admin/mod-applications/{id}", s.requirePermission("project.review", s.reviewModApplication))
	s.mux.HandleFunc("POST /api/v1/admin/mod-applications/{id}/attachments/{fileId}/presign", s.requirePermission("project.review", s.presignModApplicationAttachment))
	s.mux.HandleFunc("GET /api/v1/admin/reports", s.requirePermission("report.review", s.adminReports))
	s.mux.HandleFunc("GET /api/v1/admin/reports/{id}", s.requirePermission("report.review", s.adminReportDetail))
	s.mux.HandleFunc("POST /api/v1/admin/reports/{id}/claim", s.requirePermission("report.review", s.claimReport))
	s.mux.HandleFunc("POST /api/v1/admin/reports/{id}/resolve", s.requirePermission("report.review", s.resolveUnifiedReport))
	s.mux.HandleFunc("POST /api/v1/admin/reports/{id}/reopen", s.requirePermission("report.action.reopen", s.reopenUnifiedReport))
	s.mux.HandleFunc("GET /api/v1/admin/bans", s.requirePermission("ban.view_internal", s.adminBans))
	s.mux.HandleFunc("POST /api/v1/admin/bans", s.requirePermission("ban.create", s.adminBans))
	s.mux.HandleFunc("GET /api/v1/admin/ban-reasons", s.requirePermission("ban.create", s.adminBanReasons))
	s.mux.HandleFunc("POST /api/v1/admin/bans/{id}/revoke", s.requirePermission("ban.revoke", s.revokeBan))
	s.mux.HandleFunc("GET /api/v1/admin/seed-crawler", s.requirePermission("seed_crawler.view", s.adminSeedCrawler))
	s.mux.HandleFunc("PUT /api/v1/admin/seed-crawler", s.requirePermission("seed_crawler.configure", s.adminSeedCrawler))
	s.mux.HandleFunc("GET /api/v1/admin/seed-crawler/runs", s.requirePermission("seed_crawler.view", s.adminSeedCrawlerRuns))
	s.mux.HandleFunc("POST /api/v1/admin/seed-crawler/runs", s.requirePermission("seed_crawler.run", s.adminSeedCrawlerRuns))
	s.mux.HandleFunc("GET /api/v1/admin/seed-crawler/candidates", s.requirePermission("seed_crawler.view", s.adminSeedCrawlerCandidates))
	s.mux.HandleFunc("GET /api/v1/admin/project-auto-updates", s.requirePermission("project.auto_update.view_logs", s.adminProjectAutomationOverview))
	s.mux.HandleFunc("GET /api/v1/admin/site-affairs/about/{locale}", s.requirePermission("site_affairs.about.manage", s.adminAboutPage))
	s.mux.HandleFunc("PUT /api/v1/admin/site-affairs/about/{locale}", s.requirePermission("site_affairs.about.manage", s.adminAboutPage))
	s.mux.HandleFunc("GET /api/v1/admin/site-affairs/changelogs", s.requirePermission("site_affairs.changelog.manage", s.adminSiteChangelogs))
	s.mux.HandleFunc("POST /api/v1/admin/site-affairs/changelogs", s.requirePermission("site_affairs.changelog.manage", s.adminSiteChangelogs))
	s.mux.HandleFunc("GET /api/v1/admin/site-affairs/changelogs/{id}", s.requirePermission("site_affairs.changelog.manage", s.adminSiteChangelogDetail))
	s.mux.HandleFunc("PUT /api/v1/admin/site-affairs/changelogs/{id}", s.requirePermission("site_affairs.changelog.manage", s.adminSiteChangelogDetail))
	s.mux.HandleFunc("GET /api/v1/admin/server-reviews", s.requirePermission("server.review", s.adminMinecraftServerReviews))
	s.mux.HandleFunc("PATCH /api/v1/admin/server-reviews/{serverId}", s.requirePermission("server.review", s.reviewMinecraftServer))
	s.mux.HandleFunc("POST /api/v1/admin/server-reviews/{serverId}/attachments/{fileId}/presign", s.requirePermission("server.review", s.presignMinecraftServerProof))
	s.mux.HandleFunc("GET /api/v1/admin/server-settings", s.requirePermission("admin.config.read", s.adminServerSettings))
	s.mux.HandleFunc("PUT /api/v1/admin/server-settings", s.requirePermission("admin.config.write", s.adminServerSettings))
	s.mux.HandleFunc("GET /api/v1/admin/anti-abuse/overview", s.requirePermission("security.anti-abuse.read", s.adminAntiAbuseOverview))
	s.mux.HandleFunc("GET /api/v1/admin/infrastructure/metrics", s.requirePermission("admin.config.read", s.infrastructureMetrics))
	s.mux.HandleFunc("GET /api/v1/admin/infrastructure/dead-letters", s.requirePermission("admin.config.read", s.adminDeadLetters))
	s.mux.HandleFunc("POST /api/v1/admin/infrastructure/dead-letters/{id}/replay", s.requirePermission("admin.config.write", s.adminReplayDeadLetter))
	s.mux.HandleFunc("POST /api/v1/admin/infrastructure/unread/reconcile", s.requirePermission("admin.config.write", s.adminReconcileUnread))
	s.mux.HandleFunc("GET /api/v1/admin/anti-abuse/events", s.requirePermission("security.anti-abuse.read", s.adminAntiAbuseEvents))
	s.mux.HandleFunc("PATCH /api/v1/admin/anti-abuse/events/{id}", s.requirePermission("security.anti-abuse.write", s.adminReviewAntiAbuseEvent))
	s.mux.HandleFunc("GET /api/v1/admin/anti-abuse/config", s.requirePermission("security.anti-abuse.read", s.adminAntiAbuseConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/anti-abuse/config", s.requirePermission("security.anti-abuse.write", s.adminAntiAbuseConfig))
	s.mux.HandleFunc("POST /api/v1/admin/anti-abuse/config/reset", s.requirePermission("security.anti-abuse.write", s.adminResetAntiAbuseConfig))
	s.mux.HandleFunc("PATCH /api/v1/admin/anti-abuse/users/{id}", s.requirePermission("security.anti-abuse.write", s.adminUpdateAntiAbuseUserState))
	s.mux.HandleFunc("GET /api/v1/admin/anti-abuse/restrictions", s.requirePermission("security.anti-abuse.read", s.adminAntiAbuseRestrictions))
	s.mux.HandleFunc("POST /api/v1/admin/anti-abuse/restrictions", s.requirePermission("security.anti-abuse.write", s.adminAntiAbuseRestrictions))
	s.mux.HandleFunc("PATCH /api/v1/admin/anti-abuse/restrictions/{id}", s.requirePermission("security.anti-abuse.write", s.adminLiftAntiAbuseRestriction))
	s.mux.HandleFunc("GET /api/v1/admin/anti-abuse/bot-rules", s.requirePermission("security.anti-abuse.read", s.adminAntiAbuseBotRules))
	s.mux.HandleFunc("POST /api/v1/admin/anti-abuse/bot-rules", s.requirePermission("security.anti-abuse.write", s.adminAntiAbuseBotRules))
	s.mux.HandleFunc("DELETE /api/v1/admin/anti-abuse/bot-rules/{id}", s.requirePermission("security.anti-abuse.write", s.adminDeleteAntiAbuseBotRule))
	s.mux.HandleFunc("GET /api/v1/admin/mod-content-reviews", s.requirePermission("content.review", s.adminModContentReviews))
	s.mux.HandleFunc("GET /api/v1/admin/unresolved-references", s.requirePermission("reference.unresolved.read", s.adminUnresolvedReferences))
	s.mux.HandleFunc("GET /api/v1/admin/creator-claims", s.requirePermission("creator.claim.review", s.adminCreatorClaims))
	s.mux.HandleFunc("PATCH /api/v1/admin/creator-claims/{id}", s.requirePermission("creator.claim.review", s.reviewCreatorClaim))
	s.mux.HandleFunc("POST /api/v1/admin/creator-claims/{id}/attachments/{fileId}/presign", s.requirePermission("creator.claim.review", s.presignCreatorClaimAttachment))
	s.mux.HandleFunc("GET /api/v1/admin/activity", s.requirePermission("activity.read", s.adminActivityEvents))
	s.mux.HandleFunc("GET /api/v1/admin/economy/config", s.requirePermission("economy.read", s.adminEconomyConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/economy/config", s.requirePermission("economy.write", s.updateEconomyConfig))
	s.mux.HandleFunc("GET /api/v1/admin/economy/currencies", s.requirePermission("economy.read", s.adminCurrencies))
	s.mux.HandleFunc("POST /api/v1/admin/economy/currencies", s.requirePermission("economy.write", s.createCurrency))
	s.mux.HandleFunc("PUT /api/v1/admin/economy/currencies/{publicId}", s.requirePermission("economy.write", s.updateCurrency))
	s.mux.HandleFunc("GET /api/v1/admin/shop/items", s.requirePermission("shop.read", s.adminShopItems))
	s.mux.HandleFunc("POST /api/v1/admin/shop/items", s.requirePermission("shop.write", s.createShopItem))
	s.mux.HandleFunc("PUT /api/v1/admin/shop/items/{publicId}", s.requirePermission("shop.write", s.updateShopItem))
	s.mux.HandleFunc("GET /api/v1/admin/levels/config", s.requirePermission("level.read", s.adminLevelConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/levels/config", s.requirePermission("level.write", s.updateLevelConfig))
	s.mux.HandleFunc("GET /api/v1/admin/tasks", s.requirePermission("task.read", s.adminTasks))
	s.mux.HandleFunc("POST /api/v1/admin/tasks", s.requirePermission("task.write", s.createTask))
	s.mux.HandleFunc("PUT /api/v1/admin/tasks/{publicId}", s.requirePermission("task.write", s.updateTask))
	s.mux.HandleFunc("DELETE /api/v1/admin/tasks/{publicId}", s.requirePermission("task.write", s.deleteTask))
	s.mux.HandleFunc("PATCH /api/v1/admin/export-revisions/{revisionId}/activate", s.requirePermission("project.review", s.activateModExportRevision))
	s.mux.HandleFunc("POST /api/v1/admin/oss/uploads/presign", s.requirePermission("oss.write", s.createOSSDirectUpload))
	s.mux.HandleFunc("POST /api/v1/admin/oss/uploads/complete", s.requirePermission("oss.write", s.completeOSSDirectUpload))
	s.mux.HandleFunc("GET /api/v1/admin/oss/files", s.requirePermission("oss.read", s.ossFiles))
	s.mux.HandleFunc("PATCH /api/v1/admin/oss/files/{publicId}/scan", s.requirePermission("oss.write", s.updateOSSFileScanStatus))
	s.mux.HandleFunc("POST /api/v1/admin/oss/files/presign", s.requirePermission("oss.read", s.presignOSSFile))
	s.mux.HandleFunc("GET /api/v1/admin/oss/uploads", s.requirePermission("oss.read", s.ossUploadLogs))
	s.mux.HandleFunc("GET /api/v1/admin/oss/scans", s.requirePermission("oss.read", s.ossScanLogs))
	s.mux.HandleFunc("GET /api/v1/admin/oss/downloads", s.requirePermission("oss.read", s.ossDownloadStats))
	s.mux.HandleFunc("GET /api/v1/admin/logs", s.requirePermission("log.read", s.adminLogs))
	s.mux.HandleFunc("GET /api/v1/admin/logs/config", s.requirePermission("log.read", s.getLogConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/logs/config", s.requirePermission("log.write", s.updateLogConfig))
	s.mux.HandleFunc("GET /api/v1/admin/activity-logs/retention", s.requirePermission("log.read", s.getActivityRetentionConfig))
	s.mux.HandleFunc("GET /api/v1/admin/activity-logs/ingestion", s.requirePermission("log.read", s.activityIngestionStatus))
	s.mux.HandleFunc("PUT /api/v1/admin/activity-logs/retention", s.requirePermission("log.write", s.updateActivityRetentionConfig))
	s.mux.HandleFunc("POST /api/v1/admin/activity-logs/cleanup/preview", s.requirePermission("log.write", s.previewActivityCleanup))
	s.mux.HandleFunc("POST /api/v1/admin/activity-logs/cleanup/execute", s.requirePermission("log.write", s.executeActivityCleanup))
	s.mux.HandleFunc("GET /api/v1/admin/ai/config", s.requirePermission("ai.read", s.getAIConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/ai/config", s.requirePermission("ai.write", s.updateAIConfig))
	s.mux.HandleFunc("GET /api/v1/admin/ai/tasks", s.requirePermission("ai.read", s.adminAITasks))
	s.mux.HandleFunc("GET /api/v1/admin/ai/tasks/{id}", s.requirePermission("ai.read", s.adminAITask))
	s.mux.HandleFunc("POST /api/v1/admin/ai/tasks", s.requirePermission("ai.task.enqueue", s.createAITask))
	s.mux.HandleFunc("GET /api/v1/admin/ai/stats", s.requirePermission("ai.read", s.adminAIStats))
	s.mux.HandleFunc("POST /api/v1/admin/notifications", s.requirePermission("notification.system.publish", s.publishSystemNotification))
	s.mux.HandleFunc("DELETE /api/v1/admin/notifications/{id}", s.requirePermission("notification.system.publish", s.deleteSystemNotification))
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	s.liveRequests.Add(1)
	writeJSON(w, http.StatusOK, map[string]any{"status": "alive", "app": "mcmods-cn-backend"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	s.readyRequests.Add(1)
	statusCode := http.StatusOK
	status := "ready"
	dependencies := map[string]string{
		"postgresql": "ready",
		"redis":      "disabled",
		"nats":       "disabled",
		"search":     "disabled",
	}
	dbCtx, dbCancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer dbCancel()
	s.databasePings.Add(1)
	if s.db == nil || s.db.Ping(dbCtx) != nil {
		dependencies["postgresql"] = "unavailable"
		status, statusCode = "not_ready", http.StatusServiceUnavailable
	}
	if s.cfg.Redis.Enabled {
		dependencies["redis"] = "degraded"
		redisCtx, redisCancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
		err := s.cache.Ping(redisCtx)
		redisCancel()
		if err == nil {
			dependencies["redis"] = "ready"
		}
	}
	if s.queue != nil {
		queueStatus := s.queue.Status()
		if queueStatus.Enabled {
			dependencies["nats"] = "degraded"
			if queueStatus.Connected {
				dependencies["nats"] = "ready"
			}
		}
	}
	searchStatus := "disabled"
	if s.search != nil && s.search.Enabled() {
		searchStatus = "initializing"
		if s.search.Ready() {
			searchStatus = "ready"
		} else {
			searchStatus = "degraded"
		}
	}
	dependencies["search"] = searchStatus
	writeJSON(w, statusCode, map[string]any{
		"status":       status,
		"app":          "mcmods-cn-backend",
		"env":          s.cfg.Env,
		"dependencies": dependencies,
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin == s.cfg.FrontendOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
		w.Header().Set("Access-Control-Expose-Headers", corsExposedHeaders)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cookieRequestOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if _, err := r.Cookie(authSessionCookieName); err == nil && r.Header.Get("Authorization") == "" {
				origin := r.Header.Get("Origin")
				if origin != s.cfg.FrontendOrigin {
					writeError(w, http.StatusForbidden, "request origin is not allowed")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-site")
		next.ServeHTTP(w, r)
	})
}
