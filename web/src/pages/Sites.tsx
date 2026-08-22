import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Site } from '../api'
import { Card, Empty, SkeletonRows, StatusBadge } from '../components'
import { errorMessage, useToast } from '../toast'

export default function Sites() {
  const toast = useToast()
  const [sites, setSites] = useState<Site[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState('')

  useEffect(() => {
    api.get<{ sites: Site[] }>('/sites')
      .then((r) => setSites(r.sites))
      .catch((err) => toast.error(errorMessage(err, 'No se pudieron cargar los sitios')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const visible = sites.filter((s) => {
    const haystack = [s.name, s.php_version, ...(s.domains?.map((d) => d.fqdn) ?? [])]
      .join(' ')
      .toLowerCase()
    return haystack.includes(filter.toLowerCase())
  })

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Sitios</h1>
          <p>Cada sitio corre en su propio contenedor FrankenPHP.</p>
        </div>
        <input
          style={{ maxWidth: 260 }}
          placeholder="Buscar por nombre o dominio…"
          aria-label="Buscar sitios"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>

      <Card>
        {!loading && visible.length === 0 ? (
          <Empty text={sites.length === 0 ? 'No hay sitios.' : 'Ningún sitio coincide con la búsqueda.'} />
        ) : (
          <table>
            <thead>
              <tr>
                <th>Sitio</th><th>Dominios</th><th>PHP</th><th>Modo</th>
                <th>Contenedor</th><th>Estado</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <SkeletonRows cols={6} />
              ) : (
                visible.map((s) => (
                  <tr key={s.id}>
                    <td className="strong"><Link to={`/sitios/${s.id}`}>{s.name}</Link></td>
                    <td>{s.domains?.map((d) => d.fqdn).join(', ') || '—'}</td>
                    <td>PHP {s.php_version}</td>
                    <td>{s.worker_script ? 'Worker' : 'Clásico'}</td>
                    <td className="muted"><code className="inline">{s.container_name}</code></td>
                    <td><StatusBadge status={s.status} /></td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </Card>
    </>
  )
}
