// Package hostctl deja que el panel toque el firewall (ufw) del host que lo
// aloja, sin necesitar un contenedor privilegiado: se conecta por SSH con
// una clave dedicada cuyo authorized_keys en el host tiene un comando
// forzado (ver install.sh) — pase lo que se mande como "comando" de SSH, del
// otro lado siempre corre el mismo script con una lista blanca de acciones.
// Si el panel se ve comprometido, el atacante puede como mucho abrir/cerrar
// puertos; nunca obtiene una shell arbitraria del host.
package hostctl

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

var (
	ErrProtectedPort = fmt.Errorf("no se puede bloquear el puerto usado por SSH")
	ErrNotConfigured = fmt.Errorf("el acceso al firewall del host no está configurado")
)

type Client struct {
	addr          string
	config        *ssh.ClientConfig
	protectedPort int
}

// New arma el cliente a partir de la configuración cargada por config.Load.
// Devuelve (nil, ErrNotConfigured) si el servidor no tiene esto configurado
// (instalación previa a esta función, o admin que decidió no habilitarlo).
func New(host string, sshPort int, hostPubkeyLine, privateKeyPath string, protectedPort int) (*Client, error) {
	if host == "" || hostPubkeyLine == "" || privateKeyPath == "" {
		return nil, ErrNotConfigured
	}
	keyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("leyendo la clave privada de hostctl: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("clave privada de hostctl inválida: %w", err)
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(hostPubkeyLine))
	if err != nil {
		return nil, fmt.Errorf("clave pública del host inválida: %w", err)
	}
	return &Client{
		addr: net.JoinHostPort(host, fmt.Sprint(sshPort)),
		config: &ssh.ClientConfig{
			User:            "root",
			Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.FixedHostKey(hostKey),
			Timeout:         10 * time.Second,
		},
		protectedPort: protectedPort,
	}, nil
}

// run abre una sesión SSH nueva y corre "cmd" — el comando forzado del lado
// del host ignora lo que se le mande y ejecuta siempre el mismo script; se
// manda algo legible igual, para que quede claro en cualquier log de SSH.
func (c *Client) run(ctx context.Context, cmd string) (string, error) {
	conn, err := ssh.Dial("tcp", c.addr, c.config)
	if err != nil {
		return "", fmt.Errorf("conectando al host: %w", err)
	}
	defer conn.Close()

	sess, err := conn.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := sess.CombinedOutput(cmd)
		done <- result{string(out), err}
	}()

	select {
	case <-ctx.Done():
		_ = sess.Close()
		return "", ctx.Err()
	case r := <-done:
		return strings.TrimSpace(r.out), r.err
	}
}

// Status devuelve la salida cruda de "ufw status verbose" en el host.
func (c *Client) Status(ctx context.Context) (string, error) {
	return c.run(ctx, "status")
}

// SetRule abre ("allow") o cierra ("deny") un puerto. Nunca se manda un
// "deny" sobre el puerto protegido — ni siquiera se intenta, aunque el
// script del host también lo rechazaría.
func (c *Client) SetRule(ctx context.Context, action string, port int, proto string) (string, error) {
	if action != "allow" && action != "deny" {
		return "", fmt.Errorf("acción inválida: %q", action)
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("puerto inválido: %d", port)
	}
	proto = strings.ToLower(proto)
	if proto != "tcp" && proto != "udp" {
		return "", fmt.Errorf("protocolo inválido: %q", proto)
	}
	if action == "deny" && port == c.protectedPort {
		return "", ErrProtectedPort
	}
	return c.run(ctx, fmt.Sprintf("%s %d %s", action, port, proto))
}
