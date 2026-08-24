import { useCallback, useEffect, useState } from 'react'
import {
  api, type ApiError, type MailDNSRecords, type MailDomain,
  type MailDomainVerification, type MailInfo, type Mailbox,
} from './api'
import { Alert, Card, Empty, Modal, Spinner, useConfirm } from './components'
import { Icon } from './icons'
import { errorMessage, useToast } from './toast'

// Correo propio para dominios de clientes: buzones + habilitación de dominio
// (genera DKIM y muestra los registros DNS que el cliente debe publicar en
// su proveedor externo — el panel no controla ese DNS). Componente
// compartido entre la vista de administración (AccountDetail) y la del
// cliente (AccountPanel), igual que FileManager.
export function MailTab({ accountID, siteDomains }: { accountID: string; siteDomains: string[] }) {
  const toast = useToast()
  const [info, setInfo] = useState<MailInfo | null>(null)
  const [domains, setDomains] = useState<MailDomain[]>([])
  const [mailboxes, setMailboxes] = useState<Mailbox[]>([])
  const [loading, setLoading] = useState(true)
  const [creatingBox, setCreatingBox] = useState(false)
  const [viewingDNS, setViewingDNS] = useState<{ domain: string; records: MailDNSRecords } | null>(null)
  const { confirm, dialog } = useConfirm()

  const reload = useCallback(async () => {
    const [i, d, m] = await Promise.all([
      api.get<MailInfo>('/mail/info'),
      api.get<{ domains: MailDomain[] }>(`/accounts/${accountID}/mail/domains`),
      api.get<{ mailboxes: Mailbox[] }>(`/accounts/${accountID}/mail/mailboxes`),
    ])
    setInfo(i)
    setDomains(d.domains)
    setMailboxes(m.mailboxes)
  }, [accountID])

  useEffect(() => {
    reload().catch((err) => toast.error(errorMessage(err, 'No se pudo cargar el correo')))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])

  async function enableDomain(domain: string) {
    try {
      const res = await api.post<{ domain: MailDomain; records: MailDNSRecords }>(
        `/accounts/${accountID}/mail/domains/${domain}/enable`,
      )
      toast.success(`Correo habilitado para ${domain}`)
      setViewingDNS({ domain, records: res.records })
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo habilitar el correo para este dominio'))
    }
  }

  async function showDNS(md: MailDomain) {
    // El backend ya conoce el hostname del mailserver, pero recalcular MX/SPF
    // aquí duplicaría lógica — se pide de nuevo al endpoint de habilitar
    // (idempotente: no vuelve a tocar el contenedor si ya existe).
    try {
      const res = await api.post<{ domain: MailDomain; records: MailDNSRecords }>(
        `/accounts/${accountID}/mail/domains/${md.domain}/enable`,
      )
      setViewingDNS({ domain: md.domain, records: res.records })
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudieron obtener los registros DNS'))
    }
  }

  async function deleteMailbox(mb: Mailbox) {
    const ok = await confirm(`¿Eliminar el buzón ${mb.local_part}@${mb.domain}? Se pierde todo su correo.`)
    if (!ok) return
    try {
      await api.del(`/mail/mailboxes/${mb.id}`)
      toast.success('Buzón eliminado')
      await reload()
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar el buzón'))
    }
  }

  if (loading) return <Card title="Correo"><Spinner /></Card>

  if (!info?.enabled) {
    return (
      <Card title="Correo">
        <Empty text="El correo no está habilitado en este servidor." />
      </Card>
    )
  }

  const enabledDomainNames = new Set(domains.map((d) => d.domain))
  const pendingDomains = siteDomains.filter((d) => !enabledDomainNames.has(d))

  return (
    <>
      {dialog}
      <Card
        title="Correo"
        actions={info.webmail_host && (
          <a className="sm" href={`https://${info.webmail_host}`} target="_blank" rel="noreferrer">
            <Icon name="globe" size={14} />Abrir webmail
          </a>
        )}
      >
        {pendingDomains.length > 0 && (
          <>
            <p className="muted" style={{ marginTop: 0 }}>Dominios sin correo habilitado:</p>
            <div className="actions" style={{ marginBottom: 18, flexWrap: 'wrap' }}>
              {pendingDomains.map((d) => (
                <button key={d} className="sm" onClick={() => void enableDomain(d)}>
                  Habilitar correo para {d}
                </button>
              ))}
            </div>
          </>
        )}

        {domains.length === 0 ? (
          <Empty text="Ningún dominio de esta cuenta tiene correo habilitado todavía." />
        ) : (
          <table>
            <thead>
              <tr><th>Dominio</th><th>DKIM</th><th></th></tr>
            </thead>
            <tbody>
              {domains.map((d) => (
                <tr key={d.id}>
                  <td className="strong">{d.domain}</td>
                  <td>
                    <span className={`badge ${d.dkim_enabled_at ? 'ok' : 'idle'}`}>
                      <span className="dot" />{d.dkim_enabled_at ? 'Generado' : 'Pendiente'}
                    </span>
                  </td>
                  <td>
                    <button className="sm ghost" onClick={() => void showDNS(d)}>
                      Ver registros DNS
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      <Card
        title="Buzones"
        actions={
          domains.length > 0 && (
            <button className="sm primary" onClick={() => setCreatingBox(true)}>
              <Icon name="plus" size={14} />Nuevo buzón
            </button>
          )
        }
      >
        {mailboxes.length === 0 ? (
          <Empty text="No hay buzones creados. Habilita un dominio y crea el primero." />
        ) : (
          <table>
            <thead>
              <tr><th>Dirección</th><th>Cuota</th><th></th></tr>
            </thead>
            <tbody>
              {mailboxes.map((mb) => (
                <tr key={mb.id}>
                  <td className="strong">{mb.local_part}@{mb.domain}</td>
                  <td>{mb.quota_mb} MB</td>
                  <td>
                    <button className="sm ghost danger" onClick={() => void deleteMailbox(mb)}>
                      <Icon name="trash" size={14} />Eliminar
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      {creatingBox && (
        <CreateMailboxModal
          accountID={accountID}
          domains={domains}
          onClose={() => setCreatingBox(false)}
          onCreated={() => { setCreatingBox(false); toast.success('Buzón creado'); void reload() }}
        />
      )}

      {viewingDNS && (
        <MailDNSModal
          accountID={accountID}
          domain={viewingDNS.domain}
          records={viewingDNS.records}
          onClose={() => setViewingDNS(null)}
        />
      )}
    </>
  )
}

function CreateMailboxModal({ accountID, domains, onClose, onCreated }: {
  accountID: string
  domains: MailDomain[]
  onClose: () => void
  onCreated: () => void
}) {
  const [form, setForm] = useState({
    mail_domain_id: domains[0]?.id ?? '',
    local_part: '',
    password: '',
    quota_mb: 1024,
  })
  const [error, setError] = useState('')
  const [fields, setFields] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    setFields({})
    try {
      await api.post(`/accounts/${accountID}/mail/mailboxes`, form)
      onCreated()
    } catch (err) {
      const apiErr = err as ApiError
      const fieldErrors = apiErr.fields ?? {}
      setFields(fieldErrors)
      // apiErr.message es un texto genérico ("datos inválidos"); el motivo
      // real vive en `fields`, así que lo mostramos arriba sea cual sea el
      // campo (no todos tienen un lugar inline en este formulario, como
      // "mail_domain_id" o "plan").
      setError(Object.values(fieldErrors).join(' ') || apiErr.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title="Nuevo buzón de correo" onClose={onClose}>
      <form onSubmit={submit}>
        {error && <Alert kind="error">{error}</Alert>}

        <div className="row">
          <div className="field">
            <label htmlFor="mb-local">Usuario</label>
            <input id="mb-local" value={form.local_part} required
                   onChange={(e) => setForm({ ...form, local_part: e.target.value })}
                   placeholder="contacto" />
            {fields.local_part && <div className="field-error">{fields.local_part}</div>}
          </div>
          <div className="field">
            <label htmlFor="mb-domain">Dominio</label>
            <select id="mb-domain" value={form.mail_domain_id}
                    onChange={(e) => setForm({ ...form, mail_domain_id: e.target.value })}>
              {domains.map((d) => <option key={d.id} value={d.id}>{d.domain}</option>)}
            </select>
          </div>
        </div>

        <div className="row">
          <div className="field">
            <label htmlFor="mb-password">Contraseña</label>
            <input id="mb-password" type="password" value={form.password} required minLength={10}
                   onChange={(e) => setForm({ ...form, password: e.target.value })} />
            {fields.password && <div className="field-error">{fields.password}</div>}
          </div>
          <div className="field">
            <label htmlFor="mb-quota">Cuota (MB)</label>
            <input id="mb-quota" type="number" min={64} value={form.quota_mb}
                   onChange={(e) => setForm({ ...form, quota_mb: Number(e.target.value) })} />
          </div>
        </div>

        <div className="modal-actions">
          <button type="button" onClick={onClose}>Cancelar</button>
          <button className="primary" disabled={busy}>{busy ? 'Creando…' : 'Crear buzón'}</button>
        </div>
      </form>
    </Modal>
  )
}

function CopyField({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      /* clipboard no disponible */
    }
  }
  return (
    <div className="field">
      <label>{label}</label>
      <div style={{ display: 'flex', gap: 8 }}>
        <input readOnly value={value} style={{ fontFamily: 'monospace', fontSize: 12 }} />
        <button type="button" className="sm" onClick={() => void copy()}>
          {copied ? 'Copiado' : 'Copiar'}
        </button>
      </div>
    </div>
  )
}

// Si el proveedor de DNS pide el nombre "relativo" (sin el dominio al final,
// como hacen Cloudflare/GoDaddy/Namecheap en su campo "Nombre/Host") en vez
// del FQDN completo que devuelve el backend.
function relativeName(name: string, domain: string): string {
  if (name === domain) return '@'
  const suffix = '.' + domain
  return name.endsWith(suffix) ? name.slice(0, -suffix.length) : name
}

// Muestra un registro DNS completo tal como lo pide el formulario de
// cualquier proveedor: tipo, nombre y valor por separado (en vez de una
// sola línea que el cliente tendría que interpretar él mismo), más la
// prioridad cuando aplica (solo MX).
function DNSRecordCard({ label, domain, record }: {
  label: string
  domain: string
  record: { type: string; name: string; value: string; priority?: number }
}) {
  return (
    <div style={{ border: '1px solid var(--border)', borderRadius: 10, padding: 12, marginBottom: 12 }}>
      <div className="strong" style={{ marginBottom: 8 }}>{label}</div>
      <div className="row">
        <div className="field">
          <label>Tipo</label>
          <input readOnly value={record.type} style={{ fontFamily: 'monospace' }} />
        </div>
        <div className="field">
          <label>Nombre / Host</label>
          <input readOnly value={record.name} style={{ fontFamily: 'monospace' }} />
        </div>
        {record.priority !== undefined && (
          <div className="field" style={{ maxWidth: 100 }}>
            <label>Prioridad</label>
            <input readOnly value={record.priority} style={{ fontFamily: 'monospace' }} />
          </div>
        )}
      </div>
      <CopyField label="Valor" value={record.value} />
      <div className="muted" style={{ fontSize: 12, marginTop: -4 }}>
        Si tu proveedor pide el nombre sin el dominio, usa{' '}
        <code className="inline">{relativeName(record.name, domain)}</code> en vez del nombre completo.
      </div>
    </div>
  )
}

function CheckBadge({ label, check }: { label: string; check?: { ok: boolean; error?: string } }) {
  if (!check) return null
  return (
    <div className="field">
      <label>{label}</label>
      <span className={`badge ${check.ok ? 'ok' : 'err'}`}>
        <span className="dot" />{check.ok ? 'Propagado' : (check.error ?? 'Todavía no')}
      </span>
    </div>
  )
}

function MailDNSModal({ accountID, domain, records, onClose }: {
  accountID: string
  domain: string
  records: MailDNSRecords
  onClose: () => void
}) {
  const toast = useToast()
  const [verifying, setVerifying] = useState(false)
  const [verification, setVerification] = useState<MailDomainVerification | null>(null)

  async function verify() {
    setVerifying(true)
    try {
      const res = await api.post<MailDomainVerification>(
        `/accounts/${accountID}/mail/domains/${domain}/verify`,
      )
      setVerification(res)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo verificar el DNS'))
    } finally {
      setVerifying(false)
    }
  }

  return (
    <Modal title={`Registros DNS para ${domain}`} onClose={onClose}>
      <p className="muted" style={{ marginTop: 0 }}>
        Publica estos registros en el proveedor DNS de <strong>{domain}</strong> (el panel no lo
        controla). Pueden tardar horas en propagarse.
      </p>
      <DNSRecordCard label="MX" domain={domain} record={records.mx} />
      <DNSRecordCard label="SPF" domain={domain} record={records.spf} />
      <DNSRecordCard label="DKIM" domain={domain} record={records.dkim} />
      <DNSRecordCard label="DMARC" domain={domain} record={records.dmarc} />

      <div className="actions" style={{ marginTop: 8, marginBottom: verification ? 8 : 0 }}>
        <button className="sm" disabled={verifying} onClick={() => void verify()}>
          {verifying ? 'Verificando…' : 'Verificar propagación'}
        </button>
      </div>

      {verification && (
        <div className="row" style={{ flexWrap: 'wrap' }}>
          <CheckBadge label="MX" check={verification.mx} />
          <CheckBadge label="SPF" check={verification.spf} />
          <CheckBadge label="DKIM" check={verification.dkim} />
          <CheckBadge label="DMARC" check={verification.dmarc} />
        </div>
      )}

      <div className="modal-actions">
        <button className="primary" onClick={onClose}>Listo</button>
      </div>
    </Modal>
  )
}
