package worker

import (
	"bufio"
	"bytes"
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
		slog.Warn("waf: no se pudo abrir el log de borde", "container", r.cfg.EdgeContainerName, "error", err)
		return err
	}
	defer rc.Close()
	slog.Info("waf: siguiendo el log de borde", "container", r.cfg.EdgeContainerName)

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := stripDockerLogPrefix(sc.Bytes())
		var entry corazaLogLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // línea que no es JSON (banners de arranque, etc.): se ignora
		}
		if entry.Msg != corazaBlockMsg {
			continue
		}
		slog.Info("waf: bloqueo detectado", "hostname", entry.Hostname, "uri", entry.URI)
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

// stripDockerLogPrefix quita dos cosas que Docker antepone a cada línea:
// el framing de 8 bytes del multiplexado stdout/stderr (mismo formato que
// stripFrame en internal/api/handlers_sites.go), y el timestamp RFC3339Nano
// que antepone porque Logs() pide Timestamps:true (para mostrarlo en el
// visor de logs de un sitio) — acá solo estorba para parsear el JSON, así
// que se salta todo hasta el primer '{'.
func stripDockerLogPrefix(b []byte) []byte {
	if len(b) >= 8 && (b[0] == 1 || b[0] == 2) && b[1] == 0 {
		b = b[8:]
	}
	if i := bytes.IndexByte(b, '{'); i >= 0 {
		return b[i:]
	}
	return b
}
