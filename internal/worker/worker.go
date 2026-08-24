// Package worker agrupa las tareas de fondo del panel: muestreo de recursos,
// ejecución de las tareas cron de los sitios y reconciliación del estado real
// de los contenedores con lo que dice la base de datos.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/etasoft/gocontrolpanel/internal/config"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/provision"
	"github.com/etasoft/gocontrolpanel/internal/store"
)

type Runner struct {
	cfg *config.Config
	st  *store.Store
	svc *provision.Service
}

func New(cfg *config.Config, st *store.Store, svc *provision.Service) *Runner {
	return &Runner{cfg: cfg, st: st, svc: svc}
}

// Start lanza todos los bucles de fondo; retornan al cancelar el contexto.
func (r *Runner) Start(ctx context.Context) {
	go r.metricsLoop(ctx)
	go r.cronLoop(ctx)
	go r.reconcileLoop(ctx)
	go r.housekeepingLoop(ctx)
	go r.backupLoop(ctx)
	go r.wafLogLoop(ctx)
	go r.quotaLoop(ctx)
}

// metricsLoop guarda una muestra de CPU/memoria por sitio en ejecución.
func (r *Runner) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sites, err := r.st.ListSites(ctx, nil)
			if err != nil {
				slog.Warn("métricas: no se pudieron listar los sitios", "error", err)
				continue
			}
			for _, site := range sites {
				if site.Status != models.SiteRunning {
					continue
				}
				stats, err := r.svc.Docker().Stats(ctx, site.ContainerName)
				if err != nil {
					continue
				}
				err = r.st.RecordUsage(ctx, models.UsageSample{
					SiteID:     site.ID,
					CPUPercent: stats.CPUPercent,
					MemoryMB:   stats.MemoryMB,
					NetRxMB:    stats.NetRxMB,
					NetTxMB:    stats.NetTxMB,
				})
				if err != nil {
					slog.Warn("métricas: no se pudo guardar la muestra",
						"site", site.Name, "error", err)
				}
			}
		}
	}
}

// cronLoop despierta al inicio de cada minuto y lanza las tareas que tocan.
func (r *Runner) cronLoop(ctx context.Context) {
	// Alineamos con el reloj para no derivar.
	next := time.Now().Truncate(time.Minute).Add(time.Minute)
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-timer.C:
			r.runDueJobs(ctx, now)
			next = next.Add(time.Minute)
			if d := time.Until(next); d > 0 {
				timer.Reset(d)
			} else {
				timer.Reset(time.Second)
			}
		}
	}
}

func (r *Runner) runDueJobs(ctx context.Context, now time.Time) {
	jobs, err := r.st.ListActiveCron(ctx)
	if err != nil {
		slog.Warn("cron: no se pudieron listar las tareas", "error", err)
		return
	}
	for _, job := range jobs {
		if job.SiteStatus != models.SiteRunning {
			continue
		}
		due, err := Matches(job.Schedule, now)
		if err != nil {
			slog.Warn("cron: expresión inválida", "job", job.ID, "schedule", job.Schedule)
			continue
		}
		if !due {
			continue
		}

		go func(j store.ScheduledJob) {
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()

			code, out, err := r.svc.Docker().Exec(runCtx, j.ContainerName,
				[]string{"sh", "-lc", j.Command})
			if err != nil {
				out = out + "\n" + err.Error()
				code = -1
			}
			if err := r.st.RecordCronRun(runCtx, j.ID, code, out); err != nil {
				slog.Warn("cron: no se pudo registrar la ejecución", "job", j.ID, "error", err)
			}
			slog.Info("cron ejecutado", "job", j.ID, "container", j.ContainerName, "exit", code)
		}(job)
	}
}

// reconcileLoop compara el estado real de Docker con la base de datos y
// corrige las diferencias (contenedores caídos, reinicios manuales…).
func (r *Runner) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sites, err := r.st.ListSites(ctx, nil)
			if err != nil {
				continue
			}
			changed := false
			for _, site := range sites {
				if site.Status == models.SiteProvisioning || site.Status == models.SiteDeleting {
					continue
				}
				state, err := r.svc.Docker().State(ctx, site.ContainerName)
				if err != nil {
					continue
				}
				want := site.Status
				switch state {
				case "running":
					want = models.SiteRunning
				case "exited", "created", "paused":
					want = models.SiteStopped
				case "missing":
					want = models.SiteError
				}
				if want != site.Status {
					msg := ""
					if want == models.SiteError {
						msg = "el contenedor no existe; usa 'redesplegar' para recrearlo"
					}
					_ = r.st.UpdateSiteStatus(ctx, site.ID, want, msg)
					changed = true
					slog.Info("estado de sitio reconciliado",
						"site", site.Name, "antes", site.Status, "ahora", want)
				}
			}
			if changed {
				if err := r.svc.SyncCaddy(ctx); err != nil {
					slog.Warn("reconciliación: falló la recarga de Caddy", "error", err)
				}
			}
		}
	}
}

// backupLoop respalda archivos + bases de datos de cada cuenta activa una
// vez al día y purga los backups vencidos según GOCP_BACKUP_RETENTION_DAYS.
func (r *Runner) backupLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			accounts, err := r.st.ListAccounts(ctx, nil)
			if err != nil {
				slog.Warn("backups: no se pudieron listar las cuentas", "error", err)
				continue
			}
			for _, acct := range accounts {
				if acct.Status != models.AccountActive {
					continue
				}
				runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
				err := r.svc.RunAccountBackup(runCtx, acct.ID)
				cancel()
				if err != nil {
					slog.Warn("backups: falló el respaldo de la cuenta",
						"account", acct.SystemUser, "error", err)
					continue
				}
				slog.Info("backup de cuenta completado", "account", acct.SystemUser)
			}
		}
	}
}

// quotaInterval separa cada recálculo de cuotas: caminar el árbol de
// archivos de cada cuenta es más costoso que el resto de los bucles, así que
// corre con menos frecuencia que metricsLoop.
const quotaInterval = 15 * time.Minute

// quotaLoop recalcula disco y transferencia usados por cada cuenta activa y
// aplica lo que no se hace en el camino caliente de la API: disco solo se
// mide aquí (los handlers de archivos leen el valor ya calculado para
// bloquear subidas), y transferencia se acumula y compara contra la cuota
// mensual del plan, suspendiendo la cuenta automáticamente si se supera.
func (r *Runner) quotaLoop(ctx context.Context) {
	ticker := time.NewTicker(quotaInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.enforceQuotas(ctx)
		}
	}
}

func (r *Runner) enforceQuotas(ctx context.Context) {
	accounts, err := r.st.ListAccounts(ctx, nil)
	if err != nil {
		slog.Warn("cuotas: no se pudieron listar las cuentas", "error", err)
		return
	}
	for _, acct := range accounts {
		if acct.Status == models.AccountTerminated {
			continue
		}
		plan, err := r.st.GetPlan(ctx, acct.PlanID)
		if err != nil {
			continue
		}

		// --- Disco: solo se mide; el bloqueo de nuevas escrituras ocurre en
		// los handlers de archivos, comparando contra el valor que se guarda aquí.
		if diskMB, err := r.svc.AccountDiskUsageMB(acct.SystemUser); err == nil {
			if err := r.st.UpdateAccountDiskUsage(ctx, acct.ID, diskMB); err != nil {
				slog.Warn("cuotas: no se pudo guardar el uso de disco",
					"account", acct.SystemUser, "error", err)
			}
		} else {
			slog.Warn("cuotas: no se pudo medir el uso de disco",
				"account", acct.SystemUser, "error", err)
		}

		// --- Transferencia: se acumula un delta por sitio desde la última
		// lectura (Docker solo da tráfico acumulado desde que arrancó el
		// contenedor, así que un reinicio hace que el contador vuelva a 0).
		sites, err := r.st.ListSites(ctx, &acct.ID)
		if err != nil {
			continue
		}
		var deltaMB float64
		for _, site := range sites {
			if site.Status != models.SiteRunning {
				continue
			}
			stats, err := r.svc.Docker().Stats(ctx, site.ContainerName)
			if err != nil {
				continue
			}
			cumulative := stats.NetRxMB + stats.NetTxMB
			baseRx, baseTx, err := r.st.GetSiteNetBaseline(ctx, site.ID)
			if err != nil {
				continue
			}
			baseline := baseRx + baseTx
			if cumulative >= baseline {
				deltaMB += cumulative - baseline
			} else {
				// El contenedor se reinició: el contador de Docker volvió a 0.
				deltaMB += cumulative
			}
			if err := r.st.SetSiteNetBaseline(ctx, site.ID, stats.NetRxMB, stats.NetTxMB); err != nil {
				slog.Warn("cuotas: no se pudo guardar la referencia de tráfico",
					"site", site.Name, "error", err)
			}
		}

		// Reinicio mensual del contador de transferencia.
		if acct.BandwidthResetAt.Format("2006-01") != time.Now().Format("2006-01") {
			if err := r.st.ResetAccountBandwidthUsage(ctx, acct.ID); err != nil {
				slog.Warn("cuotas: no se pudo reiniciar la transferencia mensual",
					"account", acct.SystemUser, "error", err)
			} else {
				acct.BandwidthUsedMB = 0
				if acct.Status == models.AccountSuspended && acct.SuspendReason == provision.AutoSuspendBandwidthReason {
					if err := r.svc.UnsuspendAccount(ctx, acct.ID); err != nil {
						slog.Warn("cuotas: no se pudo reactivar la cuenta tras el reinicio mensual",
							"account", acct.SystemUser, "error", err)
					} else {
						acct.Status = models.AccountActive
						slog.Info("cuenta reactivada: nuevo ciclo mensual de transferencia",
							"account", acct.SystemUser)
					}
				}
			}
		}

		if deltaMB > 0 {
			if err := r.st.AddAccountBandwidthUsage(ctx, acct.ID, int64(deltaMB)); err != nil {
				slog.Warn("cuotas: no se pudo acumular la transferencia",
					"account", acct.SystemUser, "error", err)
			} else {
				acct.BandwidthUsedMB += int64(deltaMB)
			}
		}

		if acct.Status == models.AccountActive && plan.BandwidthQuotaMB > 0 &&
			acct.BandwidthUsedMB >= plan.BandwidthQuotaMB {
			if err := r.svc.SuspendAccount(ctx, acct.ID, provision.AutoSuspendBandwidthReason); err != nil {
				slog.Warn("cuotas: no se pudo suspender la cuenta por exceso de transferencia",
					"account", acct.SystemUser, "error", err)
			} else {
				slog.Warn("cuenta suspendida automáticamente: superó la cuota de transferencia",
					"account", acct.SystemUser, "used_mb", acct.BandwidthUsedMB, "quota_mb", plan.BandwidthQuotaMB)
			}
		}
	}
}

// housekeepingLoop limpia sesiones caducadas y muestras antiguas.
func (r *Runner) housekeepingLoop(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := r.st.PurgeExpiredSessions(ctx); err == nil && n > 0 {
				slog.Info("sesiones caducadas eliminadas", "count", n)
			}
			if err := r.st.PurgeOldUsage(ctx, 30*24*time.Hour); err != nil {
				slog.Warn("no se pudieron limpiar las métricas antiguas", "error", err)
			}
			if err := r.st.PruneOldWAFBlocks(ctx, 30*24*time.Hour); err != nil {
				slog.Warn("no se pudo limpiar el registro de bloqueos del WAF", "error", err)
			}
		}
	}
}
