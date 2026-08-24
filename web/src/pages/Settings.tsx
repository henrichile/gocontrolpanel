import { useCallback, useEffect, useState } from 'react'
import { api, type EmailTemplate, type MailServerStatus, type SMTPSettings } from '../api'
import { Alert, Card, Empty, Spinner } from '../components'
import { errorMessage, useToast } from '../toast'

type Tab = 'smtp' | 'plantillas' | 'servidor-correo'

export default function Settings() {
  const [tab, setTab] = useState<Tab>('smtp')

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Configuraciones</h1>
          <p>Servidor SMTP y plantillas de correo que usa el panel para comunicarse con los clientes.</p>
        </div>
      </div>

      <div className="tabs">
        <button className={tab === 'smtp' ? 'active' : ''} onClick={() => setTab('smtp')}>
          SMTP
        </button>
        <button className={tab === 'plantillas' ? 'active' : ''} onClick={() => setTab('plantillas')}>
          Plantillas de email
        </button>
        <button className={tab === 'servidor-correo' ? 'active' : ''} onClick={() => setTab('servidor-correo')}>
          Servidor de correo
        </button>
      </div>

      {tab === 'smtp' && <SMTPCard />}
      {tab === 'plantillas' && <TemplateCard />}
      {tab === 'servidor-correo' && <MailServerCard />}
    </>
  )
}

function MailServerCard() {
  const toast = useToast()
  const [status, setStatus] = useState<MailServerStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [checking, setChecking] = useState(false)

  const reload = useCallback(async () => {
    const s = await api.get<MailServerStatus>('/system/mailserver/status')
    setStatus(s)
  }, [])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo consultar el servidor de correo')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function check() {
    setChecking(true)
    try {
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo verificar el PTR/rDNS'))
    } finally {
      setChecking(false)
    }
  }

  if (loading) return <Card title="Servidor de correo"><Spinner /></Card>

  if (!status?.enabled) {
    return (
      <Card title="Servidor de correo">
        <Empty text="El correo no está habilitado en este servidor (perfil 'mail' de docker compose)." />
      </Card>
    )
  }

  return (
    <Card
      title="Servidor de correo"
      actions={
        <button className="sm" disabled={checking} onClick={() => void check()}>
          {checking ? 'Verificando…' : 'Verificar PTR/rDNS'}
        </button>
      }
    >
      <p className="muted" style={{ marginTop: 0 }}>
        Este es el hostname compartido (MX) que usan todos los dominios de clientes con correo
        habilitado. El PTR/rDNS de la IP del servidor debe apuntar exactamente aquí — sin eso, la
        mayoría de proveedores marcan el correo saliente como spam sin importar lo que publique
        cada cliente en su propio DNS.
      </p>

      <div className="row">
        <div className="field">
          <label>Hostname (MX compartido)</label>
          <input readOnly value={status.hostname ?? ''} style={{ fontFamily: 'monospace' }} />
        </div>
        <div className="field">
          <label>IP pública detectada</label>
          <input readOnly value={status.public_ip ?? '—'} style={{ fontFamily: 'monospace' }} />
        </div>
      </div>

      {status.ptr && (
        <div className="field">
          <label>PTR / rDNS</label>
          <span className={`badge ${status.ptr.ok ? 'ok' : 'err'}`}>
            <span className="dot" />
            {status.ptr.ok ? `Correcto (${status.ptr.found})` : (status.ptr.error ?? 'No coincide')}
          </span>
          {!status.ptr.ok && status.ptr.found && (
            <div className="muted" style={{ fontSize: 12, marginTop: 6 }}>
              PTR actual: {status.ptr.found}
            </div>
          )}
        </div>
      )}
    </Card>
  )
}

function SMTPCard() {
  const toast = useToast()
  const [settings, setSettings] = useState<SMTPSettings | null>(null)
  const [form, setForm] = useState({
    host: '', port: 587, username: '', password: '',
    from_email: '', from_name: '', encryption: 'starttls' as SMTPSettings['encryption'], enabled: false,
  })
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [testTo, setTestTo] = useState('')
  const [testing, setTesting] = useState(false)

  const reload = useCallback(async () => {
    const s = await api.get<SMTPSettings>('/system/mail/smtp')
    setSettings(s)
    setForm({
      host: s.host, port: s.port, username: s.username, password: '',
      from_email: s.from_email, from_name: s.from_name, encryption: s.encryption, enabled: s.enabled,
    })
  }, [])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar la configuración SMTP')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      const s = await api.put<SMTPSettings>('/system/mail/smtp', form)
      setSettings(s)
      setForm((f) => ({ ...f, password: '' }))
      toast.success('Configuración SMTP guardada')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo guardar')
    } finally {
      setSaving(false)
    }
  }

  async function sendTest() {
    if (!testTo) return
    setTesting(true)
    try {
      await api.post('/system/mail/smtp/test', { to: testTo })
      toast.success(`Correo de prueba enviado a ${testTo}`)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo enviar el correo de prueba'))
    } finally {
      setTesting(false)
    }
  }

  if (loading) return <Card title="SMTP"><Spinner /></Card>

  return (
    <Card title="Servidor SMTP">
      {error && <Alert kind="error">{error}</Alert>}
      <p className="muted" style={{ marginTop: 0 }}>
        Se usa para enviar los accesos generados al crear un cliente nuevo. Mientras esté deshabilitado,
        las cuentas se crean igual pero no se envía ningún correo.
      </p>
      <form onSubmit={save}>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontWeight: 400, marginBottom: 14 }}>
          <input type="checkbox" checked={form.enabled}
                 onChange={(e) => setForm({ ...form, enabled: e.target.checked })} />
          Envío de correo habilitado
        </label>

        <div className="row">
          <div className="field">
            <label htmlFor="smtp-host">Host</label>
            <input id="smtp-host" value={form.host}
                   onChange={(e) => setForm({ ...form, host: e.target.value })}
                   placeholder="smtp.miproveedor.com" required />
          </div>
          <div className="field">
            <label htmlFor="smtp-port">Puerto</label>
            <input id="smtp-port" type="number" min={1} max={65535} value={form.port}
                   onChange={(e) => setForm({ ...form, port: Number(e.target.value) })} required />
          </div>
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="smtp-username">Usuario</label>
            <input id="smtp-username" value={form.username}
                   onChange={(e) => setForm({ ...form, username: e.target.value })} />
          </div>
          <div className="field">
            <label htmlFor="smtp-password">Contraseña</label>
            <input id="smtp-password" type="password" value={form.password}
                   onChange={(e) => setForm({ ...form, password: e.target.value })}
                   placeholder={settings?.password_set ? '●●●●●●●● (sin cambios)' : ''} />
          </div>
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="smtp-from-email">Correo remitente</label>
            <input id="smtp-from-email" type="email" value={form.from_email}
                   onChange={(e) => setForm({ ...form, from_email: e.target.value })}
                   placeholder="hosting@miempresa.cl" required />
          </div>
          <div className="field">
            <label htmlFor="smtp-from-name">Nombre remitente</label>
            <input id="smtp-from-name" value={form.from_name}
                   onChange={(e) => setForm({ ...form, from_name: e.target.value })}
                   placeholder="GoControlPanel" />
          </div>
        </div>

        <div className="field">
          <label htmlFor="smtp-encryption">Cifrado</label>
          <select id="smtp-encryption" value={form.encryption}
                  onChange={(e) => setForm({ ...form, encryption: e.target.value as SMTPSettings['encryption'] })}>
            <option value="starttls">STARTTLS (587)</option>
            <option value="ssl">SSL/TLS implícito (465)</option>
            <option value="none">Sin cifrado</option>
          </select>
        </div>

        <div className="actions" style={{ marginTop: 4 }}>
          <button className="primary" disabled={saving}>
            {saving ? 'Guardando…' : 'Guardar'}
          </button>
        </div>
      </form>

      <div className="row" style={{ marginTop: 18, alignItems: 'flex-end' }}>
        <div className="field">
          <label htmlFor="smtp-test-to">Enviar correo de prueba a</label>
          <input id="smtp-test-to" type="email" value={testTo}
                 onChange={(e) => setTestTo(e.target.value)} placeholder="tu@correo.cl" />
        </div>
        <button disabled={testing || !testTo} onClick={() => void sendTest()}>
          {testing ? 'Enviando…' : 'Enviar prueba'}
        </button>
      </div>
    </Card>
  )
}

const TEMPLATE_KEY = 'bienvenida_cliente'

const AVAILABLE_VARS = [
  { name: '{{.FullName}}', desc: 'Nombre completo del cliente' },
  { name: '{{.Username}}', desc: 'Usuario generado para ingresar al panel' },
  { name: '{{.Password}}', desc: 'Contraseña generada' },
  { name: '{{.PanelURL}}', desc: 'URL pública del panel' },
  { name: '{{.Domain}}', desc: 'Dominio principal de la cuenta creada' },
]

function TemplateCard() {
  const toast = useToast()
  const [template, setTemplate] = useState<EmailTemplate | null>(null)
  const [subject, setSubject] = useState('')
  const [bodyHTML, setBodyHTML] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(async () => {
    const t = await api.get<EmailTemplate>(`/system/mail/templates/${TEMPLATE_KEY}`)
    setTemplate(t)
    setSubject(t.subject)
    setBodyHTML(t.body_html)
  }, [])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar la plantilla')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      const t = await api.put<EmailTemplate>(`/system/mail/templates/${TEMPLATE_KEY}`, {
        subject, body_html: bodyHTML,
      })
      setTemplate(t)
      toast.success('Plantilla guardada')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo guardar')
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <Card title="Plantillas de email"><Spinner /></Card>

  const dirty = !!template && (subject !== template.subject || bodyHTML !== template.body_html)

  return (
    <Card title="Correo de bienvenida al cliente">
      {error && <Alert kind="error">{error}</Alert>}
      <p className="muted" style={{ marginTop: 0 }}>
        Se envía automáticamente al crear una cuenta para un cliente nuevo, con sus accesos al panel.
      </p>
      <div style={{ display: 'flex', gap: 20, alignItems: 'flex-start' }}>
        <form onSubmit={save} style={{ flex: 1, minWidth: 0 }}>
          <div className="field">
            <label htmlFor="tpl-subject">Asunto</label>
            <input id="tpl-subject" value={subject} onChange={(e) => setSubject(e.target.value)} required />
          </div>
          <div className="field">
            <label htmlFor="tpl-body">Cuerpo (HTML)</label>
            <textarea
              id="tpl-body"
              value={bodyHTML}
              onChange={(e) => setBodyHTML(e.target.value)}
              rows={16}
              style={{ fontFamily: 'monospace', fontSize: 13 }}
              required
            />
          </div>
          <div className="actions" style={{ marginTop: 4 }}>
            <button className="primary" disabled={saving || !dirty}>
              {saving ? 'Guardando…' : 'Guardar plantilla'}
            </button>
          </div>
        </form>

        <div style={{ width: 240, flexShrink: 0 }}>
          <h3 style={{ marginTop: 0, fontSize: 14 }}>Variables disponibles</h3>
          <ul style={{ paddingLeft: 18, margin: 0, fontSize: 13 }}>
            {AVAILABLE_VARS.map((v) => (
              <li key={v.name} style={{ marginBottom: 8 }}>
                <code className="inline">{v.name}</code>
                <div className="muted">{v.desc}</div>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </Card>
  )
}
