import { useCallback, useEffect, useRef, useState } from 'react'
import {
  api, tokens, type AuditEntry, type FirewallStatus, type SecurityStatus, type SystemInfo, type WAFBlock,
} from '../api'
import { Alert, Card, Empty, Spinner, Stat, formatDate, formatUptime } from '../components'
import { Icon } from '../icons'
import { errorMessage, useToast } from '../toast'

type Tab = 'resumen' | 'auditoria' | 'seguridad'

export default function System() {
  const [tab, setTab] = useState<Tab>('resumen')
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
          <p>Estado del servidor, seguridad y bitácora de acciones del panel.</p>
        </div>
        {tab === 'resumen' && <button onClick={() => void syncCaddy()}>Sincronizar Caddy</button>}
      </div>

      {message && <Alert kind="success">{message}</Alert>}
      {error && <Alert kind="error">{error}</Alert>}

      <div className="tabs">
        <button className={tab === 'resumen' ? 'active' : ''} onClick={() => setTab('resumen')}>
          Resumen
        </button>
        <button className={tab === 'seguridad' ? 'active' : ''} onClick={() => setTab('seguridad')}>
          Seguridad
        </button>
        <button className={tab === 'auditoria' ? 'active' : ''} onClick={() => setTab('auditoria')}>
          Auditoría
        </button>
      </div>

      {tab === 'resumen' && (
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
      )}

      {tab === 'seguridad' && (
        <>
          <SecurityCard />
          <FirewallCard />
          <WAFBlocksCard />
        </>
      )}

      {tab === 'auditoria' && (
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
      )}
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

function FirewallCard() {
  const toast = useToast()
  const [status, setStatus] = useState<FirewallStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [action, setAction] = useState<'allow' | 'deny'>('allow')
  const [port, setPort] = useState('')
  const [proto, setProto] = useState<'tcp' | 'udp'>('tcp')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    const s = await api.get<FirewallStatus>('/system/security/firewall')
    setStatus(s)
  }, [])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar el estado del firewall')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    const portNum = Number(port)
    if (!portNum || portNum < 1 || portNum > 65535) {
      setError('Puerto inválido')
      return
    }
    setBusy(true)
    try {
      const s = await api.post<FirewallStatus>('/system/security/firewall/rules', {
        action, port: portNum, proto,
      })
      setStatus(s)
      setPort('')
      toast.success(action === 'allow' ? `Puerto ${portNum}/${proto} abierto` : `Puerto ${portNum}/${proto} cerrado`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo aplicar el cambio')
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Card title="Firewall"><Spinner /></Card>

  if (!status?.configured) {
    return (
      <Card title="Firewall">
        <p className="muted" style={{ marginTop: 0 }}>
          El panel todavía no tiene acceso configurado al firewall (ufw) de este servidor.
          Vuelve a correr <code className="inline">install.sh</code> en el servidor para habilitarlo.
        </p>
      </Card>
    )
  }

  return (
    <Card title="Firewall">
      {error && <Alert kind="error">{error}</Alert>}
      {status.error && <Alert kind="error">No se pudo leer el estado: {status.error}</Alert>}

      {(status.rules ?? []).length === 0 ? (
        <Empty text="No hay reglas activas." />
      ) : (
        <table>
          <thead>
            <tr><th>Puerto</th><th>Protocolo</th><th>Acción</th><th>Origen</th><th></th></tr>
          </thead>
          <tbody>
            {status.rules!.map((r, i) => (
              <tr key={i}>
                <td className="strong">{r.port}</td>
                <td>{r.proto.toUpperCase()}</td>
                <td>
                  <span className={`badge ${r.action === 'allow' ? 'ok' : 'err'}`}>
                    <span className="dot" />{r.action === 'allow' ? 'Permitido' : 'Bloqueado'}
                  </span>
                </td>
                <td className="muted">{r.from}</td>
                <td>
                  {r.port === status.protected_port ? (
                    <span className="muted" style={{ fontSize: 12 }}>Puerto de SSH — protegido</span>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <form onSubmit={submit} style={{ display: 'flex', gap: 10, alignItems: 'flex-end', marginTop: 18 }}>
        <div className="field" style={{ maxWidth: 140 }}>
          <label htmlFor="fw-port">Puerto</label>
          <input id="fw-port" type="number" min={1} max={65535} value={port} required
                 onChange={(e) => setPort(e.target.value)} placeholder="8080" />
        </div>
        <div className="field" style={{ maxWidth: 120 }}>
          <label htmlFor="fw-proto">Protocolo</label>
          <select id="fw-proto" value={proto} onChange={(e) => setProto(e.target.value as 'tcp' | 'udp')}>
            <option value="tcp">TCP</option>
            <option value="udp">UDP</option>
          </select>
        </div>
        <div className="field" style={{ maxWidth: 140 }}>
          <label htmlFor="fw-action">Acción</label>
          <select id="fw-action" value={action} onChange={(e) => setAction(e.target.value as 'allow' | 'deny')}>
            <option value="allow">Abrir</option>
            <option value="deny">Cerrar</option>
          </select>
        </div>
        <button className="primary" disabled={busy}>{busy ? 'Aplicando…' : 'Aplicar'}</button>
      </form>
    </Card>
  )
}

function WAFBlocksCard() {
  const [blocks, setBlocks] = useState<WAFBlock[]>([])
  const [loading, setLoading] = useState(true)
  const [live, setLive] = useState(true)
  const oldestLoadedID = useRef<number | null>(null)

  useEffect(() => {
    api.get<{ blocks: WAFBlock[] }>('/system/security/waf-blocks?limit=50')
      .then((res) => {
        setBlocks(res.blocks)
        if (res.blocks.length > 0) oldestLoadedID.current = res.blocks[0].id
      })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!live) return
    const es = new EventSource(
      `/api/v1/system/security/waf-blocks/stream?access_token=${tokens.access ?? ''}`,
    )
    es.onmessage = (ev) => {
      try {
        const block = JSON.parse(ev.data) as WAFBlock
        setBlocks((prev) => [...prev.slice(-500), block])
      } catch {
        /* línea no parseable: se ignora */
      }
    }
    es.onerror = () => { es.close(); setLive(false) }
    return () => es.close()
  }, [live])

  async function loadMore() {
    if (oldestLoadedID.current === null) return
    const res = await api.get<{ blocks: WAFBlock[] }>(
      `/system/security/waf-blocks?before=${oldestLoadedID.current}&limit=50`,
    )
    if (res.blocks.length === 0) return
    oldestLoadedID.current = res.blocks[0].id
    setBlocks((prev) => [...res.blocks, ...prev])
  }

  return (
    <Card
      title="Registro de bloqueos del WAF"
      actions={
        <button className={`sm ${live ? 'primary' : ''}`} onClick={() => setLive((v) => !v)}>
          <Icon name="activity" size={14} />
          {live ? 'Detener seguimiento' : 'Seguir en vivo'}
          {live && <span className="live-dot" aria-hidden="true" />}
        </button>
      }
    >
      {loading ? (
        <Spinner />
      ) : blocks.length === 0 ? (
        <Empty text="Todavía no se registró ningún bloqueo." />
      ) : (
        <>
          <table>
            <thead>
              <tr><th>Hora</th><th>IP</th><th>Dominio</th><th>URI</th></tr>
            </thead>
            <tbody>
              {[...blocks].reverse().map((b) => (
                <tr key={b.id}>
                  <td className="muted">{formatDate(b.occurred_at)}</td>
                  <td className="strong">{b.client_ip || '—'}</td>
                  <td>{b.hostname || '—'}</td>
                  <td className="muted" style={{ maxWidth: 360, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {b.uri || '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="actions" style={{ marginTop: 10 }}>
            <button className="sm ghost" onClick={() => void loadMore()}>Cargar más</button>
          </div>
        </>
      )}
    </Card>
  )
}
