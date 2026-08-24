import { useCallback, useEffect, useRef, useState } from 'react'
import {
  api, tokens, type AuditEntry, type FirewallStatus, type SecurityStatus, type SystemInfo, type WAFBlock,
} from '../api'
import { Alert, Card, Empty, Modal, Spinner, Stat, formatDate, formatUptime } from '../components'
import { Icon } from '../icons'
import { errorMessage, useToast } from '../toast'

type Tab = 'resumen' | 'firewall' | 'waf' | 'auditoria'

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
        <button className={tab === 'firewall' ? 'active' : ''} onClick={() => setTab('firewall')}>
          Firewall
        </button>
        <button className={tab === 'waf' ? 'active' : ''} onClick={() => setTab('waf')}>
          WAF
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

      {tab === 'firewall' && <FirewallCard />}

      {tab === 'waf' && (
        <>
          <SecurityCard />
          <ProtectionGrid />
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

  if (loading) return <Card title="Configuración del WAF"><Spinner /></Card>

  return (
    <Card title="Configuración del WAF">
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

// Grilla informativa de lo que cubre el WAF cuando está activo. No son
// interruptores independientes: la API solo expone un único `waf_enabled`
// (motor Coraza + reglas OWASP CRS), así que las cuatro fichas comparten ese
// mismo estado en vez de simular controles que el backend no tiene.
function ProtectionGrid() {
  const [security, setSecurity] = useState<SecurityStatus | null>(null)

  useEffect(() => {
    api.get<SecurityStatus>('/system/security').then(setSecurity).catch(() => setSecurity(null))
  }, [])

  const on = !!security?.waf_enabled

  const items: { icon: Parameters<typeof Icon>[0]['name']; title: string; desc: string; active: boolean }[] = [
    {
      icon: 'shield', title: 'Motor Coraza (ModSecurity)',
      desc: 'Motor central de reglas, compatible con OWASP CRS.', active: on,
    },
    {
      icon: 'lock', title: 'Escaneo SQLi & XSS',
      desc: 'Conjunto de reglas OWASP CRS: previene inyecciones y scripts maliciosos.', active: on,
    },
    {
      icon: 'activity', title: 'Límite de peticiones',
      desc: security ? `${security.rate_limit_per_minute} peticiones por IP y por minuto.` : '—', active: on,
    },
    {
      icon: 'check-circle', title: 'Contenedores sin privilegios',
      desc: 'Cada sitio corre con un usuario sin privilegios de root.',
      active: !!security?.site_non_root,
    },
  ]

  return (
    <Card title="Protecciones activas">
      <div className="protection-grid">
        {items.map((it) => (
          <div className="protection-item" key={it.title}>
            <span
              className="protection-icon"
              style={{ '--tone': it.active ? 'var(--good)' : 'var(--ink-muted)' } as React.CSSProperties}
            >
              <Icon name={it.icon} size={17} />
            </span>
            <div>
              <h3>
                {it.title}
                <span className={`badge ${it.active ? 'ok' : 'idle'}`} style={{ fontSize: 11 }}>
                  <span className="dot" />{it.active ? 'Activo' : 'Inactivo'}
                </span>
              </h3>
              <p>{it.desc}</p>
            </div>
          </div>
        ))}
      </div>
    </Card>
  )
}

function FirewallCard() {
  const toast = useToast()
  const [status, setStatus] = useState<FirewallStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [presetBusy, setPresetBusy] = useState<string | null>(null)
  const [enabledBusy, setEnabledBusy] = useState(false)
  const [showAddModal, setShowAddModal] = useState(false)
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

  async function toggleEnabled() {
    if (!status) return
    setError('')
    setEnabledBusy(true)
    try {
      const s = await api.put<FirewallStatus>('/system/security/firewall/enabled', { enabled: !status.enabled })
      setStatus(s)
      toast.success(s.enabled ? 'Firewall activado' : 'Firewall desactivado')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo cambiar el estado del firewall')
    } finally {
      setEnabledBusy(false)
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

  // Los puertos de cada preset salen de lo que Docker detectó publicado
  // ahora mismo (status.docker_ports, etiquetado por servicio en "from" —
  // ver dockerManagedPorts en el backend), nunca de un número inventado: así
  // el preset de FTP muestra el puerto real de SFTPGo (2022) en vez de
  // 20/21 (que este panel no usa), y el de correo solo lista lo que
  // docker-mailserver realmente publica.
  const dockerByLabel = (label: string) => (status.docker_ports ?? []).filter((r) => r.from === label)

  const presets: {
    key: string; title: string; ports: { port: number; proto: 'tcp' | 'udp' }[]; detected: boolean
  }[] = [
    { key: 'web', title: 'Servidor Web (HTTP/HTTPS)', ports: dockerByLabel('web'), detected: true },
    {
      key: 'ssh', title: 'Acceso SSH',
      ports: [{ port: status.protected_port ?? 22, proto: 'tcp' }], detected: true,
    },
    { key: 'ftp', title: 'SFTP gestionado', ports: dockerByLabel('ftp'), detected: true },
    { key: 'mail', title: 'Correo (SMTP/IMAP)', ports: dockerByLabel('mail'), detected: true },
  ]

  function presetDesc(preset: typeof presets[number]) {
    if (preset.ports.length === 0) {
      return preset.key === 'mail' && !status!.docker_ports
        ? 'Sin datos de Docker'
        : preset.key === 'mail'
          ? 'Correo no está habilitado en este servidor'
          : 'No se detectó el contenedor corriendo'
    }
    return preset.ports.map((p) => `${p.port}/${p.proto}`).join(', ')
  }

  function isPresetActive(preset: typeof presets[number]) {
    if (preset.ports.length === 0) return false
    return preset.ports.every((p) =>
      status!.rules?.some((r) => r.port === p.port && r.proto === p.proto && r.action === 'allow'))
  }

  async function togglePreset(preset: typeof presets[number]) {
    if (preset.ports.length === 0) return
    const active = isPresetActive(preset)
    setError('')
    setPresetBusy(preset.key)
    try {
      let s = status!
      for (const p of preset.ports) {
        s = await api.post<FirewallStatus>('/system/security/firewall/rules', {
          action: active ? 'deny' : 'allow', port: p.port, proto: p.proto,
        })
      }
      setStatus(s)
      toast.success(active ? `${preset.title}: cerrado` : `${preset.title}: abierto`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo aplicar el cambio')
    } finally {
      setPresetBusy(null)
    }
  }

  // Puertos que Docker ya publica pero que el firewall del host todavía no
  // tiene como regla explícita — son igual de accesibles desde afuera
  // (Docker se adelanta a ufw), pero conviene que el firewall también los
  // liste como defensa en profundidad.
  const unsynced = (status.docker_ports ?? []).filter((p) =>
    !status.rules?.some((r) => r.port === p.port && r.proto === p.proto && r.action === 'allow'))

  async function syncDocker() {
    setError('')
    setPresetBusy('sync')
    try {
      const s = await api.post<FirewallStatus>('/system/security/firewall/sync-docker')
      setStatus(s)
      toast.success('Firewall sincronizado con los puertos que Docker tiene publicados')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo sincronizar')
    } finally {
      setPresetBusy(null)
    }
  }

  return (
    <>
      <div className="fw-head">
        <div />
        <div className="fw-global">
          <div>
            <div className="fw-global-label">Estado global</div>
            <span className={`badge ${status.enabled ? 'ok' : 'idle'}`}>
              <span className="dot" />{status.enabled ? 'Firewall activo' : 'Firewall inactivo'}
            </span>
          </div>
          {status.enabled_known && (
            <button
              type="button"
              className={`switch ${status.enabled ? 'on' : ''}`}
              role="switch"
              aria-checked={status.enabled}
              aria-label="Estado global del firewall"
              disabled={enabledBusy}
              onClick={() => void toggleEnabled()}
            >
              <span className="knob" />
            </button>
          )}
        </div>
      </div>

      {error && <Alert kind="error">{error}</Alert>}
      {status.error && <Alert kind="error">No se pudo leer el estado: {status.error}</Alert>}

      {unsynced.length > 0 && (
        <Alert kind="info">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
            <span>
              Docker ya publica {unsynced.length === 1 ? 'un puerto' : `${unsynced.length} puertos`}
              {' '}({unsynced.map((p) => `${p.port}/${p.proto}`).join(', ')}) sin regla en el
              firewall del host — son accesibles igual, pero conviene reflejarlo acá.
            </span>
            <button className="sm primary" disabled={presetBusy !== null} onClick={() => void syncDocker()}>
              {presetBusy === 'sync' ? 'Sincronizando…' : 'Sincronizar'}
            </button>
          </div>
        </Alert>
      )}

      <div className="firewall-grid">
        <Card
          title="Reglas Actuales"
          actions={
            <button className="sm primary" onClick={() => setShowAddModal(true)}>
              <Icon name="plus" size={14} />Añadir Regla de Firewall
            </button>
          }
        >
          {(status.rules ?? []).length === 0 ? (
            <Empty text="No hay reglas activas." />
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Acción</th><th>Protocolo</th><th>Puerto</th><th>Origen</th><th>Comentario</th>
                </tr>
              </thead>
              <tbody>
                {status.rules!.map((r, i) => (
                  <tr key={i}>
                    <td>
                      <span className={`badge ${r.action === 'allow' ? 'ok' : 'err'}`}>
                        <span className="dot" />{r.action === 'allow' ? 'Permitir' : 'Denegar'}
                      </span>
                    </td>
                    <td>{r.proto.toUpperCase()}</td>
                    <td className="strong">{r.port}</td>
                    <td className="muted">{r.from}</td>
                    <td className="muted">
                      {r.comment || (r.port === status.protected_port ? 'Acceso SSH interno' : '—')}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>

        <Card title="Reglas Predefinidas">
          <p className="muted" style={{ marginTop: 0, fontSize: 12.5 }}>
            Activa rápidamente los servicios que este servidor ya tiene corriendo.
          </p>
          <div className="preset-list">
            {presets.map((preset) => {
              const active = isPresetActive(preset)
              const disabled = presetBusy !== null || preset.ports.length === 0
              return (
                <div className="preset-item" key={preset.key}>
                  <div>
                    <h3>{preset.title}</h3>
                    <p>{presetDesc(preset)}</p>
                  </div>
                  <button
                    type="button"
                    className={`switch ${active ? 'on' : ''}`}
                    role="switch"
                    aria-checked={active}
                    aria-label={preset.title}
                    disabled={disabled}
                    onClick={() => void togglePreset(preset)}
                  >
                    <span className="knob" />
                  </button>
                </div>
              )
            })}
          </div>
        </Card>
      </div>

      {showAddModal && (
        <AddFirewallRuleModal
          protectedPort={status.protected_port}
          onClose={() => setShowAddModal(false)}
          onCreated={(s) => { setShowAddModal(false); setStatus(s) }}
        />
      )}
    </>
  )
}

function AddFirewallRuleModal({ protectedPort, onClose, onCreated }: {
  protectedPort?: number
  onClose: () => void
  onCreated: (status: FirewallStatus) => void
}) {
  const toast = useToast()
  const [action, setAction] = useState<'allow' | 'deny'>('allow')
  const [port, setPort] = useState('')
  const [proto, setProto] = useState<'tcp' | 'udp'>('tcp')
  const [origin, setOrigin] = useState('')
  const [comment, setComment] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    const portNum = Number(port)
    if (!portNum || portNum < 1 || portNum > 65535) {
      setError('Puerto inválido')
      return
    }
    if (action === 'deny' && protectedPort && portNum === protectedPort) {
      setError('No se puede bloquear el puerto de SSH')
      return
    }
    setBusy(true)
    try {
      const s = await api.post<FirewallStatus>('/system/security/firewall/rules', {
        action, port: portNum, proto, origin: origin.trim(), comment: comment.trim(),
      })
      toast.success(`Regla ${action === 'allow' ? 'agregada' : 'de bloqueo'} para el puerto ${portNum}/${proto}`)
      onCreated(s)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo aplicar la regla')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Añadir Regla de Firewall" onClose={onClose}>
      <form onSubmit={submit}>
        {error && <Alert kind="error">{error}</Alert>}
        <div className="row">
          <div className="field">
            <label htmlFor="fw-action">Acción</label>
            <select id="fw-action" value={action} onChange={(e) => setAction(e.target.value as 'allow' | 'deny')}>
              <option value="allow">Permitir</option>
              <option value="deny">Denegar</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="fw-proto">Protocolo</label>
            <select id="fw-proto" value={proto} onChange={(e) => setProto(e.target.value as 'tcp' | 'udp')}>
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
            </select>
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="fw-port">Puerto</label>
            <input id="fw-port" type="number" min={1} max={65535} value={port} required
                   onChange={(e) => setPort(e.target.value)} placeholder="8080" />
          </div>
          <div className="field">
            <label htmlFor="fw-origin">Origen</label>
            <input id="fw-origin" value={origin} onChange={(e) => setOrigin(e.target.value)}
                   placeholder="Cualquiera (0.0.0.0/0) o 203.0.113.5/32" />
          </div>
        </div>
        <div className="field">
          <label htmlFor="fw-comment">Comentario</label>
          <input id="fw-comment" value={comment} onChange={(e) => setComment(e.target.value)}
                 placeholder="Para qué es esta regla (opcional)" maxLength={80} />
        </div>
        <div className="modal-actions">
          <button type="button" onClick={onClose}>Cancelar</button>
          <button className="primary" disabled={busy}>{busy ? 'Aplicando…' : 'Añadir regla'}</button>
        </div>
      </form>
    </Modal>
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

  const [ipFilter, setIpFilter] = useState('')
  const filtered = ipFilter
    ? blocks.filter((b) => b.client_ip.toLowerCase().includes(ipFilter.trim().toLowerCase()))
    : blocks

  const now = Date.now()
  const last24h = blocks.filter((b) => now - new Date(b.occurred_at).getTime() <= 24 * 3600 * 1000)
  // 24 cubetas de 1 hora, calculadas solo sobre lo cargado en memoria (no es
  // un total exacto del día si hay más de 50 bloqueos sin cargar), por eso
  // el hint aclara "en lo cargado" en vez de prometer un total del día.
  const buckets = Array.from({ length: 24 }, (_, i) => {
    const from = now - (24 - i) * 3600 * 1000
    const to = from + 3600 * 1000
    return blocks.filter((b) => {
      const t = new Date(b.occurred_at).getTime()
      return t >= from && t < to
    }).length
  })
  const maxBucket = Math.max(...buckets, 1)

  return (
    <>
      <Card title="Actividad del WAF">
        <div className="stat-grid" style={{ marginBottom: 0, gridTemplateColumns: '160px 1fr' }}>
          <div className="stat" style={{ boxShadow: 'none' }}>
            <span className="stat-icon tone-rose"><Icon name="alert-triangle" size={17} /></span>
            <div className="value">{last24h.length}</div>
            <div className="label">Bloqueos (24 h)</div>
            <div className="hint">en lo cargado abajo</div>
          </div>
          <div className="stat" style={{ boxShadow: 'none' }}>
            <div className="label">Bloqueos por hora (últimas 24 h)</div>
            <div className="bar-chart" style={{ '--tone': 'var(--serious)' } as React.CSSProperties}>
              {buckets.map((v, i) => (
                <span
                  key={i}
                  className={`bar ${v > 0 ? 'hot' : ''}`}
                  style={{ height: `${Math.max(4, (v / maxBucket) * 100)}%` }}
                  title={`${v} bloqueo(s)`}
                />
              ))}
            </div>
          </div>
        </div>
      </Card>

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
            <div className="field table-search">
              <input
                value={ipFilter}
                onChange={(e) => setIpFilter(e.target.value)}
                placeholder="Buscar por IP…"
                aria-label="Buscar por IP"
              />
            </div>
            {filtered.length === 0 ? (
              <Empty text="Ninguna IP cargada coincide con la búsqueda." />
            ) : (
              <table>
                <thead>
                  <tr><th>Hora</th><th>IP</th><th>Dominio</th><th>URI</th></tr>
                </thead>
                <tbody>
                  {[...filtered].reverse().map((b) => (
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
            )}
            <div className="actions" style={{ marginTop: 10 }}>
              <button className="sm ghost" onClick={() => void loadMore()}>Cargar más</button>
            </div>
          </>
        )}
      </Card>
    </>
  )
}
