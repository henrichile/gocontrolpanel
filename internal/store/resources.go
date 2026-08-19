package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/etasoft/gocontrolpanel/internal/models"
)

// --- Bases de datos de clientes --------------------------------------------

func (s *Store) CreateDatabase(ctx context.Context, d *models.SiteDatabase) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO site_databases (account_id, engine, db_name, db_user, charset)
		VALUES ($1,$2,$3,$4,$5) RETURNING id, created_at`,
		d.AccountID, d.Engine, d.DBName, d.DBUser, d.Charset,
	).Scan(&d.ID, &d.CreatedAt)
}

func (s *Store) ListDatabases(ctx context.Context, accountID uuid.UUID) ([]models.SiteDatabase, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, engine, db_name, db_user, charset, size_mb, created_at
		FROM site_databases WHERE account_id=$1 ORDER BY db_name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.SiteDatabase{}
	for rows.Next() {
		var d models.SiteDatabase
		if err := rows.Scan(&d.ID, &d.AccountID, &d.Engine, &d.DBName, &d.DBUser,
			&d.Charset, &d.SizeMB, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDatabase(ctx context.Context, id uuid.UUID) (*models.SiteDatabase, error) {
	var d models.SiteDatabase
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, engine, db_name, db_user, charset, size_mb, created_at
		FROM site_databases WHERE id=$1`, id).
		Scan(&d.ID, &d.AccountID, &d.Engine, &d.DBName, &d.DBUser, &d.Charset,
			&d.SizeMB, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

func (s *Store) DeleteDatabase(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM site_databases WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Cuentas FTP -----------------------------------------------------------

func (s *Store) CreateFTP(ctx context.Context, f *models.FTPAccount, passwordHash string) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO ftp_accounts (account_id, username, password_hash, home_path, quota_mb, is_active)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`,
		f.AccountID, f.Username, passwordHash, f.HomePath, f.QuotaMB, f.IsActive,
	).Scan(&f.ID, &f.CreatedAt)
}

func (s *Store) ListFTP(ctx context.Context, accountID uuid.UUID) ([]models.FTPAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_id, username, home_path, quota_mb, is_active, created_at
		FROM ftp_accounts WHERE account_id=$1 ORDER BY username`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.FTPAccount{}
	for rows.Next() {
		var f models.FTPAccount
		if err := rows.Scan(&f.ID, &f.AccountID, &f.Username, &f.HomePath,
			&f.QuotaMB, &f.IsActive, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) DeleteFTP(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM ftp_accounts WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Cron ------------------------------------------------------------------

func (s *Store) CreateCron(ctx context.Context, c *models.CronJob) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO cron_jobs (site_id, schedule, command, is_active)
		VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		c.SiteID, c.Schedule, c.Command, c.IsActive,
	).Scan(&c.ID, &c.CreatedAt)
}

func (s *Store) ListCron(ctx context.Context, siteID uuid.UUID) ([]models.CronJob, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, site_id, schedule, command, is_active, last_run_at,
		       last_exit_code, last_output, created_at
		FROM cron_jobs WHERE site_id=$1 ORDER BY created_at`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.CronJob{}
	for rows.Next() {
		var c models.CronJob
		if err := rows.Scan(&c.ID, &c.SiteID, &c.Schedule, &c.Command, &c.IsActive,
			&c.LastRunAt, &c.LastExitCode, &c.LastOutput, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListActiveCron devuelve todas las tareas activas junto al contenedor donde
// deben ejecutarse; lo consume el scheduler interno.
type ScheduledJob struct {
	models.CronJob
	ContainerName string
	SiteStatus    models.SiteStatus
}

func (s *Store) ListActiveCron(ctx context.Context) ([]ScheduledJob, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.site_id, c.schedule, c.command, c.is_active, c.last_run_at,
		       c.last_exit_code, c.last_output, c.created_at,
		       s.container_name, s.status
		FROM cron_jobs c
		JOIN sites s ON s.id = c.site_id
		JOIN accounts a ON a.id = s.account_id
		WHERE c.is_active AND a.status='active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ScheduledJob{}
	for rows.Next() {
		var j ScheduledJob
		if err := rows.Scan(&j.ID, &j.SiteID, &j.Schedule, &j.Command, &j.IsActive,
			&j.LastRunAt, &j.LastExitCode, &j.LastOutput, &j.CreatedAt,
			&j.ContainerName, &j.SiteStatus); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) RecordCronRun(ctx context.Context, id uuid.UUID, exitCode int, output string) error {
	if len(output) > 8000 {
		output = output[:8000] + "\n… (truncado)"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE cron_jobs SET last_run_at=now(), last_exit_code=$2, last_output=$3
		WHERE id=$1`, id, exitCode, output)
	return err
}

func (s *Store) GetCron(ctx context.Context, id uuid.UUID) (*models.CronJob, error) {
	var c models.CronJob
	err := s.pool.QueryRow(ctx, `
		SELECT id, site_id, schedule, command, is_active, last_run_at,
		       last_exit_code, last_output, created_at
		FROM cron_jobs WHERE id=$1`, id).
		Scan(&c.ID, &c.SiteID, &c.Schedule, &c.Command, &c.IsActive, &c.LastRunAt,
			&c.LastExitCode, &c.LastOutput, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (s *Store) DeleteCron(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM cron_jobs WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Auditoría -------------------------------------------------------------

func (s *Store) Audit(ctx context.Context, e models.AuditEntry) {
	var ipArg any
	if e.IPAddress != "" {
		ipArg = e.IPAddress
	}
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (actor_id, actor_username, action, target_type, target_id, detail, ip_address)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.ActorID, e.ActorUsername, e.Action, e.TargetType, e.TargetID, e.Detail, ipArg)
	if err != nil {
		// La auditoría nunca debe romper la petición del usuario.
		_ = err
	}
}

func (s *Store) ListAudit(ctx context.Context, limit int, actor *uuid.UUID) ([]models.AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, actor_id, actor_username, action, target_type, target_id,
	             detail, coalesce(host(ip_address),''), created_at
	      FROM audit_log`
	args := []any{}
	if actor != nil {
		q += ` WHERE actor_id=$1`
		args = append(args, *actor)
	}
	q += ` ORDER BY created_at DESC LIMIT ` + itoa(limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.AuditEntry{}
	for rows.Next() {
		var e models.AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorUsername, &e.Action,
			&e.TargetType, &e.TargetID, &e.Detail, &e.IPAddress, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- Muestras de uso -------------------------------------------------------

func (s *Store) RecordUsage(ctx context.Context, u models.UsageSample) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO usage_samples (site_id, cpu_percent, memory_mb, disk_mb, net_rx_mb, net_tx_mb)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		u.SiteID, u.CPUPercent, u.MemoryMB, u.DiskMB, u.NetRxMB, u.NetTxMB)
	return err
}

func (s *Store) UsageHistory(ctx context.Context, siteID uuid.UUID, since time.Duration) ([]models.UsageSample, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT site_id, cpu_percent, memory_mb, disk_mb, net_rx_mb, net_tx_mb, sampled_at
		FROM usage_samples
		WHERE site_id=$1 AND sampled_at > now() - $2::interval
		ORDER BY sampled_at`, siteID, since.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.UsageSample{}
	for rows.Next() {
		var u models.UsageSample
		if err := rows.Scan(&u.SiteID, &u.CPUPercent, &u.MemoryMB, &u.DiskMB,
			&u.NetRxMB, &u.NetTxMB, &u.SampledAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) PurgeOldUsage(ctx context.Context, keep time.Duration) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM usage_samples WHERE sampled_at < now() - $1::interval`, keep.String())
	return err
}

// --- Resumen para el dashboard ---------------------------------------------

type Overview struct {
	Accounts      int   `json:"accounts"`
	ActiveSites   int   `json:"active_sites"`
	TotalSites    int   `json:"total_sites"`
	Domains       int   `json:"domains"`
	Databases     int   `json:"databases"`
	Users         int   `json:"users"`
	DiskUsedMB    int64 `json:"disk_used_mb"`
	SuspendedAcct int   `json:"suspended_accounts"`
}

func (s *Store) Overview(ctx context.Context, scopeOwner *uuid.UUID) (*Overview, error) {
	var o Overview
	where := ""
	args := []any{}
	if scopeOwner != nil {
		where = ` WHERE owner_id = $1`
		args = append(args, *scopeOwner)
	}
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM accounts`+where+`),
			(SELECT count(*) FROM sites WHERE status='running'`+scopeSuffix(where)+`),
			(SELECT count(*) FROM sites WHERE true`+scopeSuffix(where)+`),
			(SELECT count(*) FROM domains WHERE site_id IN (SELECT id FROM sites WHERE true`+scopeSuffix(where)+`)),
			(SELECT count(*) FROM site_databases WHERE true`+scopeSuffixAcct(where)+`),
			(SELECT count(*) FROM users),
			(SELECT coalesce(sum(disk_used_mb),0) FROM accounts`+where+`),
			(SELECT count(*) FROM accounts WHERE status='suspended'`+scopeAnd(where)+`)
	`, args...).Scan(&o.Accounts, &o.ActiveSites, &o.TotalSites, &o.Domains,
		&o.Databases, &o.Users, &o.DiskUsedMB, &o.SuspendedAcct)
	return &o, err
}

// Helpers para componer el filtro de propietario en la consulta de resumen.
func scopeSuffix(where string) string {
	if where == "" {
		return ""
	}
	return ` AND account_id IN (SELECT id FROM accounts WHERE owner_id = $1)`
}

func scopeSuffixAcct(where string) string { return scopeSuffix(where) }

func scopeAnd(where string) string {
	if where == "" {
		return ""
	}
	return ` AND owner_id = $1`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
