package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SFTPManager crea y elimina usuarios virtuales en sftpgo (un servidor SFTP
// multi-tenant): cada cuenta de hosting recibe un usuario con su home
// encadenado (chroot) a su propia carpeta en el host, sin necesidad de un
// usuario Unix real ni de tocar /etc/passwd.
type SFTPManager struct {
	baseURL  string
	user     string
	password string
	http     *http.Client

	publicHost string
	publicPort int

	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time
}

// NewSFTPManager devuelve nil si no hay credenciales de administración
// configuradas (instalación sin servidor SFTP gestionado).
func NewSFTPManager(baseURL, user, password, publicHost string, publicPort int) *SFTPManager {
	if strings.TrimSpace(user) == "" || strings.TrimSpace(password) == "" {
		return nil
	}
	return &SFTPManager{
		baseURL:    strings.TrimRight(baseURL, "/"),
		user:       user,
		password:   password,
		http:       &http.Client{Timeout: 15 * time.Second},
		publicHost: publicHost,
		publicPort: publicPort,
	}
}

func (m *SFTPManager) Host() string {
	if m == nil {
		return ""
	}
	return m.publicHost
}

func (m *SFTPManager) Port() int {
	if m == nil {
		return 0
	}
	return m.publicPort
}

// token obtiene (y cachea) el bearer token de administración de sftpgo. El
// formato exacto de expires_at (unix vs. fecha) varía entre versiones de la
// API, así que en vez de decodificarlo y confiar en él, se cachea por un
// margen corto y fijo — do() igualmente vuelve a pedir uno si el servidor
// responde 401, así que un token cacheado de más nunca deja una llamada
// colgada por credenciales vencidas.
const sftpTokenTTL = 5 * time.Minute

func (m *SFTPManager) token(ctx context.Context, force bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !force && m.cachedToken != "" && time.Now().Before(m.expiresAt) {
		return m.cachedToken, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+"/api/v2/token", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(m.user, m.password)

	resp, err := m.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("sftp admin no responde en %s: %w", m.baseURL, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("sftp admin rechazó la autenticación (%s): %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("respuesta de token inválida: %w", err)
	}
	m.cachedToken = out.AccessToken
	m.expiresAt = time.Now().Add(sftpTokenTTL)
	return m.cachedToken, nil
}

func (m *SFTPManager) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	// Serializar una sola vez: si hay que reintentar tras un 401, el mismo
	// cuerpo debe volver a enviarse.
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = b
	}

	send := func(force bool) (*http.Response, error) {
		tok, err := m.token(ctx, force)
		if err != nil {
			return nil, err
		}
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return m.http.Do(req)
	}

	resp, err := send(false)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		resp, err = send(true)
		if err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// CreateUser da de alta un usuario SFTP encadenado a homeDir, con cuota
// opcional en MB (0 = sin límite propio; el disco de la cuenta ya se mide
// aparte). No falla si el usuario ya existe: sftpgo devuelve 409 y ese caso
// se trata como éxito para que reintentar sea seguro.
func (m *SFTPManager) CreateUser(ctx context.Context, username, password, homeDir string, quotaMB int64) error {
	if m == nil {
		return fmt.Errorf("no hay servidor SFTP configurado en el panel")
	}
	payload := map[string]any{
		"username":    username,
		"password":    password,
		"home_dir":    homeDir,
		"status":      1,
		"permissions": map[string][]string{"/": {"*"}},
		"quota_size":  quotaMB * 1024 * 1024,
		"filesystem":  map[string]any{"provider": 0},
	}
	resp, err := m.do(ctx, http.MethodPost, "/api/v2/users", payload)
	if err != nil {
		return fmt.Errorf("creando el usuario SFTP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sftp admin rechazó la creación (%s): %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}

// DeleteUser da de baja un usuario SFTP. Un 404 se trata como éxito: el
// objetivo (que no exista) ya está cumplido.
func (m *SFTPManager) DeleteUser(ctx context.Context, username string) error {
	if m == nil {
		return nil
	}
	resp, err := m.do(ctx, http.MethodDelete, "/api/v2/users/"+username, nil)
	if err != nil {
		return fmt.Errorf("eliminando el usuario SFTP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sftp admin rechazó la baja (%s): %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}
