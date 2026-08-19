// Package web embebe el build de producción de la SPA en el binario.
//
// El directorio dist/ lo genera `npm run build` (ver Makefile). Se incluye un
// index.html mínimo en el repositorio para que `go build` funcione aunque
// todavía no se haya compilado el frontend.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets devuelve el sistema de archivos con la raíz en dist/.
func Assets() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	return sub
}
