import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from './auth'
import { Spinner } from './components'
import { Icon } from './icons'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Accounts from './pages/Accounts'
import AccountDetail from './pages/AccountDetail'
import AccountPanel from './pages/AccountPanel'
import MyAccount from './pages/MyAccount'
import Profile from './pages/Profile'
import SiteDetail from './pages/SiteDetail'
import Users from './pages/Users'
import Plans from './pages/Plans'
import System from './pages/System'
import Settings from './pages/Settings'

// El panel tiene dos ambientes separados: "Administración del servidor"
// (admin/reseller: cuentas, usuarios, planes, sistema) y "Panel de
// hosting" (dueño de una cuenta: sus sitios, archivos, BD, SFTP, backups).
// Un mismo usuario nunca ve las dos navegaciones a la vez — la de admin
// existe solo para quien administra el servidor.
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
          <Route path="/perfil" element={<Profile />} />
          <Route path="/panel/:accountID" element={<AccountPanel />} />
          {/* Ambiente de administración del servidor: no existe para un
              cliente, que siempre entra por /mi-cuenta → /panel/:id. */}
          <Route path="/cuentas" element={isClient ? <Navigate to="/mi-cuenta" replace /> : <Accounts />} />
          <Route
            path="/cuentas/:accountID"
            element={isClient ? <Navigate to="/mi-cuenta" replace /> : <AccountDetail />}
          />
          {/* Un sitio siempre se administra dentro de su cuenta; esta ruta
              solo existe para llegar al detalle desde ahí (o desde el
              resumen), no hay un listado de sitios independiente. Es
              agnóstica de ambiente: la usan tanto admin como cliente. */}
          <Route path="/sitios/:siteID" element={<SiteDetail />} />
          <Route path="/usuarios" element={<Users />} />
          <Route path="/planes" element={<Plans />} />
          <Route path="/sistema" element={<System />} />
          <Route path="/configuraciones" element={<Settings />} />
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
        <NavLink to="/" end><Icon name="home" />Resumen</NavLink>

        {/* Ambiente "Panel de hosting": lo que ve un cliente. Cuenta y sitio
            son un solo ítem de menú (1 cuenta = 1 sitio principal); los
            sitios adicionales que permita el plan, y sus dominios, se crean
            y administran dentro de la propia cuenta, no como listado
            aparte. */}
        {isClient && <NavLink to="/mi-cuenta"><Icon name="server" />Mi hosting</NavLink>}

        {/* Ambiente "Administración del servidor": no existe para un
            cliente — es exclusivo de quien administra el servidor. */}
        {isReseller && (
          <>
            <div className="nav-section">Administración del servidor</div>
            <NavLink to="/cuentas"><Icon name="users" />Cuentas</NavLink>
            <NavLink to="/usuarios"><Icon name="user" />Usuarios</NavLink>
            {isAdmin && <NavLink to="/planes"><Icon name="tag" />Planes</NavLink>}
            {isAdmin && <NavLink to="/sistema"><Icon name="shield" />Sistema</NavLink>}
            {isAdmin && <NavLink to="/configuraciones"><Icon name="sliders" />Configuraciones</NavLink>}
          </>
        )}
      </nav>

      <div className="sidebar-footer">
        <NavLink to="/perfil" style={{ color: 'var(--ink-2)', display: 'block' }}>{user?.username}</NavLink>
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
