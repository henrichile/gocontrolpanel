// Package models contiene los tipos de dominio del panel.
package models

import (
	"time"

	"github.com/google/uuid"
)

// --- Roles y permisos ------------------------------------------------------

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleReseller Role = "reseller"
	RoleUser     Role = "user"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleReseller, RoleUser:
		return true
	}
	return false
}

// AtLeast implementa la jerarquía admin > reseller > user.
func (r Role) AtLeast(min Role) bool {
	rank := map[Role]int{RoleUser: 1, RoleReseller: 2, RoleAdmin: 3}
	return rank[r] >= rank[min]
}

// --- Entidades -------------------------------------------------------------

type Plan struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	DiskQuotaMB      int64     `json:"disk_quota_mb"`
	BandwidthQuotaMB int64     `json:"bandwidth_quota_mb"`
	MaxSites         int       `json:"max_sites"`
	MaxDatabases     int       `json:"max_databases"`
	MaxFTPAccounts   int       `json:"max_ftp_accounts"`
	MaxCronJobs      int       `json:"max_cron_jobs"`
	CPULimit         float64   `json:"cpu_limit"`
	MemoryLimitMB    int64     `json:"memory_limit_mb"`
	PHPVersions      []string  `json:"php_versions"`
	IsDefault        bool      `json:"is_default"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type User struct {
	ID           uuid.UUID  `json:"id"`
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	FullName     string     `json:"full_name"`
	Role         Role       `json:"role"`
	ParentID     *uuid.UUID `json:"parent_id,omitempty"`
	IsActive     bool       `json:"is_active"`
	TOTPSecret   *string    `json:"-"`
	TOTPEnabled  bool       `json:"totp_enabled"`
	TOTPLastStep int64      `json:"-"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type AccountStatus string

const (
	AccountActive     AccountStatus = "active"
	AccountSuspended  AccountStatus = "suspended"
	AccountTerminated AccountStatus = "terminated"
)

type Account struct {
	ID              uuid.UUID     `json:"id"`
	OwnerID         uuid.UUID     `json:"owner_id"`
	PlanID          uuid.UUID     `json:"plan_id"`
	SystemUser      string        `json:"system_user"`
	PrimaryDomain   string        `json:"primary_domain"`
	Status          AccountStatus `json:"status"`
	SuspendReason   string        `json:"suspend_reason"`
	DiskUsedMB      int64         `json:"disk_used_mb"`
	BandwidthUsedMB int64         `json:"bandwidth_used_mb"`
	Notes           string        `json:"notes"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`

	// Campos calculados para la API (no persistidos).
	Plan       *Plan `json:"plan,omitempty"`
	SiteCount  int   `json:"site_count,omitempty"`
	OwnerLogin string `json:"owner_login,omitempty"`
}

type SiteStatus string

const (
	SiteProvisioning SiteStatus = "provisioning"
	SiteRunning      SiteStatus = "running"
	SiteStopped      SiteStatus = "stopped"
	SiteError        SiteStatus = "error"
	SiteDeleting     SiteStatus = "deleting"
)

type Site struct {
	ID              uuid.UUID         `json:"id"`
	AccountID       uuid.UUID         `json:"account_id"`
	Name            string            `json:"name"`
	PHPVersion      string            `json:"php_version"`
	DocumentRoot    string            `json:"document_root"`
	HostPath        string            `json:"host_path"`
	ContainerID     string            `json:"container_id"`
	ContainerName   string            `json:"container_name"`
	UpstreamHost    string            `json:"upstream_host"`
	WorkerScript    string            `json:"worker_script"`
	WorkerCount     int               `json:"worker_count"`
	PHPIniOverrides map[string]string `json:"php_ini_overrides"`
	EnvVars         map[string]string `json:"env_vars"`
	Status          SiteStatus        `json:"status"`
	LastError       string            `json:"last_error"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`

	Domains []Domain `json:"domains,omitempty"`
}

type DomainKind string

const (
	DomainPrimary   DomainKind = "primary"
	DomainAddon     DomainKind = "addon"
	DomainSubdomain DomainKind = "subdomain"
	DomainAlias     DomainKind = "alias"
)

type Domain struct {
	ID         uuid.UUID  `json:"id"`
	SiteID     uuid.UUID  `json:"site_id"`
	FQDN       string     `json:"fqdn"`
	Kind       DomainKind `json:"kind"`
	RedirectTo string     `json:"redirect_to"`
	TLSMode    string     `json:"tls_mode"`
	ForceHTTPS bool       `json:"force_https"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type SiteDatabase struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	Engine    string    `json:"engine"`
	DBName    string    `json:"db_name"`
	DBUser    string    `json:"db_user"`
	Charset   string    `json:"charset"`
	SizeMB    int64     `json:"size_mb"`
	CreatedAt time.Time `json:"created_at"`
}

type FTPAccount struct {
	ID        uuid.UUID `json:"id"`
	AccountID uuid.UUID `json:"account_id"`
	Username  string    `json:"username"`
	HomePath  string    `json:"home_path"`
	QuotaMB   int64     `json:"quota_mb"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type CronJob struct {
	ID           uuid.UUID  `json:"id"`
	SiteID       uuid.UUID  `json:"site_id"`
	Schedule     string     `json:"schedule"`
	Command      string     `json:"command"`
	IsActive     bool       `json:"is_active"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	LastExitCode *int       `json:"last_exit_code,omitempty"`
	LastOutput   string     `json:"last_output"`
	CreatedAt    time.Time  `json:"created_at"`
}

// WAFBlock es una petición bloqueada por Coraza, capturada del log del
// contenedor de borde.
type WAFBlock struct {
	ID         int64     `json:"id"`
	OccurredAt time.Time `json:"occurred_at"`
	ClientIP   string    `json:"client_ip"`
	Hostname   string    `json:"hostname"`
	URI        string    `json:"uri"`
	UniqueID   string    `json:"unique_id"`
	RawJSON    string    `json:"raw_json"`
}

// SystemSettings es la única fila de configuración de seguridad del
// servidor, editable desde el ambiente de administración.
type SystemSettings struct {
	WAFEnabled          bool      `json:"waf_enabled"`
	RateLimitPerMinute  int       `json:"rate_limit_per_minute"`
	BackupRetentionDays int       `json:"backup_retention_days"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type SiteGitConfig struct {
	SiteID           uuid.UUID  `json:"site_id"`
	RepoURL          string     `json:"repo_url"`
	Branch           string     `json:"branch"`
	PublicKey        string     `json:"public_key"`
	PrivateKeyEnc    []byte     `json:"-"`
	WebhookSecret    string     `json:"-"`
	AutoDeploy       bool       `json:"auto_deploy"`
	LastDeployAt     *time.Time `json:"last_deploy_at,omitempty"`
	LastDeployStatus string     `json:"last_deploy_status"`
	LastDeployOutput string     `json:"last_deploy_output"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type AuditEntry struct {
	ID            int64          `json:"id"`
	ActorID       *uuid.UUID     `json:"actor_id,omitempty"`
	ActorUsername string         `json:"actor_username"`
	Action        string         `json:"action"`
	TargetType    string         `json:"target_type"`
	TargetID      string         `json:"target_id"`
	Detail        map[string]any `json:"detail"`
	IPAddress     string         `json:"ip_address,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

type UsageSample struct {
	SiteID     uuid.UUID `json:"site_id"`
	CPUPercent float64   `json:"cpu_percent"`
	MemoryMB   float64   `json:"memory_mb"`
	DiskMB     int64     `json:"disk_mb"`
	NetRxMB    float64   `json:"net_rx_mb"`
	NetTxMB    float64   `json:"net_tx_mb"`
	SampledAt  time.Time `json:"sampled_at"`
}
