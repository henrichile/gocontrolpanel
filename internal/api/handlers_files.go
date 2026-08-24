package api

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
)

// Explorador de archivos: navega la misma carpeta que ve el acceso SFTP de
// la cuenta (su raíz completa, no un sitio en particular), para que subir un
// sitio no dependa de tener un cliente SFTP a mano.

const maxUploadBytes = 512 << 20 // 512 MB por archivo subido

type fileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"` // ruta relativa a la raíz de la cuenta
	IsDir   bool      `json:"is_dir"`
	SizeB   int64     `json:"size_b"`
	ModTime time.Time `json:"mod_time"`
}

// accountFilesRoot resuelve la cuenta del path, comprueba que el usuario
// autenticado tenga acceso, y devuelve la raíz de sus archivos en el host
// junto con el ID de cuenta (lo necesitan los handlers que escriben, para
// comprobar la cuota de disco antes de guardar nada).
func (s *Server) accountFilesRoot(w http.ResponseWriter, r *http.Request) (string, uuid.UUID, bool) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return "", uuid.Nil, false
	}
	id := auth.MustIdentity(r.Context())
	acct, err := s.authorizeAccount(r.Context(), id, accountID)
	if err != nil {
		writeStoreError(w, err)
		return "", uuid.Nil, false
	}
	return filepath.Join(s.cfg.SitesRoot, acct.SystemUser), acct.ID, true
}

// diskQuotaExceeded compara el último uso de disco calculado (el worker de
// cuotas lo recalcula cada 15 min, no en cada petición: caminar el árbol de
// archivos es demasiado costoso para el camino caliente) contra la cuota del
// plan. Un plan con cuota 0 se trata como "sin cuota" (nunca bloquea).
func (s *Server) diskQuotaExceeded(ctx context.Context, accountID uuid.UUID) bool {
	acct, err := s.st.GetAccount(ctx, accountID)
	if err != nil {
		return false
	}
	plan, err := s.st.GetPlan(ctx, acct.PlanID)
	if err != nil {
		return false
	}
	return plan.DiskQuotaMB > 0 && acct.DiskUsedMB >= plan.DiskQuotaMB
}

const errDiskQuotaExceeded = "la cuenta superó la cuota de disco del plan; libera espacio antes de subir más archivos"

// safePath ancla rel dentro de root. Se limpia como si colgara de una raíz
// virtual "/" —donde ".." no puede subir más allá de esa raíz— y solo
// después se une a root, así que ninguna combinación de ".." en la entrada
// puede escapar del árbol de la cuenta.
func safePath(root, rel string) (string, error) {
	clean := filepath.Clean("/" + strings.TrimPrefix(filepath.ToSlash(rel), "/"))
	full := filepath.Join(root, clean)
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("ruta inválida")
	}
	return full, nil
}

func toRelative(root, full string) string {
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	root, _, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	dir, err := safePath(root, r.URL.Query().Get("path"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "la carpeta no existe")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // symlink roto u otro error puntual: se omite, no se aborta el listado
		}
		full := filepath.Join(dir, e.Name())
		out = append(out, fileEntry{
			Name: e.Name(), Path: toRelative(root, full),
			IsDir: e.IsDir(), SizeB: info.Size(), ModTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	httpx.OK(w, map[string]any{"path": toRelative(root, dir), "entries": out})
}

func (s *Server) handleUploadFiles(w http.ResponseWriter, r *http.Request) {
	root, accountID, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	if s.diskQuotaExceeded(r.Context(), accountID) {
		httpx.Error(w, http.StatusForbidden, errDiskQuotaExceeded)
		return
	}
	dir, err := safePath(root, r.URL.Query().Get("path"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Margen sobre el límite por archivo para el overhead del multipart y
	// permitir varios archivos en una misma subida.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes*8)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "la subida supera el límite permitido")
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo preparar la carpeta destino")
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		httpx.Error(w, http.StatusBadRequest, "no se recibió ningún archivo")
		return
	}
	saved := make([]string, 0, len(files))
	for _, fh := range files {
		name := filepath.Base(fh.Filename)
		if name == "" || name == "." || name == ".." {
			continue
		}
		dest, err := safePath(dir, name)
		if err != nil {
			continue
		}
		if err := saveUploadedFile(fh, dest); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "no se pudo guardar "+name+": "+err.Error())
			return
		}
		saved = append(saved, name)
	}
	httpx.Created(w, map[string]any{"saved": saved})
}

func saveUploadedFile(fh *multipart.FileHeader, dest string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, io.LimitReader(src, maxUploadBytes))
	return err
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	root, _, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	full, err := safePath(root, r.URL.Query().Get("path"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		httpx.Error(w, http.StatusNotFound, "archivo no encontrado")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(full)+`"`)
	http.ServeFile(w, r, full)
}

// maxEditableBytes acota lo que el editor de texto integrado puede abrir o
// guardar: es para editar configuraciones y código, no para volcar archivos
// grandes por esta vía (para eso está la descarga/subida normal).
const maxEditableBytes = 4 << 20 // 4 MB

func (s *Server) handleReadFileContent(w http.ResponseWriter, r *http.Request) {
	root, _, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	full, err := safePath(root, r.URL.Query().Get("path"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		httpx.Error(w, http.StatusNotFound, "archivo no encontrado")
		return
	}
	if info.Size() > maxEditableBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "el archivo es demasiado grande para editarlo aquí")
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if looksBinary(data) {
		httpx.Error(w, http.StatusUnsupportedMediaType, "el archivo no parece ser de texto")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleWriteFileContent(w http.ResponseWriter, r *http.Request) {
	root, accountID, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	if s.diskQuotaExceeded(r.Context(), accountID) {
		httpx.Error(w, http.StatusForbidden, errDiskQuotaExceeded)
		return
	}
	full, err := safePath(root, r.URL.Query().Get("path"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if info, err := os.Stat(full); err == nil && info.IsDir() {
		httpx.Error(w, http.StatusBadRequest, "no se puede editar una carpeta")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxEditableBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "el contenido supera el límite permitido")
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, map[string]any{"path": toRelative(root, full)})
}

// looksBinary aplica la heurística habitual: un byte nulo en los primeros
// bytes del archivo casi siempre significa que no es texto plano.
func looksBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for _, b := range data[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	root, _, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	rel := r.URL.Query().Get("path")
	if strings.TrimSpace(rel) == "" {
		httpx.Error(w, http.StatusBadRequest, "no se puede eliminar la raíz de la cuenta")
		return
	}
	full, err := safePath(root, rel)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if full == root {
		httpx.Error(w, http.StatusBadRequest, "no se puede eliminar la raíz de la cuenta")
		return
	}
	if err := os.RemoveAll(full); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

type mkdirRequest struct {
	Path string `json:"path"`
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	root, _, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	var req mkdirRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		httpx.FieldError(w, "path", "es obligatorio")
		return
	}
	full, err := safePath(root, req.Path)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.Created(w, map[string]any{"path": toRelative(root, full)})
}

type renameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Server) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	root, _, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	var req renameRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	fromFull, err := safePath(root, req.From)
	if err != nil {
		httpx.FieldError(w, "from", "ruta de origen inválida")
		return
	}
	if fromFull == root {
		httpx.Error(w, http.StatusBadRequest, "no se puede mover la raíz de la cuenta")
		return
	}
	toFull, err := safePath(root, req.To)
	if err != nil {
		httpx.FieldError(w, "to", "ruta de destino inválida")
		return
	}
	if err := os.MkdirAll(filepath.Dir(toFull), 0o755); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Rename(fromFull, toFull); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, map[string]any{"path": toRelative(root, toFull)})
}

// handleExtractZip descomprime un .zip ya subido en una carpeta nueva junto
// a él (mismo nombre, sin la extensión). Cada entrada se ancla con safePath
// para blindarse de "zip slip" (entradas con ".." que intenten escribir
// fuera de esa carpeta).
func (s *Server) handleExtractZip(w http.ResponseWriter, r *http.Request) {
	root, accountID, ok := s.accountFilesRoot(w, r)
	if !ok {
		return
	}
	if s.diskQuotaExceeded(r.Context(), accountID) {
		httpx.Error(w, http.StatusForbidden, errDiskQuotaExceeded)
		return
	}
	full, err := safePath(root, r.URL.Query().Get("path"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if !strings.EqualFold(filepath.Ext(full), ".zip") {
		httpx.Error(w, http.StatusBadRequest, "solo se pueden extraer archivos .zip")
		return
	}
	zr, err := zip.OpenReader(full)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "no se pudo abrir el .zip: "+err.Error())
		return
	}
	defer zr.Close()

	destDir := strings.TrimSuffix(full, filepath.Ext(full))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, f := range zr.File {
		target, err := safePath(destDir, f.Name)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "el .zip contiene una ruta no permitida: "+f.Name)
			return
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				httpx.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := extractZipEntry(f, target); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "extrayendo "+f.Name+": "+err.Error())
			return
		}
	}
	httpx.Created(w, map[string]any{"path": toRelative(root, destDir)})
}

func extractZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	// Tope por archivo dentro del zip: evita que un zip-bomb agote el disco.
	_, err = io.Copy(out, io.LimitReader(rc, 1<<30))
	return err
}
