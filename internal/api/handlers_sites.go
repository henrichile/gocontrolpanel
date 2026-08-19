package api

import (
	"bufio"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/provision"
)

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	id := auth.MustIdentity(r.Context())

	// ?account_id= filtra a una cuenta concreta.
	if raw := r.URL.Query().Get("account_id"); raw != "" {
		accountID, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "account_id inválido")
			return
		}
		if _, err := s.authorizeAccount(r.Context(), id, accountID); err != nil {
			writeStoreError(w, err)
			return
		}
		sites, err := s.st.ListSites(r.Context(), &accountID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		httpx.OK(w, map[string]any{"sites": sites})
		return
	}

	var (
		sites []models.Site
		err   error
	)
	if id.Role == models.RoleAdmin {
		sites, err = s.st.ListSites(r.Context(), nil)
	} else {
		sites, err = s.st.ListSitesForOwner(r.Context(), id.UserID)
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"sites": sites})
}

type createSiteRequest struct {
	AccountID    string            `json:"account_id"`
	Name         string            `json:"name"`
	PHPVersion   string            `json:"php_version"`
	Domain       string            `json:"domain"`
	DocumentRoot string            `json:"document_root"`
	WorkerScript string            `json:"worker_script"`
	WorkerCount  int               `json:"worker_count"`
	EnvVars      map[string]string `json:"env_vars"`
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var req createSiteRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		httpx.FieldError(w, "account_id", "no es un UUID válido")
		return
	}
	id := auth.MustIdentity(r.Context())
	if _, err := s.authorizeAccount(r.Context(), id, accountID); err != nil {
		writeStoreError(w, err)
		return
	}

	site, err := s.svc.CreateSite(r.Context(), provision.CreateSiteInput{
		AccountID:    accountID,
		Name:         req.Name,
		PHPVersion:   req.PHPVersion,
		Domain:       req.Domain,
		DocumentRoot: req.DocumentRoot,
		WorkerScript: req.WorkerScript,
		WorkerCount:  req.WorkerCount,
		EnvVars:      req.EnvVars,
	})
	if err != nil {
		var ve provision.ValidationError
		if asValidation(err, &ve) {
			httpx.FieldError(w, ve.Field, ve.Message)
			return
		}
		writeStoreError(w, err)
		return
	}

	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "site.create", TargetType: "site", TargetID: site.ID.String(),
		Detail: map[string]any{"name": site.Name, "php": site.PHPVersion},
		IPAddress: httpx.ClientIP(r),
	})
	httpx.Created(w, map[string]any{"site": site})
}

func (s *Server) handleGetSite(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Estado real del contenedor, que puede diferir del guardado.
	state, _ := s.svc.Docker().State(r.Context(), site.ContainerName)
	httpx.OK(w, map[string]any{"site": site, "container_state": state})
}

type updateSiteRequest struct {
	PHPVersion   *string           `json:"php_version"`
	DocumentRoot *string           `json:"document_root"`
	WorkerScript *string           `json:"worker_script"`
	WorkerCount  *int              `json:"worker_count"`
	EnvVars      map[string]string `json:"env_vars"`
	Redeploy     bool              `json:"redeploy"`
}

func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req updateSiteRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PHPVersion != nil {
		site.PHPVersion = *req.PHPVersion
	}
	if req.DocumentRoot != nil {
		site.DocumentRoot = *req.DocumentRoot
	}
	if req.WorkerScript != nil {
		site.WorkerScript = *req.WorkerScript
	}
	if req.WorkerCount != nil {
		site.WorkerCount = *req.WorkerCount
	}
	if req.EnvVars != nil {
		site.EnvVars = req.EnvVars
	}
	if err := s.st.UpdateSiteConfig(r.Context(), site); err != nil {
		writeStoreError(w, err)
		return
	}
	if req.Redeploy {
		if err := s.svc.RedeploySite(r.Context(), site.ID); err != nil {
			httpx.Error(w, http.StatusInternalServerError,
				"la configuración se guardó pero el redespliegue falló: "+err.Error())
			return
		}
	}
	updated, err := s.st.GetSite(r.Context(), site.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"site": updated})
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	id := auth.MustIdentity(r.Context())
	deleteFiles := r.URL.Query().Get("delete_files") == "true"
	if err := s.svc.DeleteSite(r.Context(), site.ID, deleteFiles); err != nil {
		writeStoreError(w, err)
		return
	}
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "site.delete", TargetType: "site", TargetID: site.ID.String(),
		Detail: map[string]any{"delete_files": deleteFiles}, IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

func (s *Server) handleSiteStart(w http.ResponseWriter, r *http.Request) {
	s.siteAction(w, r, "start", func(id uuid.UUID) error {
		return s.svc.StartSite(r.Context(), id)
	})
}

func (s *Server) handleSiteStop(w http.ResponseWriter, r *http.Request) {
	s.siteAction(w, r, "stop", func(id uuid.UUID) error {
		return s.svc.StopSite(r.Context(), id)
	})
}

func (s *Server) handleSiteRestart(w http.ResponseWriter, r *http.Request) {
	s.siteAction(w, r, "restart", func(id uuid.UUID) error {
		return s.svc.RestartSite(r.Context(), id)
	})
}

func (s *Server) handleSiteRedeploy(w http.ResponseWriter, r *http.Request) {
	s.siteAction(w, r, "redeploy", func(id uuid.UUID) error {
		return s.svc.RedeploySite(r.Context(), id)
	})
}

func (s *Server) siteAction(w http.ResponseWriter, r *http.Request, action string,
	fn func(uuid.UUID) error) {

	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := fn(site.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "site." + action, TargetType: "site", TargetID: site.ID.String(),
		IPAddress: httpx.ClientIP(r),
	})
	updated, _ := s.st.GetSite(r.Context(), site.ID)
	httpx.OK(w, map[string]any{"site": updated})
}

// handleSiteLogs devuelve las últimas líneas, o un stream SSE si ?follow=true.
func (s *Server) handleSiteLogs(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	follow := r.URL.Query().Get("follow") == "true"

	rc, err := s.svc.SiteLogs(r.Context(), site.ID, tail, follow)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudieron leer los logs: "+err.Error())
		return
	}
	defer rc.Close()

	if !follow {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if len(line) > 8 {
				line = stripFrame(line)
			}
			fmt.Fprintln(w, line)
		}
		return
	}

	// Streaming SSE para el visor de logs en vivo.
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "el servidor no soporta streaming")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		fmt.Fprintf(w, "data: %s\n\n", stripFrame(sc.Text()))
		flusher.Flush()
	}
}

func (s *Server) handleSiteStats(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	stats, err := s.svc.Docker().Stats(r.Context(), site.ContainerName)
	if err != nil {
		httpx.Error(w, http.StatusServiceUnavailable, "no se pudieron leer las métricas: "+err.Error())
		return
	}
	httpx.OK(w, map[string]any{"stats": stats})
}

func (s *Server) handleSiteUsage(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	window := 24 * time.Hour
	if raw := r.URL.Query().Get("hours"); raw != "" {
		if h, err := strconv.Atoi(raw); err == nil && h > 0 && h <= 720 {
			window = time.Duration(h) * time.Hour
		}
	}
	samples, err := s.st.UsageHistory(r.Context(), site.ID, window)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"samples": samples})
}

// --- Dominios --------------------------------------------------------------

type addDomainRequest struct {
	FQDN       string `json:"fqdn"`
	Kind       string `json:"kind"`
	RedirectTo string `json:"redirect_to"`
}

func (s *Server) handleAddDomain(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req addDomainRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := s.svc.AddDomain(r.Context(), site.ID, req.FQDN,
		models.DomainKind(req.Kind), req.RedirectTo)
	if err != nil {
		var ve provision.ValidationError
		if asValidation(err, &ve) {
			httpx.FieldError(w, ve.Field, ve.Message)
			return
		}
		writeStoreError(w, err)
		return
	}
	httpx.Created(w, map[string]any{"domain": d})
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	domainID, err := pathUUID(r, "domainID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de dominio inválido")
		return
	}
	domain, err := s.st.GetDomain(r.Context(), domainID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	id := auth.MustIdentity(r.Context())
	if _, _, err := s.authorizeSite(r.Context(), id, domain.SiteID); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.svc.RemoveDomain(r.Context(), domainID); err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- helpers ---------------------------------------------------------------

func (s *Server) resolveSite(r *http.Request) (*models.Site, *models.Account, error) {
	siteID, err := pathUUID(r, "siteID")
	if err != nil {
		return nil, nil, err
	}
	id := auth.MustIdentity(r.Context())
	return s.authorizeSite(r.Context(), id, siteID)
}

func stripFrame(line string) string {
	if len(line) >= 8 && (line[0] == 1 || line[0] == 2) && line[1] == 0 {
		return line[8:]
	}
	return line
}
