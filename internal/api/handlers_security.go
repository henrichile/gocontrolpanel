package api

import (
	"bufio"
	"context"
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
	Configured bool `json:"configured"`
	// Enabled solo es significativo si EnabledKnown es true — un host cuyo
	// script todavía no se actualizó (hace falta volver a correr
	// install.sh) no manda el estado global, y no hay forma de saber si el
	// firewall está activo sin adivinar.
	Enabled      bool           `json:"enabled"`
	EnabledKnown bool           `json:"enabled_known,omitempty"`
	Rules        []hostctl.Rule `json:"rules,omitempty"`
	// DockerPorts: puertos que algún contenedor gestionado por el panel
	// (borde, SFTP, correo) publica ahora mismo al host, detectados vía
	// Docker en vez de leídos del firewall. Docker les pone su propia regla
	// de iptables al publicarlos con "ports:", *antes* de que ufw los vea —
	// así que puede haber puertos realmente accesibles aunque no aparezcan
	// en Rules. Es solo informativo (de acá no sale ningún "action": el
	// puerto está abierto porque Docker lo abrió, no porque el firewall lo
	// permita); SincronizarDockerConFirewall es lo que agrega la regla que
	// falta en ufw para que ambos coincidan.
	DockerPorts   []hostctl.Rule `json:"docker_ports,omitempty"`
	ProtectedPort int            `json:"protected_port,omitempty"`
	Error         string         `json:"error,omitempty"`
}

// dockerManagedPorts detecta los puertos que los contenedores que el panel
// conoce (borde, SFTP y — si está habilitado — correo) tienen publicados al
// host en este momento. Nunca asume nada de docker-compose.yml a mano ni
// inventa números de puerto: inspecciona cada contenedor por su nombre real
// y lee sus bindings. El campo "From" no lleva un CIDR acá (no aplica a un
// puerto publicado por Docker) — se reutiliza como etiqueta de a qué
// servicio pertenece ("web"/"ftp"/"mail"), para que el frontend arme sus
// presets con los puertos reales de cada uno en vez de adivinarlos.
func (s *Server) dockerManagedPorts(ctx context.Context) []hostctl.Rule {
	groups := []struct {
		container string
		label     string
	}{
		{s.cfg.EdgeContainerName, "web"},
		{s.cfg.SFTPContainerName, "ftp"},
	}
	if s.cfg.MailEnabled {
		groups = append(groups, struct {
			container string
			label     string
		}{s.cfg.MailContainerName, "mail"})
	}
	seen := map[string]bool{}
	var out []hostctl.Rule
	for _, g := range groups {
		if g.container == "" {
			continue
		}
		ports, err := s.svc.Docker().PublishedPorts(ctx, g.container)
		if err != nil {
			slog.Warn("firewall: no se pudieron leer los puertos publicados", "container", g.container, "error", err)
			continue
		}
		for _, p := range ports {
			key := strconv.Itoa(p.HostPort) + "/" + p.Proto
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, hostctl.Rule{Port: p.HostPort, Proto: p.Proto, Action: "allow", From: g.label})
		}
	}
	return out
}

func (s *Server) handleGetFirewall(w http.ResponseWriter, r *http.Request) {
	dockerPorts := s.dockerManagedPorts(r.Context())

	client, err := s.hostctlClient()
	if errors.Is(err, hostctl.ErrNotConfigured) {
		httpx.OK(w, firewallResponse{Configured: false, DockerPorts: dockerPorts})
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	raw, err := client.Status(r.Context())
	if err != nil {
		httpx.OK(w, firewallResponse{
			Configured: true, Error: err.Error(), ProtectedPort: s.cfg.SSHPort, DockerPorts: dockerPorts,
		})
		return
	}
	st := hostctl.ParseStatus(raw)
	httpx.OK(w, firewallResponse{
		Configured: true, Enabled: st.Enabled, EnabledKnown: st.EnabledKnown, Rules: st.Rules,
		ProtectedPort: s.cfg.SSHPort, DockerPorts: dockerPorts,
	})
}

// handleSetFirewallEnabled activa o desactiva el firewall del host por
// completo. Acción explícita, con su propia entrada de auditoría — nunca
// corre sola.
func (s *Server) handleSetFirewallEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
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
	if out, err := client.SetEnabled(r.Context(), req.Enabled); err != nil {
		httpx.Error(w, http.StatusBadGateway, "el host rechazó el cambio: "+err.Error()+" "+out)
		return
	}

	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "system.firewall_enabled", Detail: map[string]any{"enabled": req.Enabled},
		IPAddress: httpx.ClientIP(r),
	})
	s.handleGetFirewall(w, r)
}

// handleSyncDockerFirewall agrega al firewall del host (ufw) una regla
// "allow" para cada puerto que Docker ya tiene publicado pero que todavía no
// tiene una regla explícita — no cierra ni toca nada más. Es una acción
// explícita (el admin la dispara con un botón), no algo que corra solo al
// abrir la pestaña: agregar reglas de firewall es una acción, no una lectura.
func (s *Server) handleSyncDockerFirewall(w http.ResponseWriter, r *http.Request) {
	client, err := s.hostctlClient()
	if errors.Is(err, hostctl.ErrNotConfigured) {
		httpx.Error(w, http.StatusBadRequest, "el acceso al firewall del host no está configurado en este servidor")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	raw, err := client.Status(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "no se pudo leer el estado del firewall: "+err.Error())
		return
	}
	existing := map[string]bool{}
	for _, ru := range hostctl.ParseStatus(raw).Rules {
		if ru.Action == "allow" {
			existing[strconv.Itoa(ru.Port)+"/"+ru.Proto] = true
		}
	}

	var added []hostctl.Rule
	for _, p := range s.dockerManagedPorts(r.Context()) {
		if existing[strconv.Itoa(p.Port)+"/"+p.Proto] {
			continue
		}
		if _, err := client.SetRule(r.Context(), "allow", p.Port, p.Proto); err != nil {
			slog.Warn("firewall: no se pudo sincronizar un puerto de Docker", "port", p.Port, "proto", p.Proto, "error", err)
			continue
		}
		added = append(added, p)
	}

	if len(added) > 0 {
		id := auth.MustIdentity(r.Context())
		s.st.Audit(r.Context(), models.AuditEntry{
			ActorID: &id.UserID, ActorUsername: id.Username,
			Action: "system.firewall_sync_docker", Detail: map[string]any{"added": added},
			IPAddress: httpx.ClientIP(r),
		})
	}

	s.handleGetFirewall(w, r)
}

type firewallRuleRequest struct {
	Action string `json:"action"` // "allow" | "deny"
	Port   int    `json:"port"`
	Proto  string `json:"proto"`
	// Origin/Comment son opcionales: los presets y el toggle rápido no los
	// mandan (usan la forma simple, que en "deny" borra el "allow"
	// existente — ver el comentario de SetRule). El formulario de "Añadir
	// regla" sí los manda, y ahí "deny" inserta una regla explícita en vez
	// de borrar nada — ver SetRuleFull.
	Origin  string `json:"origin,omitempty"`
	Comment string `json:"comment,omitempty"`
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

	var out string
	if req.Origin == "" && req.Comment == "" {
		out, err = client.SetRule(r.Context(), req.Action, req.Port, req.Proto)
	} else {
		out, err = client.SetRuleFull(r.Context(), req.Action, req.Port, req.Proto, req.Origin, req.Comment)
	}
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
			"origin": req.Origin, "comment": req.Comment,
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
