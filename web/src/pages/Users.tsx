import { useEffect, useState } from 'react'
import { api, type ApiError, type User } from '../api'
import { useAuth } from '../auth'
import { Alert, Card, Empty, Modal, SkeletonRows, formatDate, useConfirm } from '../components'
import { errorMessage, useToast } from '../toast'

export default function Users() {
  const { isAdmin, user: me } = useAuth()
  const toast = useToast()
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState('')
  const { confirm, dialog } = useConfirm()

  async function reload() {
    const res = await api.get<{ users: User[] }>('/users')
    setUsers(res.users)
  }

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudieron cargar los usuarios')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function toggleActive(u: User) {
    try {
      await api.put(`/users/${u.id}`, { is_active: !u.is_active })
      toast.success(u.is_active ? `${u.username} desactivado` : `${u.username} activado`)
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo actualizar el usuario'))
    }
  }

  async function remove(u: User) {
    const ok = await confirm(`¿Eliminar al usuario ${u.username}?`)
    if (!ok) return
    try {
      await api.del(`/users/${u.id}`)
      toast.success(`Usuario ${u.username} eliminado`)
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar'))
    }
  }

  const visible = users.filter((u) => {
    const haystack = [u.username, u.email, u.full_name].join(' ').toLowerCase()
    return haystack.includes(filter.toLowerCase())
  })

  return (
    <>
      {dialog}
      <div className="page-head">
        <div>
          <h1>Usuarios</h1>
          <p>Administradores, revendedores y clientes con acceso al panel.</p>
        </div>
        <button className="primary" onClick={() => setCreating(true)}>Nuevo usuario</button>
      </div>

      <Card>
        {!loading && users.length > 0 && (
          <input
            className="table-search"
            placeholder="Buscar por usuario, correo o nombre…"
            aria-label="Buscar usuarios"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        )}
        {!loading && users.length === 0 ? (
          <Empty text="No hay usuarios." />
        ) : !loading && visible.length === 0 ? (
          <Empty text="Ningún usuario coincide con la búsqueda." />
        ) : (
          <table>
            <thead>
              <tr>
                <th>Usuario</th><th>Correo</th><th>Rol</th>
                <th>Último acceso</th><th>Estado</th><th></th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows cols={6} />
              ) : (
                visible.map((u) => (
                  <tr key={u.id}>
                    <td className="strong">{u.username}</td>
                    <td>{u.email}</td>
                    <td>{roleLabel(u.role)}</td>
                    <td>{formatDate(u.last_login_at)}</td>
                    <td>
                      <span className={`badge ${u.is_active ? 'ok' : 'idle'}`}>
                        <span className="dot" />{u.is_active ? 'Activo' : 'Desactivado'}
                      </span>
                    </td>
                    <td>
                      {u.id !== me?.id && (
                        <div className="actions">
                          <button className="sm ghost" onClick={() => void toggleActive(u)}>
                            {u.is_active ? 'Desactivar' : 'Activar'}
                          </button>
                          {isAdmin && (
                            <button className="sm ghost danger" onClick={() => void remove(u)}>
                              Eliminar
                            </button>
                          )}
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
        <CreateUserModal
          canCreateAdmin={isAdmin}
          onClose={() => setCreating(false)}
          onCreated={(username) => {
            setCreating(false)
            toast.success(`Usuario ${username} creado`)
            void reload()
          }}
        />
      )}
    </>
  )
}

function CreateUserModal({ canCreateAdmin, onClose, onCreated }: {
  canCreateAdmin: boolean
  onClose: () => void
  onCreated: (username: string) => void
}) {
  const [form, setForm] = useState({
    username: '', email: '', password: '', full_name: '', role: 'user',
  })
  const [error, setError] = useState('')
  const [fields, setFields] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.post('/users', form)
      onCreated(form.username)
    } catch (err) {
      const apiErr = err as ApiError
      setError(apiErr.message)
      setFields(apiErr.fields ?? {})
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Nuevo usuario" onClose={onClose}>
      <form onSubmit={submit}>
        {error && <Alert kind="error">{error}</Alert>}

        <div className="row">
          <div className="field">
            <label htmlFor="username">Usuario</label>
            <input id="username" value={form.username} required
                   onChange={(e) => setForm({ ...form, username: e.target.value })} />
          </div>
          <div className="field">
            <label htmlFor="email">Correo</label>
            <input id="email" type="email" value={form.email} required
                   onChange={(e) => setForm({ ...form, email: e.target.value })} />
          </div>
        </div>

        <div className="field">
          <label htmlFor="full_name">Nombre completo</label>
          <input id="full_name" value={form.full_name}
                 onChange={(e) => setForm({ ...form, full_name: e.target.value })} />
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="password">Contraseña</label>
            <input id="password" type="password" value={form.password} required minLength={10}
                   onChange={(e) => setForm({ ...form, password: e.target.value })} />
            {fields.password && <div className="field-error">{fields.password}</div>}
          </div>
          <div className="field">
            <label htmlFor="role">Rol</label>
            <select id="role" value={form.role}
                    onChange={(e) => setForm({ ...form, role: e.target.value })}>
              <option value="user">Usuario</option>
              {canCreateAdmin && <option value="reseller">Revendedor</option>}
              {canCreateAdmin && <option value="admin">Administrador</option>}
            </select>
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

function roleLabel(role: string) {
  switch (role) {
    case 'admin': return 'Administrador'
    case 'reseller': return 'Revendedor'
    default: return 'Usuario'
  }
}
