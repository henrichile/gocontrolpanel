package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/provision"
	"github.com/etasoft/gocontrolpanel/internal/sysinfo"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{"status": "ok"}
	if err := s.st.Pool().Ping(r.Context()); err != nil {
		status["status"] = "degraded"
		status["database"] = err.Error()
	}
	if err := s.svc.Docker().Ping(r.Context()); err != nil {
		status["status"] = "degraded"
		status["docker"] = err.Error()
	}
	if err := s.caddy.Ping(r.Context()); err != nil {
		status["status"] = "degraded"
		status["caddy"] = err.Error()
	}
	code := http.StatusOK
	if status["status"] != "ok" {
		code = http.StatusServiceUnavailable
	}
	httpx.JSON(w, code, status)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	id := auth.MustIdentity(r.Context())
	ov, err := s.st.Overview(r.Context(), scopeFor(id))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"overview": ov})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	info, err := sysinfo.Collect()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	containers, _ := s.svc.Docker().ListManaged(r.Context())
	httpx.OK(w, map[string]any{
		"system":     info,
		"containers": len(containers),
	})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := s.st.ListAudit(r.Context(), limit, nil)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"entries": entries})
}

func (s *Server) handleCaddyConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := s.caddy.CurrentConfig(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(raw)
}

func (s *Server) handleCaddySync(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.SyncCaddy(r.Context()); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, map[string]any{"status": "sincronizado"})
}

// handleTLSAuthorize responde 200 solo si el dominio consultado pertenece a un
// sitio activo. Caddy lo usa como "ask" antes de emitir un certificado
// on-demand, lo que evita que cualquiera apunte un DNS a este servidor y nos
// haga pedir certificados en su nombre.
func (s *Server) handleTLSAuthorize(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		httpx.Error(w, http.StatusBadRequest, "falta el parámetro domain")
		return
	}
	routes, err := s.st.RoutingTable(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "error consultando dominios")
		return
	}
	for _, route := range routes {
		if route.FQDN == domain {
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	httpx.Error(w, http.StatusForbidden, "dominio no gestionado por este panel")
}

// asValidation extrae un provision.ValidationError de una cadena de errores.
func asValidation(err error, target *provision.ValidationError) bool {
	return errors.As(err, target)
}
