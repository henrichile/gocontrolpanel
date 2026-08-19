package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/database"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/store"
)

// Estas pruebas necesitan una base PostgreSQL real y desechable:
//
//	GOCP_TEST_DATABASE_URL=postgres://gocp:gocp@localhost:5432/gocp_test go test ./internal/store/
//
// Si la variable no está definida, se omiten.
func newTestStore(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("GOCP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOCP_TEST_DATABASE_URL no definida; se omite la prueba de integración")
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 5)
	if err != nil {
		t.Fatalf("conectando: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrando: %v", err)
	}
	// Partimos de un estado limpio en cada ejecución.
	_, err = pool.Exec(ctx, `TRUNCATE audit_log, usage_samples, cron_jobs, ftp_accounts,
		site_databases, domains, sites, accounts, sessions, users RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("limpiando: %v", err)
	}
	return store.New(pool), ctx
}

func TestFullLifecycle(t *testing.T) {
	st, ctx := newTestStore(t)

	// --- Usuario ---
	admin := &models.User{
		Username: "admin", Email: "admin@test.local",
		PasswordHash: "x", Role: models.RoleAdmin, IsActive: true,
	}
	if err := st.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if admin.ID == uuid.Nil {
		t.Fatal("el usuario debería recibir un UUID")
	}

	found, err := st.GetUserByLogin(ctx, "admin@test.local")
	if err != nil || found.ID != admin.ID {
		t.Fatalf("GetUserByLogin: %v", err)
	}

	// --- Plan por defecto (lo inserta la migración) ---
	plan, err := st.GetDefaultPlan(ctx)
	if err != nil {
		t.Fatalf("GetDefaultPlan: %v", err)
	}
	if len(plan.PHPVersions) == 0 {
		t.Error("el plan por defecto debería traer versiones de PHP")
	}

	// --- Cuenta ---
	acct := &models.Account{
		OwnerID: admin.ID, PlanID: plan.ID,
		SystemUser: "acme", PrimaryDomain: "acme.cl",
		Status: models.AccountActive,
	}
	if err := st.CreateAccount(ctx, acct); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	accounts, err := st.ListAccounts(ctx, nil)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("ListAccounts: %v (n=%d)", err, len(accounts))
	}
	if accounts[0].OwnerLogin != "admin" {
		t.Errorf("OwnerLogin = %q, se esperaba admin", accounts[0].OwnerLogin)
	}

	// --- Sitio ---
	site := &models.Site{
		AccountID: acct.ID, Name: "principal", PHPVersion: "8.4",
		DocumentRoot: "public", HostPath: "/srv/gocp/accounts/acme/sites/principal",
		ContainerName: "gocp-site-acme-principal", Status: models.SiteProvisioning,
		EnvVars: map[string]string{"APP_ENV": "production"},
	}
	if err := st.CreateSite(ctx, site); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}

	err = st.UpdateSiteRuntime(ctx, site.ID, "abc123",
		"gocp-site-acme-principal:8080", models.SiteRunning, "")
	if err != nil {
		t.Fatalf("UpdateSiteRuntime: %v", err)
	}

	// --- Dominios ---
	for _, fqdn := range []string{"acme.cl", "www.acme.cl"} {
		d := &models.Domain{
			SiteID: site.ID, FQDN: fqdn, Kind: models.DomainPrimary,
			TLSMode: "auto", ForceHTTPS: true,
		}
		if err := st.CreateDomain(ctx, d); err != nil {
			t.Fatalf("CreateDomain(%s): %v", fqdn, err)
		}
	}

	loaded, err := st.GetSite(ctx, site.ID)
	if err != nil {
		t.Fatalf("GetSite: %v", err)
	}
	if len(loaded.Domains) != 2 {
		t.Errorf("se esperaban 2 dominios, hay %d", len(loaded.Domains))
	}
	if loaded.EnvVars["APP_ENV"] != "production" {
		t.Errorf("las variables de entorno no se conservaron: %v", loaded.EnvVars)
	}

	// --- Tabla de enrutado (lo que consume el generador de Caddy) ---
	routes, err := st.RoutingTable(ctx)
	if err != nil {
		t.Fatalf("RoutingTable: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("se esperaban 2 rutas, hay %d", len(routes))
	}
	if routes[0].Upstream != "gocp-site-acme-principal:8080" {
		t.Errorf("upstream inesperado: %s", routes[0].Upstream)
	}

	// Una cuenta suspendida desaparece del enrutado.
	if err := st.UpdateAccountStatus(ctx, acct.ID, models.AccountSuspended, "impago"); err != nil {
		t.Fatalf("UpdateAccountStatus: %v", err)
	}
	routes, _ = st.RoutingTable(ctx)
	if len(routes) != 0 {
		t.Errorf("una cuenta suspendida no debería enrutar, hay %d rutas", len(routes))
	}
	_ = st.UpdateAccountStatus(ctx, acct.ID, models.AccountActive, "")

	// --- Resumen, con y sin filtro por propietario ---
	ov, err := st.Overview(ctx, nil)
	if err != nil {
		t.Fatalf("Overview(nil): %v", err)
	}
	if ov.Accounts != 1 || ov.ActiveSites != 1 || ov.Domains != 2 || ov.Users != 1 {
		t.Errorf("resumen global inesperado: %+v", ov)
	}

	scoped, err := st.Overview(ctx, &admin.ID)
	if err != nil {
		t.Fatalf("Overview(scoped): %v", err)
	}
	if scoped.Accounts != 1 || scoped.Domains != 2 {
		t.Errorf("resumen filtrado inesperado: %+v", scoped)
	}

	other := uuid.New()
	empty, err := st.Overview(ctx, &other)
	if err != nil {
		t.Fatalf("Overview(otro propietario): %v", err)
	}
	if empty.Accounts != 0 || empty.Domains != 0 {
		t.Errorf("otro propietario no debería ver nada: %+v", empty)
	}

	// --- Sesiones: rotación del refresh token ---
	if err := st.CreateSession(ctx, admin.ID, "hash-1", "test-agent", "10.0.0.1", time.Hour); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	uid, err := st.ConsumeSession(ctx, "hash-1")
	if err != nil || uid != admin.ID {
		t.Fatalf("ConsumeSession: %v", err)
	}
	if _, err := st.ConsumeSession(ctx, "hash-1"); err == nil {
		t.Error("un refresh token consumido no debería volver a servir")
	}

	// --- Cron y auditoría ---
	job := &models.CronJob{SiteID: site.ID, Schedule: "*/5 * * * *",
		Command: "php artisan schedule:run", IsActive: true}
	if err := st.CreateCron(ctx, job); err != nil {
		t.Fatalf("CreateCron: %v", err)
	}
	if err := st.RecordCronRun(ctx, job.ID, 0, "listo"); err != nil {
		t.Fatalf("RecordCronRun: %v", err)
	}
	active, err := st.ListActiveCron(ctx)
	if err != nil || len(active) != 1 {
		t.Fatalf("ListActiveCron: %v (n=%d)", err, len(active))
	}
	if active[0].ContainerName != "gocp-site-acme-principal" {
		t.Errorf("contenedor inesperado en la tarea: %s", active[0].ContainerName)
	}

	st.Audit(ctx, models.AuditEntry{
		ActorID: &admin.ID, ActorUsername: "admin", Action: "site.create",
		TargetType: "site", TargetID: site.ID.String(),
		Detail: map[string]any{"name": "principal"}, IPAddress: "10.0.0.1",
	})
	entries, err := st.ListAudit(ctx, 10, nil)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListAudit: %v (n=%d)", err, len(entries))
	}
	if entries[0].Detail["name"] != "principal" {
		t.Errorf("el detalle de auditoría no se guardó: %v", entries[0].Detail)
	}

	// --- Métricas ---
	err = st.RecordUsage(ctx, models.UsageSample{
		SiteID: site.ID, CPUPercent: 12.5, MemoryMB: 128, NetRxMB: 1, NetTxMB: 2,
	})
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	samples, err := st.UsageHistory(ctx, site.ID, 24*time.Hour)
	if err != nil || len(samples) != 1 {
		t.Fatalf("UsageHistory: %v (n=%d)", err, len(samples))
	}

	// --- Borrado en cascada ---
	if err := st.DeleteAccount(ctx, acct.ID); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if _, err := st.GetSite(ctx, site.ID); err == nil {
		t.Error("el sitio debería haberse borrado en cascada con la cuenta")
	}
}

func TestPlanQuotasAndCounts(t *testing.T) {
	st, ctx := newTestStore(t)

	owner := &models.User{Username: "reseller", Email: "r@test.local",
		PasswordHash: "x", Role: models.RoleReseller, IsActive: true}
	if err := st.CreateUser(ctx, owner); err != nil {
		t.Fatal(err)
	}

	plan := &models.Plan{
		Name: "Pro", DiskQuotaMB: 10240, BandwidthQuotaMB: 102400,
		MaxSites: 5, MaxDatabases: 5, MaxFTPAccounts: 5, MaxCronJobs: 10,
		CPULimit: 2, MemoryLimitMB: 1024, PHPVersions: []string{"8.4"},
	}
	if err := st.CreatePlan(ctx, plan); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	acct := &models.Account{OwnerID: owner.ID, PlanID: plan.ID,
		SystemUser: "cliente1", PrimaryDomain: "cliente1.cl", Status: models.AccountActive}
	if err := st.CreateAccount(ctx, acct); err != nil {
		t.Fatal(err)
	}

	n, err := st.CountAccountSites(ctx, acct.ID)
	if err != nil || n != 0 {
		t.Fatalf("CountAccountSites inicial = %d (%v)", n, err)
	}

	site := &models.Site{AccountID: acct.ID, Name: "web", PHPVersion: "8.4",
		DocumentRoot: "public", HostPath: "/tmp/web",
		ContainerName: "gocp-site-cliente1-web", Status: models.SiteRunning}
	if err := st.CreateSite(ctx, site); err != nil {
		t.Fatal(err)
	}

	if n, _ = st.CountAccountSites(ctx, acct.ID); n != 1 {
		t.Errorf("CountAccountSites = %d, se esperaba 1", n)
	}

	// El nombre de sitio es único por cuenta.
	dup := *site
	dup.ContainerName = "otro"
	if err := st.CreateSite(ctx, &dup); err == nil {
		t.Error("no debería permitirse un segundo sitio con el mismo nombre en la cuenta")
	}
}
