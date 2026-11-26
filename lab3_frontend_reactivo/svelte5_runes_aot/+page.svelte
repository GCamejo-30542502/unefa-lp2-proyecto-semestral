<script lang="ts">
  import { tick } from "svelte";

  interface CellTelemetryDTO {
    cellId: string;
    latencyMs: number;
    packetLossPct: number;
    throughputMbps: number;
  }

  const WINDOW_SIZE = 200;       // tamaño de la ventana deslizante
  const CRITICAL_LATENCY = 50;   // umbral de estado crítico (ms)
  const BURST_SIZE = 100;        // paquetes por ráfaga

  function generatePacket(): CellTelemetryDTO {
    return {
      cellId: `CELL-${Math.floor(Math.random() * 50)
        .toString()
        .padStart(3, "0")}`,
      latencyMs: Math.round(Math.random() * 100 * 10) / 10,
      packetLossPct: Math.round(Math.random() * 5 * 100) / 100,
      throughputMbps: Math.round(Math.random() * 1000 * 10) / 10,
    };
  }

  // Estado reactivo (señal compilada, sin Virtual DOM)
  let packets = $state<CellTelemetryDTO[]>([]);
  let lastUpdateMs = $state<number | null>(null);

  // Valor derivado: se recalcula solo cuando cambia `packets`
  let metrics = $derived.by(() => {
    if (packets.length === 0) return { avgLatency: 0, criticalCount: 0 };
    const total = packets.reduce((sum, p) => sum + p.latencyMs, 0);
    const critical = packets.filter((p) => p.latencyMs > CRITICAL_LATENCY).length;
    return { avgLatency: total / packets.length, criticalCount: critical };
  });

  // Últimos 20 paquetes, más recientes primero
  let recent = $derived(packets.slice(-20).reverse());

  async function injectBurst() {
    const start = performance.now();

    const burst = Array.from({ length: BURST_SIZE }, generatePacket);
    packets = [...packets, ...burst].slice(-WINDOW_SIZE); // ventana deslizante

    await tick(); // espera a que Svelte aplique los cambios al DOM
    requestAnimationFrame(() => {
      lastUpdateMs = performance.now() - start; // hasta el siguiente frame pintado
    });
  }
</script>

<section>
  <h2>Panel de Telemetría 5G (Svelte 5)</h2>

  <button onclick={injectBurst}>
    Inyectar ráfaga ({BURST_SIZE} paquetes)
  </button>

  <ul>
    <li>Paquetes en ventana: {packets.length}</li>
    <li>Latencia promedio: {metrics.avgLatency.toFixed(2)} ms</li>
    <li>Celdas críticas (&gt;{CRITICAL_LATENCY} ms): {metrics.criticalCount}</li>
    <li>
      Tiempo de actualización:
      {lastUpdateMs !== null ? `${lastUpdateMs.toFixed(2)} ms` : "—"}
    </li>
  </ul>

  <table border="1" cellpadding="4">
    <thead>
      <tr>
        <th>Cell ID</th>
        <th>Latencia (ms)</th>
        <th>Pérdida (%)</th>
        <th>Throughput (Mbps)</th>
      </tr>
    </thead>
    <tbody>
      {#each recent as p}
        <tr style:background={p.latencyMs > CRITICAL_LATENCY ? "#fdd" : "transparent"}>
          <td>{p.cellId}</td>
          <td>{p.latencyMs}</td>
          <td>{p.packetLossPct}</td>
          <td>{p.throughputMbps}</td>
        </tr>
      {/each}
    </tbody>
  </table>
</section>

<style>
  section {
    padding: 16px;
    font-family: sans-serif;
  }
</style>