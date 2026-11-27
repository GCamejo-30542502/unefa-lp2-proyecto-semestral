"use client"; // Frontera de Serialización: este componente se hidrata en el cliente

import { useState, useMemo, useCallback } from "react";

interface TelemetryPacket {
  cellId: string;
  latencyMs: number;
  packetLossPct: number;
  throughputMbps: number;
}

const WINDOW_SIZE = 200;       // tamaño de la ventana deslizante
const CRITICAL_LATENCY = 50;   // umbral de estado crítico (ms)
const BURST_SIZE = 100;        // paquetes por ráfaga

function generatePacket(): TelemetryPacket {
  return {
    cellId: `CELL-${Math.floor(Math.random() * 50)
      .toString()
      .padStart(3, "0")}`,
    latencyMs: Math.round(Math.random() * 100 * 10) / 10,
    packetLossPct: Math.round(Math.random() * 5 * 100) / 100,
    throughputMbps: Math.round(Math.random() * 1000 * 10) / 10,
  };
}

export default function TelemetryPanelClient() {
  const [packets, setPackets] = useState<TelemetryPacket[]>([]);
  const [lastUpdateMs, setLastUpdateMs] = useState<number | null>(null);

  // Cálculo memorizado: solo se recalcula cuando cambia `packets`
  const { avgLatency, criticalCount } = useMemo(() => {
    if (packets.length === 0) return { avgLatency: 0, criticalCount: 0 };
    const total = packets.reduce((sum, p) => sum + p.latencyMs, 0);
    const critical = packets.filter((p) => p.latencyMs > CRITICAL_LATENCY).length;
    return { avgLatency: total / packets.length, criticalCount: critical };
  }, [packets]);

  const injectBurst = useCallback(() => {
    const start = performance.now();

    const burst = Array.from({ length: BURST_SIZE }, generatePacket);
    setPackets((prev) => [...prev, ...burst].slice(-WINDOW_SIZE)); // ventana deslizante

    // Mide hasta que el navegador pinta el siguiente frame
    requestAnimationFrame(() => {
      setLastUpdateMs(performance.now() - start);
    });
  }, []);

  return (
    <section style={{ padding: 16, fontFamily: "sans-serif" }}>
      <h2>Panel de Telemetría 5G (React 19)</h2>

      <button onClick={injectBurst}>
        Inyectar ráfaga ({BURST_SIZE} paquetes)
      </button>

      <ul>
        <li>Paquetes en ventana: {packets.length}</li>
        <li>Latencia promedio: {avgLatency.toFixed(2)} ms</li>
        <li>Celdas críticas (&gt;{CRITICAL_LATENCY} ms): {criticalCount}</li>
        <li>
          Tiempo de actualización:{" "}
          {lastUpdateMs !== null ? `${lastUpdateMs.toFixed(2)} ms` : "—"}
        </li>
      </ul>

      <table border={1} cellPadding={4}>
        <thead>
          <tr>
            <th>Cell ID</th>
            <th>Latencia (ms)</th>
            <th>Pérdida (%)</th>
            <th>Throughput (Mbps)</th>
          </tr>
        </thead>
        <tbody>
          {packets.slice(-20).reverse().map((p, i) => (
            <tr
              key={`${p.cellId}-${i}`}
              style={{ background: p.latencyMs > CRITICAL_LATENCY ? "#fdd" : "transparent" }}
            >
              <td>{p.cellId}</td>
              <td>{p.latencyMs}</td>
              <td>{p.packetLossPct}</td>
              <td>{p.throughputMbps}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}