package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/mailer"
	"mcmods-cn-backend/internal/queue"
)

type Server struct {
	cfg    config.Config
	db     *pgxpool.Pool
	mailer mailer.Mailer
	queue  *queue.Client
	mux    *http.ServeMux
}

func NewServer(cfg config.Config, db *pgxpool.Pool, queueClient *queue.Client) http.Handler {
	server := &Server{
		cfg:    cfg,
		db:     db,
		mailer: mailer.New(cfg.SMTP),
		queue:  queueClient,
		mux:    http.NewServeMux(),
	}
	server.routes()
	return server.cors(server.logAccess(server.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)
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
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.logout))
	s.mux.HandleFunc("GET /api/v1/location", s.visitorLocation)
	s.mux.HandleFunc("GET /api/v1/markdown/config", s.publicMarkdownConfig)
	s.mux.HandleFunc("GET /api/v1/minecraft/versions", s.publicMinecraftVersions)
	s.mux.HandleFunc("GET /api/v1/mods", s.optionalAuth(s.publicMods))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}", s.optionalAuth(s.publicModDetail))
	s.mux.HandleFunc("POST /api/v1/mods", s.requirePermission("project.create", s.createMod))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/revisions", s.optionalAuth(s.modRevisionHistory))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/revisions", s.requireAuth(s.submitModRevision))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/revisions/compare", s.optionalAuth(s.compareModRevisions))
	s.mux.HandleFunc("PATCH /api/v1/mods/{siteId}/revisions/{revisionId}", s.requirePermission("project.review", s.reviewModRevision))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/data", s.optionalAuth(s.modDataPages))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/data/versions", s.requireAuth(s.createModDataVersion))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/data", s.requireAuth(s.createModDataPage))
	s.mux.HandleFunc("PATCH /api/v1/mods/{siteId}/data/{dataId}", s.requirePermission("project.review", s.reviewModDataPage))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/applications", s.requireAuth(s.modApplications))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/applications", s.requireAuth(s.modApplications))
	s.mux.HandleFunc("GET /api/v1/mods/{siteId}/comments", s.optionalAuth(s.modComments))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/comments", s.requireAuth(s.modComments))
	s.mux.HandleFunc("POST /api/v1/mods/{siteId}/comments/{commentId}/reactions", s.requireAuth(s.reactToModComment))
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
	s.mux.HandleFunc("GET /api/v1/users/{id}/profile", s.optionalAuth(s.userProfile))
	s.mux.HandleFunc("POST /api/v1/users/{id}/follow", s.requirePermission("user.follow.create", s.followUser))
	s.mux.HandleFunc("DELETE /api/v1/users/{id}/follow", s.requirePermission("user.follow.create", s.unfollowUser))
	s.mux.HandleFunc("GET /api/v1/messages/conversations", s.requireAuth(s.conversations))
	s.mux.HandleFunc("POST /api/v1/messages/conversations", s.requirePermission("user.message.send", s.startConversation))
	s.mux.HandleFunc("GET /api/v1/messages/conversations/{id}", s.requireAuth(s.conversationMessages))
	s.mux.HandleFunc("POST /api/v1/messages/conversations/{id}", s.requirePermission("user.message.send", s.sendConversationMessage))
	s.mux.HandleFunc("PUT /api/v1/messages/conversations/{id}/presence", s.requireAuth(s.updateConversationPresence))
	s.mux.HandleFunc("GET /api/v1/notifications", s.requireAuth(s.notifications))
	s.mux.HandleFunc("GET /api/v1/notifications/unread", s.requireAuth(s.unreadSummary))
	s.mux.HandleFunc("POST /api/v1/notifications/{id}/read", s.requireAuth(s.markNotificationRead))
	s.mux.HandleFunc("GET /api/v1/notifications/ai-balance", s.requireAuth(s.aiDailyBalance))
	s.mux.HandleFunc("POST /api/v1/notifications/{id}/translate", s.requirePermission("notification.translate", s.translateNotification))
	s.mux.HandleFunc("GET /api/v1/notifications/translations/{id}", s.requirePermission("notification.translate", s.notificationTranslationResult))

	s.mux.HandleFunc("GET /api/v1/admin/dashboard", s.requirePermission("admin.access", s.adminDashboard))
	s.mux.HandleFunc("GET /api/v1/admin/nav", s.requirePermission("admin.access", s.adminNav))
	s.mux.HandleFunc("GET /api/v1/admin/config", s.requirePermission("admin.config.read", s.adminConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/markdown", s.requirePermission("admin.config.read", s.adminMarkdownConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/markdown", s.requirePermission("admin.config.write", s.updateMarkdownConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/profile", s.requirePermission("admin.config.write", s.updateProfileConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/minecraft-versions", s.requirePermission("admin.config.write", s.updateMinecraftVersions))
	s.mux.HandleFunc("PUT /api/v1/admin/config/mail", s.requirePermission("mail.write", s.updateMailConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/oauth", s.requirePermission("admin.config.write", s.updateOAuthConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/nats", s.requirePermission("admin.config.read", s.getNATSConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/nats", s.requirePermission("admin.config.write", s.updateNATSConfig))
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
	s.mux.HandleFunc("POST /api/v1/admin/users/{id}/role-tracks/{code}/upgrade", s.requirePermission("permission.write", s.upgradeUserRoleTrack))
	s.mux.HandleFunc("POST /api/v1/admin/users/{id}/role-tracks/{code}/downgrade", s.requirePermission("permission.write", s.downgradeUserRoleTrack))
	s.mux.HandleFunc("PUT /api/v1/admin/users/{id}/roles", s.requirePermission("permission.write", s.updateUserRoles))
	s.mux.HandleFunc("GET /api/v1/admin/users/{id}/permissions", s.requirePermission("permission.read", s.userPermissionDetails))
	s.mux.HandleFunc("PUT /api/v1/admin/users/{id}/permissions", s.requirePermission("permission.write", s.updateUserPermissions))
	s.mux.HandleFunc("GET /api/v1/admin/mod-applications", s.requirePermission("project.review", s.adminModApplications))
	s.mux.HandleFunc("PATCH /api/v1/admin/mod-applications/{id}", s.requirePermission("project.review", s.reviewModApplication))
	s.mux.HandleFunc("POST /api/v1/admin/mod-applications/{id}/attachments/{fileId}/presign", s.requirePermission("project.review", s.presignModApplicationAttachment))
	s.mux.HandleFunc("GET /api/v1/admin/mod-content-reviews", s.requirePermission("project.review", s.adminModContentReviews))
	s.mux.HandleFunc("POST /api/v1/admin/oss/uploads/presign", s.requirePermission("oss.write", s.createOSSDirectUpload))
	s.mux.HandleFunc("POST /api/v1/admin/oss/uploads/complete", s.requirePermission("oss.write", s.completeOSSDirectUpload))
	s.mux.HandleFunc("GET /api/v1/admin/oss/files", s.requirePermission("oss.read", s.ossFiles))
	s.mux.HandleFunc("POST /api/v1/admin/oss/files/presign", s.requirePermission("oss.read", s.presignOSSFile))
	s.mux.HandleFunc("GET /api/v1/admin/oss/uploads", s.requirePermission("oss.read", s.ossUploadLogs))
	s.mux.HandleFunc("GET /api/v1/admin/oss/scans", s.requirePermission("oss.read", s.ossScanLogs))
	s.mux.HandleFunc("GET /api/v1/admin/oss/downloads", s.requirePermission("oss.read", s.ossDownloadStats))
	s.mux.HandleFunc("GET /api/v1/admin/logs", s.requirePermission("log.read", s.adminLogs))
	s.mux.HandleFunc("GET /api/v1/admin/logs/config", s.requirePermission("log.read", s.getLogConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/logs/config", s.requirePermission("log.write", s.updateLogConfig))
	s.mux.HandleFunc("GET /api/v1/admin/ai/config", s.requirePermission("ai.read", s.getAIConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/ai/config", s.requirePermission("ai.write", s.updateAIConfig))
	s.mux.HandleFunc("GET /api/v1/admin/ai/tasks", s.requirePermission("ai.read", s.adminAITasks))
	s.mux.HandleFunc("GET /api/v1/admin/ai/tasks/{id}", s.requirePermission("ai.read", s.adminAITask))
	s.mux.HandleFunc("POST /api/v1/admin/ai/tasks", s.requirePermission("ai.task.enqueue", s.createAITask))
	s.mux.HandleFunc("GET /api/v1/admin/ai/stats", s.requirePermission("ai.read", s.adminAIStats))
	s.mux.HandleFunc("POST /api/v1/admin/notifications", s.requirePermission("notification.system.publish", s.publishSystemNotification))
	s.mux.HandleFunc("DELETE /api/v1/admin/notifications/{id}", s.requirePermission("notification.system.publish", s.deleteSystemNotification))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"app":    "mcmods-cn-backend",
		"env":    s.cfg.Env,
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == s.cfg.FrontendOrigin || (s.cfg.Env == "development" && origin != "") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
