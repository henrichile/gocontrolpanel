// Package caddyapi habla con la Admin API del Caddy de borde para publicar
// la configuración de enrutado y TLS sin reiniciar el proceso.
package caddyapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Ping comprueba que la Admin API responde.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/config/", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("caddy admin no responde en %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("caddy admin devolvió %s", resp.Status)
	}
	return nil
}

// Load reemplaza la configuración completa de Caddy (POST /load).
func (c *Client) Load(ctx context.Context, cfg *Config) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("serializando config de caddy: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/load",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("enviando config a caddy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("caddy rechazó la config (%s): %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// CurrentConfig devuelve la configuración activa, útil para diagnóstico.
func (c *Client) CurrentConfig(ctx context.Context) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/config/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("caddy devolvió %s: %s", resp.Status, string(raw))
	}
	return raw, nil
}
