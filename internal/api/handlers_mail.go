package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/mailer"
	"github.com/etasoft/gocontrolpanel/internal/models"
)

// --- Configuración SMTP ------------------------------------------------------

// smtpSettingsResponse nunca incluye la contraseña en claro — solo si hay una
// guardada, para que el frontend decida si mostrar el campo vacío o "sin cambios".
type smtpSettingsResponse struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"password_set"`
	FromEmail   string `json:"from_email"`
	FromName    string `json:"from_name"`
	Encryption  string `json:"encryption"`
	Enabled     bool   `json:"enabled"`
}

func (s *Server) handleGetSMTPSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.st.GetSMTPSettings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	password, err := s.st.GetSMTPPassword(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, smtpSettingsResponse{
		Host: settings.Host, Port: settings.Port, Username: settings.Username,
		PasswordSet: password != "",
		FromEmail:   settings.FromEmail, FromName: settings.FromName,
		Encryption: settings.Encryption, Enabled: settings.Enabled,
	})
}

type updateSMTPSettingsRequest struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password"` // vacío = no cambiar la ya guardada
	FromEmail  string `json:"from_email"`
	FromName   string `json:"from_name"`
	Encryption string `json:"encryption"`
	Enabled    bool   `json:"enabled"`
}

func (s *Server) handleUpdateSMTPSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSMTPSettingsRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		httpx.FieldError(w, "port", "debe ser un puerto válido")
		return
	}
	switch req.Encryption {
	case "none", "starttls", "ssl":
	default:
		httpx.FieldError(w, "encryption", "debe ser 'none', 'starttls' o 'ssl'")
		return
	}

	if err := s.st.UpdateSMTPSettings(r.Context(), models.SMTPSettings{
		Host: req.Host, Port: req.Port, Username: req.Username,
		FromEmail: req.FromEmail, FromName: req.FromName,
		Encryption: req.Encryption, Enabled: req.Enabled,
	}, req.Password, req.Password != ""); err != nil {
		writeStoreError(w, err)
		return
	}

	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "system.smtp_updated", IPAddress: httpx.ClientIP(r),
	})

	s.handleGetSMTPSettings(w, r)
}

type testSMTPRequest struct {
	To string `json:"to"`
}

func (s *Server) handleTestSMTP(w http.ResponseWriter, r *http.Request) {
	var req testSMTPRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.To == "" {
		httpx.FieldError(w, "to", "obligatorio")
		return
	}

	settings, err := s.st.GetSMTPSettings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	password, err := s.st.GetSMTPPassword(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	client := mailer.New(mailer.SMTPConfig{
		Host: settings.Host, Port: settings.Port, Username: settings.Username, Password: password,
		FromEmail: settings.FromEmail, FromName: settings.FromName,
		Encryption: settings.Encryption, Enabled: settings.Enabled,
	})
	if err := client.Send(r.Context(), req.To,
		"Correo de prueba — GoControlPanel",
		"<p>Este es un correo de prueba de la configuración SMTP de tu panel.</p>"); err != nil {
		httpx.Error(w, http.StatusBadGateway, "no se pudo enviar el correo de prueba: "+err.Error())
		return
	}
	httpx.OK(w, map[string]any{"sent": true})
}

// --- Plantillas de email -----------------------------------------------------

func (s *Server) handleListEmailTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.st.ListEmailTemplates(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"templates": templates})
}

func (s *Server) handleGetEmailTemplate(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	tpl, err := s.st.GetEmailTemplate(r.Context(), key)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, tpl)
}

type updateEmailTemplateRequest struct {
	Subject  string `json:"subject"`
	BodyHTML string `json:"body_html"`
}

func (s *Server) handleUpdateEmailTemplate(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	var req updateEmailTemplateRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Subject == "" {
		httpx.FieldError(w, "subject", "obligatorio")
		return
	}
	if req.BodyHTML == "" {
		httpx.FieldError(w, "body_html", "obligatorio")
		return
	}
	if _, _, err := mailer.RenderTemplate(req.Subject, req.BodyHTML, mailer.WelcomeData{}); err != nil {
		httpx.FieldError(w, "body_html", "la plantilla no es válida: "+err.Error())
		return
	}

	if err := s.st.UpdateEmailTemplate(r.Context(), key, req.Subject, req.BodyHTML); err != nil {
		writeStoreError(w, err)
		return
	}

	id := auth.MustIdentity(r.Context())
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "system.email_template_updated", TargetType: "email_template", TargetID: key,
		IPAddress: httpx.ClientIP(r),
	})

	s.handleGetEmailTemplate(w, r)
}
