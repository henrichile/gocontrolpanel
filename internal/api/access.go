package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/store"
)

var errForbidden = errors.New("sin permisos sobre este recurso")

// pathUUID extrae y valida un UUID de la ruta.
func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, key))
}

// scopeFor devuelve el filtro de propietario que corresponde al rol:
// nil para admin (ve todo), el propio ID para reseller y usuario.
func scopeFor(id auth.Identity) *uuid.UUID {
	if id.Role == models.RoleAdmin {
		return nil
	}
	uid := id.UserID
	return &uid
}

// authorizeAccount comprueba que la identidad puede operar sobre la cuenta.
func (s *Server) authorizeAccount(ctx context.Context, id auth.Identity,
	accountID uuid.UUID) (*models.Account, error) {

	acct, err := s.st.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if id.Role == models.RoleAdmin || acct.OwnerID == id.UserID {
		return acct, nil
	}
	if id.Role == models.RoleReseller {
		// Un reseller también gestiona las cuentas de sus usuarios hijos.
		owner, err := s.st.GetUserByID(ctx, acct.OwnerID)
		if err == nil && owner.ParentID != nil && *owner.ParentID == id.UserID {
			return acct, nil
		}
	}
	return nil, errForbidden
}

// authorizeSite resuelve el sitio y valida el acceso a su cuenta.
func (s *Server) authorizeSite(ctx context.Context, id auth.Identity,
	siteID uuid.UUID) (*models.Site, *models.Account, error) {

	site, err := s.st.GetSite(ctx, siteID)
	if err != nil {
		return nil, nil, err
	}
	acct, err := s.authorizeAccount(ctx, id, site.AccountID)
	if err != nil {
		return nil, nil, err
	}
	return site, acct, nil
}

// writeStoreError traduce los errores de la capa de datos a códigos HTTP.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "recurso no encontrado")
	case errors.Is(err, errForbidden):
		httpx.Error(w, http.StatusForbidden, "no tienes permisos sobre este recurso")
	case isUniqueViolation(err):
		httpx.Error(w, http.StatusConflict, "ya existe un recurso con esos datos")
	default:
		httpx.Error(w, http.StatusInternalServerError, "error interno: "+err.Error())
	}
}

// isUniqueViolation detecta el código 23505 de PostgreSQL sin acoplarnos al
// tipo concreto del driver.
func isUniqueViolation(err error) bool {
	type sqlStater interface{ SQLState() string }
	var st sqlStater
	if errors.As(err, &st) {
		return st.SQLState() == "23505"
	}
	return false
}
