-- =====================================================================
-- Lab 6 - Paso 2.2: Auditoría MVCC e índices GIN / BRIN / B-Tree
-- PostgreSQL 16  |  REQUIERE psql (usa \gset y \timing)
--
-- Prerrequisito: haber ejecutado 01_schema_bcnf_y_outbox.sql
-- Ejecutar y guardar la salida para el reporte:
--   psql -d lab6 -v ON_ERROR_STOP=1 -f 02_auditoria_mvcc_e_indices.sql \
--        > salida_02.txt 2>&1
-- (con Docker: docker exec -i pg16 psql -U postgres -d lab6 \
--              -v ON_ERROR_STOP=1 < 02_auditoria_mvcc_e_indices.sql > salida_02.txt 2>&1)
-- =====================================================================

\set ON_ERROR_STOP on
\pset pager off
\timing on

-- pageinspect permite leer las páginas físicas del heap (requiere superusuario)
CREATE EXTENSION IF NOT EXISTS pageinspect;

-- Para que los planes sean comparables entre sí (sin paralelismo ni JIT)
SET max_parallel_workers_per_gather = 0;
SET jit = off;

-- =====================================================================
-- PARTE 1. CARGA MASIVA CON generate_series
--   50.000 abonados  +  500.000 transacciones
-- =====================================================================
\echo '=== PARTE 1: carga masiva ==='

-- Limpieza de una ejecución previa (conserva las 3 filas migradas en 01)
DELETE FROM transacciones_bss WHERE transaccion_id >= 1000;
DELETE FROM abonados_bcnf     WHERE abonado_id    >= 10000;
DROP INDEX IF EXISTS idx_tx_metadata_gin;
DROP INDEX IF EXISTS idx_tx_created_brin;
DROP INDEX IF EXISTS idx_tx_abonado_fecha_btree;

-- 1.1 Dos planes extra para tener variedad
INSERT INTO catalogo_planes_bcnf (codigo_plan, nombre_plan, tarifa_mensual)
VALUES ('PLAN-4', 'Datos Ilimitados', 59.90),
       ('PLAN-5', 'Corporativo',     199.90)
ON CONFLICT (codigo_plan) DO NOTHING;

-- 1.2 50.000 abonados
WITH p AS (
    SELECT array_agg(plan_id ORDER BY plan_id) AS arr
    FROM   catalogo_planes_bcnf
)
INSERT INTO abonados_bcnf
       (abonado_id, documento, nombre_abonado, email_abonado, plan_id, saldo_centavos)
SELECT 10000 + i,
       'DOC-'  || (10000 + i),
       'Abonado ' || i,
       'user'  || i || '@lab6.test',
       p.arr[1 + (i % array_length(p.arr, 1))],
       (random() * 100000)::bigint
FROM   generate_series(1, 50000) AS i, p;

-- 1.3 500.000 transacciones, insertadas en orden cronológico
--     (esto da correlación física alta -> condición ideal para BRIN)
--     120 antenas distintas: 6 zonas x 20 numeraciones. 'CCS-NORTE-01'
--     aparece en ~1/120 de las filas, y roaming=true en ~15 % de ellas.
INSERT INTO transacciones_bss
       (transaccion_id, abonado_id, tipo_transaccion, monto,
        fecha_transaccion, created_at, metadata_jsonb)
SELECT 1000 + i,
       10001 + (i % 50000),
       (ARRAY['RECARGA','CARGO','AJUSTE'])[1 + (i % 3)],
       round((random() * 100)::numeric, 2),
       timestamptz '2026-01-01 00:00+00' + i * interval '1 minute',
       timestamptz '2026-01-01 00:00+00' + i * interval '1 minute',
       jsonb_build_object(
           'antena_5g',
               (ARRAY['CCS-NORTE','CCS-SUR','CCS-ESTE','CCS-OESTE','VLN-CENTRO','MCB-NORTE'])[1 + (i % 6)]
               || '-' || lpad((1 + (i / 6) % 20)::text, 2, '0'),
           'roaming',  (random() < 0.15),
           'canal',    (ARRAY['app','web','agente','ussd'])[1 + (i % 4)],
           'imei',     md5(i::text)
       )
FROM   generate_series(1, 500000) AS i;

ANALYZE abonados_bcnf;
ANALYZE transacciones_bss;

SELECT 'abonados_bcnf'     AS tabla, count(*) AS filas FROM abonados_bcnf
UNION ALL
SELECT 'transacciones_bss',          count(*)          FROM transacciones_bss;

-- =====================================================================
-- PARTE 2. INSPECCIÓN FORENSE MVCC
--   Demostrar que un UPDATE NO modifica la tupla: crea una NUEVA versión.
-- =====================================================================
\echo '=== PARTE 2: forense MVCC ==='

-- 2.1 Estado ANTES del UPDATE (guardamos el bloque físico en :blk_viejo)
\echo '--- 2.1 ANTES del UPDATE: ctid / xmin / xmax ---'
SELECT ctid, xmin, xmax, saldo_centavos
FROM   abonados_bcnf
WHERE  abonado_id = 10001;

SELECT (ctid::text::point)[0]::int AS blk_viejo
FROM   abonados_bcnf WHERE abonado_id = 10001 \gset

-- 2.2 UPDATE dentro de una transacción explícita
BEGIN;

SELECT txid_current() AS xid_update \gset
\echo '--- XID de la transacción que hace el UPDATE: ' :xid_update

UPDATE abonados_bcnf
SET    saldo_centavos = saldo_centavos + 5000
WHERE  abonado_id = 10001;

\echo '--- 2.2 DESPUÉS del UPDATE (misma transacción) ---'
SELECT ctid, xmin, xmax, saldo_centavos
FROM   abonados_bcnf
WHERE  abonado_id = 10001;

SELECT (ctid::text::point)[0]::int AS blk_nuevo
FROM   abonados_bcnf WHERE abonado_id = 10001 \gset

COMMIT;

-- Observa: el ctid CAMBIÓ (otra posición física) y xmin = :xid_update.
-- La versión vieja sigue existiendo en disco pero ya no es visible.

-- 2.3 Mirar el heap directamente con pageinspect: aparecen LAS DOS versiones
--     versión vieja: t_xmax = xid del UPDATE   (la "mató")
--     versión nueva: t_xmin = xid del UPDATE   (la "creó")
\echo '--- 2.3 Páginas físicas (pageinspect): ambas versiones de la tupla ---'
SELECT b.blk                         AS bloque,
       hp.lp                         AS lp,
       CASE hp.lp_flags WHEN 0 THEN 'UNUSED' WHEN 1 THEN 'NORMAL'
                        WHEN 2 THEN 'REDIRECT' WHEN 3 THEN 'DEAD' END AS lp_estado,
       hp.t_xmin::text::bigint       AS t_xmin,
       hp.t_xmax::text::bigint       AS t_xmax,
       hp.t_ctid                     AS t_ctid_apunta_a,
       CASE WHEN hp.t_xmax::text::bigint = :xid_update THEN 'VERSION VIEJA (muerta)'
            WHEN hp.t_xmin::text::bigint = :xid_update THEN 'VERSION NUEVA (viva)' END AS rol
FROM  (SELECT :blk_viejo AS blk UNION SELECT :blk_nuevo) AS b,
      LATERAL heap_page_items(get_raw_page('abonados_bcnf', b.blk)) AS hp
WHERE hp.t_xmax::text::bigint = :xid_update
   OR hp.t_xmin::text::bigint = :xid_update
ORDER BY hp.lp;

-- 2.4 Tuplas muertas acumuladas y limpieza con VACUUM
SELECT pg_sleep(1);  -- da tiempo a que se actualicen las estadísticas
\echo '--- 2.4 Tuplas muertas ANTES de VACUUM ---'
SELECT relname, n_live_tup, n_dead_tup
FROM   pg_stat_user_tables
WHERE  relname = 'abonados_bcnf';

VACUUM (VERBOSE) abonados_bcnf;

\echo '--- 2.4 Misma página DESPUÉS de VACUUM (la versión vieja se recicla) ---'
SELECT hp.lp,
       CASE hp.lp_flags WHEN 0 THEN 'UNUSED' WHEN 1 THEN 'NORMAL'
                        WHEN 2 THEN 'REDIRECT' WHEN 3 THEN 'DEAD' END AS lp_estado,
       hp.t_xmin::text::bigint AS t_xmin,
       hp.t_xmax::text::bigint AS t_xmax
FROM   heap_page_items(get_raw_page('abonados_bcnf', :blk_viejo)) AS hp
WHERE  hp.lp_flags <> 1 OR hp.t_xmax::text::bigint <> 0
ORDER  BY hp.lp;

-- =====================================================================
-- PARTE 3. LÍNEA BASE: EXPLAIN (ANALYZE, BUFFERS) SIN ÍNDICES
--   Se ejecuta 2 veces cada consulta; reporta la SEGUNDA (caché caliente).
-- =====================================================================
\echo '=== PARTE 3: baseline sin índices ==='

\echo '--- 3.1 Consulta A (JSONB @>) SIN índice GIN: 1ª ejecución ---'
EXPLAIN (ANALYZE, BUFFERS)
SELECT count(*)
FROM   transacciones_bss
WHERE  metadata_jsonb @> '{"antena_5g": "CCS-NORTE-01", "roaming": true}';

\echo '--- 3.1 Consulta A SIN índice GIN: 2ª ejecución (la que se reporta) ---'
EXPLAIN (ANALYZE, BUFFERS)
SELECT count(*)
FROM   transacciones_bss
WHERE  metadata_jsonb @> '{"antena_5g": "CCS-NORTE-01", "roaming": true}';

\echo '--- 3.2 Consulta B (rango de un día en created_at) SIN BRIN ---'
EXPLAIN (ANALYZE, BUFFERS)
SELECT count(*)
FROM   transacciones_bss
WHERE  created_at >= '2026-06-15 00:00+00'
  AND  created_at <  '2026-06-16 00:00+00';

-- =====================================================================
-- PARTE 4. CREACIÓN DE ÍNDICES
-- =====================================================================
\echo '=== PARTE 4: creación de índices ==='

-- 4.1 GIN invertido: jsonb_path_ops es más pequeño y rápido para @>
CREATE INDEX idx_tx_metadata_gin
    ON transacciones_bss USING GIN (metadata_jsonb jsonb_path_ops);

-- 4.2 BRIN: guarda solo min/max por rango de 32 páginas; diminuto.
--     Funciona porque created_at está correlacionado con el orden físico.
CREATE INDEX idx_tx_created_brin
    ON transacciones_bss USING BRIN (created_at)
    WITH (pages_per_range = 32);

-- 4.3 B-Tree compuesto (FK + tiempo): para búsquedas puntuales por abonado
CREATE INDEX idx_tx_abonado_fecha_btree
    ON transacciones_bss USING BTREE (abonado_id, created_at);

ANALYZE transacciones_bss;

-- 4.4 Comparación de tamaños (insumo para el reporte)
\echo '--- 4.4 Tamaño de tabla e índices ---'
SELECT 'tabla transacciones_bss'                           AS objeto,
       pg_size_pretty(pg_relation_size('transacciones_bss'))      AS tamano
UNION ALL SELECT 'GIN   idx_tx_metadata_gin',
       pg_size_pretty(pg_relation_size('idx_tx_metadata_gin'))
UNION ALL SELECT 'BRIN  idx_tx_created_brin',
       pg_size_pretty(pg_relation_size('idx_tx_created_brin'))
UNION ALL SELECT 'BTREE idx_tx_abonado_fecha_btree',
       pg_size_pretty(pg_relation_size('idx_tx_abonado_fecha_btree'));

-- =====================================================================
-- PARTE 5. EXPLAIN (ANALYZE, BUFFERS) CON ÍNDICES
-- =====================================================================
\echo '=== PARTE 5: con índices ==='

\echo '--- 5.1 Consulta A (JSONB @>) CON GIN: 1ª ejecución ---'
EXPLAIN (ANALYZE, BUFFERS)
SELECT count(*)
FROM   transacciones_bss
WHERE  metadata_jsonb @> '{"antena_5g": "CCS-NORTE-01", "roaming": true}';

\echo '--- 5.1 Consulta A CON GIN: 2ª ejecución (la que se reporta) ---'
EXPLAIN (ANALYZE, BUFFERS)
SELECT count(*)
FROM   transacciones_bss
WHERE  metadata_jsonb @> '{"antena_5g": "CCS-NORTE-01", "roaming": true}';

\echo '--- 5.2 Consulta B (rango created_at) CON BRIN ---'
-- Si el planificador prefiere el B-Tree compuesto, desactívalo SOLO para
-- esta demostración quitando el comentario de la línea siguiente:
-- SET enable_indexscan = off;
EXPLAIN (ANALYZE, BUFFERS)
SELECT count(*)
FROM   transacciones_bss
WHERE  created_at >= '2026-06-15 00:00+00'
  AND  created_at <  '2026-06-16 00:00+00';

\echo '--- 5.3 Consulta C (por abonado) CON B-Tree ---'
EXPLAIN (ANALYZE, BUFFERS)
SELECT transaccion_id, monto, created_at
FROM   transacciones_bss
WHERE  abonado_id = 10001
ORDER  BY created_at DESC
LIMIT  10;

\echo '=== FIN: copia los planes al reporte_explain_buffers.md ==='
