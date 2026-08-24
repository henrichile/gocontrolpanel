// Buzones de correo propios para dominios de clientes. No confundir con
// handlers_mail.go (SMTP/plantillas de correo saliente del propio panel).
package api

import (
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/provision"
)

func normalizeMailDomain(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// --- Dominios de correo ------------------------------------------------------

func (s *Server) handleListMailDomains(w http.ResponseWriter, r *http.Request) {
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
	domains, err := s.st.ListMailDomains(r.Context(), accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"domains": domains})
}

func (s *Server) handleEnableMailDomain(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	domain := normalizeMailDomain(chi.URLParam(r, "domain"))
	id := auth.MustIdentity(r.Context())
	acct, err := s.authorizeAccount(r.Context(), id, accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.svc.Mail() == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "el panel no tiene correo habilitado")
		return
	}

	belongs, err := s.st.DomainBelongsToAccount(r.Context(), accountID, domain)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !belongs {
		httpx.FieldError(w, "domain", "ese dominio no pertenece a ningún sitio de esta cuenta")
		return
	}

	// Idempotente: si ya estaba habilitado, no vuelve a pegarle al
	// contenedor — solo devuelve los registros DNS ya conocidos (el DKIM
	// generado se guarda en la primera llamada, ver abajo).
	if existing, err := s.st.GetMailDomainByDomain(r.Context(), domain); err == nil {
		dkim := provision.DKIMRecord{Selector: existing.DKIMSelector, Name: existing.DKIMSelector + "._domainkey." + domain, Value: existing.DKIMValue}
		httpx.OK(w, map[string]any{"domain": existing, "records": s.svc.Mail().DNSRecords(domain, dkim)})
		return
	}

	dkim, err := s.svc.Mail().EnableDomain(r.Context(), domain)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "no se pudo habilitar el correo: "+err.Error())
		return
	}

	md := &models.MailDomain{
		AccountID: acct.ID, Domain: domain,
		DKIMSelector: dkim.Selector, DKIMValue: dkim.Value,
	}
	if err := s.st.CreateMailDomain(r.Context(), md); err != nil {
		writeStoreError(w, err)
		return
	}

	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "mail_domain.enable", TargetType: "mail_domain", TargetID: md.ID.String(),
		Detail: map[string]any{"domain": domain}, IPAddress: httpx.ClientIP(r),
	})

	httpx.Created(w, map[string]any{
		"domain":  md,
		"records": s.svc.Mail().DNSRecords(domain, dkim),
	})
}

func (s *Server) handleVerifyMailDomain(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	domain := normalizeMailDomain(chi.URLParam(r, "domain"))
	id := auth.MustIdentity(r.Context())
	if _, err := s.authorizeAccount(r.Context(), id, accountID); err != nil {
		writeStoreError(w, err)
		return
	}
	if s.svc.Mail() == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "el panel no tiene correo habilitado")
		return
	}
	httpx.OK(w, verifyMailDNS(domain, s.svc.Mail().Hostname()))
}

// --- Buzones -----------------------------------------------------------------

func (s *Server) handleListMailboxes(w http.ResponseWriter, r *http.Request) {
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
	boxes, err := s.st.ListMailboxes(r.Context(), accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"mailboxes": boxes})
}

type createMailboxRequest struct {
	MailDomainID string `json:"mail_domain_id"`
	LocalPart    string `json:"local_part"`
	Password     string `json:"password"`
	QuotaMB      int64  `json:"quota_mb"`
}

func (s *Server) handleCreateMailbox(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	var req createMailboxRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())
	acct, err := s.authorizeAccount(r.Context(), id, accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.svc.Mail() == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "el panel no tiene correo habilitado")
		return
	}

	mailDomainID, err := uuid.Parse(req.MailDomainID)
	if err != nil {
		httpx.FieldError(w, "mail_domain_id", "no es un UUID válido")
		return
	}
	domains, err := s.st.ListMailDomains(r.Context(), accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var domain string
	for _, d := range domains {
		if d.ID == mailDomainID {
			domain = d.Domain
			break
		}
	}
	if domain == "" {
		httpx.FieldError(w, "mail_domain_id", "ese dominio no tiene correo habilitado en esta cuenta")
		return
	}

	localPart := strings.ToLower(strings.TrimSpace(req.LocalPart))
	if localPart == "" {
		httpx.FieldError(w, "local_part", "es obligatorio")
		return
	}
	if req.Password == "" {
		httpx.FieldError(w, "password", "es obligatorio")
		return
	}

	plan, err := s.st.GetPlan(r.Context(), acct.PlanID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	count, err := s.st.CountAccountMailboxes(r.Context(), accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if count >= plan.MaxMailboxes {
		httpx.FieldError(w, "plan", "has alcanzado el límite de buzones de tu plan")
		return
	}

	address := localPart + "@" + domain
	if err := s.svc.Mail().CreateMailbox(r.Context(), address, req.Password); err != nil {
		httpx.Error(w, http.StatusBadGateway, "no se pudo crear el buzón: "+err.Error())
		return
	}

	quota := req.QuotaMB
	if quota <= 0 {
		quota = 1024
	}
	mb := &models.Mailbox{
		AccountID: acct.ID, MailDomainID: mailDomainID, LocalPart: localPart, QuotaMB: quota,
	}
	if err := s.st.CreateMailbox(r.Context(), mb); err != nil {
		_ = s.svc.Mail().DeleteMailbox(r.Context(), address)
		writeStoreError(w, err)
		return
	}
	mb.Domain = domain

	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "mailbox.create", TargetType: "mailbox", TargetID: mb.ID.String(),
		Detail: map[string]any{"address": address}, IPAddress: httpx.ClientIP(r),
	})
	httpx.Created(w, map[string]any{"mailbox": mb})
}

func (s *Server) handleDeleteMailbox(w http.ResponseWriter, r *http.Request) {
	mailboxID, err := pathUUID(r, "mailboxID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de buzón inválido")
		return
	}
	id := auth.MustIdentity(r.Context())
	mb, err := s.st.GetMailbox(r.Context(), mailboxID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := s.authorizeAccount(r.Context(), id, mb.AccountID); err != nil {
		writeStoreError(w, err)
		return
	}
	if s.svc.Mail() != nil {
		if err := s.svc.Mail().DeleteMailbox(r.Context(), mb.LocalPart+"@"+mb.Domain); err != nil {
			httpx.Error(w, http.StatusBadGateway, "no se pudo eliminar el buzón: "+err.Error())
			return
		}
	}
	if err := s.st.DeleteMailbox(r.Context(), mailboxID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "mailbox.delete", TargetType: "mailbox", TargetID: mailboxID.String(),
		IPAddress: httpx.ClientIP(r),
	})
	httpx.NoContent(w)
}

type changeMailboxPasswordRequest struct {
	Password string `json:"password"`
}

func (s *Server) handleChangeMailboxPassword(w http.ResponseWriter, r *http.Request) {
	mailboxID, err := pathUUID(r, "mailboxID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de buzón inválido")
		return
	}
	var req changeMailboxPasswordRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Password == "" {
		httpx.FieldError(w, "password", "es obligatorio")
		return
	}
	id := auth.MustIdentity(r.Context())
	mb, err := s.st.GetMailbox(r.Context(), mailboxID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := s.authorizeAccount(r.Context(), id, mb.AccountID); err != nil {
		writeStoreError(w, err)
		return
	}
	if s.svc.Mail() == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "el panel no tiene correo habilitado")
		return
	}
	if err := s.svc.Mail().ChangePassword(r.Context(), mb.LocalPart+"@"+mb.Domain, req.Password); err != nil {
		httpx.Error(w, http.StatusBadGateway, "no se pudo cambiar la contraseña: "+err.Error())
		return
	}
	httpx.NoContent(w)
}

// verifyMailDNS consulta el DNS público del dominio bajo demanda (sin poller
// de fondo) y reporta si el MX apunta al hostname esperado. No verifica
// SPF/DKIM/DMARC en detalle (bastaría con que el cliente los haya copiado tal
// cual) — el objetivo es la señal más simple y útil: "¿ya propagó el MX?".
func verifyMailDNS(domain, expectedHostname string) map[string]any {
	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		return map[string]any{"mx_ok": false, "error": "no se pudo resolver el MX: " + err.Error()}
	}
	expected := strings.TrimSuffix(expectedHostname, ".") + "."
	for _, mx := range mxRecords {
		if strings.EqualFold(mx.Host, expected) {
			return map[string]any{"mx_ok": true}
		}
	}
	found := make([]string, 0, len(mxRecords))
	for _, mx := range mxRecords {
		found = append(found, mx.Host)
	}
	return map[string]any{
		"mx_ok": false,
		"error": "el MX del dominio no apunta a " + expectedHostname,
		"found": found,
	}
}

// --- Info de correo del servidor (host de webmail, para el enlace del frontend)

func (s *Server) handleMailInfo(w http.ResponseWriter, r *http.Request) {
	if s.svc.Mail() == nil {
		httpx.OK(w, map[string]any{"enabled": false})
		return
	}
	httpx.OK(w, map[string]any{
		"enabled":      true,
		"webmail_host": provision.WebmailHost(s.cfg.PublicURL),
	})
}
