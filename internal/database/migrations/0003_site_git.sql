-- Deploy automático por Git: una fila por sitio con su propia deploy key
-- (par de claves SSH generado por el panel) y el estado del último deploy.
CREATE TABLE site_git_configs (
    site_id             UUID PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
    repo_url            TEXT        NOT NULL,
    branch              TEXT        NOT NULL DEFAULT 'main',
    public_key          TEXT        NOT NULL,
    private_key_enc     BYTEA       NOT NULL,
    webhook_secret      TEXT        NOT NULL,
    auto_deploy         BOOLEAN     NOT NULL DEFAULT TRUE,
    last_deploy_at      TIMESTAMPTZ,
    last_deploy_status  TEXT        NOT NULL DEFAULT 'never'
                                    CHECK (last_deploy_status IN ('never','running','success','failed')),
    last_deploy_output  TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
