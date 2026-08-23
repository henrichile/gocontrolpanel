import { useState } from 'react'
import { useAuth } from '../auth'
import { Alert } from '../components'

export default function Login() {
  const { login, verifyTOTP } = useAuth()
  const [loginValue, setLoginValue] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [ticket, setTicket] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const { totpTicket } = await login(loginValue, password)
      if (totpTicket) setTicket(totpTicket)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo iniciar sesión')
    } finally {
      setBusy(false)
    }
  }

  async function onVerify(e: React.FormEvent) {
    e.preventDefault()
    if (!ticket) return
    setBusy(true)
    setError('')
    try {
      await verifyTOTP(ticket, code)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Código incorrecto')
    } finally {
      setBusy(false)
    }
  }

  if (ticket) {
    return (
      <div className="login-wrap">
        <form className="login-card" onSubmit={onVerify}>
          <span className="brand-mark">Go</span>
          <h1>Verificación en dos pasos</h1>
          <p className="sub">Escribe el código de tu app de autenticación</p>

          {error && <Alert kind="error">{error}</Alert>}

          <div className="field">
            <label htmlFor="totp-code">Código de 6 dígitos</label>
            <input
              id="totp-code"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={6}
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
              required
              autoFocus
            />
          </div>

          <button className="primary" style={{ width: '100%' }} disabled={busy || code.length !== 6}>
            {busy ? 'Verificando…' : 'Verificar'}
          </button>
          <button type="button" className="ghost" style={{ width: '100%', marginTop: 8 }}
                  onClick={() => { setTicket(null); setCode(''); setError('') }}>
            Volver
          </button>
        </form>
      </div>
    )
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
