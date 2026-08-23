import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, tokens, type Account, type CronJob, type Site, type SiteGitConfig, type UsageSample } from '../api'
import { useAuth } from '../auth'
import {
  Alert, Card, Empty, Meter, Spinner, Sparkline, StatusBadge,
  formatDate, useConfirm, useLiveStats,
} from '../components'
import { Icon } from '../icons'
import { errorMessage, useToast } from '../toast'

type Tab = 'general' | 'dominios' | 'git' | 'logs' | 'cron'

const ACTION_LABEL: Record<string, string> = {
  start: 'Sitio arrancado',
  stop: 'Sitio detenido',
  restart: 'Sitio reiniciado',
  redeploy: 'Sitio redesplegado',
}

export default function SiteDetail() {
  const { siteID } = useParams()
  const toast = useToast()
  const { isClient } = useAuth()
  const [site, setSite] = useState<Site | null>(null)
  const [usage, setUsage] = useState<UsageSample[]>([])
  const [plan, setPlan] = useState<Account['plan']>(undefined)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('general')
  const { confirm, dialog } = useConfirm()

  const reload = useCallback(async () => {
    const res = await api.get<{ site: Site }>(`/sites/${siteID}`)
    setSite(res.site)
    try {
      const u = await api.get<{ samples: UsageSample[] }>(`/sites/${siteID}/usage?hours=24`)
      setUsage(u.samples)
    } catch {
      setUsage([])
    }
    // Los topes de CPU/memoria viven en el plan de la cuenta, no en el sitio.
    try {
      const acct = await api.get<{ account: Account }>(`/accounts/${res.site.account_id}`)
      setPlan(acct.account.plan)
    } catch {
      setPlan(undefined)
    }
  }, [siteID])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar el sitio')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function action(name: string, label: string) {
    setBusy(name)
    setError('')
    try {
      await api.post(`/sites/${siteID}/${name}`)
      await reload()
      toast.success(ACTION_LABEL[name] ?? 'Acción aplicada')
    } catch (err) {
      setError(err instanceof Error ? `No se pudo ${label}: ${err.message}` : 'Error')
    } finally {
      setBusy('')
    }
  }

  async function remove() {
    const ok = await confirm('Se eliminará el sitio, su contenedor y sus archivos.')
    if (!ok) return
    try {
      const accountID = site?.account_id
      await api.del(`/sites/${siteID}?delete_files=true`)
      const base = isClient ? '/panel' : '/cuentas'
      window.location.href = accountID ? `${base}/${accountID}` : '/'
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar el sitio'))
    }
  }

  if (loading) return <Spinner />
  if (!site) return <Alert kind="error">Sitio no encontrado</Alert>

  return (
    <>
      {dialog}
      <div className="page-head">
        <div style={{ display: 'flex', gap: 14, alignItems: 'center' }}>
          <span className="entity-avatar" aria-hidden="true">
            <Icon name="server" size={20} />
          </span>
          <div>
            <h1>{site.name}</h1>
            <p>
              <StatusBadge status={site.status} /> · PHP {site.php_version} ·{' '}
              <code className="inline">{site.container_name}</code>
            </p>
          </div>
        </div>
        <div className="actions">
          <button disabled={!!busy} onClick={() => void action('start', 'arrancar')}>
            <Icon name="power" size={15} />Arrancar
          </button>
          <button disabled={!!busy} onClick={() => void action('stop', 'detener')}>
            <Icon name="stop" size={15} />Detener
          </button>
          <button disabled={!!busy} onClick={() => void action('restart', 'reiniciar')}>
            <Icon name="refresh" size={15} />Reiniciar
          </button>
          <button disabled={!!busy} onClick={() => void action('redeploy', 'redesplegar')}>
            <Icon name="redeploy" size={15} />Redesplegar
          </button>
          <button className="danger ghost" onClick={() => void remove()}>
            <Icon name="trash" size={15} />Eliminar
          </button>
        </div>
      </div>

      {error && <Alert kind="error">{error}</Alert>}
      {site.last_error && <Alert kind="error">{site.last_error}</Alert>}

      <div className="tabs">
        {(['general', 'dominios', 'git', 'logs', 'cron'] as Tab[]).map((t) => (
          <button key={t} className={tab === t ? 'active' : ''} onClick={() => setTab(t)}>
            {t === 'general' ? 'General' : t === 'dominios' ? 'Dominios'
              : t === 'git' ? 'Git' : t === 'logs' ? 'Registros' : 'Tareas cron'}
          </button>
        ))}
      </div>

      {tab === 'general' && <GeneralTab site={site} usage={usage} plan={plan} onSaved={reload} />}
      {tab === 'dominios' && <DomainsTab site={site} onChanged={reload} />}
      {tab === 'git' && <GitTab siteID={site.id} />}
      {tab === 'logs' && <LogsTab siteID={site.id} />}
      {tab === 'cron' && <CronTab siteID={site.id} />}
    </>
  )
}

function GeneralTab({ site, usage, plan, onSaved }: {
  site: Site
  usage: UsageSample[]
  plan: Account['plan']
  onSaved: () => Promise<void>
}) {
  const [docRoot, setDocRoot] = useState(site.document_root)
  const [php, setPhp] = useState(site.php_version)
  const [worker, setWorker] = useState(site.worker_script)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState('')
  const { stats: live, stale } = useLiveStats(site.id, site.status === 'running')

  async function save(redeploy: boolean) {
    setSaving(true)
    setMsg('')
    try {
      await api.put(`/sites/${site.id}`, {
        document_root: docRoot,
        php_version: php,
        worker_script: worker,
        redeploy,
      })
      await onSaved()
      setMsg(redeploy ? 'Cambios aplicados y contenedor recreado.' : 'Cambios guardados.')
    } catch (err) {
      setMsg(err instanceof Error ? err.message : 'Error al guardar')
    } finally {
      setSaving(false)
    }
  }

  const cpu = usage.map((u) => u.cpu_percent)
  const mem = usage.map((u) => u.memory_mb)
  const cpuLimitPct = (plan?.cpu_limit ?? 1) * 100
  const memLimit = plan?.memory_limit_mb ?? 0
  const isLive = !!live && !stale
  const cpuNow = isLive ? live.cpu_percent : cpu[cpu.length - 1]
  const memNow = isLive ? live.memory_mb : mem[mem.length - 1]

  return (
    <>
      <div className="stat-grid">
        <div className="stat">
          <span className="stat-icon tone-blue"><Icon name="cpu" size={17} /></span>
          <div className="value" style={{ display: 'flex', alignItems: 'baseline', gap: 4 }}>
            {cpuNow !== undefined ? `${cpuNow.toFixed(1)} %` : '—'}
            {isLive && <span className="live-dot" aria-hidden="true" />}
          </div>
          <div className="label">CPU</div>
          {plan && <Meter tone="blue" value={cpuNow ?? 0} max={cpuLimitPct} />}
          <Sparkline values={cpu} label="Uso de CPU" />
          <div className="hint">últimas 24 h</div>
        </div>
        <div className="stat">
          <span className="stat-icon tone-rose"><Icon name="hard-drive" size={17} /></span>
          <div className="value" style={{ display: 'flex', alignItems: 'baseline', gap: 4 }}>
            {memNow !== undefined ? `${memNow.toFixed(0)} MB` : '—'}
            {isLive && <span className="live-dot" aria-hidden="true" />}
          </div>
          <div className="label">Memoria</div>
          {plan && <Meter tone="rose" value={memNow ?? 0} max={memLimit} />}
          <Sparkline values={mem} label="Uso de memoria" />
          <div className="hint">últimas 24 h</div>
        </div>
        <div className="stat">
          <span className="stat-icon tone-teal"><Icon name="folder" size={17} /></span>
          <div className="value" style={{ fontSize: 14, wordBreak: 'break-all' }}>
            {site.host_path}
          </div>
          <div className="label">Ruta en el host</div>
        </div>
      </div>

      <Card title="Configuración">
        {msg && <Alert kind="success">{msg}</Alert>}
        <div className="row">
          <div className="field">
            <label htmlFor="docroot">Carpeta raíz (dentro de /app)</label>
            <input id="docroot" value={docRoot} onChange={(e) => setDocRoot(e.target.value)} />
          </div>
          <div className="field">
            <label htmlFor="php">Versión de PHP</label>
            <input id="php" value={php} onChange={(e) => setPhp(e.target.value)} />
          </div>
        </div>
        <div className="field">
          <label htmlFor="worker">Script worker</label>
          <input id="worker" value={worker} placeholder="vacío = modo clásico"
                 onChange={(e) => setWorker(e.target.value)} />
        </div>
        <div className="actions">
          <button disabled={saving} onClick={() => void save(false)}>Guardar</button>
          <button className="primary" disabled={saving} onClick={() => void save(true)}>
            <Icon name="redeploy" size={15} />Guardar y redesplegar
          </button>
        </div>
      </Card>
    </>
  )
}

function DomainsTab({ site, onChanged }: { site: Site; onChanged: () => Promise<void> }) {
  const toast = useToast()
  const [fqdn, setFqdn] = useState('')
  const [kind, setKind] = useState('addon')
  const [error, setError] = useState('')

  async function add(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    try {
      await api.post(`/sites/${site.id}/domains`, { fqdn, kind, redirect_to: '' })
      setFqdn('')
      toast.success(`Dominio ${fqdn} añadido`)
      await onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo añadir el dominio')
    }
  }

  async function remove(fqdn: string, id: string) {
    try {
      await api.del(`/domains/${id}`)
      toast.success(`Dominio ${fqdn} quitado`)
      await onChanged()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo quitar el dominio'))
    }
  }

  return (
    <Card title="Dominios">
      {error && <Alert kind="error">{error}</Alert>}

      <form onSubmit={add} style={{ display: 'flex', gap: 10, marginBottom: 18 }}>
        <input placeholder="www.midominio.cl" value={fqdn} required
               onChange={(e) => setFqdn(e.target.value)} />
        <select style={{ maxWidth: 160 }} value={kind} onChange={(e) => setKind(e.target.value)}>
          <option value="addon">Adicional</option>
          <option value="subdomain">Subdominio</option>
          <option value="alias">Alias</option>
        </select>
        <button className="primary"><Icon name="plus" size={15} />Añadir</button>
      </form>

      {(site.domains ?? []).length === 0 ? (
        <Empty text="Este sitio no tiene dominios asociados." />
      ) : (
        <table>
          <thead>
            <tr><th>Dominio</th><th>Tipo</th><th>TLS</th><th></th></tr>
          </thead>
          <tbody>
            {site.domains?.map((d) => (
              <tr key={d.id}>
                <td className="strong">
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 7 }}>
                    <Icon name="globe" size={14} style={{ color: 'var(--ink-muted)' }} />
                    {d.fqdn}
                  </span>
                </td>
                <td>{d.kind}</td>
                <td>{d.tls_mode === 'auto' ? 'Automático (Let’s Encrypt)' : d.tls_mode}</td>
                <td>
                  <button className="sm ghost danger" onClick={() => void remove(d.fqdn, d.id)}>
                    <Icon name="trash" size={14} />Quitar
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  )
}

function CopyField({ label, value, secret }: { label: string; value: string; secret?: boolean }) {
  const toast = useToast()
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      toast.success(`${label} copiado`)
    } catch {
      toast.error('No se pudo copiar')
    }
  }
  return (
    <div className="field">
      <label>{label}</label>
      <div style={{ display: 'flex', gap: 8 }}>
        <code className="inline" style={{ flex: 1, overflow: 'auto', whiteSpace: secret ? 'nowrap' : 'pre-wrap' }}>
          {secret ? '•'.repeat(24) : value}
        </code>
        <button type="button" className="sm ghost" onClick={() => void copy()}>Copiar</button>
      </div>
    </div>
  )
}

const DEPLOY_STATUS_LABEL: Record<SiteGitConfig['last_deploy_status'], string> = {
  never: 'Sin desplegar aún', running: 'Desplegando…', success: 'Desplegado', failed: 'Falló',
}
const DEPLOY_STATUS_CLASS: Record<SiteGitConfig['last_deploy_status'], string> = {
  never: 'idle', running: 'warn', success: 'ok', failed: 'err',
}

function GitTab({ siteID }: { siteID: string }) {
  const toast = useToast()
  const { confirm, dialog } = useConfirm()
  const [git, setGit] = useState<SiteGitConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [repoURL, setRepoURL] = useState('')
  const [branch, setBranch] = useState('main')
  const [autoDeploy, setAutoDeploy] = useState(true)
  const [saving, setSaving] = useState(false)
  const [deploying, setDeploying] = useState(false)
  const [error, setError] = useState('')
  const [showOutput, setShowOutput] = useState(false)

  const reload = useCallback(async () => {
    const res = await api.get<{ connected: boolean; git?: SiteGitConfig }>(`/sites/${siteID}/git`)
    setGit(res.git ?? null)
    if (res.git) {
      setBranch(res.git.branch)
      setAutoDeploy(res.git.auto_deploy)
    }
  }, [siteID])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar la configuración de Git')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function connect(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      const res = await api.post<{ git: SiteGitConfig; deploy_error?: string }>(`/sites/${siteID}/git`, {
        repo_url: repoURL, branch, auto_deploy: autoDeploy,
      })
      setGit(res.git)
      if (res.deploy_error) {
        toast.error(`Repositorio conectado, pero el primer despliegue falló: ${res.deploy_error}`)
      } else {
        toast.success('Repositorio conectado y desplegado')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo conectar el repositorio')
    } finally {
      setSaving(false)
    }
  }

  async function deployNow() {
    setDeploying(true)
    try {
      const res = await api.post<{ git: SiteGitConfig; deploy_error?: string }>(`/sites/${siteID}/git/deploy`)
      setGit(res.git)
      if (res.deploy_error) {
        toast.error(`El despliegue falló: ${res.deploy_error}`)
      } else {
        toast.success('Sitio desplegado')
      }
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo desplegar'))
    } finally {
      setDeploying(false)
    }
  }

  async function disconnect() {
    const ok = await confirm('Se quitará la conexión con el repositorio (no se tocan los archivos ya desplegados).')
    if (!ok) return
    try {
      await api.del(`/sites/${siteID}/git`)
      setGit(null)
      toast.success('Repositorio desconectado')
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo desconectar'))
    }
  }

  if (loading) return <Spinner />

  if (!git) {
    return (
      <Card title="Conectar un repositorio">
        {dialog}
        {error && <Alert kind="error">{error}</Alert>}
        <p className="muted" style={{ marginTop: 0 }}>
          El panel genera una clave SSH propia para este sitio; solo tienes que agregarla como
          "Deploy Key" (de solo lectura) en tu repositorio — no necesitas subir ninguna clave tuya.
        </p>
        <form onSubmit={connect}>
          <div className="row">
            <div className="field">
              <label htmlFor="repo-url">URL del repositorio (SSH)</label>
              <input id="repo-url" placeholder="git@github.com:usuario/repo.git" value={repoURL} required
                     onChange={(e) => setRepoURL(e.target.value)} />
            </div>
            <div className="field">
              <label htmlFor="branch">Rama</label>
              <input id="branch" value={branch} onChange={(e) => setBranch(e.target.value)} />
            </div>
          </div>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontWeight: 400 }}>
            <input type="checkbox" checked={autoDeploy} onChange={(e) => setAutoDeploy(e.target.checked)} />
            Desplegar automáticamente en cada push (webhook)
          </label>
          <div className="actions" style={{ marginTop: 14 }}>
            <button className="primary" disabled={saving}>
              <Icon name="redeploy" size={15} />{saving ? 'Conectando…' : 'Conectar y desplegar'}
            </button>
          </div>
        </form>
      </Card>
    )
  }

  return (
    <>
      {dialog}
      <Card
        title="Repositorio conectado"
        actions={
          <div className="actions">
            <button className="sm primary" disabled={deploying} onClick={() => void deployNow()}>
              <Icon name="redeploy" size={14} />{deploying ? 'Desplegando…' : 'Desplegar ahora'}
            </button>
            <button className="sm ghost danger" onClick={() => void disconnect()}>
              <Icon name="trash" size={14} />Desconectar
            </button>
          </div>
        }
      >
        <div className="row">
          <div className="field">
            <label>Repositorio</label>
            <code className="inline">{git.repo_url}</code>
          </div>
          <div className="field">
            <label>Rama</label>
            <code className="inline">{git.branch}</code>
          </div>
        </div>

        <CopyField label="Clave pública (Deploy Key)" value={git.public_key} />
        <p className="muted" style={{ marginTop: -8 }}>
          Agrégala como Deploy Key de solo lectura en la configuración del repositorio.
        </p>

        <CopyField label="URL del webhook" value={git.webhook_url} />
        <CopyField label="Secreto del webhook" value={git.webhook_secret} secret />
        <p className="muted" style={{ marginTop: -8 }}>
          Configura un webhook de push con esta URL en GitHub (secreto) o GitLab (token) para
          desplegar automáticamente en cada push a <code className="inline">{git.branch}</code>.
        </p>

        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 14 }}>
          <span className={`badge ${DEPLOY_STATUS_CLASS[git.last_deploy_status]}`}>
            <span className="dot" />{DEPLOY_STATUS_LABEL[git.last_deploy_status]}
          </span>
          <span className="muted" style={{ fontSize: 13 }}>{formatDate(git.last_deploy_at)}</span>
          {git.last_deploy_output && (
            <button type="button" className="sm ghost" onClick={() => setShowOutput((v) => !v)}>
              {showOutput ? 'Ocultar salida' : 'Ver salida'}
            </button>
          )}
        </div>
        {showOutput && git.last_deploy_output && (
          <pre className="console" style={{ marginTop: 10, height: 'auto', maxHeight: 280 }}>
            {git.last_deploy_output}
          </pre>
        )}
      </Card>
    </>
  )
}

function LogsTab({ siteID }: { siteID: string }) {
  const [lines, setLines] = useState<string[]>([])
  const [live, setLive] = useState(false)
  const boxRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let cancelled = false
    api.get<string>(`/sites/${siteID}/logs?tail=300`).then((text) => {
      if (!cancelled) setLines(text.split('\n'))
    })
    return () => { cancelled = true }
  }, [siteID])

  useEffect(() => {
    if (!live) return
    // EventSource no permite cabeceras: el token va en la query.
    const es = new EventSource(
      `/api/v1/sites/${siteID}/logs?follow=true&tail=50&access_token=${tokens.access ?? ''}`,
    )
    es.onmessage = (ev) => {
      setLines((prev) => [...prev.slice(-2000), ev.data])
      boxRef.current?.scrollTo({ top: boxRef.current.scrollHeight })
    }
    es.onerror = () => { es.close(); setLive(false) }
    return () => es.close()
  }, [live, siteID])

  return (
    <Card
      title="Registros del contenedor"
      actions={
        <button className={`sm ${live ? 'primary' : ''}`} onClick={() => setLive((v) => !v)}>
          <Icon name="activity" size={14} />
          {live ? 'Detener seguimiento' : 'Seguir en vivo'}
          {live && <span className="live-dot" aria-hidden="true" />}
        </button>
      }
    >
      <div className="console" ref={boxRef}>
        {lines.length === 0 ? 'Sin registros.' : lines.join('\n')}
      </div>
    </Card>
  )
}

function CronTab({ siteID }: { siteID: string }) {
  const toast = useToast()
  const [jobs, setJobs] = useState<CronJob[]>([])
  const [schedule, setSchedule] = useState('*/5 * * * *')
  const [command, setCommand] = useState('')
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    const res = await api.get<{ cron_jobs: CronJob[] }>(`/sites/${siteID}/cron`)
    setJobs(res.cron_jobs)
  }, [siteID])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudieron cargar las tareas cron')))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function add(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    try {
      await api.post(`/sites/${siteID}/cron`, { schedule, command })
      setCommand('')
      toast.success('Tarea cron creada')
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo crear la tarea')
    }
  }

  async function remove(id: string) {
    try {
      await api.del(`/cron/${id}`)
      toast.success('Tarea cron eliminada')
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar la tarea'))
    }
  }

  return (
    <Card title="Tareas programadas">
      {error && <Alert kind="error">{error}</Alert>}

      <form onSubmit={add} style={{ display: 'flex', gap: 10, marginBottom: 18 }}>
        <input style={{ maxWidth: 160 }} value={schedule}
               onChange={(e) => setSchedule(e.target.value)} />
        <input placeholder="php artisan schedule:run" value={command} required
               onChange={(e) => setCommand(e.target.value)} />
        <button className="primary"><Icon name="plus" size={15} />Añadir</button>
      </form>

      {jobs.length === 0 ? (
        <Empty text="No hay tareas programadas." />
      ) : (
        <table>
          <thead>
            <tr><th>Programación</th><th>Comando</th><th>Última ejecución</th><th>Código</th><th></th></tr>
          </thead>
          <tbody>
            {jobs.map((j) => (
              <tr key={j.id}>
                <td className="strong"><code className="inline">{j.schedule}</code></td>
                <td>{j.command}</td>
                <td>{formatDate(j.last_run_at)}</td>
                <td>
                  {j.last_exit_code === 0 ? (
                    <span className="badge ok"><span className="dot" />0</span>
                  ) : j.last_exit_code != null ? (
                    <span className="badge err"><span className="dot" />{j.last_exit_code}</span>
                  ) : '—'}
                </td>
                <td>
                  <button className="sm ghost danger" onClick={() => void remove(j.id)}>
                    <Icon name="trash" size={14} />Eliminar
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  )
}
