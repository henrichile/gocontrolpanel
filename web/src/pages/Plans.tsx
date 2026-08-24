import { useEffect, useState } from 'react'
import { api, type Plan } from '../api'
import { Alert, Card, Empty, Modal, SkeletonRows, formatMB, useConfirm } from '../components'
import { errorMessage, useToast } from '../toast'

const emptyPlan = {
  name: '',
  description: '',
  disk_quota_mb: 5120,
  bandwidth_quota_mb: 51200,
  max_sites: 3,
  max_databases: 3,
  max_ftp_accounts: 3,
  max_cron_jobs: 5,
  max_domains: 5,
  max_mailboxes: 5,
  cpu_limit: 1,
  memory_limit_mb: 512,
  php_versions: ['8.3', '8.4'],
  is_default: false,
}

export default function Plans() {
  const toast = useToast()
  const [plans, setPlans] = useState<Plan[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<Plan | null>(null)
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState('')
  const { confirm, dialog } = useConfirm()

  async function reload() {
    const res = await api.get<{ plans: Plan[] }>('/plans')
    setPlans(res.plans)
  }

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudieron cargar los planes')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function remove(p: Plan) {
    const ok = await confirm(`¿Eliminar el plan ${p.name}? Las cuentas que lo usen lo impedirán.`)
    if (!ok) return
    try {
      await api.del(`/plans/${p.id}`)
      toast.success(`Plan ${p.name} eliminado`)
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar el plan'))
    }
  }

  const visible = plans.filter((p) => p.name.toLowerCase().includes(filter.toLowerCase()))

  return (
    <>
      {dialog}
      <div className="page-head">
        <div>
          <h1>Planes</h1>
          <p>Definen las cuotas y los límites de recursos de cada cuenta.</p>
        </div>
        <button className="primary" onClick={() => setCreating(true)}>Nuevo plan</button>
      </div>

      <Card>
        {!loading && plans.length > 3 && (
          <input
            className="table-search"
            placeholder="Buscar plan…"
            aria-label="Buscar planes"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        )}
        {!loading && plans.length === 0 ? (
          <Empty text="No hay planes definidos." />
        ) : !loading && visible.length === 0 ? (
          <Empty text="Ningún plan coincide con la búsqueda." />
        ) : (
          <table>
            <thead>
              <tr>
                <th>Plan</th><th>Disco</th><th>Sitios</th><th>Bases</th><th>Dominios</th><th>Buzones</th>
                <th>CPU</th><th>RAM</th><th>PHP</th><th></th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows cols={10} />
              ) : (
                visible.map((p) => (
                  <tr key={p.id}>
                    <td className="strong">
                      {p.name}{p.is_default && <span className="muted"> · por defecto</span>}
                    </td>
                    <td>{formatMB(p.disk_quota_mb)}</td>
                    <td>{p.max_sites}</td>
                    <td>{p.max_databases}</td>
                    <td>{p.max_domains}</td>
                    <td>{p.max_mailboxes}</td>
                    <td>{p.cpu_limit}</td>
                    <td>{p.memory_limit_mb} MB</td>
                    <td className="muted">{p.php_versions.join(', ')}</td>
                    <td>
                      <div className="actions">
                        <button className="sm ghost" onClick={() => setEditing(p)}>Editar</button>
                        <button className="sm ghost danger" onClick={() => void remove(p)}>
                          Eliminar
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </Card>

      {(creating || editing) && (
        <PlanModal
          plan={editing}
          onClose={() => { setCreating(false); setEditing(null) }}
          onSaved={(name) => {
            setCreating(false); setEditing(null)
            toast.success(editing ? `Plan ${name} actualizado` : `Plan ${name} creado`)
            void reload()
          }}
        />
      )}
    </>
  )
}

function PlanModal({ plan, onClose, onSaved }: {
  plan: Plan | null
  onClose: () => void
  onSaved: (name: string) => void
}) {
  const [form, setForm] = useState({ ...emptyPlan, ...(plan ?? {}) })
  const [phpText, setPhpText] = useState((plan?.php_versions ?? emptyPlan.php_versions).join(', '))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  function num(key: keyof typeof emptyPlan) {
    return (e: React.ChangeEvent<HTMLInputElement>) =>
      setForm({ ...form, [key]: Number(e.target.value) })
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    const payload = {
      ...form,
      php_versions: phpText.split(',').map((v) => v.trim()).filter(Boolean),
    }
    try {
      if (plan) await api.put(`/plans/${plan.id}`, payload)
      else await api.post('/plans', payload)
      onSaved(form.name)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo guardar el plan')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title={plan ? `Editar ${plan.name}` : 'Nuevo plan'} onClose={onClose}>
      <form onSubmit={submit}>
        {error && <Alert kind="error">{error}</Alert>}

        <div className="field">
          <label htmlFor="name">Nombre</label>
          <input id="name" value={form.name} required
                 onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="disk">Cuota de disco (MB)</label>
            <input id="disk" type="number" value={form.disk_quota_mb} onChange={num('disk_quota_mb')} />
          </div>
          <div className="field">
            <label htmlFor="bw">Transferencia (MB/mes)</label>
            <input id="bw" type="number" value={form.bandwidth_quota_mb}
                   onChange={num('bandwidth_quota_mb')} />
          </div>
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="sites">Máx. sitios</label>
            <input id="sites" type="number" value={form.max_sites} onChange={num('max_sites')} />
          </div>
          <div className="field">
            <label htmlFor="dbs">Máx. bases de datos</label>
            <input id="dbs" type="number" value={form.max_databases} onChange={num('max_databases')} />
          </div>
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="domains">Máx. dominios</label>
            <input id="domains" type="number" value={form.max_domains} onChange={num('max_domains')} />
          </div>
          <div className="field">
            <label htmlFor="mailboxes">Máx. buzones de correo</label>
            <input id="mailboxes" type="number" value={form.max_mailboxes} onChange={num('max_mailboxes')} />
          </div>
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="cpu">CPU por sitio (núcleos)</label>
            <input id="cpu" type="number" step="0.25" value={form.cpu_limit} onChange={num('cpu_limit')} />
          </div>
          <div className="field">
            <label htmlFor="mem">Memoria por sitio (MB)</label>
            <input id="mem" type="number" value={form.memory_limit_mb}
                   onChange={num('memory_limit_mb')} />
          </div>
        </div>

        <div className="field">
          <label htmlFor="php">Versiones de PHP permitidas</label>
          <input id="php" value={phpText} onChange={(e) => setPhpText(e.target.value)}
                 placeholder="8.3, 8.4" />
        </div>

        <label style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <input type="checkbox" style={{ width: 'auto' }} checked={form.is_default}
                 onChange={(e) => setForm({ ...form, is_default: e.target.checked })} />
          Plan por defecto para cuentas nuevas
        </label>

        <div className="modal-actions">
          <button type="button" onClick={onClose}>Cancelar</button>
          <button className="primary" disabled={busy}>{busy ? 'Guardando…' : 'Guardar'}</button>
        </div>
      </form>
    </Modal>
  )
}
