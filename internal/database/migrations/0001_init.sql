-- GoControlPanel :: esquema inicial
-- Estado propio del panel. Las bases de datos de los clientes viven en el
-- servidor MySQL/MariaDB gestionado, no aquí.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ---------------------------------------------------------------------------
-- Planes de hosting (equivalente a los "packages" de WHM)
-- ---------------------------------------------------------------------------
CREATE TABLE plans (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT        NOT NULL UNIQUE,
    description         TEXT        NOT NULL DEFAULT '',
    disk_quota_mb       BIGINT      NOT NULL DEFAULT 1024,
    bandwidth_quota_mb  BIGINT      NOT NULL DEFAULT 10240,
    max_sites           INT         NOT NULL DEFAULT 1,
    max_databases       INT         NOT NULL DEFAULT 1,
    max_ftp_accounts    INT         NOT NULL DEFAULT 1,
    max_cron_jobs       INT         NOT NULL DEFAULT 5,
    cpu_limit           NUMERIC(4,2) NOT NULL DEFAULT 1.00,   -- núcleos por contenedor
    memory_limit_mb     BIGINT      NOT NULL DEFAULT 512,
    php_versions        TEXT[]      NOT NULL DEFAULT ARRAY['8.3','8.4'],
    is_default          BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Usuarios del panel
--   role: admin      -> equivalente a root/WHM
--         reseller   -> puede crear cuentas dentro de su cuota
--         user       -> dueño de una cuenta de hosting (cPanel)
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username        TEXT        NOT NULL UNIQUE,
    email           TEXT        NOT NULL UNIQUE,
    password_hash   TEXT        NOT NULL,
    full_name       TEXT        NOT NULL DEFAULT '',
    role            TEXT        NOT NULL CHECK (role IN ('admin','reseller','user')),
    parent_id       UUID        REFERENCES users(id) ON DELETE SET NULL,
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    totp_secret     TEXT,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_users_parent ON users(parent_id);
CREATE INDEX idx_users_role   ON users(role);

-- ---------------------------------------------------------------------------
-- Cuentas de hosting
-- ---------------------------------------------------------------------------
CREATE TABLE accounts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        UUID        NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    plan_id         UUID        NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    -- nombre corto del sistema: usuario unix / prefijo de contenedores y BDs
    unix_user       TEXT        NOT NULL UNIQUE
                                CHECK (unix_user ~ '^[a-z][a-z0-9_]{2,15}$'),
    primary_domain  TEXT        NOT NULL UNIQUE,
    status          TEXT        NOT NULL DEFAULT 'active'
                                CHECK (status IN ('active','suspended','terminated')),
    suspend_reason  TEXT        NOT NULL DEFAULT '',
    disk_used_mb    BIGINT      NOT NULL DEFAULT 0,
    bandwidth_used_mb BIGINT    NOT NULL DEFAULT 0,
    notes           TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_accounts_owner ON accounts(owner_id);
CREATE INDEX idx_accounts_status ON accounts(status);

-- ---------------------------------------------------------------------------
-- Sitios: cada uno es un contenedor FrankenPHP independiente
-- ---------------------------------------------------------------------------
CREATE TABLE sites (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    php_version     TEXT        NOT NULL DEFAULT '8.4',
    document_root   TEXT        NOT NULL DEFAULT 'public',
    -- ruta en el host que se monta en /app dentro del contenedor
    host_path       TEXT        NOT NULL,
    container_id    TEXT        NOT NULL DEFAULT '',
    container_name  TEXT        NOT NULL UNIQUE,
    upstream_host   TEXT        NOT NULL DEFAULT '',   -- p.ej. gocp-site-ab12:8080
    worker_script   TEXT        NOT NULL DEFAULT '',   -- modo worker (Laravel Octane, etc.)
    worker_count    INT         NOT NULL DEFAULT 0,
    php_ini_overrides JSONB     NOT NULL DEFAULT '{}'::jsonb,
    env_vars        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT        NOT NULL DEFAULT 'provisioning'
                                CHECK (status IN ('provisioning','running','stopped','error','deleting')),
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, name)
);
CREATE INDEX idx_sites_account ON sites(account_id);

-- ---------------------------------------------------------------------------
-- Dominios y subdominios enrutados por el Caddy de borde
-- ---------------------------------------------------------------------------
CREATE TABLE domains (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id         UUID        NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    fqdn            TEXT        NOT NULL UNIQUE,
    kind            TEXT        NOT NULL DEFAULT 'addon'
                                CHECK (kind IN ('primary','addon','subdomain','alias')),
    redirect_to     TEXT        NOT NULL DEFAULT '',
    tls_mode        TEXT        NOT NULL DEFAULT 'auto'
                                CHECK (tls_mode IN ('auto','custom','off')),
    tls_cert_pem    TEXT,
    tls_key_pem     TEXT,
    force_https     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_domains_site ON domains(site_id);

-- ---------------------------------------------------------------------------
-- Bases de datos MySQL/MariaDB de los clientes
-- ---------------------------------------------------------------------------
CREATE TABLE site_databases (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    engine          TEXT        NOT NULL DEFAULT 'mysql' CHECK (engine IN ('mysql','postgres')),
    db_name         TEXT        NOT NULL UNIQUE,
    db_user         TEXT        NOT NULL,
    charset         TEXT        NOT NULL DEFAULT 'utf8mb4',
    size_mb         BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dbs_account ON site_databases(account_id);

-- ---------------------------------------------------------------------------
-- Cuentas FTP/SFTP
-- ---------------------------------------------------------------------------
CREATE TABLE ftp_accounts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    username        TEXT        NOT NULL UNIQUE,
    password_hash   TEXT        NOT NULL,
    home_path       TEXT        NOT NULL,
    quota_mb        BIGINT      NOT NULL DEFAULT 0,
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ftp_account ON ftp_accounts(account_id);

-- ---------------------------------------------------------------------------
-- Tareas programadas por sitio (se ejecutan dentro del contenedor)
-- ---------------------------------------------------------------------------
CREATE TABLE cron_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id         UUID        NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    schedule        TEXT        NOT NULL,              -- expresión cron de 5 campos
    command         TEXT        NOT NULL,
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    last_run_at     TIMESTAMPTZ,
    last_exit_code  INT,
    last_output     TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_cron_site ON cron_jobs(site_id);

-- ---------------------------------------------------------------------------
-- Sesiones / refresh tokens
-- ---------------------------------------------------------------------------
CREATE TABLE sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_hash    TEXT        NOT NULL UNIQUE,
    user_agent      TEXT        NOT NULL DEFAULT '',
    ip_address      INET,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

-- ---------------------------------------------------------------------------
-- Bitácora de auditoría
-- ---------------------------------------------------------------------------
CREATE TABLE audit_log (
    id              BIGSERIAL PRIMARY KEY,
    actor_id        UUID        REFERENCES users(id) ON DELETE SET NULL,
    actor_username  TEXT        NOT NULL DEFAULT '',
    action          TEXT        NOT NULL,
    target_type     TEXT        NOT NULL DEFAULT '',
    target_id       TEXT        NOT NULL DEFAULT '',
    detail          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    ip_address      INET,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_actor ON audit_log(actor_id);
CREATE INDEX idx_audit_created ON audit_log(created_at DESC);

-- ---------------------------------------------------------------------------
-- Muestras de uso de recursos (para gráficos del dashboard)
-- ---------------------------------------------------------------------------
CREATE TABLE usage_samples (
    id              BIGSERIAL PRIMARY KEY,
    site_id         UUID        NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    cpu_percent     NUMERIC(6,2) NOT NULL DEFAULT 0,
    memory_mb       NUMERIC(10,2) NOT NULL DEFAULT 0,
    disk_mb         BIGINT      NOT NULL DEFAULT 0,
    net_rx_mb       NUMERIC(12,2) NOT NULL DEFAULT 0,
    net_tx_mb       NUMERIC(12,2) NOT NULL DEFAULT 0,
    sampled_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_usage_site_time ON usage_samples(site_id, sampled_at DESC);

-- ---------------------------------------------------------------------------
-- Ajustes globales del panel (clave/valor)
-- ---------------------------------------------------------------------------
CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       JSONB       NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Plan por defecto para que el panel arranque usable.
INSERT INTO plans (name, description, disk_quota_mb, bandwidth_quota_mb,
                   max_sites, max_databases, max_ftp_accounts, cpu_limit,
                   memory_limit_mb, is_default)
VALUES ('Starter', 'Plan por defecto creado durante la instalación',
        5120, 51200, 1, 3, 3, 1.00, 512, TRUE);
