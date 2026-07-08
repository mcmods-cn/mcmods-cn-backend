package httpapi

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/mailer"
)

type Server struct {
	cfg    config.Config
	db     *pgxpool.Pool
	mailer mailer.Mailer
	mux    *http.ServeMux
}

func NewServer(cfg config.Config, db *pgxpool.Pool) http.Handler {
	server := &Server{
		cfg:    cfg,
		db:     db,
		mailer: mailer.New(cfg.SMTP),
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
	s.mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.me))
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.requireAuth(s.logout))

	s.mux.HandleFunc("GET /api/v1/admin/dashboard", s.requirePermission("admin.access", s.adminDashboard))
	s.mux.HandleFunc("GET /api/v1/admin/nav", s.requirePermission("admin.access", s.adminNav))
	s.mux.HandleFunc("GET /api/v1/admin/config", s.requirePermission("admin.config.read", s.adminConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/mail", s.requirePermission("mail.write", s.updateMailConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/oauth", s.requirePermission("admin.config.write", s.updateOAuthConfig))
	s.mux.HandleFunc("GET /api/v1/admin/config/oss", s.requirePermission("oss.read", s.getOSSConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/config/oss", s.requirePermission("oss.write", s.updateOSSConfig))
	s.mux.HandleFunc("POST /api/v1/admin/mail/test", s.requirePermission("mail.write", s.sendTestMail))
	s.mux.HandleFunc("GET /api/v1/admin/permissions", s.requirePermission("permission.read", s.permissionCatalog))
	s.mux.HandleFunc("POST /api/v1/admin/permissions", s.requirePermission("permission.write", s.createPermission))
	s.mux.HandleFunc("POST /api/v1/admin/roles", s.requirePermission("permission.write", s.createRole))
	s.mux.HandleFunc("PUT /api/v1/admin/roles/{code}", s.requirePermission("permission.write", s.updateRole))
	s.mux.HandleFunc("DELETE /api/v1/admin/roles/{code}", s.requirePermission("permission.write", s.deleteRole))
	s.mux.HandleFunc("GET /api/v1/admin/users", s.requirePermission("user.read", s.adminUsers))
	s.mux.HandleFunc("POST /api/v1/admin/users", s.requirePermission("user.write", s.createAdminUser))
	s.mux.HandleFunc("PUT /api/v1/admin/users/{id}/roles", s.requirePermission("permission.write", s.updateUserRoles))
	s.mux.HandleFunc("GET /api/v1/admin/users/{id}/permissions", s.requirePermission("permission.read", s.userPermissionDetails))
	s.mux.HandleFunc("PUT /api/v1/admin/users/{id}/permissions", s.requirePermission("permission.write", s.updateUserPermissions))
	s.mux.HandleFunc("POST /api/v1/admin/oss/upload", s.requirePermission("oss.write", s.uploadOSSFile))
	s.mux.HandleFunc("GET /api/v1/admin/oss/files", s.requirePermission("oss.read", s.ossFiles))
	s.mux.HandleFunc("POST /api/v1/admin/oss/files/presign", s.requirePermission("oss.read", s.presignOSSFile))
	s.mux.HandleFunc("GET /api/v1/admin/oss/uploads", s.requirePermission("oss.read", s.ossUploadLogs))
	s.mux.HandleFunc("GET /api/v1/admin/oss/scans", s.requirePermission("oss.read", s.ossScanLogs))
	s.mux.HandleFunc("GET /api/v1/admin/oss/downloads", s.requirePermission("oss.read", s.ossDownloadStats))
	s.mux.HandleFunc("GET /api/v1/admin/logs", s.requirePermission("log.read", s.adminLogs))
	s.mux.HandleFunc("GET /api/v1/admin/logs/config", s.requirePermission("log.read", s.getLogConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/logs/config", s.requirePermission("log.write", s.updateLogConfig))
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
