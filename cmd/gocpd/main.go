// Comando gocpd: demonio del panel de control.
//
//	gocpd                  arranca el servidor
//	gocpd migrate          aplica las migraciones y sale
//	gocpd createadmin      crea (o repone) el usuario administrador
//	gocpd synccaddy        regenera la configuración del Caddy de borde
//	gocpd version          muestra la versión
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/etasoft/gocontrolpanel/internal/api"
	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/caddyapi"
	"github.com/etasoft/gocontrolpanel/internal/config"
	"github.com/etasoft/gocontrolpanel/internal/database"
	"github.com/etasoft/gocontrolpanel/internal/dockerx"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/provision"
	"github.com/etasoft/gocontrolpanel/internal/store"
	"github.com/etasoft/gocontrolpanel/internal/worker"
	"github.com/etasoft/gocontrolpanel/web"
)

var version = "dev" // se inyecta con -ldflags en la compilación

func main() {
	if err := run(); err != nil {
		slog.Error("el panel no pudo arrancar", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if cmd == "version" {
		fmt.Println("gocpd", version)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogging(cfg)

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.DBMigrateOnBoot || cmd == "migrate" {
		if err := database.Migrate(ctx, pool); err != nil {
			return err
		}
	}
	if cmd == "migrate" {
		slog.Info("migraciones aplicadas")
		return nil
	}

	st := store.New(pool)

	if cmd == "createadmin" {
		return createAdmin(ctx, st, cfg)
	}

	docker, err := dockerx.New(cfg.DockerHost, cfg.DockerNetwork)
	if err != nil {
		return err
	}
	defer docker.Close()

	if err := docker.Ping(ctx); err != nil {
		return err
	}
	if err := docker.EnsureNetwork(ctx); err != nil {
		return fmt.Errorf("creando la red de sitios: %w", err)
	}

	caddy := caddyapi.NewClient(cfg.CaddyAdminURL)
	if err := caddy.Ping(ctx); err != nil {
		slog.Warn("el Caddy de borde no responde; el enrutado no se publicará "+
			"hasta que esté disponible", "error", err)
	}

	mysqlMgr, err := provision.NewMySQLManager(cfg.MySQLDSN, cfg.MySQLHost)
	if err != nil {
		slog.Warn("MySQL no disponible; la gestión de bases de datos quedará desactivada",
			"error", err)
	}
	defer mysqlMgr.Close()

	sftpMgr := provision.NewSFTPManager(
		cfg.SFTPAdminURL, cfg.SFTPAdminUser, cfg.SFTPAdminPassword,
		cfg.SFTPPublicHost, cfg.SFTPPublicPort,
	)
	if sftpMgr == nil {
		slog.Warn("SFTP no configurado; el acceso por SFTP a las cuentas quedará desactivado")
	}

	mailHostname := cfg.MailHostname
	if mailHostname == "" {
		mailHostname = "mail." + provision.PanelHost(cfg.PublicURL)
	}
	mailMgr := provision.NewMailManager(docker, cfg.MailEnabled, cfg.MailContainerName, mailHostname)
	if cfg.MailEnabled && mailMgr == nil {
		slog.Warn("correo habilitado pero sin hostname configurado; la gestión de buzones quedará desactivada")
	}

	svc := provision.New(cfg, st, docker, caddy, mysqlMgr, sftpMgr, mailMgr)

	if cmd == "synccaddy" {
		if err := svc.SyncCaddy(ctx); err != nil {
			return err
		}
		slog.Info("configuración de Caddy publicada")
		return nil
	}

	// Primer arranque: si no hay usuarios, creamos el administrador.
	if err := bootstrapAdmin(ctx, st, cfg); err != nil {
		return err
	}

	if err := svc.SyncCaddy(ctx); err != nil {
		slog.Warn("no se pudo publicar la configuración inicial de Caddy", "error", err)
	}

	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	worker.New(cfg, st, svc).Start(ctx)

	srv := api.NewServer(cfg, st, svc, tokens, caddy, web.Assets())
	slog.Info("GoControlPanel escuchando",
		"addr", cfg.ListenAddr, "env", cfg.Environment, "version", version)

	return srv.Run(ctx)
}

func setupLogging(cfg *config.Config) {
	level := slog.LevelDebug
	if cfg.IsProduction() {
		level = slog.LevelInfo
	}
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if cfg.IsProduction() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// bootstrapAdmin crea el primer administrador si la instalación está vacía.
func bootstrapAdmin(ctx context.Context, st *store.Store, cfg *config.Config) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if cfg.BootstrapAdminPassword == "" {
		slog.Warn("no hay usuarios y GOCP_ADMIN_PASSWORD está vacío; " +
			"ejecuta `gocpd createadmin` para crear el primer acceso")
		return nil
	}
	return createAdmin(ctx, st, cfg)
}

func createAdmin(ctx context.Context, st *store.Store, cfg *config.Config) error {
	password := cfg.BootstrapAdminPassword
	if password == "" {
		return errors.New("define GOCP_ADMIN_PASSWORD para crear el administrador")
	}
	hash, err := auth.HashPassword(password, cfg.BcryptCost)
	if err != nil {
		return err
	}

	if existing, err := st.GetUserByLogin(ctx, cfg.BootstrapAdminUser); err == nil {
		if err := st.UpdatePassword(ctx, existing.ID, hash); err != nil {
			return err
		}
		slog.Info("contraseña del administrador actualizada", "user", existing.Username)
		return nil
	}

	admin := &models.User{
		Username:     cfg.BootstrapAdminUser,
		Email:        cfg.BootstrapAdminEmail,
		PasswordHash: hash,
		FullName:     "Administrador",
		Role:         models.RoleAdmin,
		IsActive:     true,
	}
	if err := st.CreateUser(ctx, admin); err != nil {
		return err
	}
	slog.Info("administrador creado", "user", admin.Username, "email", admin.Email)
	return nil
}
