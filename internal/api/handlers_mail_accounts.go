// Buzones de correo propios para dominios de clientes. No confundir con
// handlers_mail.go (SMTP/plantillas de correo saliente del propio panel).
package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

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
	md, err := s.st.GetMailDomainByDomain(r.Context(), domain)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, verifyMailDNS(domain, s.svc.Mail().Hostname(), md.DKIMSelector, md.DKIMValue))
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

// dnsCheck es el resultado de comprobar un registro DNS puntual: no busca
// validar criptografía ni sintaxis exhaustiva, solo si el registro ya
// propagó y contiene lo esperado — la señal que de verdad le sirve a un
// admin/cliente esperando que su DNS se actualice.
type dnsCheck struct {
	OK    bool   `json:"ok"`
	Found string `json:"found,omitempty"`
	Error string `json:"error,omitempty"`
}

// verifyMailDNS consulta el DNS público del dominio bajo demanda (sin poller
// de fondo) y reporta MX/SPF/DKIM/DMARC.
func verifyMailDNS(domain, expectedHostname, dkimSelector, dkimValue string) map[string]any {
	return map[string]any{
		"mx":    checkMX(domain, expectedHostname),
		"spf":   checkTXTPrefix(domain, "v=spf1", "SPF"),
		"dkim":  checkDKIM(domain, dkimSelector, dkimValue),
		"dmarc": checkTXTPrefix("_dmarc."+domain, "v=DMARC1", "DMARC"),
	}
}

func checkMX(domain, expectedHostname string) dnsCheck {
	records, err := net.LookupMX(domain)
	if err != nil {
		return dnsCheck{Error: "no se pudo resolver el MX: " + err.Error()}
	}
	expected := strings.TrimSuffix(expectedHostname, ".") + "."
	found := make([]string, 0, len(records))
	for _, mx := range records {
		found = append(found, mx.Host)
		if strings.EqualFold(mx.Host, expected) {
			return dnsCheck{OK: true, Found: mx.Host}
		}
	}
	return dnsCheck{Error: "ningún MX apunta a " + expectedHostname, Found: strings.Join(found, ", ")}
}

// checkTXTPrefix busca, entre los TXT del nombre dado, uno que empiece con
// prefix (p.ej. "v=spf1", "v=DMARC1") — es la comprobación de "propagó y
// tiene la forma correcta", no una validación semántica completa del registro.
func checkTXTPrefix(name, prefix, label string) dnsCheck {
	records, err := net.LookupTXT(name)
	if err != nil {
		return dnsCheck{Error: "no se pudo resolver el TXT de " + label + ": " + err.Error()}
	}
	for _, txt := range records {
		if strings.HasPrefix(txt, prefix) {
			return dnsCheck{OK: true, Found: txt}
		}
	}
	return dnsCheck{Error: label + " no encontrado en " + name, Found: strings.Join(records, " | ")}
}

func checkDKIM(domain, selector, expectedValue string) dnsCheck {
	if selector == "" || expectedValue == "" {
		return dnsCheck{Error: "el panel todavía no generó la clave DKIM para este dominio"}
	}
	name := selector + "._domainkey." + domain
	records, err := net.LookupTXT(name)
	if err != nil {
		return dnsCheck{Error: "no se pudo resolver el TXT de DKIM: " + err.Error()}
	}
	for _, txt := range records {
		// Los TXT largos suelen venir en fragmentos que el resolver concatena;
		// alcanza con que contenga el valor que generamos.
		if strings.Contains(txt, expectedValue) || strings.Contains(expectedValue, txt) {
			return dnsCheck{OK: true, Found: txt}
		}
	}
	return dnsCheck{Error: "el TXT de " + name + " no coincide con la clave generada", Found: strings.Join(records, " | ")}
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

// handleMailServerStatus expone el estado del servidor de correo en sí (no
// de un dominio de cliente): hostname configurado y si el PTR/rDNS de la IP
// pública del servidor coincide — precondición para que cualquier correo
// saliente no caiga directo a spam, independiente de lo que cada cliente
// publique en su propio DNS.
func (s *Server) handleMailServerStatus(w http.ResponseWriter, r *http.Request) {
	if s.svc.Mail() == nil {
		httpx.OK(w, map[string]any{"enabled": false})
		return
	}
	hostname := s.svc.Mail().Hostname()
	resp := map[string]any{"enabled": true, "hostname": hostname}

	ip, err := publicIP(r.Context())
	if err != nil {
		resp["ptr"] = dnsCheck{Error: "no se pudo determinar la IP pública del servidor: " + err.Error()}
		httpx.OK(w, resp)
		return
	}
	resp["public_ip"] = ip

	names, err := net.LookupAddr(ip)
	if err != nil {
		resp["ptr"] = dnsCheck{Error: "la IP " + ip + " no tiene PTR/rDNS configurado: " + err.Error()}
		httpx.OK(w, resp)
		return
	}
	expected := strings.TrimSuffix(hostname, ".") + "."
	for _, n := range names {
		if strings.EqualFold(n, expected) {
			resp["ptr"] = dnsCheck{OK: true, Found: n}
			httpx.OK(w, resp)
			return
		}
	}
	resp["ptr"] = dnsCheck{
		Error: "el PTR de " + ip + " no apunta a " + hostname,
		Found: strings.Join(names, ", "),
	}
	httpx.OK(w, resp)
}

// publicIP resuelve la IP pública del servidor consultando un servicio
// externo — mismo mecanismo que ya usa install.sh (comprobar_dns) para lo
// mismo, aquí en Go para poder exponerlo desde el panel bajo demanda.
func publicIP(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(raw))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("respuesta inesperada: %s", ip)
	}
	return ip, nil
}
