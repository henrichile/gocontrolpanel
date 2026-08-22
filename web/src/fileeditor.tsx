import { lazy, Suspense, useEffect, useState } from 'react'
import { api } from './api'
import { Icon } from './icons'
import { errorMessage, useToast } from './toast'

const Editor = lazy(() => import('./monacoEditor'))

const LANG_BY_EXT: Record<string, string> = {
  php: 'php', js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
  ts: 'typescript', tsx: 'typescript',
  json: 'json', html: 'html', htm: 'html', css: 'css', scss: 'scss', less: 'less',
  md: 'markdown', markdown: 'markdown', yml: 'yaml', yaml: 'yaml', xml: 'xml',
  sql: 'sql', sh: 'shell', bash: 'shell', env: 'ini', ini: 'ini', conf: 'ini',
  toml: 'ini', py: 'python', go: 'go', rb: 'ruby', txt: 'plaintext', log: 'plaintext',
  htaccess: 'apache',
}

// Extensiones que tiene sentido abrir en el editor de texto; el resto se
// descarga en vez de intentar mostrarlo (binarios, imágenes, etc.).
export function isEditableFile(name: string): boolean {
  const base = name.toLowerCase()
  if (base === '.htaccess' || base === '.env' || base === 'dockerfile') return true
  const ext = base.split('.').pop() ?? ''
  return ext in LANG_BY_EXT
}

function languageFor(name: string): string {
  const base = name.toLowerCase()
  if (base === '.htaccess') return 'apache'
  if (base === 'dockerfile') return 'dockerfile'
  const ext = base.split('.').pop() ?? ''
  return LANG_BY_EXT[ext] ?? 'plaintext'
}

export function FileEditor({
  accountID, path, name, onClose,
}: { accountID: string; path: string; name: string; onClose: () => void }) {
  const toast = useToast()
  const [original, setOriginal] = useState<string | null>(null)
  const [value, setValue] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const dirty = original !== null && value !== original

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setLoadError(null)
    api.getText(`/accounts/${accountID}/files/content?path=${encodeURIComponent(path)}`)
      .then((text) => {
        if (cancelled) return
        setOriginal(text)
        setValue(text)
      })
      .catch((err) => {
        if (cancelled) return
        setLoadError(errorMessage(err, 'No se pudo abrir el archivo'))
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [accountID, path])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      if ((e.ctrlKey || e.metaKey) && e.key === 's') { e.preventDefault(); void save() }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, original])

  async function save() {
    if (original !== null && value === original) return
    setSaving(true)
    try {
      await api.putText(`/accounts/${accountID}/files/content?path=${encodeURIComponent(path)}`, value)
      setOriginal(value)
      toast.success(`${name} guardado`)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo guardar el archivo'))
    } finally {
      setSaving(false)
    }
  }

  function requestClose() {
    if (dirty && !window.confirm(`"${name}" tiene cambios sin guardar. ¿Cerrar de todas formas?`)) return
    onClose()
  }

  return (
    <div className="fm-editor-backdrop" role="presentation" onClick={requestClose}>
      <div
        className="fm-editor"
        role="dialog"
        aria-modal="true"
        aria-label={`Editar ${name}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="fm-editor-bar">
          <span className="fm-editor-name">
            <Icon name="edit" size={14} />
            {name}
            {dirty && <span className="fm-editor-dot" title="Cambios sin guardar" />}
          </span>
          <div className="actions">
            <button className="sm primary" disabled={!dirty || saving || loading} onClick={() => void save()}>
              <Icon name="save" size={14} />{saving ? 'Guardando…' : 'Guardar'}
            </button>
            <button className="sm ghost" onClick={requestClose} title="Cerrar (Esc)">
              <Icon name="x" size={14} />
            </button>
          </div>
        </div>
        <div className="fm-editor-body">
          {loading ? (
            <div className="fm-editor-status">Cargando…</div>
          ) : loadError ? (
            <div className="fm-editor-status">{loadError}</div>
          ) : (
            <Suspense fallback={<div className="fm-editor-status">Cargando editor…</div>}>
              <Editor
                height="100%"
                language={languageFor(name)}
                value={value}
                onChange={(v) => setValue(v ?? '')}
                theme="vs-dark"
                options={{
                  fontSize: 13,
                  minimap: { enabled: false },
                  automaticLayout: true,
                  scrollBeyondLastLine: false,
                  tabSize: 2,
                }}
              />
            </Suspense>
          )}
        </div>
      </div>
    </div>
  )
}
