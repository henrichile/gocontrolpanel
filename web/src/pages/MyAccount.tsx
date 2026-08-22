import { useEffect, useState } from 'react'
import { Link, Navigate } from 'react-router-dom'
import { api, type Account } from '../api'
import { Card, Empty, Spinner, StatusBadge, formatMB } from '../components'
import { errorMessage, useToast } from '../toast'

// Punto de entrada del portal de cliente: un usuario común no elige una
// cuenta de una tabla (eso es vista de administración) — entra directo a la
// suya. Si por algún motivo tuviera más de una, se le ofrece un selector
// simple en vez de la tabla estilo WHM.
export default function MyAccount() {
  const toast = useToast()
  const [accounts, setAccounts] = useState<Account[] | null>(null)

  useEffect(() => {
    api.get<{ accounts: Account[] }>('/accounts')
      .then((r) => setAccounts(r.accounts))
      .catch((err) => {
        toast.error(errorMessage(err, 'No se pudo cargar tu cuenta'))
        setAccounts([])
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (accounts === null) return <Spinner />

  if (accounts.length === 1) {
    return <Navigate to={`/cuentas/${accounts[0].id}`} replace />
  }

  if (accounts.length === 0) {
    return (
      <>
        <div className="page-head">
          <div>
            <h1>Mi cuenta</h1>
            <p>Todavía no tienes una cuenta de hosting activa.</p>
          </div>
        </div>
        <Card>
          <Empty text="Contacta al administrador para que te asigne una cuenta." />
        </Card>
      </>
    )
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Mis cuentas</h1>
          <p>Tienes más de una cuenta de hosting. Elige con cuál trabajar.</p>
        </div>
      </div>
      <div className="stat-grid">
        {accounts.map((a) => (
          <Link key={a.id} to={`/cuentas/${a.id}`} style={{ textDecoration: 'none' }}>
            <div className="stat">
              <div className="label">{a.primary_domain}</div>
              <div className="value" style={{ fontSize: 18 }}>{a.system_user}</div>
              <div className="hint">
                <StatusBadge status={a.status} /> · {formatMB(a.disk_used_mb)}
              </div>
            </div>
          </Link>
        ))}
      </div>
    </>
  )
}
