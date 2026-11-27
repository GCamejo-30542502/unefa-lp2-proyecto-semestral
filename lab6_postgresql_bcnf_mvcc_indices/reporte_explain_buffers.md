# Reporte Lab 6: MVCC forense e índices GIN / BRIN / B-Tree (PostgreSQL 16)

> Completa los campos `<...>` con la salida real de `salida_02.txt`.
> Reporta siempre la **segunda** ejecución (caché caliente) y anota la versión
> exacta con `SELECT version();`.

## 0. Entorno

| Dato | Valor |
|---|---|
| Versión PostgreSQL | `<SELECT version()>` |
| Forma de ejecución | `<Docker / local>` |
| `shared_buffers` | `<SHOW shared_buffers>` |
| Filas `abonados_bcnf` | 50.000 (+3 migradas) |
| Filas `transacciones_bss` | 500.000 (+5 migradas) |
| Ajustes de sesión | `max_parallel_workers_per_gather = 0`, `jit = off` |

---

## 1. Inspección forense MVCC

### 1.1 Evidencia (`ctid`, `xmin`, `xmax`)

| Momento | ctid | xmin | xmax | saldo_centavos |
|---|---|---|---|---|
| Antes del UPDATE | `<(blk,lp)>` | `<xid>` | `0` | `<saldo>` |
| Después del UPDATE | `<(blk,lp) distinto>` | `<xid_update>` | `0` | `<saldo + 5000>` |

### 1.2 Vista física con `pageinspect`

| lp | estado | t_xmin | t_xmax | t_ctid | rol |
|---|---|---|---|---|---|
| `<lp>` | NORMAL | `<xid>` | `<xid_update>` | `<apunta a la nueva>` | versión vieja |
| `<lp>` | NORMAL | `<xid_update>` | 0 | `<se apunta a sí misma>` | versión nueva |

### 1.3 Explicación (completar con tus palabras)

- El `UPDATE` **no sobrescribe** la fila: inserta una nueva versión física y marca la anterior con `xmax = <xid_update>`.
- El `ctid` cambió porque la nueva versión ocupa otra posición del heap.
- Cada transacción decide qué versión ve comparando su snapshot con `xmin`/`xmax`; por eso los lectores no bloquean a los escritores.
- Antes de `VACUUM`: `n_dead_tup = <valor>`. Después: la versión vieja pasó a `<UNUSED/REDIRECT/DEAD>`.
- Si fue un **HOT update** (misma página, columna sin índice), explica por qué aparece un `REDIRECT`.

---

## 2. Consulta A: filtro JSONB `@>`

```sql
SELECT count(*)
FROM transacciones_bss
WHERE metadata_jsonb @> '{"antena_5g": "CCS-NORTE-01", "roaming": true}';
```

### 2.1 Antes del índice (Seq Scan)

```
<pegar aquí el EXPLAIN (ANALYZE, BUFFERS) completo, 2ª ejecución>
```

### 2.2 Después de `idx_tx_metadata_gin` (Bitmap Index Scan)

```
<pegar aquí el EXPLAIN (ANALYZE, BUFFERS) completo, 2ª ejecución>
```

### 2.3 Comparación

| Métrica | Sin índice | Con GIN | Mejora |
|---|---|---|---|
| Nodo principal | Seq Scan | Bitmap Index Scan -> Bitmap Heap Scan | n/a |
| Execution Time (ms) | `<>` | `<>` | `<x veces>` |
| Planning Time (ms) | `<>` | `<>` | n/a |
| `Buffers: shared hit` | `<>` | `<>` | `<>` |
| `Buffers: shared read` | `<>` | `<>` | `<>` |
| Filas descartadas por filtro (`Rows Removed by Filter`) | `<>` | `<>` | n/a |
| Costo estimado (`cost=`) | `<>` | `<>` | n/a |

**Análisis:** explica por qué el `Seq Scan` lee todas las páginas de la tabla (`Buffers` ≈ páginas de la relación) y por qué el GIN con `jsonb_path_ops` solo toca las páginas que contienen los pares clave/valor buscados. Comenta el `Recheck Cond` y si hubo `Heap Blocks: exact` o `lossy`.

---

## 3. Consulta B: rango temporal con BRIN

```sql
SELECT count(*)
FROM transacciones_bss
WHERE created_at >= '2026-06-15 00:00+00' AND created_at < '2026-06-16 00:00+00';
```

| Métrica | Sin índice | Con BRIN |
|---|---|---|
| Nodo principal | Seq Scan | Bitmap Index Scan (BRIN) |
| Execution Time (ms) | `<>` | `<>` |
| `shared hit + read` | `<>` | `<>` |

```
<EXPLAIN sin índice>
```

```
<EXPLAIN con BRIN>
```

**Análisis:** el BRIN solo guarda min/max por cada 32 páginas. Funciona porque las filas se insertaron en orden de `created_at` (correlación física alta). Verifica con:
`SELECT correlation FROM pg_stats WHERE tablename='transacciones_bss' AND attname='created_at';`

---

## 4. Consulta C: B-Tree (abonado_id, created_at)

```
<EXPLAIN de la consulta C: Index Scan / Index Only Scan>
```

---

## 5. Tamaño de los índices

| Objeto | Tipo | Tamaño |
|---|---|---|
| `transacciones_bss` (heap) | n/a | `<>` |
| `idx_tx_metadata_gin` | GIN (jsonb_path_ops) | `<>` |
| `idx_tx_created_brin` | BRIN (32 págs/rango) | `<>` |
| `idx_tx_abonado_fecha_btree` | B-Tree | `<>` |

**Conclusión de tamaños:** compara cuántas veces más pequeño es el BRIN que el B-Tree y qué costo en escritura/espacio tiene el GIN.

---

## 6. Conclusiones

1. MVCC: `<resumen de 2-3 líneas>`
2. GIN vs Seq Scan: `<resumen con el factor de mejora real>`
3. Cuándo usar cada índice (GIN / BRIN / B-Tree): `<tu criterio>`
4. Costos: más índices implican más escritura por `INSERT`/`UPDATE` y más `VACUUM`.
