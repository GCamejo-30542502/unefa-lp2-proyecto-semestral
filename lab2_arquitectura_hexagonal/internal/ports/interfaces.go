package ports

import (
	"context"
	"errors"

	"lab2_arquitectura_hexagonal/internal/domain"
)

var ErrNoEncontrada = errors.New("transacción no encontrada")

// Puerto secundario: persistencia
type RepositorioTransacciones interface {
	BuscarPorIdempotencyKey(ctx context.Context, key string) (*domain.TransaccionRecarga, error)
	Guardar(ctx context.Context, t *domain.TransaccionRecarga) error
}

// Puerto secundario: banco / proveedor de pago
type PasarelaPagoBancaria interface {
	Cobrar(ctx context.Context, msisdn string, montoCentavos int64, idempotencyKey string) error
}

// Puerto secundario: auditoría
type NotificadorAuditoria interface {
	Registrar(ctx context.Context, evento string)
}
