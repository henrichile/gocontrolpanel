-- 2FA (TOTP): totp_secret (ya existía) queda como el secreto pendiente o
-- activo; totp_enabled marca si ya se confirmó; totp_last_step evita repetir
-- un código de 30s ya usado (protección contra repetición).
ALTER TABLE users
    ADD COLUMN totp_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN totp_last_step BIGINT  NOT NULL DEFAULT 0;
