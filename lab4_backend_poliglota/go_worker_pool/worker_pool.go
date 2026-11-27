package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const cantidadWorkers = 4

type Transaccion struct {
	ID            string
	MSISDN        string
	MontoCentavos int64
}

type ResultadoAuditoria struct {
	ID          string
	MSISDN      string
	EsAlerta    bool
	Descripcion string
}

type ProcesadorTransaccion interface {
	Procesar(Transaccion) ResultadoAuditoria
}

type ProcesadorAntifraude struct {
	UmbralCentavos int64
}

func (p ProcesadorAntifraude) Procesar(transaccion Transaccion) ResultadoAuditoria {
	resultado := ResultadoAuditoria{
		ID:     transaccion.ID,
		MSISDN: transaccion.MSISDN,
	}
	if transaccion.MontoCentavos >= p.UmbralCentavos {
		resultado.EsAlerta = true
		resultado.Descripcion = "monto igual o superior al umbral de riesgo"
	}
	return resultado
}

type EstadisticasPool struct {
	Procesadas int64
	Alertas    int64
}

func EjecutarWorkerPool(
	jobs <-chan Transaccion,
	results chan<- ResultadoAuditoria,
	procesador ProcesadorTransaccion,
) EstadisticasPool {
	var grupo sync.WaitGroup
	var procesadas atomic.Int64
	var alertas atomic.Int64

	grupo.Add(cantidadWorkers)
	for workerID := 0; workerID < cantidadWorkers; workerID++ {
		go func() {
			defer grupo.Done()
			for transaccion := range jobs {
				resultado := procesador.Procesar(transaccion)
				procesadas.Add(1)
				if resultado.EsAlerta {
					alertas.Add(1)
				}
				results <- resultado
			}
		}()
	}

	grupo.Wait()
	close(results)

	return EstadisticasPool{
		Procesadas: procesadas.Load(),
		Alertas:    alertas.Load(),
	}
}

func main() {
	transacciones := []Transaccion{
		{ID: "tx-001", MSISDN: "+584121000001", MontoCentavos: 120_000},
		{ID: "tx-002", MSISDN: "+584121000002", MontoCentavos: 740_000},
		{ID: "tx-003", MSISDN: "+584121000003", MontoCentavos: 85_000},
		{ID: "tx-004", MSISDN: "+584121000004", MontoCentavos: 920_000},
		{ID: "tx-005", MSISDN: "+584121000005", MontoCentavos: 315_000},
		{ID: "tx-006", MSISDN: "+584121000006", MontoCentavos: 500_000},
		{ID: "tx-007", MSISDN: "+584121000007", MontoCentavos: 46_000},
		{ID: "tx-008", MSISDN: "+584121000008", MontoCentavos: 1_200_000},
		{ID: "tx-009", MSISDN: "+584121000009", MontoCentavos: 215_000},
		{ID: "tx-010", MSISDN: "+584121000010", MontoCentavos: 650_000},
		{ID: "tx-011", MSISDN: "+584121000011", MontoCentavos: 175_000},
		{ID: "tx-012", MSISDN: "+584121000012", MontoCentavos: 510_000},
	}

	jobs := make(chan Transaccion)
	results := make(chan ResultadoAuditoria)
	estadisticasFinales := make(chan EstadisticasPool, 1)

	go func() {
		estadisticasFinales <- EjecutarWorkerPool(
			jobs,
			results,
			ProcesadorAntifraude{UmbralCentavos: 500_000},
		)
	}()

	go func() {
		for _, transaccion := range transacciones {
			jobs <- transaccion
		}
		close(jobs)
	}()

	var resultadosRecibidos int64
	for resultado := range results {
		resultadosRecibidos++
		if resultado.EsAlerta {
			fmt.Printf("ALERTA %s (%s): %s\n",
				resultado.ID, resultado.MSISDN, resultado.Descripcion)
		}
	}

	estadisticas := <-estadisticasFinales
	fmt.Printf(
		"\nResumen: procesadas=%d, alertas=%d, resultados=%d, workers=%d\n",
		estadisticas.Procesadas,
		estadisticas.Alertas,
		resultadosRecibidos,
		cantidadWorkers,
	)
	if estadisticas.Procesadas != int64(len(transacciones)) ||
		resultadosRecibidos != int64(len(transacciones)) {
		panic("el worker pool no proceso y devolvio todas las transacciones")
	}
}
