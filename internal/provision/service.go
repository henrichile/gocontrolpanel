// Package provision contiene la lógica de negocio del panel: crear cuentas,
// levantar contenedores FrankenPHP y mantener sincronizado el Caddy de borde.
package provision

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/caddyapi"
	"github.com/etasoft/gocontrolpanel/internal/config"
	"github.com/etasoft/gocontrolpanel/internal/dockerx"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/store"
)

var (
	reSystemUser = regexp.MustCompile(`^[a-z][a-z0-9_]{2,15}$`)
	reSiteName   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}$`)
	reFQDN       = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)
)

type Service struct {
	cfg    *config.Config
	st     *store.Store
	docker *dockerx.Manager
	caddy  *caddyapi.Client
	mysql  *MySQLManager
	sftp   *SFTPManager

	// Serializa las recargas de Caddy: la Admin API acepta una config completa
	// cada vez, así que dos escrituras concurrentes se pisarían.
	caddyMu sync.Mutex
}

func New(cfg *config.Config, st *store.Store, dk *dockerx.Manager,
	cd *caddyapi.Client, my *MySQLManager, sf *SFTPManager) *Service {
	return &Service{cfg: cfg, st: st, docker: dk, caddy: cd, mysql: my, sftp: sf}
}

func (s *Service) Docker() *dockerx.Manager { return s.docker }
func (s *Service) Store() *store.Store      { return s.st }
func (s *Service) MySQL() *MySQLManager     { return s.mysql }
func (s *Service) SFTP() *SFTPManager       { return s.sftp }

// --- Errores de validación -------------------------------------------------

type ValidationError struct{ Field, Message string }

func (e ValidationError) Error() string { return e.Field + ": " + e.Message }

// --- Cuentas ---------------------------------------------------------------

type CreateAccountInput struct {
	OwnerID       uuid.UUID
	PlanID        uuid.UUID
	SystemUser    string
	PrimaryDomain string
	Notes         string
	// Si es true se crea también el sitio principal y su contenedor.
	Provision  bool
	PHPVersion string
}

func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*models.Account, error) {
	in.SystemUser = strings.ToLower(strings.TrimSpace(in.SystemUser))
	in.PrimaryDomain = normalizeDomain(in.PrimaryDomain)

	if !reSystemUser.MatchString(in.SystemUser) {
		return nil, ValidationError{"system_user",
			"debe empezar por letra minúscula y tener entre 3 y 16 caracteres [a-z0-9_]"}
	}
	if !reFQDN.MatchString(in.PrimaryDomain) {
		return nil, ValidationError{"primary_domain", "no es un dominio válido"}
	}

	plan, err := s.resolvePlan(ctx, in.PlanID)
	if err != nil {
		return nil, err
	}

	acct := &models.Account{
		OwnerID:       in.OwnerID,
		PlanID:        plan.ID,
		SystemUser:    in.SystemUser,
		PrimaryDomain: in.PrimaryDomain,
		Status:        models.AccountActive,
		Notes:         in.Notes,
	}
	if err := s.st.CreateAccount(ctx, acct); err != nil {
		return nil, err
	}

	if err := s.prepareAccountDirs(acct.SystemUser); err != nil {
		_ = s.st.DeleteAccount(ctx, acct.ID)
		return nil, fmt.Errorf("preparando el directorio de la cuenta: %w", err)
	}

	if in.Provision {
		php := in.PHPVersion
		if php == "" && len(plan.PHPVersions) > 0 {
			php = plan.PHPVersions[len(plan.PHPVersions)-1]
		}
		_, err := s.CreateSite(ctx, CreateSiteInput{
			AccountID:   acct.ID,
			Name:        "principal",
			PHPVersion:  php,
			Domain:      acct.PrimaryDomain,
			DocumentRoot: "public",
		})
		if err != nil {
			slog.Error("la cuenta se creó pero el sitio principal falló",
				"account", acct.SystemUser, "error", err)
		}
	}

	acct.Plan = plan
	return acct, nil
}

func (s *Service) SuspendAccount(ctx context.Context, id uuid.UUID, reason string) error {
	if err := s.st.UpdateAccountStatus(ctx, id, models.AccountSuspended, reason); err != nil {
		return err
	}
	sites, err := s.st.ListSites(ctx, &id)
	if err != nil {
		return err
	}
	for _, site := range sites {
		if err := s.docker.Stop(ctx, site.ContainerName); err != nil {
			slog.Warn("no se pudo detener el contenedor al suspender",
				"container", site.ContainerName, "error", err)
		}
		_ = s.st.UpdateSiteStatus(ctx, site.ID, models.SiteStopped, "cuenta suspendida")
	}
	return s.SyncCaddy(ctx)
}

func (s *Service) UnsuspendAccount(ctx context.Context, id uuid.UUID) error {
	if err := s.st.UpdateAccountStatus(ctx, id, models.AccountActive, ""); err != nil {
		return err
	}
	sites, err := s.st.ListSites(ctx, &id)
	if err != nil {
		return err
	}
	for _, site := range sites {
		if err := s.docker.Start(ctx, site.ContainerName); err != nil {
			slog.Warn("no se pudo arrancar el contenedor", "container", site.ContainerName, "error", err)
			_ = s.st.UpdateSiteStatus(ctx, site.ID, models.SiteError, err.Error())
			continue
		}
		_ = s.st.UpdateSiteStatus(ctx, site.ID, models.SiteRunning, "")
	}
	return s.SyncCaddy(ctx)
}

// TerminateAccount elimina cuenta, contenedores, bases de datos y (opcional)
// los archivos del disco.
func (s *Service) TerminateAccount(ctx context.Context, id uuid.UUID, deleteFiles bool) error {
	acct, err := s.st.GetAccount(ctx, id)
	if err != nil {
		return err
	}
	sites, err := s.st.ListSites(ctx, &id)
	if err != nil {
		return err
	}
	for _, site := range sites {
		if err := s.docker.RemoveIfExists(ctx, site.ContainerName); err != nil {
			slog.Warn("no se pudo eliminar el contenedor", "container", site.ContainerName, "error", err)
		}
	}
	if s.mysql != nil {
		dbs, err := s.st.ListDatabases(ctx, id)
		if err == nil {
			for _, db := range dbs {
				if err := s.mysql.DropDatabase(ctx, db.DBName, db.DBUser); err != nil {
					slog.Warn("no se pudo eliminar la base de datos", "db", db.DBName, "error", err)
				}
			}
		}
	}
	if s.sftp != nil {
		ftpAccs, err := s.st.ListFTP(ctx, id)
		if err == nil {
			for _, f := range ftpAccs {
				if err := s.sftp.DeleteUser(ctx, f.Username); err != nil {
					slog.Warn("no se pudo eliminar el usuario SFTP", "username", f.Username, "error", err)
				}
			}
		}
	}
	if err := s.st.DeleteAccount(ctx, id); err != nil {
		return err
	}
	if deleteFiles {
		path := filepath.Join(s.cfg.SitesRoot, acct.SystemUser)
		if err := os.RemoveAll(path); err != nil {
			slog.Warn("no se pudieron borrar los archivos", "path", path, "error", err)
		}
	}
	return s.SyncCaddy(ctx)
}

// --- Sitios ----------------------------------------------------------------

type CreateSiteInput struct {
	AccountID    uuid.UUID
	Name         string
	PHPVersion   string
	Domain       string // dominio principal del sitio
	DocumentRoot string
	WorkerScript string
	WorkerCount  int
	EnvVars      map[string]string
}

func (s *Service) CreateSite(ctx context.Context, in CreateSiteInput) (*models.Site, error) {
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	if !reSiteName.MatchString(in.Name) {
		return nil, ValidationError{"name", "usa 2-31 caracteres [a-z0-9-]"}
	}
	if in.DocumentRoot == "" {
		in.DocumentRoot = "public"
	}
	if strings.Contains(in.DocumentRoot, "..") || strings.HasPrefix(in.DocumentRoot, "/") {
		return nil, ValidationError{"document_root", "debe ser una ruta relativa sin '..'"}
	}

	acct, err := s.st.GetAccount(ctx, in.AccountID)
	if err != nil {
		return nil, err
	}
	plan, err := s.st.GetPlan(ctx, acct.PlanID)
	if err != nil {
		return nil, err
	}

	count, err := s.st.CountAccountSites(ctx, acct.ID)
	if err != nil {
		return nil, err
	}
	if count >= plan.MaxSites {
		return nil, ValidationError{"plan",
			fmt.Sprintf("el plan %s permite un máximo de %d sitios", plan.Name, plan.MaxSites)}
	}

	if in.PHPVersion == "" {
		in.PHPVersion = "8.4"
	}
	if !containsStr(plan.PHPVersions, in.PHPVersion) {
		return nil, ValidationError{"php_version",
			fmt.Sprintf("el plan solo permite PHP %s", strings.Join(plan.PHPVersions, ", "))}
	}

	hostPath := filepath.Join(s.cfg.SitesRoot, acct.SystemUser, "sites", in.Name)
	site := &models.Site{
		AccountID:     acct.ID,
		Name:          in.Name,
		PHPVersion:    in.PHPVersion,
		DocumentRoot:  in.DocumentRoot,
		HostPath:      hostPath,
		ContainerName: fmt.Sprintf("%s-%s-%s", s.cfg.ContainerPrefix, acct.SystemUser, in.Name),
		WorkerScript:  in.WorkerScript,
		WorkerCount:   in.WorkerCount,
		EnvVars:       in.EnvVars,
		Status:        models.SiteProvisioning,
	}
	if err := s.st.CreateSite(ctx, site); err != nil {
		return nil, err
	}

	if err := s.prepareSiteDirs(hostPath, in.DocumentRoot, site.Name); err != nil {
		_ = s.st.UpdateSiteStatus(ctx, site.ID, models.SiteError, err.Error())
		return site, err
	}

	if in.Domain != "" {
		d := &models.Domain{
			SiteID:     site.ID,
			FQDN:       normalizeDomain(in.Domain),
			Kind:       models.DomainPrimary,
			TLSMode:    "auto",
			ForceHTTPS: true,
		}
		if !reFQDN.MatchString(d.FQDN) {
			return site, ValidationError{"domain", "no es un dominio válido"}
		}
		if err := s.st.CreateDomain(ctx, d); err != nil {
			return site, err
		}
		site.Domains = []models.Domain{*d}
	}

	if err := s.deploySite(ctx, site, acct, plan); err != nil {
		_ = s.st.UpdateSiteStatus(ctx, site.ID, models.SiteError, err.Error())
		return site, err
	}
	if err := s.SyncCaddy(ctx); err != nil {
		slog.Warn("sitio creado pero falló la recarga de Caddy", "site", site.Name, "error", err)
	}
	return s.st.GetSite(ctx, site.ID)
}

// deploySite crea (o recrea) el contenedor del sitio y lo arranca.
func (s *Service) deploySite(ctx context.Context, site *models.Site,
	acct *models.Account, plan *models.Plan) error {

	spec := dockerx.SiteSpec{
		Name:          site.ContainerName,
		Image:         s.imageFor(site.PHPVersion),
		SiteID:        site.ID.String(),
		AccountUser:   acct.SystemUser,
		HostPath:      site.HostPath,
		DocumentRoot:  site.DocumentRoot,
		Env:           site.EnvVars,
		WorkerScript:  site.WorkerScript,
		WorkerCount:   site.WorkerCount,
		CPULimit:      plan.CPULimit,
		MemoryLimitMB: plan.MemoryLimitMB,
	}

	containerID, err := s.docker.CreateOrReplace(ctx, spec)
	if err != nil {
		return err
	}
	if err := s.docker.Start(ctx, containerID); err != nil {
		return fmt.Errorf("arrancando el contenedor: %w", err)
	}
	if err := s.docker.WaitHealthy(ctx, site.ContainerName, 30*time.Second); err != nil {
		return err
	}

	upstream := site.ContainerName + ":" + dockerx.SitePort
	return s.st.UpdateSiteRuntime(ctx, site.ID, containerID, upstream, models.SiteRunning, "")
}

// RedeploySite recrea el contenedor aplicando la configuración actual
// (cambio de versión de PHP, document root, variables de entorno…).
func (s *Service) RedeploySite(ctx context.Context, siteID uuid.UUID) error {
	site, err := s.st.GetSite(ctx, siteID)
	if err != nil {
		return err
	}
	acct, err := s.st.GetAccount(ctx, site.AccountID)
	if err != nil {
		return err
	}
	plan, err := s.st.GetPlan(ctx, acct.PlanID)
	if err != nil {
		return err
	}
	if err := s.deploySite(ctx, site, acct, plan); err != nil {
		_ = s.st.UpdateSiteStatus(ctx, siteID, models.SiteError, err.Error())
		return err
	}
	return s.SyncCaddy(ctx)
}

func (s *Service) StartSite(ctx context.Context, siteID uuid.UUID) error {
	site, err := s.st.GetSite(ctx, siteID)
	if err != nil {
		return err
	}
	if err := s.docker.Start(ctx, site.ContainerName); err != nil {
		_ = s.st.UpdateSiteStatus(ctx, siteID, models.SiteError, err.Error())
		return err
	}
	if err := s.st.UpdateSiteStatus(ctx, siteID, models.SiteRunning, ""); err != nil {
		return err
	}
	return s.SyncCaddy(ctx)
}

func (s *Service) StopSite(ctx context.Context, siteID uuid.UUID) error {
	site, err := s.st.GetSite(ctx, siteID)
	if err != nil {
		return err
	}
	if err := s.docker.Stop(ctx, site.ContainerName); err != nil {
		return err
	}
	if err := s.st.UpdateSiteStatus(ctx, siteID, models.SiteStopped, ""); err != nil {
		return err
	}
	return s.SyncCaddy(ctx)
}

func (s *Service) RestartSite(ctx context.Context, siteID uuid.UUID) error {
	site, err := s.st.GetSite(ctx, siteID)
	if err != nil {
		return err
	}
	if err := s.docker.Restart(ctx, site.ContainerName); err != nil {
		_ = s.st.UpdateSiteStatus(ctx, siteID, models.SiteError, err.Error())
		return err
	}
	return s.st.UpdateSiteStatus(ctx, siteID, models.SiteRunning, "")
}

func (s *Service) DeleteSite(ctx context.Context, siteID uuid.UUID, deleteFiles bool) error {
	site, err := s.st.GetSite(ctx, siteID)
	if err != nil {
		return err
	}
	_ = s.st.UpdateSiteStatus(ctx, siteID, models.SiteDeleting, "")
	if err := s.docker.RemoveIfExists(ctx, site.ContainerName); err != nil {
		return err
	}
	if err := s.st.DeleteSite(ctx, siteID); err != nil {
		return err
	}
	if deleteFiles && strings.HasPrefix(site.HostPath, s.cfg.SitesRoot) {
		if err := os.RemoveAll(site.HostPath); err != nil {
			slog.Warn("no se pudieron borrar los archivos del sitio", "path", site.HostPath, "error", err)
		}
	}
	return s.SyncCaddy(ctx)
}

func (s *Service) SiteLogs(ctx context.Context, siteID uuid.UUID, tail int, follow bool) (io.ReadCloser, error) {
	site, err := s.st.GetSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if tail <= 0 || tail > 5000 {
		tail = 200
	}
	return s.docker.Logs(ctx, site.ContainerName, tail, follow)
}

// --- Dominios --------------------------------------------------------------

func (s *Service) AddDomain(ctx context.Context, siteID uuid.UUID, fqdn string,
	kind models.DomainKind, redirectTo string) (*models.Domain, error) {

	fqdn = normalizeDomain(fqdn)
	if !reFQDN.MatchString(fqdn) {
		return nil, ValidationError{"fqdn", "no es un dominio válido"}
	}
	if kind == "" {
		kind = models.DomainAddon
	}
	d := &models.Domain{
		SiteID:     siteID,
		FQDN:       fqdn,
		Kind:       kind,
		RedirectTo: redirectTo,
		TLSMode:    "auto",
		ForceHTTPS: true,
	}
	if err := s.st.CreateDomain(ctx, d); err != nil {
		return nil, err
	}
	return d, s.SyncCaddy(ctx)
}

func (s *Service) RemoveDomain(ctx context.Context, domainID uuid.UUID) error {
	if err := s.st.DeleteDomain(ctx, domainID); err != nil {
		return err
	}
	return s.SyncCaddy(ctx)
}

// --- Caddy -----------------------------------------------------------------

// SyncCaddy reconstruye y publica la configuración completa del borde a partir
// del estado en base de datos. Es idempotente y seguro de llamar a menudo.
func (s *Service) SyncCaddy(ctx context.Context) error {
	s.caddyMu.Lock()
	defer s.caddyMu.Unlock()

	rows, err := s.st.RoutingTable(ctx)
	if err != nil {
		return err
	}

	// Agrupamos los dominios que comparten sitio en una sola ruta.
	bySite := map[uuid.UUID]*caddyapi.SiteRoute{}
	order := []uuid.UUID{}
	for _, r := range rows {
		sr, ok := bySite[r.SiteID]
		if !ok {
			sr = &caddyapi.SiteRoute{
				Upstream:   r.Upstream,
				ForceHTTPS: r.ForceHTTPS,
				Offline:    r.SiteStatus != models.SiteRunning,
			}
			bySite[r.SiteID] = sr
			order = append(order, r.SiteID)
		}
		if r.RedirectTo != "" {
			// Un dominio con redirección se publica como ruta propia.
			continue
		}
		sr.Hosts = append(sr.Hosts, r.FQDN)
	}

	routes := make([]caddyapi.SiteRoute, 0, len(order)+4)
	for _, id := range order {
		if len(bySite[id].Hosts) > 0 {
			routes = append(routes, *bySite[id])
		}
	}
	for _, r := range rows {
		if r.RedirectTo != "" {
			routes = append(routes, caddyapi.SiteRoute{
				Hosts:      []string{r.FQDN},
				RedirectTo: r.RedirectTo,
				ForceHTTPS: r.ForceHTTPS,
			})
		}
	}

	cfg, err := caddyapi.Build(routes, caddyapi.BuildOptions{
		ACMEEmail:     s.cfg.CaddyEmail,
		PanelHost:     panelHost(s.cfg.PublicURL),
		PanelUpstream: "gocp-panel" + portOf(s.cfg.ListenAddr),
	})
	if err != nil {
		return err
	}
	return s.caddy.Load(ctx, cfg)
}

// --- Sistema de archivos ---------------------------------------------------

func (s *Service) prepareAccountDirs(systemUser string) error {
	base := filepath.Join(s.cfg.SitesRoot, systemUser)
	for _, d := range []string{"sites", "logs", "backups", "tmp"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o750); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) prepareSiteDirs(hostPath, docRoot, siteName string) error {
	full := filepath.Join(hostPath, docRoot)
	if err := os.MkdirAll(full, 0o755); err != nil {
		return err
	}
	index := filepath.Join(full, "index.php")
	if _, err := os.Stat(index); os.IsNotExist(err) {
		content := fmt.Sprintf(defaultIndexPHP, siteName)
		if err := os.WriteFile(index, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

const defaultIndexPHP = `<?php
// Página por defecto generada por GoControlPanel.
$site = %q;
?>
<!doctype html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Sitio activo</title>
  <style>
    body{font-family:system-ui,-apple-system,sans-serif;margin:0;display:grid;
         place-items:center;min-height:100vh;background:#0f172a;color:#e2e8f0}
    .card{max-width:36rem;padding:2.5rem;background:#1e293b;border-radius:12px;
          box-shadow:0 10px 40px rgba(0,0,0,.4)}
    h1{margin:0 0 .5rem;font-size:1.5rem}
    code{background:#0f172a;padding:.15rem .4rem;border-radius:4px}
    .muted{color:#94a3b8;font-size:.9rem}
  </style>
</head>
<body>
  <div class="card">
    <h1>El sitio <code><?= htmlspecialchars($site) ?></code> está funcionando</h1>
    <p class="muted">Servido por FrankenPHP <?= PHP_VERSION ?> sobre Caddy.</p>
    <p class="muted">Sube tu aplicación al directorio raíz para reemplazar esta página.</p>
  </div>
</body>
</html>
`

// --- Utilidades ------------------------------------------------------------

func (s *Service) imageFor(phpVersion string) string {
	if s.cfg.SiteImagePrefix == "" {
		return s.cfg.SiteImageDefault
	}
	return fmt.Sprintf("%s:php%s", s.cfg.SiteImagePrefix, phpVersion)
}

func (s *Service) resolvePlan(ctx context.Context, id uuid.UUID) (*models.Plan, error) {
	if id != uuid.Nil {
		return s.st.GetPlan(ctx, id)
	}
	return s.st.GetDefaultPlan(ctx)
}

func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimSuffix(d, "/")
	return d
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func panelHost(publicURL string) string {
	u := strings.TrimPrefix(strings.TrimPrefix(publicURL, "https://"), "http://")
	u = strings.SplitN(u, "/", 2)[0]
	if h, _, ok := strings.Cut(u, ":"); ok {
		return h
	}
	return u
}

func portOf(listenAddr string) string {
	if i := strings.LastIndex(listenAddr, ":"); i >= 0 {
		return listenAddr[i:]
	}
	return ":8080"
}
