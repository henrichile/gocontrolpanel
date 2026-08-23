-- Registro de bloqueos del WAF (Coraza), capturados por el worker que sigue
-- el log del contenedor de borde. raw_json guarda la línea completa tal
-- cual, para no perder nada aunque el parseo de campos individuales no
-- cubra algo.
CREATE TABLE waf_blocks (
    id          BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    client_ip   TEXT NOT NULL DEFAULT '',
    hostname    TEXT NOT NULL DEFAULT '',
    uri         TEXT NOT NULL DEFAULT '',
    unique_id   TEXT NOT NULL DEFAULT '',
    raw_json    TEXT NOT NULL
);
CREATE INDEX idx_waf_blocks_occurred ON waf_blocks(occurred_at DESC);
