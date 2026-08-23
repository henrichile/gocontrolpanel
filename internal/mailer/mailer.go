// Package mailer envía correos HTML del panel (accesos de cliente, pruebas de
// SMTP) usando la configuración guardada en base de datos.
package mailer

import (
	"context"
	"fmt"

	"gopkg.in/gomail.v2"
)

// SMTPConfig es la configuración de envío, leída de la tabla smtp_settings en
// cada envío (sin caché) para que un cambio del admin se refleje de inmediato.
type SMTPConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	FromEmail  string
	FromName   string
	Encryption string // "none" | "starttls" | "ssl"
	Enabled    bool
}

type Client struct {
	cfg SMTPConfig
}

func New(cfg SMTPConfig) *Client {
	return &Client{cfg: cfg}
}

// Send entrega un correo HTML a un único destinatario. El llamador decide qué
// hacer con el error (nunca debe abortar una operación ya confirmada, como la
// creación de una cuenta, solo por un fallo de envío).
func (c *Client) Send(ctx context.Context, to, subject, htmlBody string) error {
	if !c.cfg.Enabled {
		return fmt.Errorf("el envío de correo está deshabilitado en Configuraciones")
	}
	if c.cfg.Host == "" {
		return fmt.Errorf("falta configurar el servidor SMTP")
	}

	from := c.cfg.FromEmail
	if c.cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", c.cfg.FromName, c.cfg.FromEmail)
	}

	m := gomail.NewMessage()
	m.SetHeader("From", from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", htmlBody)

	// gomail.v2 usa STARTTLS de forma oportunista si el servidor lo anuncia
	// (con fallback a texto plano si no), así que "none" y "starttls" comparten
	// el mismo dialer; solo "ssl" (puerto 465, TLS implícito) requiere SSL=true.
	dialer := gomail.NewDialer(c.cfg.Host, c.cfg.Port, c.cfg.Username, c.cfg.Password)
	if c.cfg.Encryption == "ssl" {
		dialer.SSL = true
	}

	done := make(chan error, 1)
	go func() { done <- dialer.DialAndSend(m) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
