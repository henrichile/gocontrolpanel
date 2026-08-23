import { useEffect, useState } from 'react'
import { api, type AuditEntry, type SecurityStatus, type SystemInfo } from '../api'
import { Alert, Card, Empty, Spinner, Stat, formatDate, formatUptime } from '../components'

export default function System() {
  const [info, setInfo] = useState<SystemInfo | null>(null)
  const [containers, setContainers] = useState(0)
  const [security, setSecurity] = useState<SecurityStatus | null>(null)
  const [audit, setAudit] = useState<AuditEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([
      api.get<{ system: SystemInfo; containers: number; security: SecurityStatus }>('/system/info'),
      api.get<{ entries: AuditEntry[] }>('/system/audit?limit=60'),
    ])
      .then(([sys, log]) => {
        setInfo(sys.system)
        setContainers(sys.containers)
        setSecurity(sys.security)
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

      <Card title="Seguridad">
        <p className="muted" style={{ marginTop: 0 }}>
          Esto se configura en el servidor (variables de entorno / <code className="inline">install.sh</code>),
          no desde aquí — cambiar algo requiere editar el <code className="inline">.env</code> y reiniciar.
        </p>
        <div className="row">
          <div className="field">
            <label>WAF (Coraza + OWASP CRS)</label>
            <span className={`badge ${security?.waf_enabled ? 'ok' : 'idle'}`}>
              <span className="dot" />{security?.waf_enabled ? 'Activo' : 'Inactivo'}
            </span>
          </div>
          <div className="field">
            <label>Límite de peticiones por IP</label>
            <span>{security?.rate_limit_per_minute ?? '—'} / minuto{!security?.waf_enabled && ' (sin efecto: WAF inactivo)'}</span>
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label>Retención de backups</label>
            <span>{security?.backup_retention_days ?? '—'} días</span>
          </div>
          <div className="field">
            <label>Contenedores de sitio sin privilegios</label>
            <span className={`badge ${security?.site_non_root ? 'ok' : 'idle'}`}>
              <span className="dot" />{security?.site_non_root ? 'Activo' : 'Inactivo'}
            </span>
          </div>
        </div>
        <div className="field">
          <label>Verificación en dos pasos (admins/resellers)</label>
          <span>
            {security?.totp_enabled_admins ?? 0} de {security?.total_admins ?? 0} con 2FA activo
          </span>
        </div>
      </Card>

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
