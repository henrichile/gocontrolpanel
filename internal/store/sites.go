package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/etasoft/gocontrolpanel/internal/models"
)

const siteCols = `id, account_id, name, php_version, document_root, host_path,
                  container_id, container_name, upstream_host, worker_script,
                  worker_count, php_ini_overrides, env_vars, status, last_error,
                  created_at, updated_at`

func scanSite(row pgx.Row) (*models.Site, error) {
	var s models.Site
	err := row.Scan(&s.ID, &s.AccountID, &s.Name, &s.PHPVersion, &s.DocumentRoot,
		&s.HostPath, &s.ContainerID, &s.ContainerName, &s.UpstreamHost,
		&s.WorkerScript, &s.WorkerCount, &s.PHPIniOverrides, &s.EnvVars,
		&s.Status, &s.LastError, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (s *Store) CreateSite(ctx context.Context, site *models.Site) error {
	if site.PHPIniOverrides == nil {
		site.PHPIniOverrides = map[string]string{}
	}
	if site.EnvVars == nil {
		site.EnvVars = map[string]string{}
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO sites (account_id, name, php_version, document_root, host_path,
			container_name, worker_script, worker_count, php_ini_overrides, env_vars, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, created_at, updated_at`,
		site.AccountID, site.Name, site.PHPVersion, site.DocumentRoot, site.HostPath,
		site.ContainerName, site.WorkerScript, site.WorkerCount,
		site.PHPIniOverrides, site.EnvVars, site.Status,
	).Scan(&site.ID, &site.CreatedAt, &site.UpdatedAt)
}

func (s *Store) GetSite(ctx context.Context, id uuid.UUID) (*models.Site, error) {
	site, err := scanSite(s.pool.QueryRow(ctx, `SELECT `+siteCols+` FROM sites WHERE id=$1`, id))
	if err != nil {
		return nil, err
	}
	site.Domains, err = s.ListDomains(ctx, site.ID)
	return site, err
}

func (s *Store) ListSites(ctx context.Context, accountID *uuid.UUID) ([]models.Site, error) {
	q := `SELECT ` + siteCols + ` FROM sites`
	args := []any{}
	if accountID != nil {
		q += ` WHERE account_id=$1`
		args = append(args, *accountID)
	}
	q += ` ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Site{}
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *site)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Adjuntamos los dominios en una sola consulta extra.
	for i := range out {
		d, err := s.ListDomains(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Domains = d
	}
	return out, nil
}

// ListSitesForOwner devuelve los sitios visibles para un usuario concreto.
func (s *Store) ListSitesForOwner(ctx context.Context, ownerID uuid.UUID) ([]models.Site, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+siteCols+` FROM sites
		WHERE account_id IN (SELECT id FROM accounts WHERE owner_id=$1)
		ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Site{}
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *site)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		d, err := s.ListDomains(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Domains = d
	}
	return out, nil
}

func (s *Store) UpdateSiteRuntime(ctx context.Context, id uuid.UUID,
	containerID, upstream string, status models.SiteStatus, lastErr string) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE sites SET container_id=$2, upstream_host=$3, status=$4,
			last_error=$5, updated_at=now()
		WHERE id=$1`, id, containerID, upstream, status, lastErr)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateSiteStatus(ctx context.Context, id uuid.UUID,
	status models.SiteStatus, lastErr string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE sites SET status=$2, last_error=$3, updated_at=now() WHERE id=$1`,
		id, status, lastErr)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateSiteConfig(ctx context.Context, site *models.Site) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE sites SET php_version=$2, document_root=$3, worker_script=$4,
			worker_count=$5, php_ini_overrides=$6, env_vars=$7, updated_at=now()
		WHERE id=$1`,
		site.ID, site.PHPVersion, site.DocumentRoot, site.WorkerScript,
		site.WorkerCount, site.PHPIniOverrides, site.EnvVars)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSite(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM sites WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Dominios --------------------------------------------------------------

const domainCols = `id, site_id, fqdn, kind, redirect_to, tls_mode, force_https,
                    created_at, updated_at`

func (s *Store) CreateDomain(ctx context.Context, d *models.Domain) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO domains (site_id, fqdn, kind, redirect_to, tls_mode, force_https)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at, updated_at`,
		d.SiteID, d.FQDN, d.Kind, d.RedirectTo, d.TLSMode, d.ForceHTTPS,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}

func (s *Store) ListDomains(ctx context.Context, siteID uuid.UUID) ([]models.Domain, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+domainCols+` FROM domains WHERE site_id=$1 ORDER BY kind, fqdn`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Domain{}
	for rows.Next() {
		var d models.Domain
		if err := rows.Scan(&d.ID, &d.SiteID, &d.FQDN, &d.Kind, &d.RedirectTo,
			&d.TLSMode, &d.ForceHTTPS, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDomain(ctx context.Context, id uuid.UUID) (*models.Domain, error) {
	var d models.Domain
	err := s.pool.QueryRow(ctx, `SELECT `+domainCols+` FROM domains WHERE id=$1`, id).
		Scan(&d.ID, &d.SiteID, &d.FQDN, &d.Kind, &d.RedirectTo, &d.TLSMode,
			&d.ForceHTTPS, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

func (s *Store) DeleteDomain(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM domains WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RoutingTable devuelve todos los dominios activos junto con su upstream, que
// es exactamente lo que necesita el generador de configuración de Caddy.
type Route struct {
	FQDN       string
	Upstream   string
	RedirectTo string
	TLSMode    string
	ForceHTTPS bool
	SiteID     uuid.UUID
	SiteStatus models.SiteStatus
}

func (s *Store) RoutingTable(ctx context.Context) ([]Route, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.fqdn, s.upstream_host, d.redirect_to, d.tls_mode, d.force_https,
		       s.id, s.status
		FROM domains d
		JOIN sites s ON s.id = d.site_id
		JOIN accounts a ON a.id = s.account_id
		WHERE a.status = 'active' AND s.status IN ('running','stopped')
		ORDER BY d.fqdn`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Route{}
	for rows.Next() {
		var r Route
		if err := rows.Scan(&r.FQDN, &r.Upstream, &r.RedirectTo, &r.TLSMode,
			&r.ForceHTTPS, &r.SiteID, &r.SiteStatus); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
