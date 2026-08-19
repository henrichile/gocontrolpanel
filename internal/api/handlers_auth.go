package api

import (
	"net/http"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
)

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int          `json:"expires_in"`
	User         *models.User `json:"user"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Login == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "usuario y contraseña son obligatorios")
		return
	}

	user, err := s.st.GetUserByLogin(r.Context(), req.Login)
	if err != nil || !auth.VerifyPassword(user.PasswordHash, req.Password) {
		// Mensaje idéntico en ambos casos: no revelamos si el usuario existe.
		httpx.Error(w, http.StatusUnauthorized, "usuario o contraseña incorrectos")
		return
	}
	if !user.IsActive {
		httpx.Error(w, http.StatusForbidden, "la cuenta está desactivada")
		return
	}

	access, refresh, err := s.issueTokens(r, user)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo iniciar sesión")
		return
	}

	_ = s.st.TouchLogin(r.Context(), user.ID)
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &user.ID, ActorUsername: user.Username,
		Action: "auth.login", IPAddress: httpx.ClientIP(r),
	})

	httpx.OK(w, tokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(s.tokens.AccessTTL().Seconds()),
		User:         user,
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RefreshToken == "" {
		httpx.Error(w, http.StatusBadRequest, "falta el refresh token")
		return
	}

	// El refresh token se consume: cada renovación emite uno nuevo.
	userID, err := s.st.ConsumeSession(r.Context(), auth.HashRefresh(req.RefreshToken))
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "refresh token inválido o expirado")
		return
	}
	user, err := s.st.GetUserByID(r.Context(), userID)
	if err != nil || !user.IsActive {
		httpx.Error(w, http.StatusUnauthorized, "la sesión ya no es válida")
		return
	}

	access, refresh, err := s.issueTokens(r, user)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo renovar la sesión")
		return
	}
	httpx.OK(w, tokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(s.tokens.AccessTTL().Seconds()),
		User:         user,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	id := auth.MustIdentity(r.Context())
	if err := s.st.RevokeUserSessions(r.Context(), id.UserID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo cerrar la sesión")
		return
	}
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "auth.logout", IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id := auth.MustIdentity(r.Context())
	user, err := s.st.GetUserByID(r.Context(), id.UserID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "usuario no encontrado")
		return
	}
	httpx.OK(w, user)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())
	user, err := s.st.GetUserByID(r.Context(), id.UserID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "usuario no encontrado")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, req.CurrentPassword) {
		httpx.Error(w, http.StatusForbidden, "la contraseña actual no es correcta")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword, s.cfg.BcryptCost)
	if err != nil {
		httpx.FieldError(w, "new_password", err.Error())
		return
	}
	if err := s.st.UpdatePassword(r.Context(), user.ID, hash); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no se pudo actualizar la contraseña")
		return
	}
	// Cambiar la contraseña invalida el resto de sesiones.
	_ = s.st.RevokeUserSessions(r.Context(), user.ID)
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "auth.password_changed", IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

func (s *Server) issueTokens(r *http.Request, user *models.User) (string, string, error) {
	access, err := s.tokens.IssueAccess(user)
	if err != nil {
		return "", "", err
	}
	plain, hashed, err := auth.NewRefreshToken()
	if err != nil {
		return "", "", err
	}
	err = s.st.CreateSession(r.Context(), user.ID, hashed,
		r.UserAgent(), httpx.ClientIP(r), s.tokens.RefreshTTL())
	if err != nil {
		return "", "", err
	}
	return access, plain, nil
}
