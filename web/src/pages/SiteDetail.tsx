import { useCallback, useEffect, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, tokens, type CronJob, type Site, type UsageSample } from '../api'
import {
  Alert, Card, Empty, Spinner, Sparkline, StatusBadge, formatDate, useConfirm,
} from '../components'

type Tab = 'general' | 'dominios' | 'logs' | 'cron'

export default function SiteDetail() {
  const { siteID } = useParams()
  const [site, setSite] = useState<Site | null>(null)
  const [usage, setUsage] = useState<UsageSample[]>([])
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
  }, [siteID])

  useEffect(() => {
    reload().finally(() => setLoading(false))
  }, [reload])

  async function action(name: string, label: string) {
    setBusy(name)
    setError('')
    try {
      await api.post(`/sites/${siteID}/${name}`)
      await reload()
    } catch (err) {
      setError(err instanceof Error ? `No se pudo ${label}: ${err.message}` : 'Error')
    } finally {
      setBusy('')
    }
  }

  async function remove() {
    const ok = await confirm('Se eliminará el sitio, su contenedor y sus archivos.')
    if (!ok) return
    await api.del(`/sites/${siteID}?delete_files=true`)
    window.location.href = '/sitios'
  }

  if (loading) return <Spinner />
  if (!site) return <Alert kind="error">Sitio no encontrado</Alert>

  return (
    <>
      {dialog}
      <div className="page-head">
        <div>
          <h1>{site.name}</h1>
          <p>
            <StatusBadge status={site.status} /> · PHP {site.php_version} ·{' '}
            <code className="inline">{site.container_name}</code>
          </p>
        </div>
        <div className="actions">
          <button disabled={!!busy} onClick={() => void action('start', 'arrancar')}>Arrancar</button>
          <button disabled={!!busy} onClick={() => void action('stop', 'detener')}>Detener</button>
          <button disabled={!!busy} onClick={() => void action('restart', 'reiniciar')}>Reiniciar</button>
          <button disabled={!!busy} onClick={() => void action('redeploy', 'redesplegar')}>
            Redesplegar
          </button>
          <button className="danger ghost" onClick={() => void remove()}>Eliminar</button>
        </div>
      </div>

      {error && <Alert kind="error">{error}</Alert>}
      {site.last_error && <Alert kind="error">{site.last_error}</Alert>}

      <div className="tabs">
        {(['general', 'dominios', 'logs', 'cron'] as Tab[]).map((t) => (
          <button key={t} className={tab === t ? 'active' : ''} onClick={() => setTab(t)}>
            {t === 'general' ? 'General' : t === 'dominios' ? 'Dominios'
              : t === 'logs' ? 'Registros' : 'Tareas cron'}
          </button>
        ))}
      </div>

      {tab === 'general' && <GeneralTab site={site} usage={usage} onSaved={reload} />}
      {tab === 'dominios' && <DomainsTab site={site} onChanged={reload} />}
      {tab === 'logs' && <LogsTab siteID={site.id} />}
      {tab === 'cron' && <CronTab siteID={site.id} />}
    </>
  )
}

function GeneralTab({ site, usage, onSaved }: {
  site: Site
  usage: UsageSample[]
  onSaved: () => Promise<void>
}) {
  const [docRoot, setDocRoot] = useState(site.document_root)
  const [php, setPhp] = useState(site.php_version)
  const [worker, setWorker] = useState(site.worker_script)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState('')

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

  return (
    <>
      <div className="stat-grid">
        <div className="stat">
          <div className="label">CPU (24 h)</div>
          <div className="value">{cpu.length ? `${cpu[cpu.length - 1].toFixed(1)} %` : '—'}</div>
          <Sparkline values={cpu} label="Uso de CPU" />
        </div>
        <div className="stat">
          <div className="label">Memoria (24 h)</div>
          <div className="value">{mem.length ? `${mem[mem.length - 1].toFixed(0)} MB` : '—'}</div>
          <Sparkline values={mem} label="Uso de memoria" />
        </div>
        <div className="stat">
          <div className="label">Ruta en el host</div>
          <div className="value" style={{ fontSize: 14, wordBreak: 'break-all' }}>
            {site.host_path}
          </div>
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
            Guardar y redesplegar
          </button>
        </div>
      </Card>
    </>
  )
}

function DomainsTab({ site, onChanged }: { site: Site; onChanged: () => Promise<void> }) {
  const [fqdn, setFqdn] = useState('')
  const [kind, setKind] = useState('addon')
  const [error, setError] = useState('')

  async function add(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    try {
      await api.post(`/sites/${site.id}/domains`, { fqdn, kind, redirect_to: '' })
      setFqdn('')
      await onChanged()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo añadir el dominio')
    }
  }

  async function remove(id: string) {
    await api.del(`/domains/${id}`)
    await onChanged()
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
        <button className="primary">Añadir</button>
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
                <td className="strong">{d.fqdn}</td>
                <td>{d.kind}</td>
                <td>{d.tls_mode === 'auto' ? 'Automático (Let’s Encrypt)' : d.tls_mode}</td>
                <td>
                  <button className="sm ghost danger" onClick={() => void remove(d.id)}>
                    Quitar
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
        <button className="sm" onClick={() => setLive((v) => !v)}>
          {live ? 'Detener seguimiento' : 'Seguir en vivo'}
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
  const [jobs, setJobs] = useState<CronJob[]>([])
  const [schedule, setSchedule] = useState('*/5 * * * *')
  const [command, setCommand] = useState('')
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    const res = await api.get<{ cron_jobs: CronJob[] }>(`/sites/${siteID}/cron`)
    setJobs(res.cron_jobs)
  }, [siteID])

  useEffect(() => { void reload() }, [reload])

  async function add(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    try {
      await api.post(`/sites/${siteID}/cron`, { schedule, command })
      setCommand('')
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo crear la tarea')
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
        <button className="primary">Añadir</button>
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
                <td>{j.last_exit_code ?? '—'}</td>
                <td>
                  <button className="sm ghost danger"
                          onClick={async () => { await api.del(`/cron/${j.id}`); await reload() }}>
                    Eliminar
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
