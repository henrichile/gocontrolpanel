// Package api expone la API REST del panel y sirve la SPA.
package api

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/caddyapi"
	"github.com/etasoft/gocontrolpanel/internal/config"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/provision"
	"github.com/etasoft/gocontrolpanel/internal/store"
)

type Server struct {
	cfg          *config.Config
	st           *store.Store
	svc          *provision.Service
	tokens       *auth.TokenIssuer
	caddy        *caddyapi.Client
	webFS        fs.FS // build de la SPA, embebido en el binario
	loginLockout *rateLimiter
}

func NewServer(cfg *config.Config, st *store.Store, svc *provision.Service,
	tokens *auth.TokenIssuer, caddy *caddyapi.Client, webFS fs.FS) *Server {
	return &Server{
		cfg: cfg, st: st, svc: svc, tokens: tokens, caddy: caddy, webFS: webFS,
		// Bloqueo por cuenta (no por IP): 6 intentos cada 15 minutos, sea
		// login con contraseña o verificación del código TOTP.
		loginLockout: newRateLimiter(6, 15*time.Minute),
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(s.requestLogger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(120 * time.Second))
	r.Use(securityHeaders)
	r.Use(s.cors)

	r.Route("/api/v1", func(r chi.Router) {
		// --- Público ---
		r.Group(func(r chi.Router) {
			r.Use(newRateLimiter(20, time.Minute).middleware)
			r.Post("/auth/login", s.handleLogin)
			r.Post("/auth/refresh", s.handleRefresh)
			r.Post("/auth/totp/verify", s.handleTOTPVerify)
		})
		r.Get("/health", s.handleHealth)
		// Endpoint que consulta Caddy antes de emitir un certificado on-demand.
		r.Get("/tls/authorize", s.handleTLSAuthorize)
		// Webhook de push (GitHub/GitLab/genérico): se autentica con el
		// secreto propio del sitio, no con sesión.
		r.Group(func(r chi.Router) {
			r.Use(newRateLimiter(30, time.Minute).middleware)
			r.Post("/webhooks/git/{siteID}", s.handleGitWebhook)
		})

		// --- Autenticado ---
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/password", s.handleChangeOwnPassword)
			r.Post("/auth/2fa/setup", s.handleTOTPSetup)
			r.Post("/auth/2fa/confirm", s.handleTOTPConfirm)
			r.Post("/auth/2fa/disable", s.handleTOTPDisable)

			r.Get("/overview", s.handleOverview)

			// Planes
			r.Get("/plans", s.handleListPlans)
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(models.RoleAdmin))
				r.Post("/plans", s.handleCreatePlan)
				r.Put("/plans/{planID}", s.handleUpdatePlan)
				r.Delete("/plans/{planID}", s.handleDeletePlan)
			})

			// Usuarios (admin y resellers)
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(models.RoleReseller))
				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Put("/users/{userID}", s.handleUpdateUser)
				r.Post("/users/{userID}/password", s.handleResetPassword)
				r.Delete("/users/{userID}", s.handleDeleteUser)
			})

			// Cuentas de hosting
			r.Get("/accounts", s.handleListAccounts)
			r.Get("/accounts/{accountID}", s.handleGetAccount)
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(models.RoleReseller))
				r.Post("/accounts", s.handleCreateAccount)
				r.Post("/accounts/{accountID}/suspend", s.handleSuspendAccount)
				r.Post("/accounts/{accountID}/unsuspend", s.handleUnsuspendAccount)
				r.Put("/accounts/{accountID}/plan", s.handleChangeAccountPlan)
				r.Delete("/accounts/{accountID}", s.handleTerminateAccount)
			})

			// Sitios
			r.Get("/sites", s.handleListSites)
			r.Post("/sites", s.handleCreateSite)
			r.Get("/sites/{siteID}", s.handleGetSite)
			r.Put("/sites/{siteID}", s.handleUpdateSite)
			r.Delete("/sites/{siteID}", s.handleDeleteSite)
			r.Post("/sites/{siteID}/start", s.handleSiteStart)
			r.Post("/sites/{siteID}/stop", s.handleSiteStop)
			r.Post("/sites/{siteID}/restart", s.handleSiteRestart)
			r.Post("/sites/{siteID}/redeploy", s.handleSiteRedeploy)
			r.Get("/sites/{siteID}/logs", s.handleSiteLogs)
			r.Get("/sites/{siteID}/stats", s.handleSiteStats)
			r.Get("/sites/{siteID}/usage", s.handleSiteUsage)

			// Dominios
			r.Post("/sites/{siteID}/domains", s.handleAddDomain)
			r.Delete("/domains/{domainID}", s.handleDeleteDomain)

			// Deploy por Git
			r.Get("/sites/{siteID}/git", s.handleGetSiteGit)
			r.Post("/sites/{siteID}/git", s.handleConnectSiteGit)
			r.Post("/sites/{siteID}/git/deploy", s.handleSiteGitDeploy)
			r.Delete("/sites/{siteID}/git", s.handleDisconnectSiteGit)

			// Bases de datos
			r.Get("/accounts/{accountID}/databases", s.handleListDatabases)
			r.Post("/accounts/{accountID}/databases", s.handleCreateDatabase)
			r.Delete("/databases/{databaseID}", s.handleDeleteDatabase)

			// Acceso SFTP
			r.Get("/accounts/{accountID}/ftp", s.handleListFTP)
			r.Post("/accounts/{accountID}/ftp", s.handleCreateFTP)
			r.Delete("/ftp/{ftpID}", s.handleDeleteFTP)

			// Correo propio de dominios de clientes (buzones + webmail)
			r.Get("/mail/info", s.handleMailInfo)
			r.Get("/accounts/{accountID}/mail/domains", s.handleListMailDomains)
			r.Post("/accounts/{accountID}/mail/domains/{domain}/enable", s.handleEnableMailDomain)
			r.Post("/accounts/{accountID}/mail/domains/{domain}/verify", s.handleVerifyMailDomain)
			r.Get("/accounts/{accountID}/mail/mailboxes", s.handleListMailboxes)
			r.Post("/accounts/{accountID}/mail/mailboxes", s.handleCreateMailbox)
			r.Delete("/mail/mailboxes/{mailboxID}", s.handleDeleteMailbox)
			r.Post("/mail/mailboxes/{mailboxID}/password", s.handleChangeMailboxPassword)

			// Backups (archivos + bases de datos) por cuenta
			r.Get("/accounts/{accountID}/backups", s.handleListBackups)
			r.Post("/accounts/{accountID}/backups", s.handleCreateBackup)
			r.Get("/accounts/{accountID}/backups/download", s.handleDownloadBackup)
			r.Delete("/accounts/{accountID}/backups", s.handleDeleteBackup)

			// Explorador de archivos (misma raíz que ve el acceso SFTP)
			r.Get("/accounts/{accountID}/files", s.handleListFiles)
			r.Post("/accounts/{accountID}/files/upload", s.handleUploadFiles)
			r.Get("/accounts/{accountID}/files/download", s.handleDownloadFile)
			r.Get("/accounts/{accountID}/files/content", s.handleReadFileContent)
			r.Put("/accounts/{accountID}/files/content", s.handleWriteFileContent)
			r.Delete("/accounts/{accountID}/files", s.handleDeleteFile)
			r.Post("/accounts/{accountID}/files/mkdir", s.handleMkdir)
			r.Post("/accounts/{accountID}/files/rename", s.handleRenameFile)
			r.Post("/accounts/{accountID}/files/extract", s.handleExtractZip)

			// Cron
			r.Get("/sites/{siteID}/cron", s.handleListCron)
			r.Post("/sites/{siteID}/cron", s.handleCreateCron)
			r.Delete("/cron/{cronID}", s.handleDeleteCron)

			// Sistema
			r.Group(func(r chi.Router) {
				r.Use(s.requireRole(models.RoleAdmin))
				r.Get("/system/info", s.handleSystemInfo)
				r.Get("/system/audit", s.handleAudit)
				r.Get("/system/caddy", s.handleCaddyConfig)
				r.Post("/system/caddy/sync", s.handleCaddySync)
				r.Get("/system/security", s.handleGetSecuritySettings)
				r.Put("/system/security", s.handleUpdateSecuritySettings)
				r.Get("/system/security/firewall", s.handleGetFirewall)
				r.Post("/system/security/firewall/rules", s.handleSetFirewallRule)
				r.Get("/system/security/waf-blocks", s.handleListWAFBlocks)
				r.Get("/system/security/waf-blocks/stream", s.handleStreamWAFBlocks)

				// Correo saliente: configuración SMTP y plantillas editables
				r.Get("/system/mail/smtp", s.handleGetSMTPSettings)
				r.Put("/system/mail/smtp", s.handleUpdateSMTPSettings)
				r.Post("/system/mail/smtp/test", s.handleTestSMTP)
				r.Get("/system/mail/templates", s.handleListEmailTemplates)
				r.Get("/system/mail/templates/{key}", s.handleGetEmailTemplate)
				r.Put("/system/mail/templates/{key}", s.handleUpdateEmailTemplate)

				// Estado del servidor de correo gestionado (docker-mailserver):
				// no confundir con /system/mail/smtp, que es el correo saliente
				// del propio panel hacia sus usuarios.
				r.Get("/system/mailserver/status", s.handleMailServerStatus)
			})
		})
	})

	// SPA: todo lo que no sea /api se resuelve contra el build de React.
	r.NotFound(s.serveSPA)

	return r
}

// Run arranca el servidor HTTP y lo apaga de forma ordenada.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // 0 para permitir streaming de logs (SSE)
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
