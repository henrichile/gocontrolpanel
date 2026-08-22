import type { ReactNode } from 'react'
import { createContext, useCallback, useContext, useRef, useState } from 'react'

type ToastKind = 'success' | 'error' | 'info'
type Toast = { id: number; kind: ToastKind; text: string }

type ToastAPI = {
  success: (text: string) => void
  error: (text: string) => void
  info: (text: string) => void
}

const ToastContext = createContext<ToastAPI | null>(null)

// Duración más larga para errores: el usuario suele necesitar más tiempo
// para leerlos que un simple "listo".
const DURATION: Record<ToastKind, number> = { success: 3200, error: 6000, info: 4000 }

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const nextID = useRef(0)

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const push = useCallback((kind: ToastKind, text: string) => {
    const id = ++nextID.current
    setToasts((prev) => [...prev, { id, kind, text }])
    window.setTimeout(() => dismiss(id), DURATION[kind])
  }, [dismiss])

  const api: ToastAPI = {
    success: (text) => push('success', text),
    error: (text) => push('error', text),
    info: (text) => push('info', text),
  }

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="toast-host" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.kind}`}>
            <span>{t.text}</span>
            <button
              className="toast-close"
              aria-label="Cerrar notificación"
              onClick={() => dismiss(t.id)}
            >
              ×
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastAPI {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast debe usarse dentro de <ToastProvider>')
  return ctx
}

// Extrae un mensaje legible de cualquier error capturado en un catch.
export function errorMessage(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message) return err.message
  return fallback
}
