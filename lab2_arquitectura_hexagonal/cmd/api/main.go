package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"lab2_arquitectura_hexagonal/internal/adapters"
	"lab2_arquitectura_hexagonal/internal/application"
	"lab2_arquitectura_hexagonal/internal/domain"
)

func main() {
	ctx := context.Background()

	// ---- Composition Root: ensamblado de adaptadores y servicio ----
	repo := adapters.NuevoRepoMemoria()
	pasarela := &adapters.PasarelaSimulada{}
	auditoria := adapters.AuditoriaConsola{}
	servicio := application.NuevoServicioRecarga(repo, pasarela, auditoria)

	fallos := 0

	// ---- (a) Recarga exitosa ----
	fmt.Println("(a) Recarga exitosa")
	tx, err := servicio.EjecutarRecarga(ctx,
		"tx-1", "11111111-1111-4111-8111-111111111111", "+573001234567", 5000)
	if err != nil || tx.Estado() != domain.Aprobada {
		fmt.Println("   FALLO:", err)
		fallos++
	} else {
		fmt.Println("   OK, estado:", tx.Estado())
	}

	// ---- (b) Número que viola E.164 ----
	fmt.Println("(b) Rechazo por número que viola E.164")
	_, err = servicio.EjecutarRecarga(ctx,
		"tx-2", "22222222-2222-4222-8222-222222222222", "3001234567", 5000)
	if errors.Is(err, domain.ErrMSISDNInvalido) {
		fmt.Println("   OK, rechazado:", err)
	} else {
		fmt.Println("   FALLO: debió rechazarse con ErrMSISDNInvalido, obtuvo:", err)
		fallos++
	}

	// ---- (c) Tres reintentos concurrentes con la misma Idempotency-Key ----
	fmt.Println("(c) Tres reintentos de red con la misma Idempotency-Key")
	antes := pasarela.TotalCobros()

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(intento int) {
			defer wg.Done()
			_, e := servicio.EjecutarRecarga(ctx,
				"tx-3", "33333333-3333-4333-8333-333333333333", "+573009998877", 10000)
			if e != nil {
				fmt.Printf("   intento %d con error: %v\n", intento, e)
			}
		}(i)
	}
	wg.Wait()

	cobros := pasarela.TotalCobros() - antes
	if cobros == 1 {
		fmt.Println("   OK: se cobró una sola vez")
	} else {
		fmt.Println("   FALLO: cobros realizados =", cobros)
		fallos++
	}

	// ---- Resultado final ----
	if fallos > 0 {
		fmt.Printf("\n%d prueba(s) fallaron\n", fallos)
		os.Exit(1)
	}
	fmt.Println("\nTodas las pruebas pasaron")
}
