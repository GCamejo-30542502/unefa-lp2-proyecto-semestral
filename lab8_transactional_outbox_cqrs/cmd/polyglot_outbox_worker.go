// EJERCICIO 8: Integración Políglota sin Pérdida mediante Transactional Outbox
// (FOR UPDATE SKIP LOCKED). Arquitectura CQRS híbrida PostgreSQL 16 + ScyllaDB 5.4+.
//
//   Command Side (PostgreSQL, ACID): abonados_bcnf, transacciones_bss, outbox_events
//   Query Side   (ScyllaDB)        : unefa_telco_5g.cdr_por_abonado_dia
//
// Fases:
//   1. ProcesarMovimiento : Agregado DDD (Suscripcion5G + Dinero) + una única
//      transacción PostgreSQL: saldo + transacción BSS + evento PENDING en el outbox.
//   2. Extracción         : pool de goroutines con SELECT ... FOR UPDATE SKIP LOCKED.
//   3. Proyección         : INSERT idempotente en ScyllaDB (LOCAL_QUORUM) y solo tras el
//                           ACK se marca status = 'PROCESSED' en PostgreSQL.
//
// Ejecución (desde la raíz unefa-lp2-proyecto-semestral/):
//   go run lab8_transactional_outbox_cqrs/cmd/polyglot_outbox_worker.go -demo 30
//
// Dependencias (go.mod en la raíz del proyecto):
//   go get github.com/jackc/pgx/v5 github.com/gocql/gocql
//
// Variables de entorno (valores por defecto entre paréntesis):
//   PG_DSN          (postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable)
//   SCYLLA_HOSTS    (127.0.0.1)  separados por coma
//   SCYLLA_KEYSPACE (unefa_telco_5g)
//   SCYLLA_DC       (datacenter1)
//   WORKERS (4)  BATCH_SIZE (50)  POLL_INTERVAL (500ms)
//
// ---------------------------------------------------------------------------
// CONTRATO DE ESQUEMA (debe coincidir con lab6 y lab7)
//
// PostgreSQL (lab6/01_schema_bcnf_y_outbox.sql):
//   abonados_bcnf    (abonado_id BIGINT PK, msisdn TEXT UNIQUE NOT NULL,
//                     plan_id ..., saldo_centavos BIGINT NOT NULL CHECK (saldo_centavos >= 0),
//                     moneda CHAR(3) NOT NULL DEFAULT 'USD')
//   transacciones_bss(transaccion_id BIGSERIAL PK, abonado_id BIGINT NOT NULL,
//                     tipo TEXT NOT NULL, monto_centavos BIGINT NOT NULL,
//                     idempotency_key TEXT NOT NULL UNIQUE,
//                     metadata_jsonb JSONB NOT NULL,
//                     created_at TIMESTAMPTZ NOT NULL DEFAULT now())
//   outbox_events    (event_id UUID PK DEFAULT gen_random_uuid(),
//                     aggregate_id TEXT NOT NULL, event_type TEXT NOT NULL,
//                     payload_jsonb JSONB NOT NULL,
//                     status TEXT NOT NULL DEFAULT 'PENDING',
//                     created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
//                     processed_at TIMESTAMPTZ)
//
// ScyllaDB (lab7/esquema_telco_5g.cql), columnas que escribe este worker:
//   PRIMARY KEY ((msisdn, bucket_fecha), timestamp_evento, id_cdr)
//   msisdn text, bucket_fecha date, timestamp_evento timestamp, id_cdr uuid,
//   tipo_evento text, monto_centavos bigint, moneda text,
//   saldo_final_centavos bigint, antena_5g text, roaming boolean
// ---------------------------------------------------------------------------

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gocql/gocql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================================
// DOMINIO (DDD, Ejercicio 5): Value Object Dinero + Agregado Suscripcion5G
// ============================================================================

var (
	ErrSaldoInsuficiente = errors.New("saldo insuficiente: el saldo no puede quedar negativo")
	ErrMonedaDistinta    = errors.New("monedas distintas")
	ErrMontoInvalido     = errors.New("el monto debe ser positivo")
	ErrDesbordamiento    = errors.New("desbordamiento aritmético en int64")
	ErrAbonadoNoExiste   = errors.New("abonado inexistente")
)

// Dinero es un Value Object inmutable: todas las operaciones devuelven un valor nuevo.
type Dinero struct {
	MontoCentavos int64
	Moneda        string
}

func NuevoDinero(centavos int64, moneda string) (Dinero, error) {
	if centavos < 0 {
		return Dinero{}, ErrSaldoInsuficiente
	}
	return Dinero{MontoCentavos: centavos, Moneda: moneda}, nil
}

func (d Dinero) Sumar(o Dinero) (Dinero, error) {
	if d.Moneda != o.Moneda {
		return Dinero{}, ErrMonedaDistinta
	}
	if o.MontoCentavos > math.MaxInt64-d.MontoCentavos {
		return Dinero{}, ErrDesbordamiento
	}
	return Dinero{MontoCentavos: d.MontoCentavos + o.MontoCentavos, Moneda: d.Moneda}, nil
}

// Restar impide saldos negativos.
func (d Dinero) Restar(o Dinero) (Dinero, error) {
	if d.Moneda != o.Moneda {
		return Dinero{}, ErrMonedaDistinta
	}
	if o.MontoCentavos > d.MontoCentavos {
		return Dinero{}, ErrSaldoInsuficiente
	}
	return Dinero{MontoCentavos: d.MontoCentavos - o.MontoCentavos, Moneda: d.Moneda}, nil
}

type TipoMovimiento string

const (
	Recarga TipoMovimiento = "RECARGA"
	Consumo TipoMovimiento = "CONSUMO"
)

// CargoAplicadoEvent es un evento de dominio inmutable (se crea una vez, se
// pasa por valor y nunca se modifica; solo se serializa al outbox).
type CargoAplicadoEvent struct {
	IdempotencyKey     string          `json:"idempotency_key"`
	MSISDN             string          `json:"msisdn"`
	Tipo               TipoMovimiento  `json:"tipo"`
	MontoCentavos      int64           `json:"monto_centavos"`
	Moneda             string          `json:"moneda"`
	SaldoFinalCentavos int64           `json:"saldo_final_centavos"`
	TransaccionID      int64           `json:"transaccion_id"`
	Metadata           json.RawMessage `json:"metadata"`
	OcurridoEn         time.Time       `json:"ocurrido_en"`
}

// conTransaccion devuelve una copia con el ID asignado por PostgreSQL.
func (e CargoAplicadoEvent) conTransaccion(id int64) CargoAplicadoEvent {
	e.TransaccionID = id
	return e
}

// Suscripcion5G es la Raíz de Agregado: única vía para mutar el saldo.
type Suscripcion5G struct {
	msisdn string
	saldo  Dinero
}

func RehidratarSuscripcion(msisdn string, saldo Dinero) *Suscripcion5G {
	return &Suscripcion5G{msisdn: msisdn, saldo: saldo}
}

func (s *Suscripcion5G) Saldo() Dinero { return s.saldo }

// AplicarMovimiento protege las invariantes (monto > 0, misma moneda, saldo >= 0)
// y emite el evento inmutable.
func (s *Suscripcion5G) AplicarMovimiento(tipo TipoMovimiento, monto Dinero, key string, meta json.RawMessage, ahora time.Time) (CargoAplicadoEvent, error) {
	if monto.MontoCentavos <= 0 {
		return CargoAplicadoEvent{}, ErrMontoInvalido
	}
	var nuevo Dinero
	var err error
	switch tipo {
	case Recarga:
		nuevo, err = s.saldo.Sumar(monto)
	case Consumo:
		nuevo, err = s.saldo.Restar(monto)
	default:
		return CargoAplicadoEvent{}, fmt.Errorf("tipo de movimiento desconocido: %q", tipo)
	}
	if err != nil {
		return CargoAplicadoEvent{}, err
	}
	s.saldo = nuevo
	return CargoAplicadoEvent{
		IdempotencyKey:     key,
		MSISDN:             s.msisdn,
		Tipo:               tipo,
		MontoCentavos:      monto.MontoCentavos,
		Moneda:             monto.Moneda,
		SaldoFinalCentavos: nuevo.MontoCentavos,
		Metadata:           meta,
		// Scylla guarda timestamps en milisegundos: se trunca para que el valor
		// en PostgreSQL y en Scylla sea idéntico (clave de idempotencia estable).
		OcurridoEn: ahora.UTC().Truncate(time.Millisecond),
	}, nil
}

// ============================================================================
// FASE 1: Command Side, transacción ACID en PostgreSQL
// ============================================================================

// ProcesarMovimiento ejecuta, en una única transacción:
//
//	saldo (abonados_bcnf) + operación (transacciones_bss) + evento (outbox_events PENDING).
//
// Devuelve duplicado=true si la idempotency_key ya fue procesada (sin efectos nuevos).
func ProcesarMovimiento(ctx context.Context, pool *pgxpool.Pool, msisdn string, tipo TipoMovimiento,
	montoCentavos int64, idempotencyKey string, metadata json.RawMessage) (duplicado bool, err error) {

	tx, err := pool.Begin(ctx) // tx.Begin()
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			log.Printf("rollback: %v", rbErr)
		}
	}()

	// 0) Rehidratar el agregado bloqueando la fila del abonado (evita lost updates).
	var abonadoID, saldoActual int64
	var moneda string
	err = tx.QueryRow(ctx, `
		SELECT abonado_id, saldo_centavos, moneda
		  FROM abonados_bcnf
		 WHERE msisdn = $1
		   FOR UPDATE`, msisdn).Scan(&abonadoID, &saldoActual, &moneda)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("%w: %s", ErrAbonadoNoExiste, msisdn)
	}
	if err != nil {
		return false, fmt.Errorf("leer abonado: %w", err)
	}
	saldo, err := NuevoDinero(saldoActual, strings.TrimSpace(moneda))
	if err != nil {
		return false, err
	}
	suscripcion := RehidratarSuscripcion(msisdn, saldo)

	// 1) Lógica de dominio: el agregado valida invariantes y emite el evento.
	monto := Dinero{MontoCentavos: montoCentavos, Moneda: saldo.Moneda}
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	evento, err := suscripcion.AplicarMovimiento(tipo, monto, idempotencyKey, metadata, time.Now())
	if err != nil {
		return false, err
	}

	// 2) Registrar la operación en transacciones_bss. La UNIQUE sobre
	//    idempotency_key convierte un reintento del cliente en un no-op.
	var txnID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO transacciones_bss (abonado_id, tipo, monto_centavos, idempotency_key, metadata_jsonb, created_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING transaccion_id`,
		abonadoID, string(tipo), montoCentavos, idempotencyKey, []byte(metadata), evento.OcurridoEn).Scan(&txnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil // ya procesada: el defer hace Rollback sin efectos
	}
	if err != nil {
		return false, fmt.Errorf("insertar transaccion: %w", err)
	}

	// 3) Descontar / acreditar el saldo en abonados_bcnf (valor ya validado por el agregado).
	if _, err = tx.Exec(ctx,
		`UPDATE abonados_bcnf SET saldo_centavos = $1 WHERE abonado_id = $2`,
		suscripcion.Saldo().MontoCentavos, abonadoID); err != nil {
		return false, fmt.Errorf("actualizar saldo: %w", err)
	}

	// 4) Insertar el evento serializado en outbox_events con estado PENDING.
	payload, err := json.Marshal(evento.conTransaccion(txnID))
	if err != nil {
		return false, fmt.Errorf("serializar evento: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_id, event_type, payload_jsonb, status)
		VALUES ($1, 'CargoAplicadoEvent', $2::jsonb, 'PENDING')`,
		msisdn, payload); err != nil {
		return false, fmt.Errorf("insertar outbox: %w", err)
	}

	// 5) Único commit atómico.
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return false, nil
}

// ============================================================================
// FASES 2 y 3: Despachadores (SKIP LOCKED) y proyección en ScyllaDB
// ============================================================================

type Config struct {
	PGDSN          string
	ScyllaHosts    []string
	ScyllaKeyspace string
	ScyllaLocalDC  string
	Workers        int
	BatchSize      int
	PollInterval   time.Duration
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadConfig() Config {
	w, _ := strconv.Atoi(getenv("WORKERS", "4"))
	b, _ := strconv.Atoi(getenv("BATCH_SIZE", "50"))
	p, err := time.ParseDuration(getenv("POLL_INTERVAL", "500ms"))
	if err != nil {
		p = 500 * time.Millisecond
	}
	if w < 1 {
		w = 1
	}
	if b < 1 {
		b = 50
	}
	return Config{
		PGDSN:          getenv("PG_DSN", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"),
		ScyllaHosts:    strings.Split(getenv("SCYLLA_HOSTS", "127.0.0.1"), ","),
		ScyllaKeyspace: getenv("SCYLLA_KEYSPACE", "unefa_telco_5g"),
		ScyllaLocalDC:  getenv("SCYLLA_DC", "datacenter1"),
		Workers:        w,
		BatchSize:      b,
		PollInterval:   p,
	}
}

type outboxRow struct {
	EventID string
	Payload []byte
}

type metaCDR struct {
	Antena5G string `json:"antena_5g"`
	Roaming  bool   `json:"roaming"`
}

type Dispatcher struct {
	pg         *pgxpool.Pool
	scylla     *gocql.Session
	batch      int
	poll       time.Duration
	nombre     string
	procesados *atomic.Int64
	fallidos   *atomic.Int64
}

func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.poll)
	defer ticker.Stop()
	log.Printf("[%s] iniciado", d.nombre)
	for {
		// Drena lotes completos sin esperar al ticker.
		for {
			n, err := d.procesarLote(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[%s] error en lote: %v", d.nombre, err)
				}
				break
			}
			if n < d.batch {
				break
			}
		}
		select {
		case <-ctx.Done():
			log.Printf("[%s] detenido", d.nombre)
			return
		case <-ticker.C:
		}
	}
}

// procesarLote extrae hasta `batch` eventos PENDING con SKIP LOCKED, los proyecta
// a ScyllaDB y marca PROCESSED solo los confirmados, todo dentro de una transacción.
// Si el worker muere antes del commit, PostgreSQL libera los locks y los eventos
// vuelven a ser PENDING (at-least-once). La idempotencia del INSERT en Scylla
// (mismo id_cdr, misma clave primaria) hace inocuo el reintento.
func (d *Dispatcher) procesarLote(ctx context.Context) (int, error) {
	tx, err := d.pg.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			log.Printf("[%s] rollback: %v", d.nombre, rbErr)
		}
	}()

	// FASE 2: extracción sin contención (event_id::text simplifica el escaneo del UUID).
	rows, err := tx.Query(ctx, `
		SELECT event_id::text, payload_jsonb
		  FROM outbox_events
		 WHERE status = 'PENDING'
		 ORDER BY created_at
		 LIMIT $1
		   FOR UPDATE SKIP LOCKED`, d.batch)
	if err != nil {
		return 0, fmt.Errorf("select outbox: %w", err)
	}
	var eventos []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.EventID, &r.Payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan outbox: %w", err)
		}
		eventos = append(eventos, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterar outbox: %w", err)
	}
	if len(eventos) == 0 {
		return 0, nil
	}

	// FASE 3: proyección idempotente en ScyllaDB (LOCAL_QUORUM).
	confirmados := make([]string, 0, len(eventos))
	nFallidos := 0
	for _, ev := range eventos {
		if err := d.proyectarEnScylla(ctx, ev); err != nil {
			nFallidos++
			log.Printf("[%s] evento %s sigue PENDING: %v", d.nombre, ev.EventID, err)
			continue
		}
		confirmados = append(confirmados, ev.EventID)
	}

	// Solo tras la confirmación de Scylla se marca PROCESSED.
	if len(confirmados) > 0 {
		if _, err = tx.Exec(ctx, `
			UPDATE outbox_events
			   SET status = 'PROCESSED', processed_at = now()
			 WHERE event_id = ANY($1::uuid[])`, confirmados); err != nil {
			return len(eventos), fmt.Errorf("marcar PROCESSED: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return len(eventos), fmt.Errorf("commit: %w", err)
	}

	d.procesados.Add(int64(len(confirmados)))
	d.fallidos.Add(int64(nFallidos))
	log.Printf("[%s] lote: %d extraídos, %d PROCESSED, %d pendientes", d.nombre, len(eventos), len(confirmados), nFallidos)
	return len(eventos), nil
}

// proyectarEnScylla escribe el CDR en cdr_por_abonado_dia.
//
// Idempotencia: PRIMARY KEY ((msisdn, bucket_fecha), timestamp_evento, id_cdr) se
// deriva solo del evento (id_cdr = event_id del outbox, timestamp = ocurrido_en
// del dominio, nunca now()), así que reintentar reescribe la misma fila.
func (d *Dispatcher) proyectarEnScylla(ctx context.Context, ev outboxRow) error {
	var p CargoAplicadoEvent
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		return fmt.Errorf("payload inválido: %w", err)
	}
	idCDR, err := gocql.ParseUUID(ev.EventID)
	if err != nil {
		return fmt.Errorf("event_id inválido: %w", err)
	}
	var meta metaCDR
	if len(p.Metadata) > 0 {
		_ = json.Unmarshal(p.Metadata, &meta) // metadata opcional: campos ausentes quedan en cero
	}
	ocurrido := p.OcurridoEn.UTC()
	bucketFecha := time.Date(ocurrido.Year(), ocurrido.Month(), ocurrido.Day(), 0, 0, 0, 0, time.UTC)

	return d.scylla.Query(`
		INSERT INTO cdr_por_abonado_dia
		  (msisdn, bucket_fecha, timestamp_evento, id_cdr, tipo_evento,
		   monto_centavos, moneda, saldo_final_centavos, antena_5g, roaming)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.MSISDN, bucketFecha, ocurrido, idCDR, string(p.Tipo),
		p.MontoCentavos, p.Moneda, p.SaldoFinalCentavos, meta.Antena5G, meta.Roaming).
		Consistency(gocql.LocalQuorum).
		WithContext(ctx).
		Exec()
}

func conectarScylla(cfg Config) (*gocql.Session, error) {
	cluster := gocql.NewCluster(cfg.ScyllaHosts...)
	cluster.Keyspace = cfg.ScyllaKeyspace
	cluster.Consistency = gocql.LocalQuorum
	cluster.Timeout = 5 * time.Second
	cluster.ConnectTimeout = 10 * time.Second
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{NumRetries: 3, Min: 50 * time.Millisecond, Max: 2 * time.Second}
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.DCAwareRoundRobinPolicy(cfg.ScyllaLocalDC))
	return cluster.CreateSession()
}

// ============================================================================
// Demo y main
// ============================================================================

// generarDemo siembra abonados y ejecuta n movimientos aleatorios. Para probar
// idempotencia repite además la primera llave de idempotencia.
func generarDemo(ctx context.Context, pool *pgxpool.Pool, n int) {
	msisdns := []string{"+584121000001", "+584121000002", "+584121000003"}
	for i, m := range msisdns {
		// Ajuste las columnas NOT NULL adicionales (p. ej. plan_id) a su esquema de lab6.
		_, err := pool.Exec(ctx, `
			INSERT INTO abonados_bcnf (abonado_id, msisdn, saldo_centavos, moneda)
			VALUES ($1, $2, 10000, 'USD')
			ON CONFLICT (msisdn) DO NOTHING`, int64(i+1), m)
		if err != nil {
			log.Printf("demo: sembrar %s: %v", m, err)
			return
		}
	}
	run := time.Now().UnixNano()
	var primera struct {
		msisdn string
		tipo   TipoMovimiento
		monto  int64
		key    string
		meta   json.RawMessage
	}
	for i := 0; i < n; i++ {
		m := msisdns[rand.Intn(len(msisdns))]
		tipo, monto := Recarga, int64(500+rand.Intn(4500))
		if rand.Intn(2) == 0 {
			tipo, monto = Consumo, int64(50+rand.Intn(500))
		}
		key := fmt.Sprintf("demo-%d-%d", run, i)
		meta := json.RawMessage(fmt.Sprintf(`{"antena_5g":"CCS-NORTE-%02d","roaming":%t}`, 1+rand.Intn(3), rand.Intn(2) == 0))
		if i == 0 {
			primera.msisdn, primera.tipo, primera.monto, primera.key, primera.meta = m, tipo, monto, key, meta
		}
		if _, err := ProcesarMovimiento(ctx, pool, m, tipo, monto, key, meta); err != nil {
			log.Printf("demo: %s %s %d: %v", m, tipo, monto, err)
		}
	}
	if n > 0 {
		dup, err := ProcesarMovimiento(ctx, pool, primera.msisdn, primera.tipo, primera.monto, primera.key, primera.meta)
		log.Printf("demo: reintento con la misma idempotency_key -> duplicado=%t err=%v", dup, err)
	}
	log.Printf("demo: %d movimientos generados", n)
}

func main() {
	demo := flag.Int("demo", 0, "genera N movimientos de prueba antes de arrancar el worker")
	flag.Parse()

	cfg := loadConfig()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolCfg, err := pgxpool.ParseConfig(cfg.PGDSN)
	if err != nil {
		log.Fatalf("DSN de PostgreSQL inválido: %v", err)
	}
	poolCfg.MaxConns = int32(cfg.Workers + 4)
	pg, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.Fatalf("conectar PostgreSQL: %v", err)
	}
	defer pg.Close()
	if err := pg.Ping(ctx); err != nil {
		log.Fatalf("ping PostgreSQL: %v", err)
	}

	scylla, err := conectarScylla(cfg)
	if err != nil {
		log.Fatalf("conectar ScyllaDB: %v", err)
	}
	defer scylla.Close()

	if *demo > 0 {
		generarDemo(ctx, pg, *demo)
	}

	var procesados, fallidos atomic.Int64
	var wg sync.WaitGroup
	for i := 1; i <= cfg.Workers; i++ {
		d := &Dispatcher{
			pg: pg, scylla: scylla, batch: cfg.BatchSize, poll: cfg.PollInterval,
			nombre: fmt.Sprintf("worker-%d", i), procesados: &procesados, fallidos: &fallidos,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Run(ctx)
		}()
	}

	log.Printf("outbox worker activo: %d goroutines, lote=%d, intervalo=%s", cfg.Workers, cfg.BatchSize, cfg.PollInterval)
	<-ctx.Done()
	log.Println("apagando, esperando workers...")
	wg.Wait()
	log.Printf("resumen: %d eventos PROCESSED, %d reintentos pendientes", procesados.Load(), fallidos.Load())
}
