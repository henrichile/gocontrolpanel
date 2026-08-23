package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/hostctl"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
)

// hostctlClient arma el cliente SSH del firewall del host a partir de la
// config cargada — se crea por request (es una acción de admin poco
// frecuente, no vale la pena mantener una conexión persistente).
func (s *Server) hostctlClient() (*hostctl.Client, error) {
	return hostctl.New(s.cfg.HostctlHost, s.cfg.HostctlSSHPort,
		s.cfg.HostctlHostPubkey, s.cfg.HostctlKeyPath, s.cfg.SSHPort)
}

type firewallResponse struct {
	Configured    bool           `json:"configured"`
	Rules         []hostctl.Rule `json:"rules,omitempty"`
	ProtectedPort int            `json:"protected_port,omitempty"`
	Error         string         `json:"error,omitempty"`
}

func (s *Server) handleGetFirewall(w http.ResponseWriter, r *http.Request) {
	client, err := s.hostctlClient()
	if errors.Is(err, hostctl.ErrNotConfigured) {
		httpx.OK(w, firewallResponse{Configured: false})
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	raw, err := client.Status(r.Context())
	slog.Info("firewall status", "raw", raw, "err", err, "rules_parsed", len(hostctl.ParseStatus(raw)))
	if err != nil {
		httpx.OK(w, firewallResponse{Configured: true, Error: err.Error(), ProtectedPort: s.cfg.SSHPort})
		return
	}
	httpx.OK(w, firewallResponse{
		Configured: true, Rules: hostctl.ParseStatus(raw), ProtectedPort: s.cfg.SSHPort,
	})
}

type firewallRuleRequest struct {
	Action string `json:"action"` // "allow" | "deny"
	Port   int    `json:"port"`
	Proto  string `json:"proto"`
}

func (s *Server) handleSetFirewallRule(w http.ResponseWriter, r *http.Request) {
	var req firewallRuleRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	client, err := s.hostctlClient()
	if errors.Is(err, hostctl.ErrNotConfigured) {
		httpx.Error(w, http.StatusBadRequest, "el acceso al firewall del host no está configurado en este servidor")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := client.SetRule(r.Context(), req.Action, req.Port, req.Proto)
	if errors.Is(err, hostctl.ErrProtectedPort) {
		httpx.FieldError(w, "port", "es el puerto de SSH; no se puede bloquear desde el panel")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "el host rechazó el cambio: "+err.Error()+" "+out)
		return
	}

	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "system.firewall_rule", Detail: map[string]any{
			"action": req.Action, "port": req.Port, "proto": req.Proto,
		}, IPAddress: httpx.ClientIP(r),
	})
	s.handleGetFirewall(w, r)
}

func (s *Server) handleListWAFBlocks(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	if before := r.URL.Query().Get("before"); before != "" {
		beforeID, err := strconv.ParseInt(before, 10, 64)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "before inválido")
			return
		}
		blocks, err := s.st.ListWAFBlocksBefore(r.Context(), beforeID, limit)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.OK(w, map[string]any{"blocks": blocks})
		return
	}

	if after := r.URL.Query().Get("after"); after != "" {
		afterID, err := strconv.ParseInt(after, 10, 64)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "after inválido")
			return
		}
		blocks, err := s.st.ListWAFBlocks(r.Context(), afterID, limit)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.OK(w, map[string]any{"blocks": blocks})
		return
	}

	blocks, err := s.st.ListLatestWAFBlocks(r.Context(), limit)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, map[string]any{"blocks": blocks})
}

// handleStreamWAFBlocks transmite por SSE los bloqueos nuevos del WAF a
// medida que van llegando (poll simple a la BD cada 2s — el volumen de
// bloqueos no justifica un bus de eventos en memoria).
func (s *Server) handleStreamWAFBlocks(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "el servidor no soporta streaming")
		return
	}
	lastID, err := s.st.LatestWAFBlockID(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	bw := bufio.NewWriter(w)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			blocks, err := s.st.ListWAFBlocks(r.Context(), lastID, 100)
			if err != nil || len(blocks) == 0 {
				continue
			}
			for _, b := range blocks {
				data, err := json.Marshal(b)
				if err != nil {
					continue
				}
				bw.WriteString("data: ")
				bw.Write(data)
				bw.WriteString("\n\n")
				lastID = b.ID
			}
			bw.Flush()
			flusher.Flush()
		}
	}
}
