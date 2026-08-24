// Buzones de correo propios para dominios de clientes. No confundir con
// mail.go (smtp_settings/email_templates), que es el correo saliente del
// propio panel hacia sus usuarios.
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/etasoft/gocontrolpanel/internal/models"
)

// --- Dominios de correo ------------------------------------------------------

const mailDomainCols = `id, account_id, domain, dkim_selector, dkim_value, dkim_enabled_at, created_at`

func scanMailDomain(row pgx.Row) (*models.MailDomain, error) {
	var d models.MailDomain
	err := row.Scan(&d.ID, &d.AccountID, &d.Domain, &d.DKIMSelector, &d.DKIMValue,
		&d.DKIMEnabledAt, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

// CreateMailDomain persiste el dominio ya con su clave DKIM (generada y leída
// del contenedor antes de llamar aquí) y marca dkim_enabled_at de una vez.
func (s *Store) CreateMailDomain(ctx context.Context, d *models.MailDomain) error {
	if d.DKIMSelector == "" {
		d.DKIMSelector = "mail"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO mail_domains (account_id, domain, dkim_selector, dkim_value, dkim_enabled_at)
		VALUES ($1,$2,$3,$4,now())
		RETURNING id, created_at`,
		d.AccountID, d.Domain, d.DKIMSelector, d.DKIMValue,
	).Scan(&d.ID, &d.CreatedAt)
}

func (s *Store) GetMailDomainByDomain(ctx context.Context, domain string) (*models.MailDomain, error) {
	return scanMailDomain(s.pool.QueryRow(ctx,
		`SELECT `+mailDomainCols+` FROM mail_domains WHERE domain=$1`, domain))
}

func (s *Store) ListMailDomains(ctx context.Context, accountID uuid.UUID) ([]models.MailDomain, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+mailDomainCols+` FROM mail_domains WHERE account_id=$1 ORDER BY domain`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.MailDomain{}
	for rows.Next() {
		var d models.MailDomain
		if err := rows.Scan(&d.ID, &d.AccountID, &d.Domain, &d.DKIMSelector, &d.DKIMValue,
			&d.DKIMEnabledAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// --- Buzones -----------------------------------------------------------------

func (s *Store) CreateMailbox(ctx context.Context, mb *models.Mailbox) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO mailboxes (account_id, mail_domain_id, local_part, quota_mb)
		VALUES ($1,$2,$3,$4)
		RETURNING id, created_at`,
		mb.AccountID, mb.MailDomainID, mb.LocalPart, mb.QuotaMB,
	).Scan(&mb.ID, &mb.CreatedAt)
}

// ListMailboxes trae los buzones de la cuenta junto con el dominio (para
// poder mostrar/componer la dirección completa sin una consulta aparte).
func (s *Store) ListMailboxes(ctx context.Context, accountID uuid.UUID) ([]models.Mailbox, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.account_id, m.mail_domain_id, m.local_part, m.quota_mb, m.created_at, d.domain
		FROM mailboxes m JOIN mail_domains d ON d.id = m.mail_domain_id
		WHERE m.account_id=$1 ORDER BY d.domain, m.local_part`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Mailbox{}
	for rows.Next() {
		var mb models.Mailbox
		if err := rows.Scan(&mb.ID, &mb.AccountID, &mb.MailDomainID, &mb.LocalPart,
			&mb.QuotaMB, &mb.CreatedAt, &mb.Domain); err != nil {
			return nil, err
		}
		out = append(out, mb)
	}
	return out, rows.Err()
}

// GetMailbox trae el buzón junto con su dominio, necesario para poder llamar
// al MailManager (que opera sobre la dirección completa usuario@dominio).
func (s *Store) GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error) {
	var mb models.Mailbox
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, m.account_id, m.mail_domain_id, m.local_part, m.quota_mb, m.created_at, d.domain
		FROM mailboxes m JOIN mail_domains d ON d.id = m.mail_domain_id
		WHERE m.id=$1`, id,
	).Scan(&mb.ID, &mb.AccountID, &mb.MailDomainID, &mb.LocalPart, &mb.QuotaMB, &mb.CreatedAt, &mb.Domain)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &mb, nil
}

func (s *Store) DeleteMailbox(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM mailboxes WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
