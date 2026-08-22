import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type Account, type ApiError, type Site, type SiteDatabase } from '../api'
import {
  Alert, Card, Empty, Modal, Spinner, StatusBadge, formatMB, useConfirm,
} from '../components'
import { errorMessage, useToast } from '../toast'

export default function AccountDetail() {
  const { accountID } = useParams()
  const toast = useToast()
  const [account, setAccount] = useState<Account | null>(null)
  const [sites, setSites] = useState<Site[]>([])
  const [databases, setDatabases] = useState<SiteDatabase[]>([])
  const [dbHost, setDbHost] = useState('')
  const [loading, setLoading] = useState(true)
  const [tab, setTab] = useState<'sitios' | 'bases'>('sitios')
  const [creatingSite, setCreatingSite] = useState(false)
  const [creatingDB, setCreatingDB] = useState(false)
  const [notice, setNotice] = useState('')
  const { confirm, dialog } = useConfirm()

  const reload = useCallback(async () => {
    const res = await api.get<{ account: Account; sites: Site[] }>(`/accounts/${accountID}`)
    setAccount(res.account)
    setSites(res.sites)
    try {
      const dbs = await api.get<{ databases: SiteDatabase[]; host: string }>(
        `/accounts/${accountID}/databases`,
      )
      setDatabases(dbs.databases)
      setDbHost(dbs.host)
    } catch {
      setDatabases([])
    }
  }, [accountID])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar la cuenta')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function dropDatabase(db: SiteDatabase) {
    const ok = await confirm(`Se eliminará la base de datos ${db.db_name} y su usuario.`)
    if (!ok) return
    try {
      await api.del(`/databases/${db.id}`)
      toast.success(`Base de datos ${db.db_name} eliminada`)
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar la base de datos'))
    }
  }

  if (loading) return <Spinner />
  if (!account) return <Alert kind="error">Cuenta no encontrada</Alert>

  return (
    <>
      {dialog}
      <div className="page-head">
        <div>
          <h1>{account.system_user}</h1>
          <p>
            {account.primary_domain} · plan {account.plan?.name ?? '—'} ·{' '}
            <StatusBadge status={account.status} />
          </p>
        </div>
        <div className="actions">
          <button className="primary" onClick={() => setCreatingSite(true)}>Nuevo sitio</button>
          <button onClick={() => setCreatingDB(true)}>Nueva base de datos</button>
        </div>
      </div>

      {notice && <Alert kind="success">{notice}</Alert>}
      {account.status === 'suspended' && (
        <Alert kind="error">
          Cuenta suspendida{account.suspend_reason ? `: ${account.suspend_reason}` : ''}
        </Alert>
      )}

      <div className="stat-grid">
        <div className="stat">
          <div className="label">Disco</div>
          <div className="value">{formatMB(account.disk_used_mb)}</div>
          <div className="hint">de {formatMB(account.plan?.disk_quota_mb ?? 0)}</div>
        </div>
        <div className="stat">
          <div className="label">Sitios</div>
          <div className="value">{sites.length}</div>
          <div className="hint">máximo {account.plan?.max_sites ?? '—'}</div>
        </div>
        <div className="stat">
          <div className="label">Bases de datos</div>
          <div className="value">{databases.length}</div>
          <div className="hint">máximo {account.plan?.max_databases ?? '—'}</div>
        </div>
        <div className="stat">
          <div className="label">Memoria por sitio</div>
          <div className="value">{account.plan?.memory_limit_mb ?? 0} MB</div>
          <div className="hint">{account.plan?.cpu_limit ?? 0} vCPU</div>
        </div>
      </div>

      <div className="tabs">
        <button className={tab === 'sitios' ? 'active' : ''} onClick={() => setTab('sitios')}>
          Sitios
        </button>
        <button className={tab === 'bases' ? 'active' : ''} onClick={() => setTab('bases')}>
          Bases de datos
        </button>
      </div>

      {tab === 'sitios' && (
        <Card>
          {sites.length === 0 ? (
            <Empty text="Esta cuenta no tiene sitios." />
          ) : (
            <table>
              <thead>
                <tr><th>Sitio</th><th>Dominios</th><th>PHP</th><th>Estado</th></tr>
              </thead>
              <tbody>
                {sites.map((s) => (
                  <tr key={s.id}>
                    <td className="strong"><Link to={`/sitios/${s.id}`}>{s.name}</Link></td>
                    <td>{s.domains?.map((d) => d.fqdn).join(', ') || '—'}</td>
                    <td>PHP {s.php_version}</td>
                    <td><StatusBadge status={s.status} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      )}

      {tab === 'bases' && (
        <Card>
          {databases.length === 0 ? (
            <Empty text="No hay bases de datos en esta cuenta." />
          ) : (
            <table>
              <thead>
                <tr><th>Nombre</th><th>Usuario</th><th>Host</th><th>Tamaño</th><th></th></tr>
              </thead>
              <tbody>
                {databases.map((d) => (
                  <tr key={d.id}>
                    <td className="strong">{d.db_name}</td>
                    <td>{d.db_user}</td>
                    <td className="muted">{dbHost || '—'}</td>
                    <td>{formatMB(d.size_mb)}</td>
                    <td>
                      <button className="sm ghost danger" onClick={() => void dropDatabase(d)}>
                        Eliminar
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      )}

      {creatingSite && (
        <CreateSiteModal
          account={account}
          onClose={() => setCreatingSite(false)}
          onCreated={() => {
            setCreatingSite(false)
            toast.success('Sitio creado')
            void reload()
          }}
        />
      )}

      {creatingDB && (
        <CreateDatabaseModal
          account={account}
          onClose={() => setCreatingDB(false)}
          onCreated={(msg) => { setCreatingDB(false); setNotice(msg); void reload() }}
        />
      )}
    </>
  )
}

function CreateSiteModal({ account, onClose, onCreated }: {
  account: Account
  onClose: () => void
  onCreated: () => void
}) {
  const [form, setForm] = useState({
    account_id: account.id,
    name: '',
    domain: '',
    php_version: account.plan?.php_versions?.slice(-1)[0] ?? '8.4',
    document_root: 'public',
    worker_script: '',
  })
  const [error, setError] = useState('')
  const [fields, setFields] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.post('/sites', form)
      onCreated()
    } catch (err) {
      const apiErr = err as ApiError
      setError(apiErr.message)
      setFields(apiErr.fields ?? {})
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Nuevo sitio" onClose={onClose}>
      <form onSubmit={submit}>
        {error && <Alert kind="error">{error}</Alert>}

        <div className="row">
          <div className="field">
            <label htmlFor="name">Nombre interno</label>
            <input id="name" value={form.name} required
                   onChange={(e) => setForm({ ...form, name: e.target.value })}
                   placeholder="tienda" />
            {fields.name && <div className="field-error">{fields.name}</div>}
          </div>
          <div className="field">
            <label htmlFor="domain">Dominio</label>
            <input id="domain" value={form.domain}
                   onChange={(e) => setForm({ ...form, domain: e.target.value })}
                   placeholder="tienda.miempresa.cl" />
            {fields.domain && <div className="field-error">{fields.domain}</div>}
          </div>
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="php">Versión de PHP</label>
            <select id="php" value={form.php_version}
                    onChange={(e) => setForm({ ...form, php_version: e.target.value })}>
              {(account.plan?.php_versions ?? ['8.3', '8.4']).map((v) => (
                <option key={v} value={v}>PHP {v}</option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="docroot">Carpeta raíz</label>
            <input id="docroot" value={form.document_root}
                   onChange={(e) => setForm({ ...form, document_root: e.target.value })} />
            {fields.document_root && <div className="field-error">{fields.document_root}</div>}
          </div>
        </div>

        <div className="field">
          <label htmlFor="worker">Script worker (opcional)</label>
          <input id="worker" value={form.worker_script} placeholder="public/index.php"
                 onChange={(e) => setForm({ ...form, worker_script: e.target.value })} />
          <div className="muted" style={{ fontSize: 12, marginTop: 4 }}>
            Actívalo solo para frameworks preparados para el modo worker de FrankenPHP
            (Laravel Octane, Symfony Runtime).
          </div>
        </div>

        <div className="modal-actions">
          <button type="button" onClick={onClose}>Cancelar</button>
          <button className="primary" disabled={busy}>{busy ? 'Creando…' : 'Crear sitio'}</button>
        </div>
      </form>
    </Modal>
  )
}

function CreateDatabaseModal({ account, onClose, onCreated }: {
  account: Account
  onClose: () => void
  onCreated: (message: string) => void
}) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ password: string; database: SiteDatabase; host: string }>(
        `/accounts/${account.id}/databases`, { name },
      )
      onCreated(
        `Base de datos ${res.database.db_name} creada. Contraseña (se muestra una sola vez): ${res.password}`,
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo crear la base de datos')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Nueva base de datos" onClose={onClose}>
      <form onSubmit={submit}>
        {error && <Alert kind="error">{error}</Alert>}
        <div className="field">
          <label htmlFor="dbname">Nombre</label>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <code className="inline">{account.system_user}_</code>
            <input id="dbname" value={name} required
                   onChange={(e) => setName(e.target.value)} placeholder="tienda" />
          </div>
        </div>
        <div className="modal-actions">
          <button type="button" onClick={onClose}>Cancelar</button>
          <button className="primary" disabled={busy}>{busy ? 'Creando…' : 'Crear'}</button>
        </div>
      </form>
    </Modal>
  )
}
