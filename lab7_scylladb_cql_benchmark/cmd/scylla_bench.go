package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"
)

const (
	workerCount     = 16
	defaultKeyspace = "unefa_telco_5g"
	insertStatement = `INSERT INTO cdr_por_abonado_dia
		(msisdn, bucket_fecha, timestamp_evento, id_cdr, tipo_red,
		 celda_id, puerto_origen, monto_centavos, codigo_estado, bandera_roaming)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
)

type benchmarkConfig struct {
	hosts       string
	operations  int
	subscribers int
	burstSize   int
}

type insertJob struct {
	sequence int
}

type insertResult struct {
	latency time.Duration
	err     error
}

func main() {
	config := benchmarkConfig{}
	flag.StringVar(&config.hosts, "hosts", "127.0.0.1:9042", "Hosts Scylla/Cassandra separados por coma")
	flag.IntVar(&config.operations, "operations", 100_000, "Cantidad de CDR a insertar")
	flag.IntVar(&config.subscribers, "subscribers", 1_000, "Cantidad de abonados del lote")
	flag.IntVar(&config.burstSize, "burst-size", 100, "CDR consecutivos por particion abonado/dia")
	flag.Parse()

	if err := validateConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "Configuracion invalida: %v\n", err)
		os.Exit(2)
	}

	if err := runBenchmark(config); err != nil {
		fmt.Fprintf(os.Stderr, "Benchmark fallido: %v\n", err)
		os.Exit(1)
	}
}

func validateConfig(config benchmarkConfig) error {
	switch {
	case strings.TrimSpace(config.hosts) == "":
		return errors.New("debe indicarse al menos un host")
	case config.operations <= 0:
		return errors.New("operations debe ser mayor que cero")
	case config.subscribers <= 0:
		return errors.New("subscribers debe ser mayor que cero")
	case config.burstSize <= 0:
		return errors.New("burst-size debe ser mayor que cero")
	default:
		return nil
	}
}

func runBenchmark(config benchmarkConfig) error {
	hosts := strings.Split(config.hosts, ",")
	for i := range hosts {
		hosts[i] = strings.TrimSpace(hosts[i])
		if hosts[i] == "" {
			return errors.New("la lista de hosts contiene un elemento vacio")
		}
	}

	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = defaultKeyspace
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 10 * time.Second
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
		gocql.RoundRobinHostPolicy(),
	)

	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("conectar a ScyllaDB: %w", err)
	}
	defer session.Close()

	jobs := make(chan insertJob, workerCount*2)
	results := make(chan insertResult, workerCount*2)
	var workers sync.WaitGroup
	workers.Add(workerCount)

	bucketDate := time.Now().UTC().Truncate(24 * time.Hour)
	for workerID := 0; workerID < workerCount; workerID++ {
		go func() {
			defer workers.Done()
			for job := range jobs {
				latency, err := insertCDR(session, config, bucketDate, job.sequence)
				results <- insertResult{
					latency: latency,
					err:     err,
				}
			}
		}()
	}

	started := time.Now()
	go func() {
		for sequence := 0; sequence < config.operations; sequence++ {
			jobs <- insertJob{sequence: sequence}
		}
		close(jobs)
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	latencies := make([]time.Duration, 0, config.operations)
	var failedOperations int
	var firstError error
	for result := range results {
		if result.err != nil {
			failedOperations++
			if firstError == nil {
				firstError = result.err
			}
			continue
		}
		latencies = append(latencies, result.latency)
	}
	elapsed := time.Since(started)

	reportResults(config.operations, len(latencies), failedOperations, elapsed, latencies)
	if firstError != nil {
		return fmt.Errorf("%d inserciones fallaron; primer error: %w", failedOperations, firstError)
	}
	return nil
}

func insertCDR(
	session *gocql.Session,
	config benchmarkConfig,
	bucketDate time.Time,
	sequence int,
) (time.Duration, error) {
	subscriberIndex := (sequence / config.burstSize) % config.subscribers
	msisdn := fmt.Sprintf("+58412%07d", subscriberIndex)
	eventTime := time.Now().UTC()

	query := session.Query(
		insertStatement,
		msisdn,
		bucketDate,
		eventTime,
		gocql.TimeUUID(),
		"5G",
		731+subscriberIndex%100,
		8080,
		int64(1_000+sequence%50_000),
		"OK",
		sequence%10 == 0,
	)
	started := time.Now()
	err := query.Exec()
	return time.Since(started), err
}

func reportResults(
	requested int,
	succeeded int,
	failed int,
	elapsed time.Duration,
	latencies []time.Duration,
) {
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	throughput := 0.0
	if elapsed > 0 {
		throughput = float64(succeeded) / elapsed.Seconds()
	}

	fmt.Println("Benchmark de ingesta ScyllaDB")
	fmt.Printf("  Workers: %d\n", workerCount)
	fmt.Printf("  Operaciones solicitadas: %d\n", requested)
	fmt.Printf("  Operaciones exitosas: %d\n", succeeded)
	fmt.Printf("  Operaciones fallidas: %d\n", failed)
	fmt.Printf("  Tiempo total: %s\n", elapsed)
	fmt.Printf("  Throughput exitoso: %.2f operaciones/segundo\n", throughput)

	if len(latencies) == 0 {
		fmt.Println("  Percentiles: no disponibles (ninguna insercion exitosa)")
		return
	}

	printPercentile("P50", percentile(latencies, 0.50))
	printPercentile("P95", percentile(latencies, 0.95))
	printPercentile("P99", percentile(latencies, 0.99))
}

func percentile(sortedLatencies []time.Duration, quantile float64) time.Duration {
	index := int(math.Ceil(quantile*float64(len(sortedLatencies)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedLatencies) {
		index = len(sortedLatencies) - 1
	}
	return sortedLatencies[index]
}

func printPercentile(name string, latency time.Duration) {
	microseconds := float64(latency.Nanoseconds()) / 1_000
	milliseconds := float64(latency.Nanoseconds()) / 1_000_000
	fmt.Printf("  %s: %.2f us (%.3f ms)\n", name, microseconds, milliseconds)
}
