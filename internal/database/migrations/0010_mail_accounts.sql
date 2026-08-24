-- Buzones de correo propios para dominios de clientes (docker-mailserver).
-- No confundir con smtp_settings/email_templates (0007_mail.sql), que es el
-- correo SALIENTE del propio panel hacia sus usuarios (notificaciones).
CREATE TABLE mail_domains (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    domain          TEXT NOT NULL UNIQUE,
    dkim_selector   TEXT NOT NULL DEFAULT 'mail',
    -- Valor del TXT "p=..." generado por OpenDKIM la primera vez que se
    -- habilita el dominio: se guarda para no tener que volver a leerlo del
    -- contenedor en cada consulta (setup.sh no lo expone de otra forma).
    dkim_value      TEXT NOT NULL DEFAULT '',
    dkim_enabled_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_mail_domains_account ON mail_domains(account_id);

-- domain UNIQUE en mail_domains + (mail_domain_id, local_part) UNIQUE aquí ya
-- garantiza que "usuario@dominio" no se repita en todo el servidor.
CREATE TABLE mailboxes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id      UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    mail_domain_id  UUID NOT NULL REFERENCES mail_domains(id) ON DELETE CASCADE,
    local_part      TEXT NOT NULL,
    quota_mb        INT NOT NULL DEFAULT 1024,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (mail_domain_id, local_part)
);
CREATE INDEX idx_mailboxes_account ON mailboxes(account_id);

ALTER TABLE plans ADD COLUMN max_mailboxes INT NOT NULL DEFAULT 0;
