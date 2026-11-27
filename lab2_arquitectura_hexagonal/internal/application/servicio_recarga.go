package application

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"lab2_arquitectura_hexagonal/internal/domain"
	"lab2_arquitectura_hexagonal/internal/ports"
)

type ServicioRecarga struct {
	repo      ports.RepositorioTransacciones
	pasarela  ports.PasarelaPagoBancaria
	auditoria ports.NotificadorAuditoria
	locks     sync.Map // idempotencyKey -> *sync.Mutex
}

// Solo depende de las interfaces (Principio DIP)
func NuevoServicioRecarga(r ports.RepositorioTransacciones, p ports.PasarelaPagoBancaria, a ports.NotificadorAuditoria) *ServicioRecarga {
	return &ServicioRecarga{repo: r, pasarela: p, auditoria: a}
}

func (s *ServicioRecarga) bloquear(key string) func() {
	m, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *ServicioRecarga) EjecutarRecarga(ctx context.Context, id, idempotencyKey, msisdn string, monto int64) (*domain.TransaccionRecarga, error) {
	unlock := s.bloquear(idempotencyKey)
	defer unlock()

	// 1) Idempotencia: si ya se procesó, retornar la original sin debitar
	existente, err := s.repo.BuscarPorIdempotencyKey(ctx, idempotencyKey)
	if err == nil {
		s.auditoria.Registrar(ctx, "REINTENTO_IDEMPOTENTE key="+idempotencyKey)
		return existente, nil
	}
	if !errors.Is(err, ports.ErrNoEncontrada) {
		return nil, err
	}

	// 2) Validaciones de dominio
	tx, err := domain.NuevaTransaccionRecarga(id, idempotencyKey, msisdn, monto)
	if err != nil {
		s.auditoria.Registrar(ctx, "RECHAZO_VALIDACION: "+err.Error())
		return nil, err
	}

	// 3) Cobro (si falla por red, no se guarda: el cliente puede reintentar con la misma clave)
	if err := s.pasarela.Cobrar(ctx, msisdn, monto, idempotencyKey); err != nil {
		s.auditoria.Registrar(ctx, "ERROR_PASARELA: "+err.Error())
		return nil, fmt.Errorf("pasarela: %w", err)
	}
	if err := tx.Aprobar(); err != nil {
		return nil, err
	}
	if err := s.repo.Guardar(ctx, tx); err != nil {
		return nil, err
	}
	s.auditoria.Registrar(ctx, "RECARGA_APROBADA id="+tx.ID())
	return tx, nil
}
