package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/etasoft/gocontrolpanel/internal/api"
	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/caddyapi"
	"github.com/etasoft/gocontrolpanel/internal/config"
	"github.com/etasoft/gocontrolpanel/internal/database"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/store"
)

// Igual que las pruebas del store: requieren GOCP_TEST_DATABASE_URL.
func newTestServer(t *testing.T) (http.Handler, *store.Store, *config.Config) {
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
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE audit_log, usage_samples, cron_jobs, ftp_accounts,
		site_databases, domains, sites, accounts, sessions, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Environment:     "test",
		PublicURL:       "http://localhost:8080",
		BcryptCost:      10,
		JWTSecret:       "una-clave-de-pruebas-suficientemente-larga-1234",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: time.Hour,
	}
	st := store.New(pool)
	tokens := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	// El servicio de aprovisionamiento no interviene en las rutas que se
	// prueban aquí (auth, resumen, permisos), así que no se instancia Docker.
	srv := api.NewServer(cfg, st, nil, tokens, caddyapi.NewClient("http://127.0.0.1:2019"), nil)
	return srv.Handler(), st, cfg
}

func seedUser(t *testing.T, st *store.Store, cfg *config.Config,
	username string, role models.Role) *models.User {
	t.Helper()
	hash, err := auth.HashPassword("contrasena-larga-1", cfg.BcryptCost)
	if err != nil {
		t.Fatal(err)
	}
	u := &models.User{
		Username: username, Email: username + "@test.local",
		PasswordHash: hash, Role: role, IsActive: true,
	}
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func doJSON(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		raw, _ := json.Marshal(body)
		buf = bytes.NewBuffer(raw)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthFlow(t *testing.T) {
	h, st, cfg := newTestServer(t)
	seedUser(t, st, cfg, "admin", models.RoleAdmin)

	// Credenciales incorrectas.
	rec := doJSON(t, h, "POST", "/api/v1/auth/login", "",
		map[string]string{"login": "admin", "password": "mala"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login con contraseña mala = %d, se esperaba 401", rec.Code)
	}

	// Login correcto.
	rec = doJSON(t, h, "POST", "/api/v1/auth/login", "",
		map[string]string{"login": "admin", "password": "contrasena-larga-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d (%s)", rec.Code, rec.Body.String())
	}
	var login struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	if login.AccessToken == "" || login.RefreshToken == "" {
		t.Fatal("el login debería devolver ambos tokens")
	}
	if login.User.Role != "admin" {
		t.Errorf("rol = %s", login.User.Role)
	}

	// La respuesta nunca debe incluir el hash de la contraseña.
	if bytes.Contains(rec.Body.Bytes(), []byte("password_hash")) {
		t.Error("la respuesta filtró el hash de la contraseña")
	}

	// /auth/me con el token.
	rec = doJSON(t, h, "GET", "/api/v1/auth/me", login.AccessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/auth/me = %d", rec.Code)
	}

	// Sin token: 401.
	rec = doJSON(t, h, "GET", "/api/v1/auth/me", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("/auth/me sin token = %d, se esperaba 401", rec.Code)
	}

	// Token falsificado: 401.
	rec = doJSON(t, h, "GET", "/api/v1/auth/me", "no.es.un.token", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("/auth/me con token inválido = %d, se esperaba 401", rec.Code)
	}

	// Rotación del refresh token: el primero deja de servir.
	rec = doJSON(t, h, "POST", "/api/v1/auth/refresh", "",
		map[string]string{"refresh_token": login.RefreshToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, "POST", "/api/v1/auth/refresh", "",
		map[string]string{"refresh_token": login.RefreshToken})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("reutilizar el refresh token = %d, se esperaba 401", rec.Code)
	}
}

func TestRoleBoundaries(t *testing.T) {
	h, st, cfg := newTestServer(t)
	seedUser(t, st, cfg, "admin", models.RoleAdmin)
	seedUser(t, st, cfg, "cliente", models.RoleUser)

	tokenFor := func(username string) string {
		rec := doJSON(t, h, "POST", "/api/v1/auth/login", "",
			map[string]string{"login": username, "password": "contrasena-larga-1"})
		var out struct {
			AccessToken string `json:"access_token"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out.AccessToken
	}

	adminTok := tokenFor("admin")
	userTok := tokenFor("cliente")

	// Un usuario normal no puede listar usuarios ni ver el sistema.
	for _, path := range []string{"/api/v1/users", "/api/v1/system/info", "/api/v1/system/audit"} {
		rec := doJSON(t, h, "GET", path, userTok, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s como usuario = %d, se esperaba 403", path, rec.Code)
		}
	}

	// El admin sí.
	rec := doJSON(t, h, "GET", "/api/v1/users", adminTok, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /users como admin = %d (%s)", rec.Code, rec.Body.String())
	}

	// Un usuario normal tampoco puede crear cuentas de hosting.
	rec = doJSON(t, h, "POST", "/api/v1/accounts", userTok,
		map[string]any{"system_user": "hack", "primary_domain": "hack.cl"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /accounts como usuario = %d, se esperaba 403", rec.Code)
	}

	// El resumen sí está abierto a cualquier autenticado.
	rec = doJSON(t, h, "GET", "/api/v1/overview", userTok, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /overview como usuario = %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestValidationAndRateLimit(t *testing.T) {
	h, st, cfg := newTestServer(t)
	seedUser(t, st, cfg, "admin", models.RoleAdmin)

	rec := doJSON(t, h, "POST", "/api/v1/auth/login", "",
		map[string]string{"login": "admin", "password": "contrasena-larga-1"})
	var out struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	// Contraseña demasiado corta al crear un usuario.
	rec = doJSON(t, h, "POST", "/api/v1/users", out.AccessToken, map[string]any{
		"username": "nuevo", "email": "nuevo@test.local", "password": "corta", "role": "user",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("contraseña corta = %d, se esperaba 422 (%s)", rec.Code, rec.Body.String())
	}

	// Campo desconocido en el cuerpo: 400 en lugar de ignorarlo en silencio.
	rec = doJSON(t, h, "POST", "/api/v1/auth/login", "",
		map[string]string{"login": "admin", "password": "x", "campo_raro": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("campo desconocido = %d, se esperaba 400", rec.Code)
	}

	// El limitador del login corta antes de los 30 intentos.
	blocked := false
	for i := 0; i < 30; i++ {
		r := doJSON(t, h, "POST", "/api/v1/auth/login", "",
			map[string]string{"login": "admin", "password": "mala"})
		if r.Code == http.StatusTooManyRequests {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Error("el endpoint de login debería limitar los intentos por IP")
	}
}
