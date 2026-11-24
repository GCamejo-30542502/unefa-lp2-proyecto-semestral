package main

import (
	"fmt"
	"time"
)

const (
	cantidadAbonados           = 100_000
	cantidadConsultas          = 1_000
	repeticionesBusquedaLineal = 10
	repeticionesBusquedaHash   = 1_000
)

type Sesion5G struct {
	IMSI           uint64
	CeldaID        uint32
	PuertoOrigen   uint16
	TipoRed        string
	BanderaRoaming bool
}

func CrearEnStackPorValor() Sesion5G {
	s := Sesion5G{
		IMSI:           500123456789012,
		CeldaID:        731,
		PuertoOrigen:   8080,
		TipoRed:        "5G",
		BanderaRoaming: false,
	}
	return s
}

func CrearConEscapeAlHeap() *Sesion5G {
	s := Sesion5G{
		IMSI:           500123456789013,
		CeldaID:        732,
		PuertoOrigen:   8081,
		TipoRed:        "5G",
		BanderaRoaming: true,
	}
	return &s
}

func main() {
	sesionPorValor := CrearEnStackPorValor()
	sesionConEscape := CrearConEscapeAlHeap()
	fmt.Printf("Sesion por valor: IMSI=%d, celda=%d\n",
		sesionPorValor.IMSI, sesionPorValor.CeldaID)
	fmt.Printf("Sesion con escape: IMSI=%d, celda=%d\n",
		sesionConEscape.IMSI, sesionConEscape.CeldaID)

	abonados := make([]int, cantidadAbonados)
	indiceHash := make(map[int]int, cantidadAbonados)
	for id := range abonados {
		abonados[id] = id
		indiceHash[id] = id
	}

	consultas := make([]int, cantidadConsultas)
	for i := range consultas {
		consultas[i] = cantidadAbonados - cantidadConsultas + i
	}

	var sumaLineal uint64
	inicio := time.Now()
	for repeticion := 0; repeticion < repeticionesBusquedaLineal; repeticion++ {
		for _, idBuscado := range consultas {
			for indice, id := range abonados {
				if id == idBuscado {
					sumaLineal += uint64(indice)
					break
				}
			}
		}
	}
	duracionLineal := time.Since(inicio)

	var sumaHash uint64
	inicio = time.Now()
	for repeticion := 0; repeticion < repeticionesBusquedaHash; repeticion++ {
		for _, idBuscado := range consultas {
			sumaHash += uint64(indiceHash[idBuscado])
		}
	}
	duracionHash := time.Since(inicio)

	if sumaLineal/uint64(repeticionesBusquedaLineal) != sumaHash/uint64(repeticionesBusquedaHash) {
		panic("las busquedas lineal y hash produjeron resultados distintos")
	}

	microsegundosLineales := float64(duracionLineal.Nanoseconds()) /
		float64(repeticionesBusquedaLineal) / 1000.0
	microsegundosHash := float64(duracionHash.Nanoseconds()) /
		float64(repeticionesBusquedaHash) / 1000.0
	fmt.Printf("\nBusqueda lineal O(n): promedio de %d consultas en %.3f us (%d lotes)\n",
		cantidadConsultas, microsegundosLineales, repeticionesBusquedaLineal)
	fmt.Printf("Busqueda en mapa O(1) promedio: %d consultas en %.3f us (%d lotes)\n",
		cantidadConsultas, microsegundosHash, repeticionesBusquedaHash)
	fmt.Printf("Suma de indices por lote verificada: %d\n",
		sumaLineal/uint64(repeticionesBusquedaLineal))
}
