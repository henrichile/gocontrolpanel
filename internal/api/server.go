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
	cfg    *config.Config
	st     *store.Store
	svc    *provision.Service
	tokens *auth.TokenIssuer
	caddy  *caddyapi.Client
	webFS  fs.FS // build de la SPA, embebido en el binario
}

func NewServer(cfg *config.Config, st *store.Store, svc *provision.Service,
	tokens *auth.TokenIssuer, caddy *caddyapi.Client, webFS fs.FS) *Server {
	return &Server{cfg: cfg, st: st, svc: svc, tokens: tokens, caddy: caddy, webFS: webFS}
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
		})
		r.Get("/health", s.handleHealth)
		// Endpoint que consulta Caddy antes de emitir un certificado on-demand.
		r.Get("/tls/authorize", s.handleTLSAuthorize)

		// --- Autenticado ---
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/password", s.handleChangeOwnPassword)

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

			// Bases de datos
			r.Get("/accounts/{accountID}/databases", s.handleListDatabases)
			r.Post("/accounts/{accountID}/databases", s.handleCreateDatabase)
			r.Delete("/databases/{databaseID}", s.handleDeleteDatabase)

			// Acceso SFTP
			r.Get("/accounts/{accountID}/ftp", s.handleListFTP)
			r.Post("/accounts/{accountID}/ftp", s.handleCreateFTP)
			r.Delete("/ftp/{ftpID}", s.handleDeleteFTP)

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
