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

	// Reglas del WAF (Coraza + OWASP CRS). El on/off y el rate limit viven
	// en la base de datos (system_settings, editable desde el panel); esto
	// se queda como variable de entorno porque son reglas avanzadas que no
	// se tocan desde la UI. Requiere que la imagen de "edge" se haya
	// compilado con esos plugins (ver deploy/edge/Dockerfile); si el WAF se
	// activa contra un Caddy sin ellos, el /load de la config falla.
	CorazaDirectives  string
	EdgeContainerName string // nombre real del contenedor de borde, para leer sus logs (registro de bloqueos del WAF)

	// Acceso al firewall del host (ufw) vía SSH con comando forzado — ver
	// internal/hostctl y install.sh. Vacío = la pestaña de firewall se
	// muestra como "no configurado", nada más.
	HostctlHost       string
	HostctlSSHPort    int
	HostctlHostPubkey string // clave pública del host, para fijar la verificación (no se ignora la verificación)
	HostctlKeyPath    string // clave privada del panel, montada de solo lectura
	SSHPort           int    // puerto SSH del host; nunca se puede bloquear desde el panel
	// "ufw" (Debian/Ubuntu) o "firewalld" (RHEL/AlmaLinux/Rocky/Fedora) — lo
	// fija install.sh según la distribución detectada en el host.
	FirewallBackend string

	// Docker
	DockerHost       string
	DockerNetwork    string
	SiteImagePrefix  string // p.ej. gocp/frankenphp
	SiteImageDefault string // tag por defecto si no hay imagen propia
	SitesRoot        string // raíz en el host donde viven los /home de las cuentas
	ContainerPrefix  string

	// MySQL/MariaDB gestionado para los sitios de los clientes
	MySQLDSN           string
	MySQLHost          string
	MySQLContainerName string // nombre real del contenedor, para docker exec (mysqldump)

	// SFTP gestionado (sftpgo): un usuario virtual por cuenta, con su home
	// encadenado (chroot) a la carpeta de esa cuenta en el host.
	SFTPAdminURL      string // API de administración de sftpgo (solo red interna)
	SFTPAdminUser     string
	SFTPAdminPassword string
	SFTPPublicHost    string // host que el cliente usa para conectarse (dominio o IP)
	SFTPPublicPort    int

	// Correo gestionado (opcional): docker-mailserver + Roundcube, un
	// contenedor fijo administrado por "docker exec" (ver
	// internal/provision/mail.go). MailEnabled en false = la sección de
	// correo del panel se oculta y no se intenta hablar con el contenedor.
	MailEnabled         bool
	MailContainerName   string // nombre real del contenedor, para docker exec
	MailHostname        string // FQDN que TODOS los dominios de clientes usan como MX
	MailWebmailUpstream string // upstream de Roundcube para Caddy (webmail.<host>)

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
		ListenAddr:      env("GOCP_LISTEN_ADDR", ":8080"),
		PublicURL:       env("GOCP_PUBLIC_URL", "http://localhost:8080"),
		Environment:     env("GOCP_ENV", "development"),
		TrustedProxy:    env("GOCP_TRUSTED_PROXY", ""),
		DatabaseURL:     env("GOCP_DATABASE_URL", "postgres://gocp:gocp@localhost:5432/gocp?sslmode=disable"),
		DBMaxConns:      int32(envInt("GOCP_DB_MAX_CONNS", 10)),
		DBMigrateOnBoot: envBool("GOCP_DB_MIGRATE", true),
		JWTSecret:       env("GOCP_JWT_SECRET", ""),
		AccessTokenTTL:  envDuration("GOCP_ACCESS_TTL", 15*time.Minute),
		RefreshTokenTTL: envDuration("GOCP_REFRESH_TTL", 720*time.Hour),
		BcryptCost:      envInt("GOCP_BCRYPT_COST", 12),
		CaddyAdminURL:   env("GOCP_CADDY_ADMIN_URL", "http://localhost:2019"),
		CaddyEmail:      env("GOCP_CADDY_EMAIL", ""),

		CorazaDirectives: env("GOCP_CORAZA_DIRECTIVES",
			"Include /etc/coraza-crs/crs-setup.conf\n"+
				"Include /etc/coraza-crs/rules/*.conf\n"+
				"SecRuleEngine On"),
		EdgeContainerName: env("GOCP_EDGE_CONTAINER_NAME", "gocp-edge"),

		HostctlHost:       env("GOCP_HOSTCTL_HOST", ""),
		HostctlSSHPort:    envInt("GOCP_HOSTCTL_SSH_PORT", 22),
		HostctlHostPubkey: env("GOCP_HOSTCTL_HOST_PUBKEY", ""),
		HostctlKeyPath:    env("GOCP_HOSTCTL_KEY_PATH", ""),
		SSHPort:           envInt("GOCP_SSH_PORT", 22),
		FirewallBackend:   env("GOCP_FIREWALL_BACKEND", "ufw"),

		DockerHost:         env("GOCP_DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerNetwork:      env("GOCP_DOCKER_NETWORK", "gocp_sites"),
		SiteImagePrefix:    env("GOCP_SITE_IMAGE_PREFIX", "gocp/frankenphp"),
		SiteImageDefault:   env("GOCP_SITE_IMAGE_DEFAULT", "dunglas/frankenphp:1-php8.4"),
		SitesRoot:          env("GOCP_SITES_ROOT", "/srv/gocp/accounts"),
		ContainerPrefix:    env("GOCP_CONTAINER_PREFIX", "gocp-site"),
		MySQLDSN:           env("GOCP_MYSQL_DSN", ""),
		MySQLHost:          env("GOCP_MYSQL_HOST", "mysql"),
		MySQLContainerName: env("GOCP_MYSQL_CONTAINER_NAME", "gocp-mysql"),

		SFTPAdminURL:      env("GOCP_SFTP_ADMIN_URL", "http://sftp:8080"),
		SFTPAdminUser:     env("GOCP_SFTP_ADMIN_USER", ""),
		SFTPAdminPassword: env("GOCP_SFTP_ADMIN_PASSWORD", ""),
		SFTPPublicHost:    env("GOCP_SFTP_PUBLIC_HOST", ""),
		SFTPPublicPort:    envInt("GOCP_SFTP_PUBLIC_PORT", 2022),

		MailEnabled:       envBool("GOCP_MAIL_ENABLED", false),
		MailContainerName: env("GOCP_MAIL_CONTAINER_NAME", "gocp-mailserver"),
		MailHostname:      env("GOCP_MAIL_HOSTNAME", ""),
		// Sin esquema: Caddy espera "host:puerto" en el campo "dial" del
		// reverse_proxy, no una URL — con "http://" antepuesto interpreta
		// "http" como si fuera la red de transporte y falla con
		// "dial http:: unknown network http:".
		MailWebmailUpstream: env("GOCP_MAIL_WEBMAIL_UPSTREAM", "roundcube:80"),

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
