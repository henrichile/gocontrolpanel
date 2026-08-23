package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/etasoft/gocontrolpanel/internal/models"
)

// corazaLogLine es el subconjunto de campos que nos importa del log JSON de
// Caddy — Coraza registra cada bloqueo con
// logger.Error("WAF rule violation detected", zap.String("hostname",...),
// zap.String("uri",...), zap.String("client_ip",...), zap.String("unique_id",...))
// (confirmado contra el código fuente de corazawaf/coraza-caddy, no
// adivinado). El resto de los campos del log no nos interesan acá, pero se
// guarda la línea completa igual (ver RecordWAFBlock) por si hace falta algo
// más adelante.
type corazaLogLine struct {
	Msg      string `json:"msg"`
	Hostname string `json:"hostname"`
	URI      string `json:"uri"`
	ClientIP string `json:"client_ip"`
	UniqueID string `json:"unique_id"`
}

const corazaBlockMsg = "WAF rule violation detected"

// wafLogLoop sigue el log del contenedor de borde y guarda cada bloqueo del
// WAF en la base de datos. Se reconecta solo si el stream se corta (por
// ejemplo, si el contenedor se reinicia al aplicar una config nueva).
func (r *Runner) wafLogLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := r.followEdgeLogs(ctx); err != nil {
			slog.Warn("waf: se cortó el seguimiento del log de borde, reintentando", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (r *Runner) followEdgeLogs(ctx context.Context) error {
	rc, err := r.svc.Docker().Logs(ctx, r.cfg.EdgeContainerName, 0, true)
	if err != nil {
		return err
	}
	defer rc.Close()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := stripDockerLogFrame(sc.Bytes())
		var entry corazaLogLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // línea que no es JSON (banners de arranque, etc.): se ignora
		}
		if entry.Msg != corazaBlockMsg {
			continue
		}
		block := &models.WAFBlock{
			ClientIP: entry.ClientIP, Hostname: entry.Hostname,
			URI: entry.URI, UniqueID: entry.UniqueID, RawJSON: string(line),
		}
		if err := r.st.RecordWAFBlock(ctx, block); err != nil {
			slog.Warn("waf: no se pudo guardar el bloqueo", "error", err)
		}
	}
	return sc.Err()
}

// stripDockerLogFrame quita el framing de 8 bytes que antepone el daemon de
// Docker a cada línea de log cuando el contenedor no usa TTY (mismo formato
// que ya maneja stripFrame en internal/api/handlers_sites.go).
func stripDockerLogFrame(b []byte) []byte {
	if len(b) >= 8 && (b[0] == 1 || b[0] == 2) && b[1] == 0 {
		return b[8:]
	}
	return b
}
