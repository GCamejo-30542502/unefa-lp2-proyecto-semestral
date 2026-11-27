# Auditoría Comparativa: React 19 (Next.js 15) vs. Svelte 5 (Runes)

Panel de telemetría de celdas 5G implementado en dos arquitecturas reactivas.
Se audita su impacto en el Critical Rendering Path.

## 1. Presupuesto de cuadro (Frame Budget)

El presupuesto por cuadro es el inverso de la tasa de refresco:

$$t_{frame} = \frac{1000\ \text{ms}}{f_{Hz}}$$

| Tasa de refresco | Cálculo | Presupuesto por cuadro |
|------------------|---------|------------------------|
| 60 Hz  | $\dfrac{1000}{60}$  | $16.67\ \text{ms}$ |
| 120 Hz | $\dfrac{1000}{120}$ | $8.33\ \text{ms}$  |

El navegador necesita parte de ese tiempo para Style, Layout, Paint y Composite
(aprox. 4–6 ms). El presupuesto efectivo para JavaScript queda así:

$$t_{JS} \approx t_{frame} - t_{render\ pipeline}$$

| Tasa | Presupuesto total | Presupuesto JS aproximado |
|------|-------------------|---------------------------|
| 60 Hz  | 16.67 ms | ~10–12 ms |
| 120 Hz | 8.33 ms  | ~2–4 ms   |

**Criterio de evaluación:** si el "Tiempo de actualización" que muestra cada panel
supera $16.67\ \text{ms}$, se pierde al menos un cuadro a 60 Hz. Si supera
$8.33\ \text{ms}$, se pierde al menos un cuadro a 120 Hz.

## 2. Complejidad de actualización

Sea $N$ el número de nodos del subárbol afectado y $W$ el tamaño de la ventana
deslizante (200 paquetes).

| Aspecto | React 19 (VDOM) | Svelte 5 (Runes / DAG de señales) |
|---------|-----------------|-----------------------------------|
| Modelo | Re-render del componente y diff del árbol virtual | Grafo acíclico dirigido (DAG) de señales; el compilador genera actualizaciones directas al DOM |
| Disparo | `setPackets` re-ejecuta la función del componente | La asignación a `$state` notifica solo a sus dependientes |
| Reconciliación | Diffing heurístico: $O(N)$ sobre el árbol virtual | Propagación por el grafo: $O(1)$ por señal, sin diff de árbol |
| Memorización | Manual (`useMemo` con array de dependencias) | Automática (`$derived.by` rastrea dependencias) |
| Cálculo de métricas | $O(W)$ (reduce + filter) | $O(W)$ (reduce + filter) |

> **Nota de rigor:** el cálculo de métricas (promedio y conteo crítico) es $O(W)$
> en ambos frameworks, porque recorre la ventana. La diferencia de $O(N)$ vs.
> $O(1)$ aplica a **cómo se propaga el cambio al DOM**: React recorre y compara el
> árbol virtual completo del componente; Svelte actualiza solo los nodos enlazados
> a la señal modificada. La lista de 20 filas (`{#each}` / `.map`) sigue teniendo
> un costo proporcional al número de filas que cambian.

## 3. Tabla comparativa de mediciones

Mediciones en **modo producción** (`npm run build` + `npm start` / `npm run preview`),
Chrome en ventana de incógnito, sin extensiones.

| Métrica | `TelemetryPanelClient.tsx` (React 19 / Next.js 15) | `+page.svelte` (Svelte 5) |
|---------|----------------------------------------------------|---------------------------|
| Complejidad de actualización | $O(N)$ VDOM diffing | $O(1)$ DAG de señales |
| Tamaño bundle JS (gzip) | _____ kB | _____ kB |
| Tiempo de hidratación | _____ ms | _____ ms |
| INP (clic en "Inyectar ráfaga") | _____ ms | _____ ms |
| Tiempo de actualización (promedio de 10 clics) | _____ ms | _____ ms |
| ¿Cabe en 16.67 ms (60 Hz)? | Sí / No | Sí / No |
| ¿Cabe en 8.33 ms (120 Hz)? | Sí / No | Sí / No |

### Registro por ráfagas sucesivas (ventana deslizante)

| Escenario | Paquetes por ráfaga | Paquetes en ventana | React 19 (`performance.now()` ms) | Svelte 5 (`performance.now()` ms) | Observaciones |
|-----------|---------------------|---------------------|------------------------------------|-----------------------------------|--------------|
| Ráfaga 1 | 100 | 100 | _____ ms | _____ ms | Inicio de ventana |
| Ráfaga 2 | 100 | 200 | _____ ms | _____ ms | Ventana al límite (`WINDOW_SIZE = 200`) |
| Ráfaga 3 | 100 | 200 | _____ ms | _____ ms | Desplazamiento continuo con `.slice(-200)` |

### Cómo obtener cada dato

- **Bundle JS:** al terminar `npm run build`, la consola lista el tamaño por ruta
  (Next.js muestra "First Load JS"). En Svelte, revisa `.svelte-kit/output/client`
  o la pestaña **Network** de DevTools filtrando por JS (columna *Size*, gzip).
- **Tiempo de hidratación:** DevTools → **Performance** → grabar recarga de la página
  y medir la tarea de hidratación (en React aparece como `hydrateRoot`).
- **INP:** DevTools → **Performance** (panel *Interactions*) o la extensión
  *Web Vitals*. Umbrales de Google: bueno $\le 200\ \text{ms}$, mejorable
  $\le 500\ \text{ms}$, deficiente $> 500\ \text{ms}$.
- **Tiempo de actualización:** el valor que muestra la propia interfaz; haz 10 clics
  y promedia.

## 4. Análisis

_(Completa tras medir.)_

- ¿Qué componente respetó el presupuesto de 8.33 ms?
- ¿Cuánto del tiempo corresponde a JS y cuánto a Layout/Paint?
- ¿Cómo cambia el resultado al aumentar `BURST_SIZE` y `WINDOW_SIZE`?
- ¿Qué impacto tuvo el tamaño del bundle en la hidratación y en el INP?

## 5. Conclusión

_(Resume en 3–4 líneas qué arquitectura rindió mejor para este caso de uso y por qué.)_
