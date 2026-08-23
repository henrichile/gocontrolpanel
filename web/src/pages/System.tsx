import { useCallback, useEffect, useState } from 'react'
import { api, type AuditEntry, type SecurityStatus, type SystemInfo } from '../api'
import { Alert, Card, Empty, Spinner, Stat, formatDate, formatUptime } from '../components'
import { errorMessage, useToast } from '../toast'

export default function System() {
  const [info, setInfo] = useState<SystemInfo | null>(null)
  const [containers, setContainers] = useState(0)
  const [audit, setAudit] = useState<AuditEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([
      api.get<{ system: SystemInfo; containers: number }>('/system/info'),
      api.get<{ entries: AuditEntry[] }>('/system/audit?limit=60'),
    ])
      .then(([sys, log]) => {
        setInfo(sys.system)
        setContainers(sys.containers)
        setAudit(log.entries)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Error'))
      .finally(() => setLoading(false))
  }, [])

  async function syncCaddy() {
    setMessage('')
    setError('')
    try {
      await api.post('/system/caddy/sync')
      setMessage('Configuración de Caddy regenerada y publicada.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo sincronizar Caddy')
    }
  }

  if (loading) return <Spinner />

  const memPct = info && info.mem_total_mb
    ? Math.round((info.mem_used_mb / info.mem_total_mb) * 100)
    : 0

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Sistema</h1>
          <p>Estado del servidor y bitácora de acciones del panel.</p>
        </div>
        <button onClick={() => void syncCaddy()}>Sincronizar Caddy</button>
      </div>

      {message && <Alert kind="success">{message}</Alert>}
      {error && <Alert kind="error">{error}</Alert>}

      <div className="stat-grid">
        <Stat label="Servidor" value={info?.hostname ?? '—'}
              hint={`${info?.os} / ${info?.arch}`} />
        <Stat label="Carga (1 min)" value={info?.load_avg?.[0]?.toFixed(2) ?? '—'}
              hint={`${info?.cpu_cores ?? 0} núcleos`} />
        <Stat label="Memoria" value={`${memPct} %`}
              hint={`${info?.mem_used_mb ?? 0} de ${info?.mem_total_mb ?? 0} MB`} />
        <Stat label="Disco libre" value={`${info?.disk_free_gb ?? 0} GB`}
              hint={`de ${info?.disk_total_gb ?? 0} GB`} />
        <Stat label="Contenedores" value={containers} hint="gestionados por el panel" />
        <Stat label="Tiempo encendido" value={formatUptime(info?.uptime_secs ?? 0)} />
      </div>

      <SecurityCard />

      <Card title="Bitácora de auditoría">
        {audit.length === 0 ? (
          <Empty text="Sin actividad registrada." />
        ) : (
          <table>
            <thead>
              <tr><th>Fecha</th><th>Usuario</th><th>Acción</th><th>Objetivo</th><th>IP</th></tr>
            </thead>
            <tbody>
              {audit.map((e) => (
                <tr key={e.id}>
                  <td>{formatDate(e.created_at)}</td>
                  <td className="strong">{e.actor_username || '—'}</td>
                  <td><code className="inline">{e.action}</code></td>
                  <td className="muted">{e.target_type} {e.target_id.slice(0, 8)}</td>
                  <td className="muted">{e.ip_address || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </>
  )
}

function SecurityCard() {
  const toast = useToast()
  const [security, setSecurity] = useState<SecurityStatus | null>(null)
  const [waf, setWaf] = useState(false)
  const [rateLimit, setRateLimit] = useState(240)
  const [retention, setRetention] = useState(14)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    const s = await api.get<SecurityStatus>('/system/security')
    setSecurity(s)
    setWaf(s.waf_enabled)
    setRateLimit(s.rate_limit_per_minute)
    setRetention(s.backup_retention_days)
  }, [])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar la configuración de seguridad')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      const s = await api.put<SecurityStatus>('/system/security', {
        waf_enabled: waf, rate_limit_per_minute: rateLimit, backup_retention_days: retention,
      })
      setSecurity(s)
      toast.success('Configuración aplicada')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo guardar')
    } finally {
      setSaving(false)
    }
  }

  const dirty = !!security && (
    waf !== security.waf_enabled ||
    rateLimit !== security.rate_limit_per_minute ||
    retention !== security.backup_retention_days
  )

  if (loading) return <Card title="Seguridad"><Spinner /></Card>

  return (
    <Card title="Seguridad">
      {error && <Alert kind="error">{error}</Alert>}
      <p className="muted" style={{ marginTop: 0 }}>
        Se aplica en caliente contra Caddy al guardar. Si el WAF no arranca (por ejemplo, porque el
        borde todavía usa la imagen oficial de Caddy sin Coraza compilado), el guardado falla y no
        se aplica ningún cambio — no puede dejar el servidor sin certificados por un toggle.
      </p>
      <form onSubmit={save}>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontWeight: 400, marginBottom: 14 }}>
          <input type="checkbox" checked={waf} onChange={(e) => setWaf(e.target.checked)} />
          WAF activo (Coraza + OWASP CRS)
        </label>
        <div className="row">
          <div className="field">
            <label htmlFor="rate-limit">Límite de peticiones por IP (por minuto)</label>
            <input id="rate-limit" type="number" min={10} value={rateLimit}
                   onChange={(e) => setRateLimit(Number(e.target.value))} />
          </div>
          <div className="field">
            <label htmlFor="retention">Retención de backups (días)</label>
            <input id="retention" type="number" min={1} value={retention}
                   onChange={(e) => setRetention(Number(e.target.value))} />
          </div>
        </div>
        <div className="actions" style={{ marginTop: 4 }}>
          <button className="primary" disabled={saving || !dirty}>
            {saving ? 'Aplicando…' : 'Guardar y aplicar'}
          </button>
        </div>
      </form>

      <div className="row" style={{ marginTop: 18 }}>
        <div className="field">
          <label>Contenedores de sitio sin privilegios</label>
          <span className={`badge ${security?.site_non_root ? 'ok' : 'idle'}`}>
            <span className="dot" />{security?.site_non_root ? 'Activo' : 'Inactivo'}
          </span>
        </div>
        <div className="field">
          <label>Verificación en dos pasos (admins/resellers)</label>
          <span>{security?.totp_enabled_admins ?? 0} de {security?.total_admins ?? 0} con 2FA activo</span>
        </div>
      </div>
    </Card>
  )
}
