package adapters

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"lab2_arquitectura_hexagonal/internal/domain"
	"lab2_arquitectura_hexagonal/internal/ports"
)

// Comprobaciones en tiempo de compilación: si un adaptador deja de cumplir
// su puerto, Go avisa de inmediato.
var (
	_ ports.RepositorioTransacciones = (*RepoMemoria)(nil)
	_ ports.PasarelaPagoBancaria     = (*PasarelaSimulada)(nil)
	_ ports.NotificadorAuditoria     = AuditoriaConsola{}
)

// ---------- Repositorio en memoria (seguro para concurrencia) ----------

type RepoMemoria struct {
	mu   sync.RWMutex
	data map[string]*domain.TransaccionRecarga // clave: idempKey
}

func NuevoRepoMemoria() *RepoMemoria {
	return &RepoMemoria{data: make(map[string]*domain.TransaccionRecarga)}
}

func (r *RepoMemoria) BuscarPorIdempotencyKey(_ context.Context, idempKey string) (*domain.TransaccionRecarga, error) {
	r.mu.RLock() // lectura: varios hilos pueden leer a la vez
	defer r.mu.RUnlock()
	if t, ok := r.data[idempKey]; ok {
		return t, nil
	}
	return nil, ports.ErrNoEncontrada
}

func (r *RepoMemoria) Guardar(_ context.Context, t *domain.TransaccionRecarga) error {
	r.mu.Lock() // escritura: acceso exclusivo
	defer r.mu.Unlock()
	r.data[t.IdempKey()] = t
	return nil
}

// ---------- Pasarela bancaria simulada ----------

type PasarelaSimulada struct {
	cobros atomic.Int64 // contador seguro entre hilos
}

func (p *PasarelaSimulada) Cobrar(_ context.Context, msisdn string, montoCentavos int64, idempKey string) error {
	p.cobros.Add(1)
	fmt.Printf("   [BANCO] Cobro de %d centavos a %s (idempKey=%s)\n", montoCentavos, msisdn, idempKey)
	return nil
}

// TotalCobros permite a las pruebas verificar cuántas veces se cobró de verdad.
func (p *PasarelaSimulada) TotalCobros() int64 {
	return p.cobros.Load()
}

// ---------- Auditoría por consola ----------

type AuditoriaConsola struct{}

func (AuditoriaConsola) Registrar(_ context.Context, evento string) {
	fmt.Println("   [AUDITORIA]", evento)
}
