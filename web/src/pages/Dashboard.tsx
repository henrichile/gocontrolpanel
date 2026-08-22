import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Overview, type Site } from '../api'
import { Card, Empty, Spinner, Stat, StatusBadge, formatMB } from '../components'
import { useAuth } from '../auth'
import { errorMessage, useToast } from '../toast'

export default function Dashboard() {
  const { user } = useAuth()
  const toast = useToast()
  const [overview, setOverview] = useState<Overview | null>(null)
  const [sites, setSites] = useState<Site[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    async function load() {
      try {
        const [ov, list] = await Promise.all([
          api.get<{ overview: Overview }>('/overview'),
          api.get<{ sites: Site[] }>('/sites'),
        ])
        setOverview(ov.overview)
        setSites(list.sites)
      } catch (err) {
        toast.error(errorMessage(err, 'No se pudo cargar el resumen'))
      } finally {
        setLoading(false)
      }
    }
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (loading) return <Spinner />

  const problems = sites.filter((s) => s.status === 'error')

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Hola, {user?.full_name || user?.username}</h1>
          <p>Estado general de tu infraestructura de hosting.</p>
        </div>
      </div>

      <div className="stat-grid">
        <Stat label="Cuentas" value={overview?.accounts ?? 0}
              hint={`${overview?.suspended_accounts ?? 0} suspendidas`} />
        <Stat label="Sitios activos" value={overview?.active_sites ?? 0}
              hint={`${overview?.total_sites ?? 0} en total`} />
        <Stat label="Dominios" value={overview?.domains ?? 0} />
        <Stat label="Bases de datos" value={overview?.databases ?? 0} />
        <Stat label="Disco usado" value={formatMB(overview?.disk_used_mb ?? 0)} />
      </div>

      {problems.length > 0 && (
        <Card title="Sitios con incidencias">
          <table>
            <thead>
              <tr><th>Sitio</th><th>Estado</th><th>Detalle</th></tr>
            </thead>
            <tbody>
              {problems.map((s) => (
                <tr key={s.id}>
                  <td className="strong"><Link to={`/sitios/${s.id}`}>{s.name}</Link></td>
                  <td><StatusBadge status={s.status} /></td>
                  <td className="muted">{s.last_error || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      <Card title="Sitios recientes">
        {sites.length === 0 ? (
          <Empty text="Todavía no hay sitios. Crea una cuenta de hosting para empezar." />
        ) : (
          <table>
            <thead>
              <tr>
                <th>Nombre</th><th>Dominios</th><th>PHP</th><th>Estado</th>
              </tr>
            </thead>
            <tbody>
              {sites.slice(0, 8).map((s) => (
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
    </>
  )
}
