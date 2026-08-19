import type { ReactNode } from 'react'
import { useEffect, useState } from 'react'

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

export function Modal({ title, onClose, children }: {
  title: string
  onClose: () => void
  children: ReactNode
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="modal-backdrop" onClick={onClose} role="presentation">
      <div
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
