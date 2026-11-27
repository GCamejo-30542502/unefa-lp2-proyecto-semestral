-- =====================================================================
-- Lab 6 - Paso 2.1: Normalización hasta BCNF + tabla Outbox
-- PostgreSQL 16
-- Ejecutar con:  psql -d lab6 -v ON_ERROR_STOP=1 -f 01_schema_bcnf_y_outbox.sql
-- =====================================================================

BEGIN;

-- ---------------------------------------------------------------------
-- 0. Limpieza idempotente (permite re-ejecutar el script)
-- ---------------------------------------------------------------------
DROP TABLE IF EXISTS outbox_events          CASCADE;
DROP TABLE IF EXISTS transacciones_bss      CASCADE;
DROP TABLE IF EXISTS abonados_bcnf          CASCADE;
DROP TABLE IF EXISTS catalogo_planes_bcnf   CASCADE;
DROP TABLE IF EXISTS facturacion_legada     CASCADE;

-- ---------------------------------------------------------------------
-- 1. TABLA LEGADA DESNORMALIZADA (punto de partida)
--
-- Dependencias funcionales que causan el problema:
--   id_factura  -> id_abonado, monto, fecha_factura       (correcta, es la PK)
--   id_abonado  -> nombre_abonado, email_abonado, id_plan (parcial/transitiva)
--   id_plan     -> nombre_plan, tarifa_mensual            (TRANSITIVA:
--                  id_factura -> id_abonado -> id_plan -> tarifa_mensual)
--   email_abonado -> id_abonado                           (otro determinante)
-- Anomalías: cambiar la tarifa de un plan obliga a actualizar miles de
-- filas (update), borrar la última factura de un abonado borra al abonado
-- (delete), no se puede registrar un plan sin facturas (insert).
-- ---------------------------------------------------------------------
CREATE TABLE facturacion_legada (
    id_factura       BIGINT        PRIMARY KEY,
    fecha_factura    TIMESTAMPTZ   NOT NULL,
    monto            NUMERIC(12,2) NOT NULL,
    id_abonado       INTEGER       NOT NULL,
    nombre_abonado   TEXT          NOT NULL,
    email_abonado    TEXT          NOT NULL,
    id_plan          INTEGER       NOT NULL,
    nombre_plan      TEXT          NOT NULL,
    tarifa_mensual   NUMERIC(12,2) NOT NULL,
    tipo_transaccion TEXT          NOT NULL,
    detalle_extra    TEXT
);

-- Datos de ejemplo para demostrar la migración
INSERT INTO facturacion_legada VALUES
 (1, '2026-01-05 10:00+00',  49.90, 101, 'Ana Torres',  'ana@mail.com',  1, 'Prepago Basico',   49.90, 'RECARGA',  'canal=app'),
 (2, '2026-01-20 11:30+00',  49.90, 101, 'Ana Torres',  'ana@mail.com',  1, 'Prepago Basico',   49.90, 'CARGO',    'canal=web'),
 (3, '2026-02-02 09:15+00',  89.90, 102, 'Luis Rojas',  'luis@mail.com', 2, 'Postpago Plus',    89.90, 'CARGO',    'canal=agente'),
 (4, '2026-02-14 16:45+00',  15.00, 102, 'Luis Rojas',  'luis@mail.com', 2, 'Postpago Plus',    89.90, 'AJUSTE',   'motivo=descuento'),
 (5, '2026-03-01 08:00+00', 129.90, 103, 'Marta Diaz',  'marta@mail.com',3, 'Postpago Premium', 129.90,'CARGO',    'canal=app');

-- ---------------------------------------------------------------------
-- 2. CATÁLOGO DE PLANES  (determinante: plan_id y codigo_plan, ambos
--    superclaves => BCNF)
-- ---------------------------------------------------------------------
CREATE TABLE catalogo_planes_bcnf (
    plan_id         INTEGER       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    codigo_plan     TEXT          NOT NULL UNIQUE,
    nombre_plan     TEXT          NOT NULL,
    tarifa_mensual  NUMERIC(12,2) NOT NULL CHECK (tarifa_mensual >= 0),
    activo          BOOLEAN       NOT NULL DEFAULT TRUE
);

-- ---------------------------------------------------------------------
-- 3. ABONADOS  (determinantes: abonado_id, documento, email: todos
--    superclaves => BCNF)
-- ---------------------------------------------------------------------
CREATE TABLE abonados_bcnf (
    abonado_id      INTEGER       PRIMARY KEY,
    documento       TEXT          NOT NULL UNIQUE,
    nombre_abonado  TEXT          NOT NULL,
    email_abonado   TEXT          NOT NULL UNIQUE,
    plan_id         INTEGER       NOT NULL
                    REFERENCES catalogo_planes_bcnf (plan_id)
                    ON UPDATE CASCADE ON DELETE RESTRICT,
    saldo_centavos  BIGINT        NOT NULL DEFAULT 0,   -- usado en la auditoría MVCC (02)
    creado_en       TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------
-- 4. TRANSACCIONES BSS  (hechos; solo depende de transaccion_id)
--    metadata_jsonb recibe los atributos semiestructurados (canal,
--    motivo, etiquetas...) que luego indexaremos con GIN.
-- ---------------------------------------------------------------------
CREATE TABLE transacciones_bss (
    transaccion_id    BIGINT        PRIMARY KEY,
    abonado_id        INTEGER       NOT NULL
                      REFERENCES abonados_bcnf (abonado_id)
                      ON UPDATE CASCADE ON DELETE RESTRICT,
    tipo_transaccion  TEXT          NOT NULL
                      CHECK (tipo_transaccion IN ('RECARGA','CARGO','AJUSTE')),
    monto             NUMERIC(12,2) NOT NULL,
    fecha_transaccion TIMESTAMPTZ   NOT NULL,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),  -- candidato a índice BRIN (02)
    metadata_jsonb    JSONB         NOT NULL
);

-- ---------------------------------------------------------------------
-- 5. OUTBOX (patrón Transactional Outbox, se usa en el Ejercicio 4)
--    Se escribe en la MISMA transacción que el cambio de negocio; un
--    relay publica luego los eventos pendientes (published_at IS NULL).
-- ---------------------------------------------------------------------
CREATE TABLE outbox_events (
    event_id        UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type  TEXT         NOT NULL,
    aggregate_id    TEXT         NOT NULL,
    event_type      TEXT         NOT NULL,
    payload         JSONB        NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ
);

-- Índice parcial: el relay solo consulta eventos pendientes
CREATE INDEX idx_outbox_pendientes
    ON outbox_events (created_at)
    WHERE published_at IS NULL;

-- ---------------------------------------------------------------------
-- 6. MIGRACIÓN desde la tabla legada
-- ---------------------------------------------------------------------
-- 6.1 Planes (DISTINCT elimina la repetición)
INSERT INTO catalogo_planes_bcnf (codigo_plan, nombre_plan, tarifa_mensual)
SELECT DISTINCT
       'PLAN-' || id_plan, nombre_plan, tarifa_mensual
FROM   facturacion_legada
ORDER  BY 1;

-- 6.2 Abonados (documento sintético porque el legado no lo tenía)
INSERT INTO abonados_bcnf (abonado_id, documento, nombre_abonado, email_abonado, plan_id)
SELECT DISTINCT
       f.id_abonado,
       'DOC-' || f.id_abonado,
       f.nombre_abonado,
       f.email_abonado,
       p.plan_id
FROM   facturacion_legada f
JOIN   catalogo_planes_bcnf p ON p.codigo_plan = 'PLAN-' || f.id_plan;

-- 6.3 Transacciones (detalle_extra "k=v" -> JSONB)
INSERT INTO transacciones_bss
       (transaccion_id, abonado_id, tipo_transaccion, monto,
        fecha_transaccion, created_at, metadata_jsonb)
SELECT id_factura,
       id_abonado,
       tipo_transaccion,
       monto,
       fecha_factura,
       fecha_factura,
       jsonb_build_object(
           split_part(detalle_extra, '=', 1),
           split_part(detalle_extra, '=', 2),
           'origen', 'migracion_legada'
       )
FROM   facturacion_legada;

-- ---------------------------------------------------------------------
-- 7. Verificación dentro de la transacción (si falla, ROLLBACK total)
-- ---------------------------------------------------------------------
DO $$
DECLARE
    n_leg INT; n_tx INT;
BEGIN
    SELECT count(*) INTO n_leg FROM facturacion_legada;
    SELECT count(*) INTO n_tx  FROM transacciones_bss;
    IF n_leg <> n_tx THEN
        RAISE EXCEPTION 'Migración incompleta: legada=% normalizada=%', n_leg, n_tx;
    END IF;
END $$;

COMMIT;

-- Comprobación rápida (fuera de la transacción)
SELECT a.nombre_abonado, p.nombre_plan, t.tipo_transaccion, t.monto, t.metadata_jsonb
FROM   transacciones_bss t
JOIN   abonados_bcnf a        USING (abonado_id)
JOIN   catalogo_planes_bcnf p USING (plan_id)
ORDER  BY t.transaccion_id;
