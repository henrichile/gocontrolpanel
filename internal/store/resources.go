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

func (s *Store) GetFTP(ctx context.Context, id uuid.UUID) (*models.FTPAccount, error) {
	var f models.FTPAccount
	err := s.pool.QueryRow(ctx, `
		SELECT id, account_id, username, home_path, quota_mb, is_active, created_at
		FROM ftp_accounts WHERE id=$1`, id).
		Scan(&f.ID, &f.AccountID, &f.Username, &f.HomePath, &f.QuotaMB, &f.IsActive, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &f, err
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

// --- Deploy por Git ----------------------------------------------------------

func (s *Store) CreateSiteGitConfig(ctx context.Context, c *models.SiteGitConfig) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO site_git_configs
			(site_id, repo_url, branch, public_key, private_key_enc, webhook_secret, auto_deploy)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING last_deploy_status, created_at, updated_at`,
		c.SiteID, c.RepoURL, c.Branch, c.PublicKey, c.PrivateKeyEnc, c.WebhookSecret, c.AutoDeploy,
	).Scan(&c.LastDeployStatus, &c.CreatedAt, &c.UpdatedAt)
}

func (s *Store) GetSiteGitConfig(ctx context.Context, siteID uuid.UUID) (*models.SiteGitConfig, error) {
	var c models.SiteGitConfig
	err := s.pool.QueryRow(ctx, `
		SELECT site_id, repo_url, branch, public_key, private_key_enc, webhook_secret,
		       auto_deploy, last_deploy_at, last_deploy_status, last_deploy_output,
		       created_at, updated_at
		FROM site_git_configs WHERE site_id=$1`, siteID).
		Scan(&c.SiteID, &c.RepoURL, &c.Branch, &c.PublicKey, &c.PrivateKeyEnc, &c.WebhookSecret,
			&c.AutoDeploy, &c.LastDeployAt, &c.LastDeployStatus, &c.LastDeployOutput,
			&c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (s *Store) UpdateSiteGitConfig(ctx context.Context, siteID uuid.UUID, repoURL, branch string, autoDeploy bool) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE site_git_configs
		SET repo_url=$2, branch=$3, auto_deploy=$4, updated_at=now()
		WHERE site_id=$1`, siteID, repoURL, branch, autoDeploy)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecordGitDeploy(ctx context.Context, siteID uuid.UUID, status, output string) error {
	if len(output) > 8000 {
		output = output[:8000] + "\n… (truncado)"
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE site_git_configs
		SET last_deploy_at=now(), last_deploy_status=$2, last_deploy_output=$3
		WHERE site_id=$1`, siteID, status, output)
	return err
}

func (s *Store) DeleteSiteGitConfig(ctx context.Context, siteID uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM site_git_configs WHERE site_id=$1`, siteID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Log de bloqueos del WAF ------------------------------------------------

func (s *Store) RecordWAFBlock(ctx context.Context, b *models.WAFBlock) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO waf_blocks (client_ip, hostname, uri, unique_id, raw_json)
		VALUES ($1,$2,$3,$4,$5) RETURNING id, occurred_at`,
		b.ClientIP, b.Hostname, b.URI, b.UniqueID, b.RawJSON,
	).Scan(&b.ID, &b.OccurredAt)
}

// ListWAFBlocks devuelve como mucho `limit` bloqueos con id > afterID,
// ordenados del más antiguo al más reciente — sirve tanto para paginar
// historial hacia atrás (afterID=0, se invierte en el handler si hace
// falta) como para el polling del stream en vivo (afterID = último id visto).
func (s *Store) ListWAFBlocks(ctx context.Context, afterID int64, limit int) ([]models.WAFBlock, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, occurred_at, client_ip, hostname, uri, unique_id, raw_json
		FROM waf_blocks WHERE id > $1 ORDER BY id ASC LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.WAFBlock{}
	for rows.Next() {
		var b models.WAFBlock
		if err := rows.Scan(&b.ID, &b.OccurredAt, &b.ClientIP, &b.Hostname,
			&b.URI, &b.UniqueID, &b.RawJSON); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListWAFBlocksBefore trae hasta `limit` bloqueos con id < beforeID,
// ordenados del más antiguo al más reciente — para paginar el historial
// hacia atrás desde la UI ("cargar más").
func (s *Store) ListWAFBlocksBefore(ctx context.Context, beforeID int64, limit int) ([]models.WAFBlock, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, occurred_at, client_ip, hostname, uri, unique_id, raw_json
		FROM waf_blocks WHERE id < $1 ORDER BY id DESC LIMIT $2`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.WAFBlock{}
	for rows.Next() {
		var b models.WAFBlock
		if err := rows.Scan(&b.ID, &b.OccurredAt, &b.ClientIP, &b.Hostname,
			&b.URI, &b.UniqueID, &b.RawJSON); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	// Se pidió DESC (para traer los más recientes por debajo de beforeID),
	// pero se devuelve en orden cronológico ascendente, igual que ListWAFBlocks.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// ListLatestWAFBlocks trae los `limit` bloqueos más recientes, en orden
// cronológico ascendente (igual que las demás — el más nuevo va al final).
// Es lo que carga la pestaña de Seguridad al abrirla.
func (s *Store) ListLatestWAFBlocks(ctx context.Context, limit int) ([]models.WAFBlock, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, occurred_at, client_ip, hostname, uri, unique_id, raw_json
		FROM waf_blocks ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.WAFBlock{}
	for rows.Next() {
		var b models.WAFBlock
		if err := rows.Scan(&b.ID, &b.OccurredAt, &b.ClientIP, &b.Hostname,
			&b.URI, &b.UniqueID, &b.RawJSON); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// LatestWAFBlockID sirve para que un cliente que recién abre el stream en
// vivo arranque desde "ahora" en vez de recibir todo el historial de golpe.
func (s *Store) LatestWAFBlockID(ctx context.Context) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(id), 0) FROM waf_blocks`).Scan(&id)
	return id, err
}

func (s *Store) PruneOldWAFBlocks(ctx context.Context, olderThan time.Duration) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM waf_blocks WHERE occurred_at < $1`, time.Now().Add(-olderThan))
	return err
}

// --- Configuración de seguridad del servidor --------------------------------

func (s *Store) GetSystemSettings(ctx context.Context) (*models.SystemSettings, error) {
	var st models.SystemSettings
	err := s.pool.QueryRow(ctx, `
		SELECT waf_enabled, rate_limit_per_minute, backup_retention_days, updated_at
		FROM system_settings WHERE id`).
		Scan(&st.WAFEnabled, &st.RateLimitPerMinute, &st.BackupRetentionDays, &st.UpdatedAt)
	return &st, err
}

func (s *Store) UpdateSystemSettings(ctx context.Context, wafEnabled bool, rateLimitPerMinute, backupRetentionDays int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE system_settings
		SET waf_enabled=$1, rate_limit_per_minute=$2, backup_retention_days=$3, updated_at=now()
		WHERE id`, wafEnabled, rateLimitPerMinute, backupRetentionDays)
	return err
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
