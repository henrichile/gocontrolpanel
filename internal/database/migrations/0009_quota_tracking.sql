-- disk_quota_mb y bandwidth_quota_mb existían en el modelo pero nada los
-- aplicaba: disk_used_mb/bandwidth_used_mb nunca se actualizaban. Esto agrega
-- lo necesario para medirlos de verdad:
--   - bandwidth_reset_at: para reiniciar bandwidth_used_mb cada mes (Docker
--     solo entrega tráfico acumulado desde que arrancó el contenedor).
--   - last_net_rx_mb/last_net_tx_mb por sitio: última lectura acumulada de
--     Docker, para poder calcular el delta de tráfico en cada muestreo sin
--     perder ni duplicar bytes cuando el contenedor se reinicia.
ALTER TABLE accounts ADD COLUMN bandwidth_reset_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE sites ADD COLUMN last_net_rx_mb DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE sites ADD COLUMN last_net_tx_mb DOUBLE PRECISION NOT NULL DEFAULT 0;
