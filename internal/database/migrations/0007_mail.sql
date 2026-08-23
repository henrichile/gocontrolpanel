-- Configuración de correo saliente del panel: SMTP (fila única, mismo patrón
-- que system_settings) y plantillas de email editables desde el administrador.
CREATE TABLE smtp_settings (
    id          BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    host        TEXT        NOT NULL DEFAULT '',
    port        INT         NOT NULL DEFAULT 587,
    username    TEXT        NOT NULL DEFAULT '',
    password    TEXT        NOT NULL DEFAULT '',
    from_email  TEXT        NOT NULL DEFAULT '',
    from_name   TEXT        NOT NULL DEFAULT 'GoControlPanel',
    encryption  TEXT        NOT NULL DEFAULT 'starttls',
    enabled     BOOLEAN     NOT NULL DEFAULT FALSE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO smtp_settings (id) VALUES (TRUE);

CREATE TABLE email_templates (
    key         TEXT PRIMARY KEY,
    subject     TEXT        NOT NULL,
    body_html   TEXT        NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO email_templates (key, subject, body_html) VALUES (
  'bienvenida_cliente',
  'Tus accesos a {{.PanelURL}}',
  '<p>Hola {{.FullName}},</p>
<p>Tu cuenta de hosting para <strong>{{.Domain}}</strong> ya está lista.</p>
<p>Estos son tus accesos al panel de control:</p>
<p>Usuario: <strong>{{.Username}}</strong><br>
Contraseña: <strong>{{.Password}}</strong></p>
<p><a href="{{.PanelURL}}">Ingresar al panel</a></p>
<p>Te recomendamos cambiar la contraseña la primera vez que ingreses.</p>'
);

-- Marca que el usuario debe cambiar su contraseña generada automáticamente
-- (creación de cliente nuevo desde el flujo de cuentas). No se aplica todavía
-- ningún bloqueo en el login; queda como base para una futura iteración.
ALTER TABLE users ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE;
