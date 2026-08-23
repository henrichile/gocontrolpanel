import { useState } from 'react'
import QRCode from 'qrcode'
import { useAuth } from '../auth'
import { api } from '../api'
import { Alert, Card } from '../components'
import { errorMessage, useToast } from '../toast'

export default function Profile() {
  const { user } = useAuth()
  if (!user) return null

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Mi perfil</h1>
          <p>{user.username} · {user.email}</p>
        </div>
      </div>
      <PasswordCard />
      <TOTPCard enabled={user.totp_enabled} />
    </>
  )
}

function PasswordCard() {
  const toast = useToast()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      await api.post('/auth/password', { current_password: current, new_password: next })
      setCurrent('')
      setNext('')
      toast.success('Contraseña actualizada; se cerraron el resto de sesiones')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo cambiar la contraseña')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card title="Contraseña">
      {error && <Alert kind="error">{error}</Alert>}
      <form onSubmit={submit} className="row">
        <div className="field">
          <label htmlFor="current-password">Contraseña actual</label>
          <input id="current-password" type="password" autoComplete="current-password"
                 value={current} required onChange={(e) => setCurrent(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="new-password">Contraseña nueva</label>
          <input id="new-password" type="password" autoComplete="new-password"
                 value={next} required minLength={10} onChange={(e) => setNext(e.target.value)} />
        </div>
        <div className="actions" style={{ alignItems: 'flex-end' }}>
          <button className="primary" disabled={saving}>{saving ? 'Guardando…' : 'Cambiar contraseña'}</button>
        </div>
      </form>
    </Card>
  )
}

function TOTPCard({ enabled }: { enabled: boolean }) {
  const toast = useToast()
  const [active, setActive] = useState(enabled)
  const [setup, setSetup] = useState<{ secret: string; qr: string } | null>(null)
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function startSetup() {
    setError('')
    setBusy(true)
    try {
      const res = await api.post<{ secret: string; provisioning_uri: string }>('/auth/2fa/setup')
      const qr = await QRCode.toDataURL(res.provisioning_uri)
      setSetup({ secret: res.secret, qr })
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo iniciar la activación'))
    } finally {
      setBusy(false)
    }
  }

  async function confirm(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await api.post('/auth/2fa/confirm', { code })
      setActive(true)
      setSetup(null)
      setCode('')
      toast.success('Verificación en dos pasos activada')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Código incorrecto')
    } finally {
      setBusy(false)
    }
  }

  async function disable(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await api.post('/auth/2fa/disable', { password })
      setActive(false)
      setPassword('')
      toast.success('Verificación en dos pasos desactivada')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo desactivar')
    } finally {
      setBusy(false)
    }
  }

  if (active) {
    return (
      <Card title="Verificación en dos pasos">
        <Alert kind="success">Está activa en tu cuenta.</Alert>
        {error && <Alert kind="error">{error}</Alert>}
        <form onSubmit={disable} className="row">
          <div className="field">
            <label htmlFor="disable-password">Contraseña actual</label>
            <input id="disable-password" type="password" autoComplete="current-password"
                   value={password} required onChange={(e) => setPassword(e.target.value)} />
          </div>
          <div className="actions" style={{ alignItems: 'flex-end' }}>
            <button className="danger ghost" disabled={busy}>{busy ? 'Desactivando…' : 'Desactivar'}</button>
          </div>
        </form>
      </Card>
    )
  }

  return (
    <Card title="Verificación en dos pasos">
      {error && <Alert kind="error">{error}</Alert>}
      {!setup ? (
        <>
          <p className="muted" style={{ marginTop: 0 }}>
            Añade una capa extra de seguridad: además de tu contraseña, el login pedirá un
            código de 6 dígitos generado por una app como Google Authenticator o Authy.
          </p>
          <button className="primary" disabled={busy} onClick={() => void startSetup()}>
            {busy ? 'Generando…' : 'Activar verificación en dos pasos'}
          </button>
        </>
      ) : (
        <form onSubmit={confirm}>
          <p className="muted" style={{ marginTop: 0 }}>
            Escanea este código con tu app de autenticación y escribe el código que te muestre.
          </p>
          <img src={setup.qr} alt="Código QR para la app de autenticación" width={180} height={180} />
          <p className="muted">
            ¿No puedes escanear? Ingresa este código manualmente: <code className="inline">{setup.secret}</code>
          </p>
          <div className="field">
            <label htmlFor="confirm-code">Código de 6 dígitos</label>
            <input id="confirm-code" inputMode="numeric" maxLength={6} value={code} required
                   onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} />
          </div>
          <div className="actions">
            <button className="primary" disabled={busy || code.length !== 6}>
              {busy ? 'Confirmando…' : 'Confirmar y activar'}
            </button>
            <button type="button" className="ghost" onClick={() => { setSetup(null); setCode('') }}>
              Cancelar
            </button>
          </div>
        </form>
      )}
    </Card>
  )
}

