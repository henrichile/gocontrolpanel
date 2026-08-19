import { useState } from 'react'
import { useAuth } from '../auth'
import { Alert } from '../components'

export default function Login() {
  const { login } = useAuth()
  const [loginValue, setLoginValue] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await login(loginValue, password)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo iniciar sesión')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={onSubmit}>
        <span className="brand-mark">Go</span>
        <h1>GoControlPanel</h1>
        <p className="sub">Administración de hosting sobre FrankenPHP y Caddy</p>

        {error && <Alert kind="error">{error}</Alert>}

        <div className="field">
          <label htmlFor="login">Usuario o correo</label>
          <input
            id="login"
            autoComplete="username"
            value={loginValue}
            onChange={(e) => setLoginValue(e.target.value)}
            required
            autoFocus
          />
        </div>

        <div className="field">
          <label htmlFor="password">Contraseña</label>
          <input
            id="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </div>

        <button className="primary" style={{ width: '100%' }} disabled={busy}>
          {busy ? 'Entrando…' : 'Entrar'}
        </button>
      </form>
    </div>
  )
}
