package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/provision"
)

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	id := auth.MustIdentity(r.Context())
	accounts, err := s.st.ListAccounts(r.Context(), scopeFor(id))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"accounts": accounts})
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	id := auth.MustIdentity(r.Context())
	acct, err := s.authorizeAccount(r.Context(), id, accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if plan, err := s.st.GetPlan(r.Context(), acct.PlanID); err == nil {
		acct.Plan = plan
	}
	sites, err := s.st.ListSites(r.Context(), &acct.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"account": acct, "sites": sites})
}

type createAccountRequest struct {
	OwnerID       string `json:"owner_id"`
	PlanID        string `json:"plan_id"`
	SystemUser    string `json:"system_user"`
	PrimaryDomain string `json:"primary_domain"`
	Notes         string `json:"notes"`
	Provision     bool   `json:"provision"`
	PHPVersion    string `json:"php_version"`
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())

	ownerID := id.UserID
	if req.OwnerID != "" {
		parsed, err := uuid.Parse(req.OwnerID)
		if err != nil {
			httpx.FieldError(w, "owner_id", "no es un UUID válido")
			return
		}
		// Un reseller solo puede asignar cuentas a sí mismo o a sus hijos.
		if id.Role != models.RoleAdmin && parsed != id.UserID {
			owner, err := s.st.GetUserByID(r.Context(), parsed)
			if err != nil || owner.ParentID == nil || *owner.ParentID != id.UserID {
				httpx.Error(w, http.StatusForbidden, "no puedes crear cuentas para ese usuario")
				return
			}
		}
		ownerID = parsed
	}

	var planID uuid.UUID
	if req.PlanID != "" {
		parsed, err := uuid.Parse(req.PlanID)
		if err != nil {
			httpx.FieldError(w, "plan_id", "no es un UUID válido")
			return
		}
		planID = parsed
	}

	acct, err := s.svc.CreateAccount(r.Context(), provision.CreateAccountInput{
		OwnerID:       ownerID,
		PlanID:        planID,
		SystemUser:    req.SystemUser,
		PrimaryDomain: req.PrimaryDomain,
		Notes:         req.Notes,
		Provision:     req.Provision,
		PHPVersion:    req.PHPVersion,
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
		Action: "account.create", TargetType: "account", TargetID: acct.ID.String(),
		Detail:    map[string]any{"system_user": acct.SystemUser, "domain": acct.PrimaryDomain},
		IPAddress: httpx.ClientIP(r),
	})
	httpx.Created(w, map[string]any{"account": acct})
}

type suspendRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleSuspendAccount(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	var req suspendRequest
	_ = httpx.Decode(w, r, &req)

	id := auth.MustIdentity(r.Context())
	if _, err := s.authorizeAccount(r.Context(), id, accountID); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.svc.SuspendAccount(r.Context(), accountID, req.Reason); err != nil {
		writeStoreError(w, err)
		return
	}
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "account.suspend", TargetType: "account", TargetID: accountID.String(),
		Detail: map[string]any{"reason": req.Reason}, IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

func (s *Server) handleUnsuspendAccount(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	id := auth.MustIdentity(r.Context())
	if _, err := s.authorizeAccount(r.Context(), id, accountID); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.svc.UnsuspendAccount(r.Context(), accountID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "account.unsuspend", TargetType: "account", TargetID: accountID.String(),
		IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

type changePlanRequest struct {
	PlanID string `json:"plan_id"`
}

func (s *Server) handleChangeAccountPlan(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	var req changePlanRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	planID, err := uuid.Parse(req.PlanID)
	if err != nil {
		httpx.FieldError(w, "plan_id", "no es un UUID válido")
		return
	}
	id := auth.MustIdentity(r.Context())
	if _, err := s.authorizeAccount(r.Context(), id, accountID); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.st.UpdateAccountPlan(r.Context(), accountID, planID); err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.NoContent(w)
}

func (s *Server) handleTerminateAccount(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	id := auth.MustIdentity(r.Context())
	if _, err := s.authorizeAccount(r.Context(), id, accountID); err != nil {
		writeStoreError(w, err)
		return
	}
	deleteFiles := r.URL.Query().Get("delete_files") == "true"
	if err := s.svc.TerminateAccount(r.Context(), accountID, deleteFiles); err != nil {
		writeStoreError(w, err)
		return
	}
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "account.terminate", TargetType: "account", TargetID: accountID.String(),
		Detail: map[string]any{"delete_files": deleteFiles}, IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

// --- Planes ----------------------------------------------------------------

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.st.ListPlans(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"plans": plans})
}

func (s *Server) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	var p models.Plan
	if err := httpx.Decode(w, r, &p); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.Name == "" {
		httpx.FieldError(w, "name", "es obligatorio")
		return
	}
	if len(p.PHPVersions) == 0 {
		p.PHPVersions = []string{"8.3", "8.4"}
	}
	if err := s.st.CreatePlan(r.Context(), &p); err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.Created(w, map[string]any{"plan": p})
}

func (s *Server) handleUpdatePlan(w http.ResponseWriter, r *http.Request) {
	planID, err := pathUUID(r, "planID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de plan inválido")
		return
	}
	var p models.Plan
	if err := httpx.Decode(w, r, &p); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	p.ID = planID
	if err := s.st.UpdatePlan(r.Context(), &p); err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"plan": p})
}

func (s *Server) handleDeletePlan(w http.ResponseWriter, r *http.Request) {
	planID, err := pathUUID(r, "planID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de plan inválido")
		return
	}
	if err := s.st.DeletePlan(r.Context(), planID); err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.NoContent(w)
}
