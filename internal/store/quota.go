package store

import (
	"context"

	"github.com/google/uuid"
)

// GetSiteNetBaseline devuelve la última lectura acumulada de tráfico de red
// de Docker para el sitio (ver AddAccountBandwidthUsage): permite calcular el
// delta de tráfico en el siguiente muestreo sin perder ni duplicar bytes.
func (s *Store) GetSiteNetBaseline(ctx context.Context, siteID uuid.UUID) (rxMB, txMB float64, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT last_net_rx_mb, last_net_tx_mb FROM sites WHERE id=$1`, siteID,
	).Scan(&rxMB, &txMB)
	return rxMB, txMB, err
}

func (s *Store) SetSiteNetBaseline(ctx context.Context, siteID uuid.UUID, rxMB, txMB float64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sites SET last_net_rx_mb=$2, last_net_tx_mb=$3 WHERE id=$1`, siteID, rxMB, txMB)
	return err
}
