package api

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// serveSPA sirve el build de React embebido en el binario. Cualquier ruta que
// no exista como archivo cae en index.html para que funcione el router del
// cliente.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if s.webFS == nil {
		http.Error(w, "la interfaz web no está compilada en este binario "+
			"(ejecuta `make web` antes de `make build`)", http.StatusNotFound)
		return
	}

	clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if clean == "" || clean == "." {
		clean = "index.html"
	}

	f, err := s.webFS.Open(clean)
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || st.IsDir() {
		s.serveIndex(w, r)
		return
	}

	// Los assets con hash en el nombre pueden cachearse de forma agresiva.
	if strings.HasPrefix(clean, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, st.Name(), st.ModTime(), rs)
		return
	}
	w.Header().Set("Content-Type", contentTypeFor(clean))
	_, _ = io.Copy(w, f)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.webFS, "index.html")
	if err != nil {
		http.Error(w, "interfaz web no disponible", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func contentTypeFor(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}
