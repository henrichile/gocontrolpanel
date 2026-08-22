import type { ReactNode } from 'react'
import { useEffect, useRef, useState } from 'react'

// --- Piezas de interfaz reutilizables -------------------------------------

export function Card({ title, actions, children }: {
  title?: string
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="card">
      {(title || actions) && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          {title && <h2>{title}</h2>}
          {actions}
        </div>
      )}
      {children}
    </section>
  )
}

export function Stat({ label, value, hint }: { label: string; value: ReactNode; hint?: string }) {
  return (
    <div className="stat">
      <div className="label">{label}</div>
      <div className="value">{value}</div>
      {hint && <div className="hint">{hint}</div>}
    </div>
  )
}

type BadgeTone = 'ok' | 'warn' | 'err' | 'idle'

const siteTone: Record<string, BadgeTone> = {
  running: 'ok',
  provisioning: 'warn',
  stopped: 'idle',
  error: 'err',
  deleting: 'warn',
  active: 'ok',
  suspended: 'warn',
  terminated: 'err',
}

const label: Record<string, string> = {
  running: 'En ejecución',
  provisioning: 'Aprovisionando',
  stopped: 'Detenido',
  error: 'Error',
  deleting: 'Eliminando',
  active: 'Activa',
  suspended: 'Suspendida',
  terminated: 'Terminada',
}

// El estado nunca se comunica solo con color: siempre lleva punto + texto.
export function StatusBadge({ status }: { status: string }) {
  const tone = siteTone[status] ?? 'idle'
  return (
    <span className={`badge ${tone}`}>
      <span className="dot" />
      {label[status] ?? status}
    </span>
  )
}

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), ' +
  'select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

export function Modal({ title, onClose, children }: {
  title: string
  onClose: () => void
  children: ReactNode
}) {
  const boxRef = useRef<HTMLDivElement>(null)

  // Al abrir: foco en el primer campo (o botón) del modal. Al cerrar: el foco
  // vuelve a donde estaba, para no perder el lugar si se navega con teclado.
  useEffect(() => {
    const trigger = document.activeElement as HTMLElement | null
    const first = boxRef.current?.querySelector<HTMLElement>(FOCUSABLE)
    first?.focus()
    return () => trigger?.focus()
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (e.key !== 'Tab' || !boxRef.current) return
      const items = Array.from(boxRef.current.querySelectorAll<HTMLElement>(FOCUSABLE))
      if (items.length === 0) return
      const first = items[0]
      const last = items[items.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="modal-backdrop" onClick={onClose} role="presentation">
      <div
        ref={boxRef}
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onClick={(e) => e.stopPropagation()}
      >
        <h2>{title}</h2>
        {children}
      </div>
    </div>
  )
}

export function Alert({ kind = 'info', children }: {
  kind?: 'info' | 'error' | 'success'
  children: ReactNode
}) {
  return <div className={`alert ${kind}`}>{children}</div>
}

export function Spinner({ text = 'Cargando…' }: { text?: string }) {
  return <div className="spinner">{text}</div>
}

export function Empty({ text }: { text: string }) {
  return <div className="empty">{text}</div>
}

/**
 * Sparkline de una sola serie: sin leyenda (el título nombra la serie),
 * trazo de 2px en el azul de la serie 1 y sin cuadrícula.
 */
export function Sparkline({ values, height = 48, label: title }: {
  values: number[]
  height?: number
  label?: string
}) {
  if (values.length < 2) {
    return <div className="muted" style={{ fontSize: 12 }}>Sin datos suficientes todavía</div>
  }
  const width = 240
  const max = Math.max(...values, 1)
  const min = Math.min(...values, 0)
  const span = max - min || 1
  const points = values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * width
      const y = height - ((v - min) / span) * (height - 4) - 2
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      width="100%"
      height={height}
      role="img"
      aria-label={title ? `${title}: último valor ${values[values.length - 1].toFixed(1)}` : 'serie temporal'}
      preserveAspectRatio="none"
    >
      <polyline
        points={points}
        fill="none"
        stroke="var(--series-1)"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

// Diálogo de confirmación para acciones destructivas.
export function useConfirm() {
  const [pending, setPending] = useState<{ text: string; resolve: (v: boolean) => void } | null>(null)

  const confirm = (text: string) =>
    new Promise<boolean>((resolve) => setPending({ text, resolve }))

  const dialog = pending ? (
    <Modal title="Confirmar acción" onClose={() => { pending.resolve(false); setPending(null) }}>
      <p>{pending.text}</p>
      <div className="modal-actions">
        <button onClick={() => { pending.resolve(false); setPending(null) }}>Cancelar</button>
        <button className="danger" onClick={() => { pending.resolve(true); setPending(null) }}>
          Confirmar
        </button>
      </div>
    </Modal>
  ) : null

  return { confirm, dialog }
}

// Diálogo para pedir un texto corto (p. ej. el motivo de una suspensión),
// en vez de recurrir a window.prompt() que rompe el estilo del resto de la app.
export function useReasonPrompt() {
  const [pending, setPending] = useState<{
    title: string
    label: string
    resolve: (v: string | null) => void
  } | null>(null)
  const [value, setValue] = useState('')

  const ask = (title: string, label: string) =>
    new Promise<string | null>((resolve) => {
      setValue('')
      setPending({ title, label, resolve })
    })

  function close(result: string | null) {
    pending?.resolve(result)
    setPending(null)
  }

  const dialog = pending ? (
    <Modal title={pending.title} onClose={() => close(null)}>
      <form onSubmit={(e) => { e.preventDefault(); close(value) }}>
        <div className="field">
          <label htmlFor="reason-prompt">{pending.label}</label>
          <input
            id="reason-prompt"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder="Opcional"
          />
        </div>
        <div className="modal-actions">
          <button type="button" onClick={() => close(null)}>Cancelar</button>
          <button className="primary">Confirmar</button>
        </div>
      </form>
    </Modal>
  ) : null

  return { ask, dialog }
}

// Filas de tabla en estado de carga: evita el "salto" de layout y el
// parpadeo de un spinner de página completa en recargas cortas.
export function SkeletonRows({ cols, rows = 4 }: { cols: number; rows?: number }) {
  return (
    <>
      {Array.from({ length: rows }).map((_, r) => (
        <tr key={r} aria-hidden="true">
          {Array.from({ length: cols }).map((_, c) => (
            <td key={c}>
              <div className="skeleton" style={{ height: 14, width: c === 0 ? '70%' : '50%' }} />
            </td>
          ))}
        </tr>
      ))}
    </>
  )
}

export function formatMB(mb: number): string {
  if (mb >= 1024 * 1024) return `${(mb / 1024 / 1024).toFixed(1)} TB`
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${Math.round(mb)} MB`
}

export function formatDate(iso?: string): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString('es-CL', {
    day: '2-digit', month: '2-digit', year: 'numeric',
    hour: '2-digit', minute: '2-digit',
  })
}

export function formatUptime(secs: number): string {
  const d = Math.floor(secs / 86400)
  const h = Math.floor((secs % 86400) / 3600)
  const m = Math.floor((secs % 3600) / 60)
  if (d > 0) return `${d} d ${h} h`
  if (h > 0) return `${h} h ${m} min`
  return `${m} min`
}
