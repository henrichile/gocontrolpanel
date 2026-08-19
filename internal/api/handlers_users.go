package api

import (
	"net/http"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	id := auth.MustIdentity(r.Context())
	users, err := s.st.ListUsers(r.Context(), scopeFor(id))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"users": users})
}

type createUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())

	role := models.Role(req.Role)
	if role == "" {
		role = models.RoleUser
	}
	if !role.Valid() {
		httpx.FieldError(w, "role", "debe ser admin, reseller o user")
		return
	}
	// Nadie puede crear un usuario con más privilegios que él mismo.
	if !id.Role.AtLeast(role) || (id.Role == models.RoleReseller && role != models.RoleUser) {
		httpx.Error(w, http.StatusForbidden, "no puedes crear usuarios con ese rol")
		return
	}
	if req.Username == "" || req.Email == "" {
		httpx.Error(w, http.StatusBadRequest, "usuario y email son obligatorios")
		return
	}

	hash, err := auth.HashPassword(req.Password, s.cfg.BcryptCost)
	if err != nil {
		httpx.FieldError(w, "password", err.Error())
		return
	}

	user := &models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hash,
		FullName:     req.FullName,
		Role:         role,
		IsActive:     true,
	}
	if id.Role == models.RoleReseller {
		parent := id.UserID
		user.ParentID = &parent
	}
	if err := s.st.CreateUser(r.Context(), user); err != nil {
		writeStoreError(w, err)
		return
	}

	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "user.create", TargetType: "user", TargetID: user.ID.String(),
		Detail: map[string]any{"username": user.Username, "role": string(user.Role)},
		IPAddress: httpx.ClientIP(r),
	})
	httpx.Created(w, map[string]any{"user": user})
}

type updateUserRequest struct {
	Email    *string `json:"email"`
	FullName *string `json:"full_name"`
	Role     *string `json:"role"`
	IsActive *bool   `json:"is_active"`
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	userID, err := pathUUID(r, "userID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de usuario inválido")
		return
	}
	var req updateUserRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())

	target, err := s.st.GetUserByID(r.Context(), userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.canManageUser(id, target) {
		httpx.Error(w, http.StatusForbidden, "no puedes modificar este usuario")
		return
	}

	if req.Email != nil {
		target.Email = *req.Email
	}
	if req.FullName != nil {
		target.FullName = *req.FullName
	}
	if req.Role != nil {
		role := models.Role(*req.Role)
		if !role.Valid() || !id.Role.AtLeast(role) {
			httpx.FieldError(w, "role", "rol no permitido")
			return
		}
		target.Role = role
	}
	if req.IsActive != nil {
		// Un admin no puede desactivarse a sí mismo y quedarse fuera.
		if !*req.IsActive && target.ID == id.UserID {
			httpx.Error(w, http.StatusConflict, "no puedes desactivar tu propio usuario")
			return
		}
		target.IsActive = *req.IsActive
	}

	if err := s.st.UpdateUser(r.Context(), target); err != nil {
		writeStoreError(w, err)
		return
	}
	if req.IsActive != nil && !*req.IsActive {
		_ = s.st.RevokeUserSessions(r.Context(), target.ID)
	}
	httpx.OK(w, map[string]any{"user": target})
}

type resetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	userID, err := pathUUID(r, "userID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de usuario inválido")
		return
	}
	var req resetPasswordRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())
	target, err := s.st.GetUserByID(r.Context(), userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.canManageUser(id, target) {
		httpx.Error(w, http.StatusForbidden, "no puedes modificar este usuario")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword, s.cfg.BcryptCost)
	if err != nil {
		httpx.FieldError(w, "new_password", err.Error())
		return
	}
	if err := s.st.UpdatePassword(r.Context(), target.ID, hash); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = s.st.RevokeUserSessions(r.Context(), target.ID)

	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "user.password_reset", TargetType: "user", TargetID: target.ID.String(),
		IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID, err := pathUUID(r, "userID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de usuario inválido")
		return
	}
	id := auth.MustIdentity(r.Context())
	if userID == id.UserID {
		httpx.Error(w, http.StatusConflict, "no puedes eliminar tu propio usuario")
		return
	}
	target, err := s.st.GetUserByID(r.Context(), userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.canManageUser(id, target) {
		httpx.Error(w, http.StatusForbidden, "no puedes eliminar este usuario")
		return
	}
	if err := s.st.DeleteUser(r.Context(), userID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "user.delete", TargetType: "user", TargetID: userID.String(),
		IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

// canManageUser: el admin gestiona a todos; el reseller solo a sus hijos.
func (s *Server) canManageUser(actor auth.Identity, target *models.User) bool {
	if actor.Role == models.RoleAdmin {
		return true
	}
	if actor.Role == models.RoleReseller {
		return target.ParentID != nil && *target.ParentID == actor.UserID
	}
	return target.ID == actor.UserID
}
