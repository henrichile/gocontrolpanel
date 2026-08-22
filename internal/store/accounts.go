package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/etasoft/gocontrolpanel/internal/models"
)

// --- Planes ----------------------------------------------------------------

const planCols = `id, name, description, disk_quota_mb, bandwidth_quota_mb,
                  max_sites, max_databases, max_ftp_accounts, max_cron_jobs,
                  cpu_limit, memory_limit_mb, php_versions, is_default,
                  created_at, updated_at`

func scanPlan(row pgx.Row) (*models.Plan, error) {
	var p models.Plan
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.DiskQuotaMB, &p.BandwidthQuotaMB,
		&p.MaxSites, &p.MaxDatabases, &p.MaxFTPAccounts, &p.MaxCronJobs,
		&p.CPULimit, &p.MemoryLimitMB, &p.PHPVersions, &p.IsDefault,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) ListPlans(ctx context.Context) ([]models.Plan, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+planCols+` FROM plans ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) GetPlan(ctx context.Context, id uuid.UUID) (*models.Plan, error) {
	return scanPlan(s.pool.QueryRow(ctx, `SELECT `+planCols+` FROM plans WHERE id=$1`, id))
}

func (s *Store) GetDefaultPlan(ctx context.Context) (*models.Plan, error) {
	return scanPlan(s.pool.QueryRow(ctx,
		`SELECT `+planCols+` FROM plans ORDER BY is_default DESC, created_at LIMIT 1`))
}

func (s *Store) CreatePlan(ctx context.Context, p *models.Plan) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO plans (name, description, disk_quota_mb, bandwidth_quota_mb,
			max_sites, max_databases, max_ftp_accounts, max_cron_jobs,
			cpu_limit, memory_limit_mb, php_versions, is_default)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, created_at, updated_at`,
		p.Name, p.Description, p.DiskQuotaMB, p.BandwidthQuotaMB, p.MaxSites,
		p.MaxDatabases, p.MaxFTPAccounts, p.MaxCronJobs, p.CPULimit,
		p.MemoryLimitMB, p.PHPVersions, p.IsDefault,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (s *Store) UpdatePlan(ctx context.Context, p *models.Plan) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE plans SET name=$2, description=$3, disk_quota_mb=$4, bandwidth_quota_mb=$5,
			max_sites=$6, max_databases=$7, max_ftp_accounts=$8, max_cron_jobs=$9,
			cpu_limit=$10, memory_limit_mb=$11, php_versions=$12, is_default=$13,
			updated_at=now()
		WHERE id=$1`,
		p.ID, p.Name, p.Description, p.DiskQuotaMB, p.BandwidthQuotaMB, p.MaxSites,
		p.MaxDatabases, p.MaxFTPAccounts, p.MaxCronJobs, p.CPULimit,
		p.MemoryLimitMB, p.PHPVersions, p.IsDefault)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePlan(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM plans WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Cuentas de hosting ----------------------------------------------------

const accountCols = `a.id, a.owner_id, a.plan_id, a.unix_user, a.primary_domain,
                     a.status, a.suspend_reason, a.disk_used_mb, a.bandwidth_used_mb,
                     a.notes, a.created_at, a.updated_at`

func scanAccount(row pgx.Row) (*models.Account, error) {
	var a models.Account
	err := row.Scan(&a.ID, &a.OwnerID, &a.PlanID, &a.SystemUser, &a.PrimaryDomain,
		&a.Status, &a.SuspendReason, &a.DiskUsedMB, &a.BandwidthUsedMB,
		&a.Notes, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (s *Store) CreateAccount(ctx context.Context, a *models.Account) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO accounts (owner_id, plan_id, unix_user, primary_domain, status, notes)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at, updated_at`,
		a.OwnerID, a.PlanID, a.SystemUser, a.PrimaryDomain, a.Status, a.Notes,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

func (s *Store) GetAccount(ctx context.Context, id uuid.UUID) (*models.Account, error) {
	return scanAccount(s.pool.QueryRow(ctx,
		`SELECT `+accountCols+` FROM accounts a WHERE a.id=$1`, id))
}

// ListAccounts filtra por propietario cuando scopeOwner no es nil.
func (s *Store) ListAccounts(ctx context.Context, scopeOwner *uuid.UUID) ([]models.Account, error) {
	q := `SELECT ` + accountCols + `, u.username,
	             (SELECT count(*) FROM sites s WHERE s.account_id = a.id) AS site_count
	      FROM accounts a JOIN users u ON u.id = a.owner_id`
	args := []any{}
	if scopeOwner != nil {
		q += ` WHERE a.owner_id = $1`
		args = append(args, *scopeOwner)
	}
	q += ` ORDER BY a.created_at DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Account{}
	for rows.Next() {
		var a models.Account
		if err := rows.Scan(&a.ID, &a.OwnerID, &a.PlanID, &a.SystemUser, &a.PrimaryDomain,
			&a.Status, &a.SuspendReason, &a.DiskUsedMB, &a.BandwidthUsedMB, &a.Notes,
			&a.CreatedAt, &a.UpdatedAt, &a.OwnerLogin, &a.SiteCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateAccountStatus(ctx context.Context, id uuid.UUID,
	status models.AccountStatus, reason string) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE accounts SET status=$2, suspend_reason=$3, updated_at=now() WHERE id=$1`,
		id, status, reason)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateAccountPlan(ctx context.Context, id, planID uuid.UUID) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE accounts SET plan_id=$2, updated_at=now() WHERE id=$1`, id, planID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateAccountUsage(ctx context.Context, id uuid.UUID, diskMB, bwMB int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE accounts SET disk_used_mb=$2, bandwidth_used_mb=$3, updated_at=now()
		WHERE id=$1`, id, diskMB, bwMB)
	return err
}

func (s *Store) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountAccountSites se usa para validar la cuota max_sites del plan.
func (s *Store) CountAccountSites(ctx context.Context, accountID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM sites WHERE account_id=$1`, accountID).Scan(&n)
	return n, err
}

func (s *Store) CountAccountDatabases(ctx context.Context, accountID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM site_databases WHERE account_id=$1`, accountID).Scan(&n)
	return n, err
}

func (s *Store) CountAccountFTP(ctx context.Context, accountID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM ftp_accounts WHERE account_id=$1`, accountID).Scan(&n)
	return n, err
}
