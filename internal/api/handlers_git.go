package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/provision"
	"github.com/etasoft/gocontrolpanel/internal/store"
)

// gitConfigResponse expone al dueño del sitio los campos que el modelo oculta
// por defecto (webhook_secret, private_key_enc) — el modelo los marca
// json:"-" para que ningún otro endpoint los filtre por accidente; solo
// estos handlers, ya autorizados vía resolveSite, los arman a mano.
type gitConfigResponse struct {
	SiteID           string     `json:"site_id"`
	RepoURL          string     `json:"repo_url"`
	Branch           string     `json:"branch"`
	PublicKey        string     `json:"public_key"`
	WebhookURL       string     `json:"webhook_url"`
	WebhookSecret    string     `json:"webhook_secret"`
	AutoDeploy       bool       `json:"auto_deploy"`
	LastDeployAt     *time.Time `json:"last_deploy_at,omitempty"`
	LastDeployStatus string     `json:"last_deploy_status"`
	LastDeployOutput string     `json:"last_deploy_output"`
	CreatedAt        time.Time  `json:"created_at"`
}

func (s *Server) gitResponse(cfg *models.SiteGitConfig) gitConfigResponse {
	return gitConfigResponse{
		SiteID: cfg.SiteID.String(), RepoURL: cfg.RepoURL, Branch: cfg.Branch,
		PublicKey:        cfg.PublicKey,
		WebhookURL:       strings.TrimSuffix(s.cfg.PublicURL, "/") + "/api/v1/webhooks/git/" + cfg.SiteID.String(),
		WebhookSecret:    cfg.WebhookSecret,
		AutoDeploy:       cfg.AutoDeploy,
		LastDeployAt:     cfg.LastDeployAt,
		LastDeployStatus: cfg.LastDeployStatus,
		LastDeployOutput: cfg.LastDeployOutput,
		CreatedAt:        cfg.CreatedAt,
	}
}

func (s *Server) handleGetSiteGit(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cfg, err := s.st.GetSiteGitConfig(r.Context(), site.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.OK(w, map[string]any{"connected": false})
			return
		}
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"connected": true, "git": s.gitResponse(cfg)})
}

type connectGitRequest struct {
	RepoURL    string `json:"repo_url"`
	Branch     string `json:"branch"`
	AutoDeploy bool   `json:"auto_deploy"`
}

func (s *Server) handleConnectSiteGit(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req connectGitRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := s.svc.ConnectGit(r.Context(), site.ID, req.RepoURL, req.Branch, req.AutoDeploy)
	if err != nil {
		var ve provision.ValidationError
		if asValidation(err, &ve) {
			httpx.FieldError(w, ve.Field, ve.Message)
			return
		}
		writeStoreError(w, err)
		return
	}

	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "site.git_connect", TargetType: "site", TargetID: site.ID.String(),
		Detail: map[string]any{"repo_url": cfg.RepoURL, "branch": cfg.Branch}, IPAddress: httpx.ClientIP(r),
	})

	// Primer deploy: se hace en la misma request para que el usuario vea de
	// inmediato si la clave ya quedó autorizada en el repositorio.
	output, deployErr := s.svc.GitDeploy(r.Context(), site.ID)
	cfg, _ = s.st.GetSiteGitConfig(r.Context(), site.ID)
	resp := map[string]any{"git": s.gitResponse(cfg)}
	if deployErr != nil {
		resp["deploy_error"] = deployErr.Error()
		resp["deploy_output"] = output
	}
	httpx.Created(w, resp)
}

func (s *Server) handleSiteGitDeploy(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	output, deployErr := s.svc.GitDeploy(r.Context(), site.ID)
	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "site.git_deploy", TargetType: "site", TargetID: site.ID.String(),
		IPAddress: httpx.ClientIP(r),
	})
	cfg, err := s.st.GetSiteGitConfig(r.Context(), site.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	resp := map[string]any{"git": s.gitResponse(cfg)}
	if deployErr != nil {
		resp["deploy_error"] = deployErr.Error()
		resp["deploy_output"] = output
	}
	httpx.OK(w, resp)
}

func (s *Server) handleDisconnectSiteGit(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.svc.DisconnectGit(r.Context(), site.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "site.git_disconnect", TargetType: "site", TargetID: site.ID.String(),
		IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

// handleGitWebhook es la única ruta pública de este archivo: no hay sesión,
// así que la autenticación es la firma/token propio del proveedor de Git
// contra el webhook_secret generado al conectar el repositorio.
func (s *Server) handleGitWebhook(w http.ResponseWriter, r *http.Request) {
	siteID, err := pathUUID(r, "siteID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de sitio inválido")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "no se pudo leer el cuerpo")
		return
	}

	cfg, err := s.st.GetSiteGitConfig(r.Context(), siteID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "el sitio no tiene un repositorio conectado")
			return
		}
		writeStoreError(w, err)
		return
	}

	if !verifyWebhookAuth(r, body, cfg.WebhookSecret) {
		httpx.Error(w, http.StatusUnauthorized, "firma del webhook inválida")
		return
	}

	if !cfg.AutoDeploy {
		httpx.OK(w, map[string]any{"deployed": false, "reason": "auto_deploy está desactivado"})
		return
	}
	if ref := pushPayloadRef(body); ref != "" && ref != "refs/heads/"+cfg.Branch {
		httpx.OK(w, map[string]any{"deployed": false, "reason": "push a una rama distinta de " + cfg.Branch})
		return
	}

	httpx.JSON(w, http.StatusAccepted, map[string]any{"deployed": true})

	clientIP := httpx.ClientIP(r)
	go func(siteID uuid.UUID) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, err := s.svc.GitDeploy(ctx, siteID)
		status := "success"
		if err != nil {
			status = "failed"
		}
		s.st.Audit(ctx, models.AuditEntry{
			ActorUsername: "webhook", Action: "site.git_deploy", TargetType: "site",
			TargetID: siteID.String(), Detail: map[string]any{"status": status}, IPAddress: clientIP,
		})
	}(cfg.SiteID)
}

// verifyWebhookAuth acepta el esquema de GitHub (HMAC-SHA256 sobre el body
// en X-Hub-Signature-256), el de GitLab (token exacto en X-Gitlab-Token) o,
// si ninguno de los dos headers viene, un secreto plano en ?token= — para
// proveedores genéricos o para disparar el deploy a mano con curl.
func verifyWebhookAuth(r *http.Request, body []byte, secret string) bool {
	if sig := r.Header.Get("X-Hub-Signature-256"); sig != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(expected))
	}
	if tok := r.Header.Get("X-Gitlab-Token"); tok != "" {
		return subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1
	}
	if tok := r.URL.Query().Get("token"); tok != "" {
		return subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1
	}
	return false
}

// pushPayloadRef lee el campo "ref" de un payload de push de GitHub/GitLab
// (mismo nombre en ambos), sin exigir el resto del esquema.
func pushPayloadRef(body []byte) string {
	var p struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return ""
	}
	return p.Ref
}
