import { useCallback, useEffect, useState } from 'react'
import {
  api, type ApiError, type DKIMRecord, type MailDNSRecords, type MailDomain,
  type MailInfo, type Mailbox,
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
    const dkim: DKIMRecord = { selector: md.dkim_selector, name: `${md.dkim_selector}._domainkey.${md.domain}`, value: md.dkim_value }
    // El backend ya conoce el hostname del mailserver, pero recalcular MX/SPF
    // aquí duplicaría lógica — se pide de nuevo al endpoint de habilitar
    // (idempotente: no vuelve a tocar el contenedor si ya existe).
    try {
      const res = await api.post<{ domain: MailDomain; records: MailDNSRecords }>(
        `/accounts/${accountID}/mail/domains/${md.domain}/enable`,
      )
      setViewingDNS({ domain: md.domain, records: res.records })
    } catch {
      setViewingDNS({ domain: md.domain, records: { mx: '', spf: '', dkim, dmarc: '' } })
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
        <MailDNSModal domain={viewingDNS.domain} records={viewingDNS.records} onClose={() => setViewingDNS(null)} />
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
      setError(apiErr.message)
      setFields(apiErr.fields ?? {})
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

function DNSRow({ label, value }: { label: string; value: string }) {
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

function MailDNSModal({ domain, records, onClose }: {
  domain: string
  records: MailDNSRecords
  onClose: () => void
}) {
  return (
    <Modal title={`Registros DNS para ${domain}`} onClose={onClose}>
      <p className="muted" style={{ marginTop: 0 }}>
        Publica estos registros en el proveedor DNS de <strong>{domain}</strong> (el panel no lo
        controla). Pueden tardar horas en propagarse.
      </p>
      {records.mx && <DNSRow label="MX" value={records.mx} />}
      {records.spf && <DNSRow label="SPF (registro TXT en la raíz)" value={records.spf} />}
      {records.dkim?.value && (
        <DNSRow label={`DKIM (registro TXT en ${records.dkim.name})`} value={records.dkim.value} />
      )}
      {records.dmarc && <DNSRow label="DMARC (registro TXT en _dmarc)" value={records.dmarc} />}
      <div className="modal-actions">
        <button className="primary" onClick={onClose}>Listo</button>
      </div>
    </Modal>
  )
}
