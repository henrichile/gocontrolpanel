package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
)

// authorizeAccountID resuelve y autoriza la cuenta de la URL; es el mismo
// chequeo que ya usa el explorador de archivos, factorizado aquí para no
// repetirlo en cada handler de backups.
func (s *Server) authorizeAccountID(w http.ResponseWriter, r *http.Request) (*models.Account, bool) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return nil, false
	}
	id := auth.MustIdentity(r.Context())
	acct, err := s.authorizeAccount(r.Context(), id, accountID)
	if err != nil {
		writeStoreError(w, err)
		return nil, false
	}
	return acct, true
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.authorizeAccountID(w, r)
	if !ok {
		return
	}
	backups, err := s.svc.ListBackups(r.Context(), acct.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, map[string]any{"backups": backups})
}

// handleCreateBackup dispara un backup completo (archivos + bases de datos)
// de la cuenta, de forma síncrona: puede tardar según el tamaño del sitio.
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.authorizeAccountID(w, r)
	if !ok {
		return
	}
	if err := s.svc.RunAccountBackup(r.Context(), acct.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo completar el backup: "+err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "account.backup", TargetType: "account", TargetID: acct.ID.String(),
		IPAddress: httpx.ClientIP(r),
	})
	backups, err := s.svc.ListBackups(r.Context(), acct.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.Created(w, map[string]any{"backups": backups})
}

func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.authorizeAccountID(w, r)
	if !ok {
		return
	}
	full, err := s.svc.BackupPath(r.Context(), acct.ID, r.URL.Query().Get("name"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := os.Stat(full); err != nil {
		httpx.Error(w, http.StatusNotFound, "backup no encontrado")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(full)+`"`)
	http.ServeFile(w, r, full)
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.authorizeAccountID(w, r)
	if !ok {
		return
	}
	full, err := s.svc.BackupPath(r.Context(), acct.ID, r.URL.Query().Get("name"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.Remove(full); err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "backup no encontrado")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}
