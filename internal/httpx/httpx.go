// Package httpx agrupa utilidades de serialización y manejo de errores para
// la API REST.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// Envelope es la forma estándar de las respuestas de error.
type Envelope struct {
	Error   string            `json:"error"`
	Code    string            `json:"code,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
	TraceID string            `json:"trace_id,omitempty"`
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("no se pudo escribir la respuesta JSON", "error", err)
	}
}

func OK(w http.ResponseWriter, payload any) { JSON(w, http.StatusOK, payload) }

func Created(w http.ResponseWriter, payload any) { JSON(w, http.StatusCreated, payload) }

func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, Envelope{Error: msg})
}

func FieldError(w http.ResponseWriter, field, msg string) {
	JSON(w, http.StatusUnprocessableEntity, Envelope{
		Error:  "datos inválidos",
		Code:   "validation_error",
		Fields: map[string]string{field: msg},
	})
}

// Decode lee el cuerpo JSON con un límite de tamaño y rechaza campos
// desconocidos, lo que evita errores silenciosos en el cliente.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			return errors.New("el cuerpo de la petición no es JSON válido")
		case errors.As(err, &typeErr):
			return errors.New("campo '" + typeErr.Field + "' con tipo incorrecto")
		case strings.Contains(err.Error(), "unknown field"):
			return errors.New(strings.Replace(err.Error(), "json: unknown field", "campo desconocido:", 1))
		default:
			return errors.New("no se pudo leer el cuerpo de la petición")
		}
	}
	return nil
}

// ClientIP resuelve la IP real teniendo en cuenta el proxy de borde.
func ClientIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
