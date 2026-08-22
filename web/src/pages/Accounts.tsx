import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Account, type ApiError, type Plan } from '../api'
import { useAuth } from '../auth'
import {
  Alert, Card, Empty, Modal, SkeletonRows, StatusBadge,
  formatMB, useConfirm, useReasonPrompt,
} from '../components'
import { errorMessage, useToast } from '../toast'

export default function Accounts() {
  const { isReseller } = useAuth()
  const toast = useToast()
  const [accounts, setAccounts] = useState<Account[]>([])
  const [plans, setPlans] = useState<Plan[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState('')
  const { confirm, dialog: confirmDialog } = useConfirm()
  const { ask, dialog: promptDialog } = useReasonPrompt()

  async function reload() {
    const [a, p] = await Promise.all([
      api.get<{ accounts: Account[] }>('/accounts'),
      api.get<{ plans: Plan[] }>('/plans'),
    ])
    setAccounts(a.accounts)
    setPlans(p.plans)
  }

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudieron cargar las cuentas')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function toggleSuspend(acct: Account) {
    const suspending = acct.status === 'active'
    try {
      if (suspending) {
        const reason = await ask('Suspender cuenta', 'Motivo de la suspensión')
        if (reason === null) return
        await api.post(`/accounts/${acct.id}/suspend`, { reason })
        toast.success(`Cuenta ${acct.system_user} suspendida`)
      } else {
        await api.post(`/accounts/${acct.id}/unsuspend`)
        toast.success(`Cuenta ${acct.system_user} reactivada`)
      }
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo actualizar el estado de la cuenta'))
    }
  }

  async function terminate(acct: Account) {
    const ok = await confirm(
      `Se eliminará la cuenta ${acct.system_user}, sus sitios y sus bases de datos. Esta acción no se puede deshacer.`,
    )
    if (!ok) return
    try {
      await api.del(`/accounts/${acct.id}?delete_files=true`)
      toast.success(`Cuenta ${acct.system_user} eliminada`)
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar la cuenta'))
    }
  }

  const visible = accounts.filter((a) => {
    const haystack = [a.system_user, a.primary_domain, a.owner_login ?? ''].join(' ').toLowerCase()
    return haystack.includes(filter.toLowerCase())
  })

  return (
    <>
      {confirmDialog}
      {promptDialog}
      <div className="page-head">
        <div>
          <h1>Cuentas de hosting</h1>
          <p>Cada cuenta agrupa sitios, dominios y bases de datos bajo un mismo plan.</p>
        </div>
        {isReseller && (
          <button className="primary" onClick={() => setCreating(true)}>Crear cuenta</button>
        )}
      </div>

      <Card>
        {!loading && accounts.length > 0 && (
          <input
            className="table-search"
            placeholder="Buscar por usuario, dominio o propietario…"
            aria-label="Buscar cuentas"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        )}
        {!loading && accounts.length === 0 ? (
          <Empty text="No hay cuentas creadas todavía." />
        ) : !loading && visible.length === 0 ? (
          <Empty text="Ninguna cuenta coincide con la búsqueda." />
        ) : (
          <table>
            <thead>
              <tr>
                <th>Usuario</th><th>Dominio principal</th><th>Propietario</th>
                <th>Sitios</th><th>Disco</th><th>Estado</th><th></th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows cols={7} />
              ) : (
                visible.map((a) => (
                  <tr key={a.id}>
                    <td className="strong">
                      <Link to={`/cuentas/${a.id}`}>{a.system_user}</Link>
                    </td>
                    <td>{a.primary_domain}</td>
                    <td className="muted">{a.owner_login ?? '—'}</td>
                    <td>{a.site_count ?? 0}</td>
                    <td>{formatMB(a.disk_used_mb)}</td>
                    <td><StatusBadge status={a.status} /></td>
                    <td>
                      {isReseller && (
                        <div className="actions">
                          <button className="sm ghost" onClick={() => void toggleSuspend(a)}>
                            {a.status === 'active' ? 'Suspender' : 'Reactivar'}
                          </button>
                          <button className="sm ghost danger" onClick={() => void terminate(a)}>
                            Eliminar
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </Card>

      {creating && (
        <CreateAccountModal
          plans={plans}
          onClose={() => setCreating(false)}
          onCreated={(name) => {
            setCreating(false)
            toast.success(`Cuenta ${name} creada`)
            void reload()
          }}
        />
      )}
    </>
  )
}

function CreateAccountModal({ plans, onClose, onCreated }: {
  plans: Plan[]
  onClose: () => void
  onCreated: (systemUser: string) => void
}) {
  const [form, setForm] = useState({
    system_user: '',
    primary_domain: '',
    plan_id: plans.find((p) => p.is_default)?.id ?? plans[0]?.id ?? '',
    php_version: '8.4',
    provision: true,
  })
  const [error, setError] = useState('')
  const [fields, setFields] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    setFields({})
    try {
      await api.post('/accounts', form)
      onCreated(form.system_user)
    } catch (err) {
      const apiErr = err as ApiError
      setError(apiErr.message)
      setFields(apiErr.fields ?? {})
    } finally {
      setBusy(false)
    }
  }

  const selectedPlan = plans.find((p) => p.id === form.plan_id)

  return (
    <Modal title="Crear cuenta de hosting" onClose={onClose}>
      <form onSubmit={submit}>
        {error && <Alert kind="error">{error}</Alert>}

        <div className="row">
          <div className="field">
            <label htmlFor="system_user">Usuario del sistema</label>
            <input
              id="system_user"
              value={form.system_user}
              onChange={(e) => setForm({ ...form, system_user: e.target.value })}
              placeholder="miempresa"
              required
            />
            {fields.system_user && <div className="field-error">{fields.system_user}</div>}
          </div>

          <div className="field">
            <label htmlFor="primary_domain">Dominio principal</label>
            <input
              id="primary_domain"
              value={form.primary_domain}
              onChange={(e) => setForm({ ...form, primary_domain: e.target.value })}
              placeholder="miempresa.cl"
              required
            />
            {fields.primary_domain && <div className="field-error">{fields.primary_domain}</div>}
          </div>
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="plan">Plan</label>
            <select
              id="plan"
              value={form.plan_id}
              onChange={(e) => setForm({ ...form, plan_id: e.target.value })}
            >
              {plans.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          </div>

          <div className="field">
            <label htmlFor="php">Versión de PHP</label>
            <select
              id="php"
              value={form.php_version}
              onChange={(e) => setForm({ ...form, php_version: e.target.value })}
            >
              {(selectedPlan?.php_versions ?? ['8.3', '8.4']).map((v) => (
                <option key={v} value={v}>PHP {v}</option>
              ))}
            </select>
          </div>
        </div>

        <label style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <input
            type="checkbox"
            style={{ width: 'auto' }}
            checked={form.provision}
            onChange={(e) => setForm({ ...form, provision: e.target.checked })}
          />
          Crear el sitio principal y levantar su contenedor FrankenPHP
        </label>

        <div className="modal-actions">
          <button type="button" onClick={onClose}>Cancelar</button>
          <button className="primary" disabled={busy}>
            {busy ? 'Creando…' : 'Crear cuenta'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
