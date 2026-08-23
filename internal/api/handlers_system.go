package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
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

// securityStatusResponse es lo que ve el ambiente de administración: los
// valores editables (de system_settings, en BD) más datos de solo lectura
// calculados en vivo (adopción de 2FA, endurecimiento de contenedores).
type securityStatusResponse struct {
	WAFEnabled          bool `json:"waf_enabled"`
	RateLimitPerMinute  int  `json:"rate_limit_per_minute"`
	BackupRetentionDays int  `json:"backup_retention_days"`
	SiteNonRoot         bool `json:"site_non_root"`
	TOTPEnabledAdmins   int  `json:"totp_enabled_admins"`
	TotalAdmins         int  `json:"total_admins"`
}

func (s *Server) handleGetSecuritySettings(w http.ResponseWriter, r *http.Request) {
	resp, err := s.securityStatus(r)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, resp)
}

type updateSecuritySettingsRequest struct {
	WAFEnabled          bool `json:"waf_enabled"`
	RateLimitPerMinute  int  `json:"rate_limit_per_minute"`
	BackupRetentionDays int  `json:"backup_retention_days"`
}

// handleUpdateSecuritySettings guarda y aplica en caliente: si Caddy rechaza
// la config resultante (p.ej. WAF activado contra un edge sin Coraza
// compilado — el incidente real que motivó esto), se revierte el cambio en
// BD y se devuelve el error de Caddy tal cual, para que el admin sepa por
// qué antes de quedarse con el servidor roto.
func (s *Server) handleUpdateSecuritySettings(w http.ResponseWriter, r *http.Request) {
	var req updateSecuritySettingsRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RateLimitPerMinute < 10 {
		httpx.FieldError(w, "rate_limit_per_minute", "debe ser al menos 10")
		return
	}
	if req.BackupRetentionDays < 1 {
		httpx.FieldError(w, "backup_retention_days", "debe ser al menos 1 día")
		return
	}

	previous, err := s.st.GetSystemSettings(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.UpdateSystemSettings(r.Context(), req.WAFEnabled, req.RateLimitPerMinute, req.BackupRetentionDays); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.svc.SyncCaddy(r.Context()); err != nil {
		// Revertir: no dejar la BD con un estado que Caddy no puede servir.
		_ = s.st.UpdateSystemSettings(r.Context(),
			previous.WAFEnabled, previous.RateLimitPerMinute, previous.BackupRetentionDays)
		httpx.Error(w, http.StatusBadGateway,
			"Caddy rechazó la nueva configuración, no se aplicó ningún cambio: "+err.Error())
		return
	}

	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "system.security_updated", Detail: map[string]any{
			"waf_enabled": req.WAFEnabled, "rate_limit_per_minute": req.RateLimitPerMinute,
			"backup_retention_days": req.BackupRetentionDays,
		}, IPAddress: httpx.ClientIP(r),
	})

	resp, err := s.securityStatus(r)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, resp)
}

func (s *Server) securityStatus(r *http.Request) (securityStatusResponse, error) {
	settings, err := s.st.GetSystemSettings(r.Context())
	if err != nil {
		return securityStatusResponse{}, err
	}
	resp := securityStatusResponse{
		WAFEnabled:          settings.WAFEnabled,
		RateLimitPerMinute:  settings.RateLimitPerMinute,
		BackupRetentionDays: settings.BackupRetentionDays,
		// Los contenedores de sitio siempre corren como SiteUID desde que
		// se agregó esto (internal/dockerx/manager.go): no depende de config.
		SiteNonRoot: true,
	}
	users, err := s.st.ListUsers(r.Context(), nil)
	if err != nil {
		return resp, nil
	}
	for _, u := range users {
		if u.Role == models.RoleAdmin || u.Role == models.RoleReseller {
			resp.TotalAdmins++
			if u.TOTPEnabled {
				resp.TOTPEnabledAdmins++
			}
		}
	}
	return resp, nil
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
	// Defensa en profundidad: el dominio del propio panel ya debería estar
	// cubierto por la política ACME explícita en caddyapi.Build (no por el
	// catch-all on-demand), pero si por lo que sea Caddy igual pregunta acá,
	// no queremos que el panel se rechace a sí mismo y se quede sin cert.
	if domain == provision.PanelHost(s.cfg.PublicURL) {
		w.WriteHeader(http.StatusOK)
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
