import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type FileEntry } from './api'
import { Card, Empty, Spinner, useConfirm, useReasonPrompt } from './components'
import { Icon } from './icons'
import { errorMessage, useToast } from './toast'

function formatBytes(b: number): string {
  if (b >= 1024 ** 3) return `${(b / 1024 ** 3).toFixed(1)} GB`
  if (b >= 1024 ** 2) return `${(b / 1024 ** 2).toFixed(1)} MB`
  if (b >= 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${b} B`
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString('es-CL', {
    day: '2-digit', month: '2-digit', year: 'numeric',
    hour: '2-digit', minute: '2-digit',
  })
}

function joinPath(dir: string, name: string): string {
  return dir ? `${dir}/${name}` : name
}

function iconFor(entry: FileEntry): 'folder' | 'archive' | 'image' | 'file' {
  if (entry.is_dir) return 'folder'
  const ext = entry.name.toLowerCase().split('.').pop() ?? ''
  if (['zip', 'tar', 'gz', 'tgz', 'rar', '7z'].includes(ext)) return 'archive'
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'avif'].includes(ext)) return 'image'
  return 'file'
}

// Explorador de archivos de la cuenta: navega la misma carpeta que ve el
// acceso SFTP (la raíz completa, no un sitio en particular).
export function FileManager({ accountID }: { accountID: string }) {
  const toast = useToast()
  const [path, setPath] = useState('')
  const [entries, setEntries] = useState<FileEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [uploading, setUploading] = useState(false)
  const [dragOver, setDragOver] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const { confirm, dialog: confirmDialog } = useConfirm()
  const { ask, dialog: promptDialog } = useReasonPrompt()

  const load = useCallback(async (p: string) => {
    setLoading(true)
    try {
      const res = await api.get<{ path: string; entries: FileEntry[] }>(
        `/accounts/${accountID}/files?path=${encodeURIComponent(p)}`,
      )
      setPath(res.path)
      setEntries(res.entries)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo leer la carpeta'))
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accountID])

  useEffect(() => { void load('') }, [load])

  async function uploadFiles(files: FileList | File[]) {
    const list = Array.from(files)
    if (list.length === 0) return
    setUploading(true)
    try {
      const form = new FormData()
      for (const f of list) form.append('files', f)
      await api.upload(`/accounts/${accountID}/files/upload?path=${encodeURIComponent(path)}`, form)
      toast.success(list.length === 1 ? `${list[0].name} subido` : `${list.length} archivos subidos`)
      await load(path)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo subir el archivo'))
    } finally {
      setUploading(false)
    }
  }

  async function download(entry: FileEntry) {
    try {
      await api.download(
        `/accounts/${accountID}/files/download?path=${encodeURIComponent(entry.path)}`,
        entry.name,
      )
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo descargar el archivo'))
    }
  }

  async function remove(entry: FileEntry) {
    const ok = await confirm(
      entry.is_dir
        ? `Se eliminará la carpeta "${entry.name}" y todo su contenido.`
        : `Se eliminará "${entry.name}".`,
    )
    if (!ok) return
    try {
      await api.del(`/accounts/${accountID}/files?path=${encodeURIComponent(entry.path)}`)
      toast.success(`${entry.name} eliminado`)
      await load(path)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo eliminar'))
    }
  }

  async function rename(entry: FileEntry) {
    const name = await ask(`Renombrar "${entry.name}"`, 'Nuevo nombre')
    if (!name || name === entry.name) return
    const to = joinPath(path, name)
    try {
      await api.post('/accounts/' + accountID + '/files/rename', { from: entry.path, to })
      toast.success(`Renombrado a ${name}`)
      await load(path)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo renombrar'))
    }
  }

  async function mkdir() {
    const name = await ask('Nueva carpeta', 'Nombre de la carpeta')
    if (!name) return
    try {
      await api.post(`/accounts/${accountID}/files/mkdir`, { path: joinPath(path, name) })
      toast.success(`Carpeta ${name} creada`)
      await load(path)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo crear la carpeta'))
    }
  }

  async function extract(entry: FileEntry) {
    try {
      await api.post(`/accounts/${accountID}/files/extract?path=${encodeURIComponent(entry.path)}`)
      toast.success(`${entry.name} extraído`)
      await load(path)
    } catch (err) {
      toast.error(errorMessage(err, 'No se pudo extraer el .zip'))
    }
  }

  const crumbs = path ? path.split('/') : []

  return (
    <Card>
      {confirmDialog}
      {promptDialog}

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 14, flexWrap: 'wrap', gap: 10 }}>
        <div>
          <nav aria-label="Ruta actual" style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 13, flexWrap: 'wrap' }}>
            <button className="sm ghost" onClick={() => void load('')} disabled={!path}>
              <Icon name="home" size={14} />raíz
            </button>
            {crumbs.map((c, i) => (
              <span key={i} style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                <Icon name="chevron-right" size={13} style={{ color: 'var(--ink-muted)' }} />
                <button className="sm ghost" onClick={() => void load(crumbs.slice(0, i + 1).join('/'))}>
                  {c}
                </button>
              </span>
            ))}
          </nav>
          {!loading && (
            <p className="muted" style={{ fontSize: 12, margin: '6px 0 0' }}>
              {entries.length === 0
                ? 'Carpeta vacía — arrastra archivos aquí o usa "Subir archivos".'
                : `${entries.length} elemento${entries.length === 1 ? '' : 's'} · arrastra archivos aquí para subirlos`}
            </p>
          )}
        </div>
        <div className="actions">
          <button className="sm" onClick={mkdir}>
            <Icon name="plus" size={14} />Carpeta
          </button>
          <button className="sm primary" disabled={uploading} onClick={() => fileInputRef.current?.click()}>
            <Icon name="upload" size={14} />{uploading ? 'Subiendo…' : 'Subir archivos'}
          </button>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            style={{ display: 'none' }}
            onChange={(e) => {
              if (e.target.files) void uploadFiles(e.target.files)
              e.target.value = ''
            }}
          />
        </div>
      </div>

      {uploading && (
        <div className="fm-progress" role="progressbar" aria-label="Subiendo archivos">
          <span />
        </div>
      )}

      <div
        className={`fm-dropzone${dragOver ? ' over' : ''}`}
        onDragOver={(e) => { e.preventDefault(); setDragOver(true) }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragOver(false)
          if (e.dataTransfer.files.length) void uploadFiles(e.dataTransfer.files)
        }}
      >
        {loading ? (
          <Spinner />
        ) : entries.length === 0 ? (
          <Empty text="Esta carpeta está vacía. Arrastra archivos aquí para subirlos." />
        ) : (
          <table>
            <thead>
              <tr><th>Nombre</th><th>Tamaño</th><th>Modificado</th><th></th></tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr
                  key={e.path}
                  className={`fm-row${e.is_dir ? ' dir' : ''}`}
                  onClick={e.is_dir ? () => void load(e.path) : undefined}
                >
                  <td className="strong">
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
                      <Icon name={iconFor(e)} size={15}
                            style={{ color: e.is_dir ? 'var(--series-1)' : 'var(--ink-muted)' }} />
                      {e.name}
                    </span>
                  </td>
                  <td className="muted">{e.is_dir ? '—' : formatBytes(e.size_b)}</td>
                  <td className="muted">{formatDate(e.mod_time)}</td>
                  <td>
                    <div className="actions fm-row-actions" onClick={(ev) => ev.stopPropagation()}>
                      {!e.is_dir && e.name.toLowerCase().endsWith('.zip') && (
                        <button className="sm ghost" onClick={() => void extract(e)} title="Extraer .zip">
                          <Icon name="archive" size={14} />
                        </button>
                      )}
                      {!e.is_dir && (
                        <button className="sm ghost" onClick={() => void download(e)} title="Descargar">
                          <Icon name="download" size={14} />
                        </button>
                      )}
                      <button className="sm ghost" onClick={() => void rename(e)} title="Renombrar">
                        <Icon name="edit" size={14} />
                      </button>
                      <button className="sm ghost danger" onClick={() => void remove(e)} title="Eliminar">
                        <Icon name="trash" size={14} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Card>
  )
}
