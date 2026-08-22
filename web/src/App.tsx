import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from './auth'
import { Spinner } from './components'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Accounts from './pages/Accounts'
import AccountDetail from './pages/AccountDetail'
import MyAccount from './pages/MyAccount'
import Sites from './pages/Sites'
import SiteDetail from './pages/SiteDetail'
import Users from './pages/Users'
import Plans from './pages/Plans'
import System from './pages/System'

export default function App() {
  const { user, loading, isClient } = useAuth()

  if (loading) return <Spinner text="Cargando el panel…" />
  if (!user) return <Login />

  return (
    <div className="app">
      <Sidebar />
      <main className="main">
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/mi-cuenta" element={<MyAccount />} />
          {/* La tabla de cuentas es una vista de administración (estilo WHM);
              un cliente entra directo a la suya por /mi-cuenta. */}
          <Route path="/cuentas" element={isClient ? <Navigate to="/mi-cuenta" replace /> : <Accounts />} />
          <Route path="/cuentas/:accountID" element={<AccountDetail />} />
          <Route path="/sitios" element={<Sites />} />
          <Route path="/sitios/:siteID" element={<SiteDetail />} />
          <Route path="/usuarios" element={<Users />} />
          <Route path="/planes" element={<Plans />} />
          <Route path="/sistema" element={<System />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  )
}

function Sidebar() {
  const { user, logout, isAdmin, isReseller, isClient } = useAuth()

  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-mark">Go</span>
        <span>ControlPanel</span>
      </div>

      <nav className="nav">
        <NavLink to="/" end>Resumen</NavLink>
        {isClient ? (
          <>
            <NavLink to="/mi-cuenta">Mi cuenta</NavLink>
            <NavLink to="/sitios">Mis sitios</NavLink>
          </>
        ) : (
          <>
            <NavLink to="/cuentas">Cuentas</NavLink>
            <NavLink to="/sitios">Sitios</NavLink>
          </>
        )}
        {isReseller && <NavLink to="/usuarios">Usuarios</NavLink>}
        {isAdmin && <NavLink to="/planes">Planes</NavLink>}
        {isAdmin && <NavLink to="/sistema">Sistema</NavLink>}
      </nav>

      <div className="sidebar-footer">
        <div style={{ color: 'var(--ink-2)' }}>{user?.username}</div>
        <div style={{ fontSize: 12, marginBottom: 10 }}>{roleLabel(user?.role)}</div>
        <button className="ghost sm" onClick={() => void logout()}>Cerrar sesión</button>
      </div>
    </aside>
  )
}

function roleLabel(role?: string) {
  switch (role) {
    case 'admin': return 'Administrador'
    case 'reseller': return 'Revendedor'
    default: return 'Usuario'
  }
}
