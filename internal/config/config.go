// Package config carga la configuración del panel desde variables de entorno
// (con un archivo .env opcional para desarrollo).
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Servidor HTTP del panel
	ListenAddr   string
	PublicURL    string
	Environment  string // development | production
	TrustedProxy string

	// PostgreSQL (estado del panel)
	DatabaseURL     string
	DBMaxConns      int32
	DBMigrateOnBoot bool

	// Autenticación
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	BcryptCost      int

	// Caddy de borde (reverse proxy + TLS)
	CaddyAdminURL string
	CaddyEmail    string // e-mail para ACME/Let's Encrypt

	// Docker
	DockerHost       string
	DockerNetwork    string
	SiteImagePrefix  string // p.ej. gocp/frankenphp
	SiteImageDefault string // tag por defecto si no hay imagen propia
	SitesRoot        string // raíz en el host donde viven los /home de las cuentas
	ContainerPrefix  string

	// MySQL/MariaDB gestionado para los sitios de los clientes
	MySQLDSN  string
	MySQLHost string

	// SFTP gestionado (sftpgo): un usuario virtual por cuenta, con su home
	// encadenado (chroot) a la carpeta de esa cuenta en el host.
	SFTPAdminURL      string // API de administración de sftpgo (solo red interna)
	SFTPAdminUser     string
	SFTPAdminPassword string
	SFTPPublicHost    string // host que el cliente usa para conectarse (dominio o IP)
	SFTPPublicPort    int

	// Primer administrador (solo se usa si la tabla users está vacía)
	BootstrapAdminUser     string
	BootstrapAdminEmail    string
	BootstrapAdminPassword string

	// Telemetría interna
	MetricsInterval time.Duration
}

// Load lee la configuración del entorno, aplicando .env si existe.
func Load() (*Config, error) {
	loadDotEnv(".env")

	c := &Config{
		ListenAddr:       env("GOCP_LISTEN_ADDR", ":8080"),
		PublicURL:        env("GOCP_PUBLIC_URL", "http://localhost:8080"),
		Environment:      env("GOCP_ENV", "development"),
		TrustedProxy:     env("GOCP_TRUSTED_PROXY", ""),
		DatabaseURL:      env("GOCP_DATABASE_URL", "postgres://gocp:gocp@localhost:5432/gocp?sslmode=disable"),
		DBMaxConns:       int32(envInt("GOCP_DB_MAX_CONNS", 10)),
		DBMigrateOnBoot:  envBool("GOCP_DB_MIGRATE", true),
		JWTSecret:        env("GOCP_JWT_SECRET", ""),
		AccessTokenTTL:   envDuration("GOCP_ACCESS_TTL", 15*time.Minute),
		RefreshTokenTTL:  envDuration("GOCP_REFRESH_TTL", 720*time.Hour),
		BcryptCost:       envInt("GOCP_BCRYPT_COST", 12),
		CaddyAdminURL:    env("GOCP_CADDY_ADMIN_URL", "http://localhost:2019"),
		CaddyEmail:       env("GOCP_CADDY_EMAIL", ""),
		DockerHost:       env("GOCP_DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerNetwork:    env("GOCP_DOCKER_NETWORK", "gocp_sites"),
		SiteImagePrefix:  env("GOCP_SITE_IMAGE_PREFIX", "gocp/frankenphp"),
		SiteImageDefault: env("GOCP_SITE_IMAGE_DEFAULT", "dunglas/frankenphp:1-php8.4"),
		SitesRoot:        env("GOCP_SITES_ROOT", "/srv/gocp/accounts"),
		ContainerPrefix:  env("GOCP_CONTAINER_PREFIX", "gocp-site"),
		MySQLDSN:         env("GOCP_MYSQL_DSN", ""),
		MySQLHost:        env("GOCP_MYSQL_HOST", "mysql"),

		SFTPAdminURL:      env("GOCP_SFTP_ADMIN_URL", "http://sftp:8080"),
		SFTPAdminUser:     env("GOCP_SFTP_ADMIN_USER", ""),
		SFTPAdminPassword: env("GOCP_SFTP_ADMIN_PASSWORD", ""),
		SFTPPublicHost:    env("GOCP_SFTP_PUBLIC_HOST", ""),
		SFTPPublicPort:    envInt("GOCP_SFTP_PUBLIC_PORT", 2022),

		BootstrapAdminUser:     env("GOCP_ADMIN_USER", "admin"),
		BootstrapAdminEmail:    env("GOCP_ADMIN_EMAIL", "admin@localhost"),
		BootstrapAdminPassword: env("GOCP_ADMIN_PASSWORD", ""),

		MetricsInterval: envDuration("GOCP_METRICS_INTERVAL", 60*time.Second),
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) IsProduction() bool { return c.Environment == "production" }

func (c *Config) validate() error {
	if c.JWTSecret == "" {
		if c.IsProduction() {
			return fmt.Errorf("GOCP_JWT_SECRET es obligatorio en producción")
		}
		// En desarrollo generamos uno efímero para no bloquear el arranque.
		c.JWTSecret = "dev-only-insecure-secret-change-me-0123456789abcdef"
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("GOCP_JWT_SECRET debe tener al menos 32 caracteres")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("GOCP_DATABASE_URL es obligatorio")
	}
	if c.BcryptCost < 10 || c.BcryptCost > 15 {
		return fmt.Errorf("GOCP_BCRYPT_COST debe estar entre 10 y 15")
	}
	if c.IsProduction() && c.BootstrapAdminPassword == "" {
		// No es fatal: el instalador puede crear el admin por CLI.
		_, _ = fmt.Fprintln(os.Stderr, "aviso: GOCP_ADMIN_PASSWORD vacío; usa `gocpd createadmin` para crear el primer usuario")
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}

// loadDotEnv aplica un archivo .env sin sobrescribir variables ya definidas.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
