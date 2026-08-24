import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  api, type Account, type ApiError, type BackupFile, type FTPAccount, type Site, type SiteDatabase,
} from '../api'
import {
  Alert, Card, Empty, LiveMetric, Meter, Modal, Spinner, StatCard, StatusBadge,
  formatMB, useConfirm, useLiveStats,
} from '../components'
import { FileManager } from '../filemanager'
import { Icon } from '../icons'
import { MailTab } from '../mailtab'
import { errorMessage, useToast } from '../toast'

// Panel de hosting del cliente: mismo tipo de datos que la vista de
// administración de cuentas (AccountDetail.tsx), pero es una página propia
// del "ambiente" del cliente — sin nada que sugiera que hay otras cuentas
// en el sistema (sin selector, sin encabezado de "gestionando cuenta X").

function formatBytes(b: number): string {
  if (b >= 1024 ** 3) return `${(b / 1024 ** 3).toFixed(1)} GB`
  if (b >= 1024 ** 2) return `${(b / 1024 ** 2).toFixed(1)} MB`
  if (b >= 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${b} B`
}

export default function AccountPanel() {
  const { accountID } = useParams()
  const toast = useToast()
  const [account, setAccount] = useState<Account | null>(null)
  const [sites, setSites] = useState<Site[]>([])
  const [databases, setDatabases] = useState<SiteDatabase[]>([])
  const [dbHost, setDbHost] = useState('')
  const [ftpAccounts, setFtpAccounts] = useState<FTPAccount[]>([])
  const [sftpHost, setSftpHost] = useState('')
  const [sftpPort, setSftpPort] = useState(0)
  const [loading, setLoading] = useState(true)
  const [tab, setTab] = useState<'sitios' | 'archivos' | 'bases' | 'sftp' | 'correo' | 'backups'>('sitios')
  const [backups, setBackups] = useState<BackupFile[]>([])
  const [backingUp, setBackingUp] = useState(false)
  const [creatingSite, setCreatingSite] = useState(false)
  const [creatingDB, setCreatingDB] = useState(false)
  const [creatingFTP, setCreatingFTP] = useState(false)
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
    try {
      const ftp = await api.get<{ ftp_accounts: FTPAccount[]; host: string; port: number }>(
        `/accounts/${accountID}/ftp`,
      )
      setFtpAccounts(ftp.ftp_accounts)
      setSftpHost(ftp.host)
      setSftpPort(ftp.port)
    } catch {
      setFtpAccounts([])
    }
    try {
      const b = await api.get<{ backups: BackupFile[] }>(`/accounts/${accountID}/backups`)
      setBackups(b.backups)
    } catch {
      setBackups([])
    }
  }, [accountID])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar tu hosting')))
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

  async function dropFTP(f: FTPAccount) {
    const ok = await confirm(`Se eliminará el acceso SFTP ${f.username}.`)
    if (!ok) return
    try {
      await api.del(`/ftp/${f.id}`)
      toast.success(`Acceso SFTP ${f.username} eliminado`)
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar el acceso SFTP'))
    }
  }

  async function runBackup() {
    setBackingUp(true)
    try {
      await api.post(`/accounts/${accountID}/backups`)
      toast.success('Backup completado')
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo completar el backup'))
    } finally {
      setBackingUp(false)
    }
  }

  async function downloadBackup(b: BackupFile) {
    try {
      await api.download(`/accounts/${accountID}/backups/download?name=${encodeURIComponent(b.name)}`, b.name)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo descargar el backup'))
    }
  }

  async function dropBackup(b: BackupFile) {
    const ok = await confirm(`Se eliminará el backup "${b.name}".`)
    if (!ok) return
    try {
      await api.del(`/accounts/${accountID}/backups?name=${encodeURIComponent(b.name)}`)
      toast.success('Backup eliminado')
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar el backup'))
    }
  }

  if (loading) return <Spinner />
  if (!account) return <Alert kind="error">No se pudo cargar tu hosting</Alert>

  return (
    <>
      {dialog}
      <div className="page-head">
        <div>
          <h1>Mi hosting</h1>
          <p>
            {account.primary_domain || 'Sin dominio principal aún'} · plan {account.plan?.name ?? '—'} ·{' '}
            <StatusBadge status={account.status} />
          </p>
        </div>
        <div className="actions">
          <button className="primary" onClick={() => setCreatingSite(true)}>
            <Icon name="plus" />Nuevo sitio
          </button>
          <button onClick={() => setCreatingDB(true)}>
            <Icon name="database" />Nueva base de datos
          </button>
          <button onClick={() => setCreatingFTP(true)}>
            <Icon name="folder" />Nuevo acceso SFTP
          </button>
        </div>
      </div>

      {notice && <Alert kind="success">{notice}</Alert>}
      {account.status === 'suspended' && (
        <Alert kind="error">
          Tu cuenta está suspendida{account.suspend_reason ? `: ${account.suspend_reason}` : ''}.
          Contacta al administrador.
        </Alert>
      )}

      <div className="stat-grid">
        <StatCard
          icon="hard-drive" tone="blue" label="Disco"
          value={formatMB(account.disk_used_mb)}
          hint={`de ${formatMB(account.plan?.disk_quota_mb ?? 0)}`}
          used={account.disk_used_mb} max={account.plan?.disk_quota_mb ?? 0}
        />
        <StatCard
          icon="activity" tone="violet" label="Transferencia (este mes)"
          value={formatMB(account.bandwidth_used_mb)}
          hint={`de ${formatMB(account.plan?.bandwidth_quota_mb ?? 0)}`}
          used={account.bandwidth_used_mb} max={account.plan?.bandwidth_quota_mb ?? 0}
        />
        <StatCard
          icon="server" tone="violet" label="Sitios"
          value={sites.length}
          hint={`máximo ${account.plan?.max_sites ?? '—'}`}
          used={sites.length} max={account.plan?.max_sites ?? 0}
        />
        <StatCard
          icon="database" tone="teal" label="Bases de datos"
          value={databases.length}
          hint={`máximo ${account.plan?.max_databases ?? '—'}`}
          used={databases.length} max={account.plan?.max_databases ?? 0}
        />
        <StatCard
          icon="cpu" tone="rose" label="Memoria por sitio"
          value={`${account.plan?.memory_limit_mb ?? 0} MB`}
          hint={`${account.plan?.cpu_limit ?? 0} vCPU`}
        />
      </div>

      <div className="tabs">
        <button className={tab === 'sitios' ? 'active' : ''} onClick={() => setTab('sitios')}>
          Sitios
        </button>
        <button className={tab === 'archivos' ? 'active' : ''} onClick={() => setTab('archivos')}>
          Archivos
        </button>
        <button className={tab === 'bases' ? 'active' : ''} onClick={() => setTab('bases')}>
          Bases de datos
        </button>
        <button className={tab === 'sftp' ? 'active' : ''} onClick={() => setTab('sftp')}>
          Acceso SFTP
        </button>
        <button className={tab === 'correo' ? 'active' : ''} onClick={() => setTab('correo')}>
          Correo
        </button>
        <button className={tab === 'backups' ? 'active' : ''} onClick={() => setTab('backups')}>
          Backups
        </button>
      </div>

      {tab === 'sitios' && (
        <Card>
          {sites.length === 0 ? (
            <Empty text="Todavía no tienes sitios. Crea el primero." />
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Sitio</th><th>Dominios</th><th>PHP</th>
                  <th>CPU</th><th>Memoria</th><th>Estado</th>
                </tr>
              </thead>
              <tbody>
                {sites.map((s) => (
                  <SiteRow
                    key={s.id} site={s}
                    cpuLimit={account.plan?.cpu_limit ?? 1}
                    memLimit={account.plan?.memory_limit_mb ?? 0}
                  />
                ))}
              </tbody>
            </table>
          )}
        </Card>
      )}

      {tab === 'archivos' && <FileManager accountID={account.id} />}

      {tab === 'bases' && (
        <Card>
          {databases.length === 0 ? (
            <Empty text="No tienes bases de datos todavía." />
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
                        <Icon name="trash" size={14} />Eliminar
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      )}

      {tab === 'sftp' && (
        <Card>
          {ftpAccounts.length === 0 ? (
            <Empty text="Todavía no tienes accesos SFTP. Crea uno para subir tu sitio." />
          ) : (
            <table>
              <thead>
                <tr><th>Usuario</th><th>Servidor</th><th>Ruta</th><th></th></tr>
              </thead>
              <tbody>
                {ftpAccounts.map((f) => (
                  <tr key={f.id}>
                    <td className="strong">{f.username}</td>
                    <td className="muted">
                      <code className="inline">{sftpHost || '—'}:{sftpPort || 22}</code>
                    </td>
                    <td className="muted" style={{ wordBreak: 'break-all' }}>{f.home_path}</td>
                    <td>
                      <button className="sm ghost danger" onClick={() => void dropFTP(f)}>
                        <Icon name="trash" size={14} />Eliminar
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      )}

      {tab === 'correo' && (
        <MailTab
          accountID={account.id}
          siteDomains={Array.from(new Set(sites.flatMap((s) => s.domains?.map((d) => d.fqdn) ?? [])))}
        />
      )}

      {tab === 'backups' && (
        <Card
          title="Backups"
          actions={
            <button className="sm primary" disabled={backingUp} onClick={() => void runBackup()}>
              <Icon name="redeploy" size={14} />{backingUp ? 'Respaldando…' : 'Respaldar ahora'}
            </button>
          }
        >
          <p className="muted" style={{ marginTop: 0 }}>
            Incluye los archivos de todos tus sitios y un dump de cada base de datos. Se genera
            uno automáticamente cada día; los más viejos se eliminan según la retención configurada
            por el administrador del servidor.
          </p>
          {backups.length === 0 ? (
            <Empty text="Todavía no hay backups." />
          ) : (
            <table>
              <thead>
                <tr><th>Archivo</th><th>Tamaño</th><th>Fecha</th><th></th></tr>
              </thead>
              <tbody>
                {backups.map((b) => (
                  <tr key={b.name}>
                    <td className="strong">
                      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
                        <Icon name="archive" size={14} style={{ color: 'var(--ink-muted)' }} />
                        {b.name}
                      </span>
                    </td>
                    <td className="muted">{formatBytes(b.size_b)}</td>
                    <td className="muted">{new Date(b.mod_time).toLocaleString('es-CL')}</td>
                    <td>
                      <div className="actions">
                        <button className="sm ghost" onClick={() => void downloadBackup(b)}>
                          <Icon name="download" size={14} />
                        </button>
                        <button className="sm ghost danger" onClick={() => void dropBackup(b)}>
                          <Icon name="trash" size={14} />
                        </button>
                      </div>
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

      {creatingFTP && (
        <CreateFTPModal
          account={account}
          onClose={() => setCreatingFTP(false)}
          onCreated={(msg) => { setCreatingFTP(false); setNotice(msg); void reload() }}
        />
      )}
    </>
  )
}

function SiteRow({ site, cpuLimit, memLimit }: { site: Site; cpuLimit: number; memLimit: number }) {
  const { stats, stale } = useLiveStats(site.id, site.status === 'running')
  const live = stats && !stale

  return (
    <tr>
      <td className="strong"><Link to={`/sitios/${site.id}`}>{site.name}</Link></td>
      <td>{site.domains?.map((d) => d.fqdn).join(', ') || '—'}</td>
      <td>PHP {site.php_version}</td>
      <td>
        {live ? (
          <div className="live-cell">
            <LiveMetric icon="cpu" value={stats.cpu_percent.toFixed(1)} unit="%" title="Uso de CPU en vivo" />
            <Meter compact tone="blue" value={stats.cpu_percent} max={cpuLimit * 100} />
          </div>
        ) : (
          <span className="muted">—</span>
        )}
      </td>
      <td>
        {live ? (
          <div className="live-cell">
            <LiveMetric icon="hard-drive" value={stats.memory_mb.toFixed(0)} unit="MB" title="Memoria en uso en vivo" />
            <Meter compact tone="rose" value={stats.memory_mb} max={memLimit} />
          </div>
        ) : (
          <span className="muted">—</span>
        )}
      </td>
      <td><StatusBadge status={site.status} /></td>
    </tr>
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

function CreateFTPModal({ account, onClose, onCreated }: {
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
      const res = await api.post<{ password: string; ftp_account: FTPAccount; host: string; port: number }>(
        `/accounts/${account.id}/ftp`, { name },
      )
      onCreated(
        `Acceso SFTP ${res.ftp_account.username} creado en ${res.host}:${res.port}. ` +
        `Contraseña (se muestra una sola vez): ${res.password}`,
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo crear el acceso SFTP')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Nuevo acceso SFTP" onClose={onClose}>
      <form onSubmit={submit}>
        {error && <Alert kind="error">{error}</Alert>}
        <div className="field">
          <label htmlFor="ftpname">Usuario</label>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <code className="inline">{account.system_user}</code>
            <span className="muted">(dejar vacío = usuario principal)</span>
          </div>
          <input id="ftpname" value={name} placeholder="colaborador (opcional)"
                 onChange={(e) => setName(e.target.value)} style={{ marginTop: 8 }} />
        </div>
        <p className="muted" style={{ fontSize: 12 }}>
          El acceso llega a toda la cuenta: desde ahí se navega a cada sitio en
          <code className="inline" style={{ margin: '0 4px' }}>sites/&lt;nombre&gt;</code>
          para subir su código.
        </p>
        <div className="modal-actions">
          <button type="button" onClick={onClose}>Cancelar</button>
          <button className="primary" disabled={busy}>{busy ? 'Creando…' : 'Crear'}</button>
        </div>
      </form>
    </Modal>
  )
}
