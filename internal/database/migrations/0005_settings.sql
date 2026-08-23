-- Configuración de seguridad del servidor (WAF, rate limit, retención de
-- backups), editable desde el panel. Fila única forzada con el truco
-- "id BOOLEAN PRIMARY KEY CHECK (id)": solo puede existir id=TRUE.
CREATE TABLE system_settings (
    id                     BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    waf_enabled            BOOLEAN     NOT NULL DEFAULT FALSE,
    rate_limit_per_minute  INT         NOT NULL DEFAULT 240,
    backup_retention_days  INT         NOT NULL DEFAULT 14,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO system_settings (id) VALUES (TRUE);
