package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/etasoft/gocontrolpanel/internal/models"
)

// --- Configuración SMTP ------------------------------------------------------

func (s *Store) GetSMTPSettings(ctx context.Context) (*models.SMTPSettings, error) {
	var st models.SMTPSettings
	err := s.pool.QueryRow(ctx, `
		SELECT host, port, username, from_email, from_name, encryption, enabled, updated_at
		FROM smtp_settings WHERE id`).
		Scan(&st.Host, &st.Port, &st.Username, &st.FromEmail, &st.FromName,
			&st.Encryption, &st.Enabled, &st.UpdatedAt)
	return &st, err
}

// GetSMTPPassword devuelve la contraseña SMTP en claro — solo para uso interno
// del mailer, nunca se serializa hacia el frontend.
func (s *Store) GetSMTPPassword(ctx context.Context) (string, error) {
	var password string
	err := s.pool.QueryRow(ctx, `SELECT password FROM smtp_settings WHERE id`).Scan(&password)
	return password, err
}

// UpdateSMTPSettings guarda la configuración SMTP. Si passwordChanged es
// false, la columna password no se toca (el frontend nunca reenvía la
// contraseña real salvo que el admin la haya editado).
func (s *Store) UpdateSMTPSettings(ctx context.Context, in models.SMTPSettings, password string, passwordChanged bool) error {
	if passwordChanged {
		_, err := s.pool.Exec(ctx, `
			UPDATE smtp_settings
			SET host=$1, port=$2, username=$3, password=$4, from_email=$5, from_name=$6,
			    encryption=$7, enabled=$8, updated_at=now()
			WHERE id`,
			in.Host, in.Port, in.Username, password, in.FromEmail, in.FromName,
			in.Encryption, in.Enabled)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE smtp_settings
		SET host=$1, port=$2, username=$3, from_email=$4, from_name=$5,
		    encryption=$6, enabled=$7, updated_at=now()
		WHERE id`,
		in.Host, in.Port, in.Username, in.FromEmail, in.FromName, in.Encryption, in.Enabled)
	return err
}

// --- Plantillas de email -----------------------------------------------------

func (s *Store) GetEmailTemplate(ctx context.Context, key string) (*models.EmailTemplate, error) {
	var t models.EmailTemplate
	err := s.pool.QueryRow(ctx,
		`SELECT key, subject, body_html, updated_at FROM email_templates WHERE key=$1`, key,
	).Scan(&t.Key, &t.Subject, &t.BodyHTML, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListEmailTemplates(ctx context.Context) ([]models.EmailTemplate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, subject, body_html, updated_at FROM email_templates ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.EmailTemplate{}
	for rows.Next() {
		var t models.EmailTemplate
		if err := rows.Scan(&t.Key, &t.Subject, &t.BodyHTML, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateEmailTemplate(ctx context.Context, key, subject, bodyHTML string) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE email_templates SET subject=$2, body_html=$3, updated_at=now() WHERE key=$1`,
		key, subject, bodyHTML)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
