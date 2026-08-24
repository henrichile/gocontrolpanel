package provision

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/etasoft/gocontrolpanel/internal/dockerx"
)

var (
	reMailboxAddress = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}@[a-z0-9.-]{1,253}$`)
)

// MailManager administra el contenedor de correo (docker-mailserver) por
// "docker exec" hacia su script setup.sh — a diferencia de MySQLManager
// (SQL directo) y SFTPManager (API REST propia), docker-mailserver no expone
// ninguna interfaz de administración salvo ese script dentro del contenedor.
type MailManager struct {
	docker    *dockerx.Manager
	container string
	hostname  string
}

// NewMailManager devuelve nil si el correo no está habilitado en este
// servidor (mismo patrón "opcional, degrada con gracia" que MySQL/SFTP).
func NewMailManager(dk *dockerx.Manager, enabled bool, container, hostname string) *MailManager {
	if !enabled || strings.TrimSpace(hostname) == "" {
		return nil
	}
	return &MailManager{docker: dk, container: container, hostname: hostname}
}

func (m *MailManager) Hostname() string {
	if m == nil {
		return ""
	}
	return m.hostname
}

func (m *MailManager) exec(ctx context.Context, cmd ...string) (string, error) {
	code, out, err := m.docker.ExecAs(ctx, m.container, cmd, "", nil)
	if err != nil {
		return "", fmt.Errorf("ejecutando en el contenedor de correo: %w", err)
	}
	if code != 0 {
		return "", fmt.Errorf("el contenedor de correo devolvió un error: %s", strings.TrimSpace(out))
	}
	return out, nil
}

// CreateMailbox da de alta un buzón nuevo. docker-mailserver crea el dominio
// implícitamente con el primer buzón que recibe — no hace falta un paso
// aparte, salvo EnableDomain para las claves DKIM (ver abajo).
func (m *MailManager) CreateMailbox(ctx context.Context, address, password string) error {
	if m == nil {
		return fmt.Errorf("el correo no está habilitado en este servidor")
	}
	if !reMailboxAddress.MatchString(address) {
		return fmt.Errorf("dirección de correo inválida: %s", address)
	}
	_, err := m.exec(ctx, "setup", "email", "add", address, password)
	return err
}

func (m *MailManager) DeleteMailbox(ctx context.Context, address string) error {
	if m == nil {
		return nil
	}
	if !reMailboxAddress.MatchString(address) {
		return fmt.Errorf("dirección de correo inválida: %s", address)
	}
	_, err := m.exec(ctx, "setup", "email", "del", address)
	return err
}

func (m *MailManager) ChangePassword(ctx context.Context, address, newPassword string) error {
	if m == nil {
		return fmt.Errorf("el correo no está habilitado en este servidor")
	}
	if !reMailboxAddress.MatchString(address) {
		return fmt.Errorf("dirección de correo inválida: %s", address)
	}
	_, err := m.exec(ctx, "setup", "email", "update", address, newPassword)
	return err
}

// DKIMRecord es el registro TXT que el cliente debe publicar en el DNS de su
// dominio para que el correo saliente de sus buzones firme DKIM.
type DKIMRecord struct {
	Selector string `json:"selector"`
	Name     string `json:"name"`  // p.ej. "mail._domainkey.dominio.cl"
	Value    string `json:"value"` // "v=DKIM1; k=rsa; p=..."
}

// MailDNSRecords son todos los registros que el cliente debe publicar en su
// proveedor de DNS externo para que el correo funcione — el panel no
// controla ese DNS, solo lo calcula y lo muestra.
type MailDNSRecords struct {
	MX    string     `json:"mx"`
	SPF   string     `json:"spf"`
	DKIM  DKIMRecord `json:"dkim"`
	DMARC string     `json:"dmarc"`
}

// EnableDomain genera (si no existían) las claves DKIM del dominio dentro del
// contenedor y devuelve el registro TXT a publicar.
func (m *MailManager) EnableDomain(ctx context.Context, domain string) (DKIMRecord, error) {
	if m == nil {
		return DKIMRecord{}, fmt.Errorf("el correo no está habilitado en este servidor")
	}
	if !reFQDN.MatchString(domain) {
		return DKIMRecord{}, fmt.Errorf("dominio inválido: %s", domain)
	}
	// Idempotente: si las claves ya existen, setup.sh no las regenera.
	if _, err := m.exec(ctx, "setup", "config", "dkim", "domain", domain); err != nil {
		return DKIMRecord{}, err
	}
	out, err := m.exec(ctx, "cat", "/tmp/docker-mailserver/opendkim/keys/"+domain+"/mail.txt")
	if err != nil {
		return DKIMRecord{}, fmt.Errorf("leyendo la clave DKIM generada: %w", err)
	}
	return parseDKIMRecord(domain, out)
}

// parseDKIMRecord interpreta el archivo que genera OpenDKIM, con forma:
//
//	mail._domainkey	IN	TXT	( "v=DKIM1; h=sha256; k=rsa; "
//		"p=MIGfMA0G...primerFragmento..."
//		"segundoFragmento..." )  ; ----- clave DKIM mail para dominio.cl
//
// Los fragmentos entre comillas hay que concatenarlos para reconstruir el
// valor completo del TXT.
func parseDKIMRecord(domain, raw string) (DKIMRecord, error) {
	start := strings.Index(raw, "(")
	end := strings.LastIndex(raw, ")")
	if start < 0 || end < 0 || end <= start {
		return DKIMRecord{}, fmt.Errorf("no se pudo interpretar la clave DKIM generada")
	}
	body := raw[start+1 : end]

	var sb strings.Builder
	inQuotes := false
	for _, r := range body {
		switch r {
		case '"':
			inQuotes = !inQuotes
		default:
			if inQuotes {
				sb.WriteRune(r)
			}
		}
	}
	value := strings.TrimSpace(sb.String())
	if value == "" {
		return DKIMRecord{}, fmt.Errorf("la clave DKIM generada vino vacía")
	}
	return DKIMRecord{
		Selector: "mail",
		Name:     "mail._domainkey." + domain,
		Value:    value,
	}, nil
}

// DNSRecords arma el conjunto completo de registros a publicar para un
// dominio: MX/SPF/DMARC son estáticos (no requieren tocar el contenedor), el
// DKIM debe venir de una llamada previa a EnableDomain.
func (m *MailManager) DNSRecords(domain string, dkim DKIMRecord) MailDNSRecords {
	hostname := ""
	if m != nil {
		hostname = m.hostname
	}
	return MailDNSRecords{
		MX:    "10 " + hostname + ".",
		SPF:   "v=spf1 mx a:" + hostname + " ~all",
		DKIM:  dkim,
		DMARC: "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + domain,
	}
}
