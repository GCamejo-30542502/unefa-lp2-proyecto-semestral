package main

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"sync"
	"time"
)

 
// 1. Value Object inmutable: Dinero
 

// Dinero representa un importe financiero en centavos. Sus operaciones usan
// receptores por valor y devuelven una copia nueva: nunca mutan el original.
type Dinero struct {
	MontoCentavos int64
	Moneda        string
}

var (
	ErrMonedaVacia       = errors.New("la moneda es obligatoria")
	ErrMonedaDistinta    = errors.New("no se pueden operar montos de monedas distintas")
	ErrMontoNegativo     = errors.New("el monto no puede ser negativo")
	ErrSaldoInsuficiente = errors.New("saldo insuficiente: el resultado no puede ser negativo")
	ErrOverflowDinero    = errors.New("desbordamiento al operar el monto")
)

// NuevoDinero valida y construye el Value Object.
func NuevoDinero(montoCentavos int64, moneda string) (Dinero, error) {
	if moneda == "" {
		return Dinero{}, ErrMonedaVacia
	}
	if montoCentavos < 0 {
		return Dinero{}, ErrMontoNegativo
	}
	return Dinero{MontoCentavos: montoCentavos, Moneda: moneda}, nil
}

// Sumar devuelve un Dinero nuevo con la suma, sin mutar el receptor.
func (d Dinero) Sumar(otro Dinero) (Dinero, error) {
	if d.Moneda != otro.Moneda {
		return Dinero{}, ErrMonedaDistinta
	}
	if otro.MontoCentavos < 0 {
		return Dinero{}, ErrMontoNegativo
	}
	if d.MontoCentavos > math.MaxInt64-otro.MontoCentavos {
		return Dinero{}, ErrOverflowDinero
	}
	return Dinero{MontoCentavos: d.MontoCentavos + otro.MontoCentavos, Moneda: d.Moneda}, nil
}

// Restar devuelve un Dinero nuevo. Si el resultado seria negativo, falla.
func (d Dinero) Restar(otro Dinero) (Dinero, error) {
	if d.Moneda != otro.Moneda {
		return Dinero{}, ErrMonedaDistinta
	}
	if otro.MontoCentavos < 0 {
		return Dinero{}, ErrMontoNegativo
	}
	if otro.MontoCentavos > d.MontoCentavos {
		return Dinero{}, ErrSaldoInsuficiente
	}
	return Dinero{MontoCentavos: d.MontoCentavos - otro.MontoCentavos, Moneda: d.Moneda}, nil
}

func (d Dinero) String() string {
	return fmt.Sprintf("%d centavos %s", d.MontoCentavos, d.Moneda)
}

 
// 2. Agregado DDD: Suscripcion5G y evento CargoAplicadoEvent
 

type EstadoSuscripcion string

const (
	SuscripcionActiva     EstadoSuscripcion = "ACTIVA"
	SuscripcionSuspendida EstadoSuscripcion = "SUSPENDIDA"
)

var (
	ErrSuscripcionNoActiva = errors.New("la suscripcion no esta activa")
	ErrIdempotencyKeyVacia = errors.New("la llave de idempotencia es obligatoria")
	ErrMSISDNInvalido      = errors.New("el msisdn viola el estandar ITU-T E.164")
)

var patronE164 = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

// CargoAplicadoEvent es un evento de dominio inmutable: se devuelve por valor
// y el agregado conserva una copia interna.
type CargoAplicadoEvent struct {
	EventID        string
	SuscripcionID  string
	MSISDN         string
	Monto          Dinero
	IdempotencyKey string
	OcurridoEn     time.Time
}

// Suscripcion5G es la raiz de agregado. Encapsula su estado para que ninguna
// operacion externa pueda violar las invariantes del dominio.
type Suscripcion5G struct {
	mu                 sync.Mutex
	id                 string
	msisdn             string
	saldo              Dinero
	estado             EstadoSuscripcion
	eventos            []CargoAplicadoEvent
	cargosPorClave     map[string]CargoAplicadoEvent
	reembolsosPorClave map[string]bool
}

// NuevaSuscripcion5G valida las invariantes de construccion.
func NuevaSuscripcion5G(id, msisdn string, saldoInicial Dinero) (*Suscripcion5G, error) {
	if id == "" {
		return nil, errors.New("el id de la suscripcion es obligatorio")
	}
	if !patronE164.MatchString(msisdn) {
		return nil, ErrMSISDNInvalido
	}
	if saldoInicial.Moneda == "" {
		return nil, ErrMonedaVacia
	}
	if saldoInicial.MontoCentavos < 0 {
		return nil, ErrSaldoInsuficiente
	}
	return &Suscripcion5G{
		id:                 id,
		msisdn:             msisdn,
		saldo:              saldoInicial,
		estado:             SuscripcionActiva,
		cargosPorClave:     make(map[string]CargoAplicadoEvent),
		reembolsosPorClave: make(map[string]bool),
	}, nil
}

// AplicarCargo debita la billetera y emite un CargoAplicadoEvent. La llave de
// idempotencia evita debitar dos veces la misma operacion.
func (s *Suscripcion5G) AplicarCargo(monto Dinero, idempotencyKey string) (CargoAplicadoEvent, error) {
	if idempotencyKey == "" {
		return CargoAplicadoEvent{}, ErrIdempotencyKeyVacia
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if evento, existe := s.cargosPorClave[idempotencyKey]; existe {
		return evento, nil
	}
	if s.estado != SuscripcionActiva {
		return CargoAplicadoEvent{}, ErrSuscripcionNoActiva
	}

	nuevoSaldo, err := s.saldo.Restar(monto)
	if err != nil {
		return CargoAplicadoEvent{}, err
	}

	s.saldo = nuevoSaldo
	evento := CargoAplicadoEvent{
		EventID:        fmt.Sprintf("evt-%s-%d", s.id, len(s.eventos)+1),
		SuscripcionID:  s.id,
		MSISDN:         s.msisdn,
		Monto:          monto,
		IdempotencyKey: idempotencyKey,
		OcurridoEn:     time.Now(),
	}
	s.eventos = append(s.eventos, evento)
	s.cargosPorClave[idempotencyKey] = evento
	return evento, nil
}

// Reembolsar compensa un cargo. Tambien es idempotente por llave.
func (s *Suscripcion5G) Reembolsar(monto Dinero, idempotencyKey string) (Dinero, error) {
	if idempotencyKey == "" {
		return Dinero{}, ErrIdempotencyKeyVacia
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.reembolsosPorClave[idempotencyKey] {
		return s.saldo, nil
	}

	nuevoSaldo, err := s.saldo.Sumar(monto)
	if err != nil {
		return Dinero{}, err
	}
	s.saldo = nuevoSaldo
	s.reembolsosPorClave[idempotencyKey] = true
	return s.saldo, nil
}

func (s *Suscripcion5G) ID() string {
	return s.id
}

func (s *Suscripcion5G) MSISDN() string {
	return s.msisdn
}

func (s *Suscripcion5G) Estado() EstadoSuscripcion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.estado
}

func (s *Suscripcion5G) SaldoActual() Dinero {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saldo
}

// Eventos devuelve una copia del historial para preservar la inmutabilidad.
func (s *Suscripcion5G) Eventos() []CargoAplicadoEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	copia := make([]CargoAplicadoEvent, len(s.eventos))
	copy(copia, s.eventos)
	return copia
}

 
// 3. Circuit Breaker thread-safe con tres estados
 

type EstadoCircuito string

const (
	CircuitoClosed   EstadoCircuito = "CLOSED"
	CircuitoOpen     EstadoCircuito = "OPEN"
	CircuitoHalfOpen EstadoCircuito = "HALF-OPEN"
)

var ErrCircuitoAbierto = errors.New("circuit breaker abierto: llamada rechazada")

type CircuitBreaker struct {
	mu             sync.Mutex
	estado         EstadoCircuito
	fallos         int
	umbralFallos   int
	timeoutAbierto time.Duration
	abiertoHasta   time.Time
	pruebaEnCurso  bool
}

func NuevoCircuitBreaker(umbralFallos int, timeoutAbierto time.Duration) *CircuitBreaker {
	if umbralFallos < 1 {
		umbralFallos = 1
	}
	return &CircuitBreaker{
		estado:         CircuitoClosed,
		umbralFallos:   umbralFallos,
		timeoutAbierto: timeoutAbierto,
	}
}

func (cb *CircuitBreaker) Estado() EstadoCircuito {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.estado
}

// Ejecutar aplica la politica del automata alrededor de una operacion.
func (cb *CircuitBreaker) Ejecutar(operacion func() error) error {
	if err := cb.adquirirPermiso(); err != nil {
		return err
	}
	err := operacion()
	cb.registrarResultado(err)
	return err
}

func (cb *CircuitBreaker) adquirirPermiso() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.estado {
	case CircuitoOpen:
		if time.Now().Before(cb.abiertoHasta) {
			return ErrCircuitoAbierto
		}
		// El timeout vencio: se permite una unica prueba.
		cb.estado = CircuitoHalfOpen
		cb.pruebaEnCurso = true
		return nil
	case CircuitoHalfOpen:
		if cb.pruebaEnCurso {
			return ErrCircuitoAbierto
		}
		cb.pruebaEnCurso = true
		return nil
	default:
		return nil
	}
}

func (cb *CircuitBreaker) registrarResultado(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err == nil {
		cb.fallos = 0
		cb.estado = CircuitoClosed
		cb.pruebaEnCurso = false
		return
	}

	cb.fallos++
	if cb.estado == CircuitoHalfOpen || cb.fallos >= cb.umbralFallos {
		cb.estado = CircuitoOpen
		cb.abiertoHasta = time.Now().Add(cb.timeoutAbierto)
	}
	cb.pruebaEnCurso = false
}

 
// 4. Exponential Backoff + Full Jitter
 

const (
	backoffBase = 100 * time.Millisecond
	backoffMax  = 5 * time.Second
)

// topeBackoff calcula min(Tmax, Tbase * 2^k) sin desbordar.
func topeBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	tope := backoffBase
	for i := 0; i < attempt; i++ {
		if tope >= backoffMax {
			return backoffMax
		}
		tope *= 2
	}
	if tope > backoffMax {
		return backoffMax
	}
	return tope
}

// BackoffFullJitter aplica T = Uniform(0, min(Tmax, Tbase * 2^k)), con k =
// attempt. El resultado siempre cae en [0, tope).
func BackoffFullJitter(attempt int) time.Duration {
	tope := topeBackoff(attempt)
	if tope <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(tope)))
}

 
// 5. Idempotencia y servicios simulados de la Saga
 

// RegistroIdempotencia garantiza que una operacion se ejecute una sola vez.
type RegistroIdempotencia struct {
	mu          sync.Mutex
	operaciones map[string]bool
}

func NuevoRegistroIdempotencia() *RegistroIdempotencia {
	return &RegistroIdempotencia{operaciones: make(map[string]bool)}
}

func (r *RegistroIdempotencia) Ejecutar(llave string, operacion func() error) error {
	r.mu.Lock()
	if r.operaciones[llave] {
		r.mu.Unlock()
		return nil
	}
	r.operaciones[llave] = true
	r.mu.Unlock()

	if err := operacion(); err != nil {
		r.mu.Lock()
		delete(r.operaciones, llave)
		r.mu.Unlock()
		return err
	}
	return nil
}

// ProveedorQoS simula el aprovisionamiento del perfil 5G.
type ProveedorQoS struct {
	mu             sync.Mutex
	aprovisionadas map[string]bool
}

func NuevoProveedorQoS() *ProveedorQoS {
	return &ProveedorQoS{aprovisionadas: make(map[string]bool)}
}

func (p *ProveedorQoS) Aprovisionar(llave string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.aprovisionadas[llave] = true
	return nil
}

func (p *ProveedorQoS) Desaprovisionar(llave string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.aprovisionadas, llave)
	return nil
}

func (p *ProveedorQoS) EstaAprovisionada(llave string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.aprovisionadas[llave]
}

// Facturador simula la emision de la factura del paso T3.
type Facturador struct {
	mu       sync.Mutex
	facturas map[string]string
}

func NuevoFacturador() *Facturador {
	return &Facturador{facturas: make(map[string]string)}
}

func (f *Facturador) Emitir(llave, suscripcionID string, monto Dinero) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, existe := f.facturas[llave]; existe {
		return nil
	}
	f.facturas[llave] = fmt.Sprintf("FAC-%s-%s", suscripcionID, llave)
	return nil
}

func (f *Facturador) Existe(llave string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, existe := f.facturas[llave]
	return existe
}

 
// 6. Orquestador Saga de tres pasos con compensaciones
 

type SagaOrchestrator struct {
	Suscripcion  *Suscripcion5G
	QoS          *ProveedorQoS
	Facturador   *Facturador
	Breaker      *CircuitBreaker
	Idempotencia *RegistroIdempotencia
	Monto        Dinero
	FallarEnT2   bool
	FallarEnT3   bool
}

type ResultadoSaga struct {
	SagaID         string
	Estado         string
	Pasos          []string
	Compensaciones []string
	SaldoFinal     Dinero
	QoSActivo      bool
	FacturaEmitida bool
}

// Ejecutar corre T1 -> T2 -> T3. Si T2 o T3 fallan, ejecuta C2 y C1 en orden
// inverso, cada una con su propia llave de idempotencia.
func (s *SagaOrchestrator) Ejecutar(sagaID string) (ResultadoSaga, error) {
	resultado := ResultadoSaga{SagaID: sagaID, Estado: "PENDIENTE"}
	if sagaID == "" {
		return resultado, errors.New("el id de la saga es obligatorio")
	}

	// T1: Debitar billetera.
	if err := s.Idempotencia.Ejecutar(sagaID+":T1", func() error {
		_, err := s.Suscripcion.AplicarCargo(s.Monto, sagaID+":T1")
		return err
	}); err != nil {
		resultado.Estado = "FALLIDA"
		resultado.SaldoFinal = s.Suscripcion.SaldoActual()
		return resultado, fmt.Errorf("T1 debitar billetera: %w", err)
	}
	resultado.Pasos = append(resultado.Pasos, "T1 Debitar billetera")

	// T2: Aprovisionar QoS 5G.
	err := s.Breaker.Ejecutar(func() error {
		return s.Idempotencia.Ejecutar(sagaID+":T2", func() error {
			if s.FallarEnT2 {
				return errors.New("fallo simulado en T2: aprovisionamiento QoS")
			}
			return s.QoS.Aprovisionar(sagaID)
		})
	})
	if err != nil {
		resultado.Estado = "COMPENSADA"
		s.compensar(sagaID, &resultado)
		resultado.SaldoFinal = s.Suscripcion.SaldoActual()
		resultado.QoSActivo = s.QoS.EstaAprovisionada(sagaID)
		return resultado, fmt.Errorf("T2 aprovisionar QoS: %w", err)
	}
	resultado.Pasos = append(resultado.Pasos, "T2 Aprovisionar QoS 5G")

	// T3: Emitir factura.
	err = s.Breaker.Ejecutar(func() error {
		return s.Idempotencia.Ejecutar(sagaID+":T3", func() error {
			if s.FallarEnT3 {
				return errors.New("fallo simulado en T3: emision de factura")
			}
			return s.Facturador.Emitir(sagaID, s.Suscripcion.ID(), s.Monto)
		})
	})
	if err != nil {
		resultado.Estado = "COMPENSADA"
		s.compensar(sagaID, &resultado)
		resultado.SaldoFinal = s.Suscripcion.SaldoActual()
		resultado.QoSActivo = s.QoS.EstaAprovisionada(sagaID)
		return resultado, fmt.Errorf("T3 emitir factura: %w", err)
	}
	resultado.Pasos = append(resultado.Pasos, "T3 Emitir factura")

	resultado.Estado = "COMPLETADA"
	resultado.SaldoFinal = s.Suscripcion.SaldoActual()
	resultado.QoSActivo = s.QoS.EstaAprovisionada(sagaID)
	resultado.FacturaEmitida = s.Facturador.Existe(sagaID)
	return resultado, nil
}

// compensar ejecuta las transacciones compensatorias en orden inverso:
// primero C2 (revertir QoS) y luego C1 (reembolsar la billetera).
func (s *SagaOrchestrator) compensar(sagaID string, resultado *ResultadoSaga) {
	if err := s.Idempotencia.Ejecutar(sagaID+":C2", func() error {
		return s.QoS.Desaprovisionar(sagaID)
	}); err == nil {
		resultado.Compensaciones = append(resultado.Compensaciones, "C2 Revertir aprovisionamiento QoS")
	}

	if err := s.Idempotencia.Ejecutar(sagaID+":C1", func() error {
		_, err := s.Suscripcion.Reembolsar(s.Monto, sagaID+":C1")
		return err
	}); err == nil {
		resultado.Compensaciones = append(resultado.Compensaciones, "C1 Reembolsar billetera")
	}
}

 
// 7. Demostracion ejecutable
 

func main() {
	fmt.Println("=== Lab 5: DDD, gRPC, Circuit Breaker y Saga ===")

	demostrarDinero()
	demostrarAgregado()
	demostrarBackoff()
	demostrarCircuitBreaker()
	demostrarSagas()
}

func demostrarDinero() {
	fmt.Println("\n[1] Value Object Dinero (inmutable)")
	saldo, _ := NuevoDinero(10_000, "USD")
	cargo, _ := NuevoDinero(2_500, "USD")

	restante, err := saldo.Restar(cargo)
	fmt.Printf("  Saldo inicial: %s\n", saldo)
	fmt.Printf("  Cargo:         %s\n", cargo)
	fmt.Printf("  Restar:        %s (err=%v)\n", restante, err)
	fmt.Printf("  El saldo original no cambio: %s\n", saldo)

	_, err = restante.Restar(saldo)
	fmt.Printf("  Intento de sobregiro (restar %s de %s): %v\n", saldo, restante, err)

	suma, err := restante.Sumar(cargo)
	fmt.Printf("  Sumar de vuelta: %s (err=%v)\n", suma, err)
}

func demostrarAgregado() {
	fmt.Println("\n[2] Agregado Suscripcion5G y evento CargoAplicadoEvent")
	saldo, _ := NuevoDinero(10_000, "USD")
	cargo, _ := NuevoDinero(2_500, "USD")

	suscripcion, err := NuevaSuscripcion5G("sub-001", "+584121000001", saldo)
	if err != nil {
		fmt.Printf("  Error al crear la suscripcion: %v\n", err)
		return
	}

	evento, err := suscripcion.AplicarCargo(cargo, "cargo-001")
	fmt.Printf("  Evento: %s | %s | %s | key=%s | err=%v\n",
		evento.EventID, evento.SuscripcionID, evento.Monto, evento.IdempotencyKey, err)
	fmt.Printf("  Saldo tras el cargo: %s\n", suscripcion.SaldoActual())

	duplicado, _ := suscripcion.AplicarCargo(cargo, "cargo-001")
	fmt.Printf("  Reintento con la misma key devuelve el mismo evento: %v\n",
		duplicado.EventID == evento.EventID)
	fmt.Printf("  Saldo tras el reintento: %s\n", suscripcion.SaldoActual())

	grande, _ := NuevoDinero(100_000, "USD")
	_, err = suscripcion.AplicarCargo(grande, "cargo-002")
	fmt.Printf("  Cargo superior al saldo: %v\n", err)

	invalida, err := NuevaSuscripcion5G("sub-002", "04121234567", saldo)
	fmt.Printf("  MSISDN invalido: suscripcion=%v err=%v\n", invalida, err)
}

func demostrarBackoff() {
	fmt.Println("\n[3] Exponential Backoff + Full Jitter (Tbase=100ms, Tmax=5s)")
	for k := 0; k <= 6; k++ {
		fmt.Printf("  k=%d tope=%v jitter=%v\n", k, topeBackoff(k), BackoffFullJitter(k))
	}
}

func demostrarCircuitBreaker() {
	fmt.Println("\n[4] Circuit Breaker (umbral=2 fallos, timeout=40ms)")
	cb := NuevoCircuitBreaker(2, 40*time.Millisecond)

	for intento := 1; intento <= 3; intento++ {
		err := cb.Ejecutar(func() error {
			return errors.New("fallo de red simulado")
		})
		fmt.Printf("  intento %d: estado=%s err=%v\n", intento, cb.Estado(), err)
	}

	time.Sleep(50 * time.Millisecond)
	err := cb.Ejecutar(func() error { return nil })
	fmt.Printf("  tras el timeout, prueba exitosa: estado=%s err=%v\n", cb.Estado(), err)
}

func demostrarSagas() {
	fmt.Println("\n[5] Saga T1 -> T2 -> T3 con compensaciones C2 y C1")

	escenarios := []struct {
		nombre   string
		fallarT2 bool
		fallarT3 bool
	}{
		{nombre: "exito"},
		{nombre: "fallo en T2", fallarT2: true},
		{nombre: "fallo en T3", fallarT3: true},
	}

	for _, escenario := range escenarios {
		saldo, _ := NuevoDinero(10_000, "USD")
		cargo, _ := NuevoDinero(2_500, "USD")
		suscripcion, _ := NuevaSuscripcion5G("sub-"+escenario.nombre, "+584121000001", saldo)

		saga := &SagaOrchestrator{
			Suscripcion:  suscripcion,
			QoS:          NuevoProveedorQoS(),
			Facturador:   NuevoFacturador(),
			Breaker:      NuevoCircuitBreaker(5, time.Second),
			Idempotencia: NuevoRegistroIdempotencia(),
			Monto:        cargo,
			FallarEnT2:   escenario.fallarT2,
			FallarEnT3:   escenario.fallarT3,
		}

		resultado, err := saga.Ejecutar("saga-" + escenario.nombre)
		fmt.Printf("\n  Escenario: %s\n", escenario.nombre)
		for _, paso := range resultado.Pasos {
			fmt.Printf("    %s\n", paso)
		}
		for _, compensacion := range resultado.Compensaciones {
			fmt.Printf("    %s\n", compensacion)
		}
		fmt.Printf("    Estado: %s\n", resultado.Estado)
		fmt.Printf("    Saldo final: %s\n", resultado.SaldoFinal)
		fmt.Printf("    QoS activo: %v | Factura emitida: %v\n",
			resultado.QoSActivo, resultado.FacturaEmitida)
		if err != nil {
			fmt.Printf("    Error controlado: %v\n", err)
		}
	}
}
