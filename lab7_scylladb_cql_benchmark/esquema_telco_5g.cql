CREATE KEYSPACE IF NOT EXISTS unefa_telco_5g
WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'datacenter1': 3
};

USE unefa_telco_5g;

CREATE TABLE IF NOT EXISTS cdr_por_abonado_dia (
    msisdn text,
    bucket_fecha date,
    timestamp_evento timestamp,
    id_cdr timeuuid,
    tipo_red text,
    celda_id int,
    puerto_origen int,
    monto_centavos bigint,
    codigo_estado text,
    bandera_roaming boolean,
    PRIMARY KEY ((msisdn, bucket_fecha), timestamp_evento, id_cdr)
)
WITH CLUSTERING ORDER BY (timestamp_evento DESC, id_cdr ASC)
AND default_time_to_live = 2592000
AND compaction = {
    'class': 'TimeWindowCompactionStrategy',
    'compaction_window_unit': 'DAYS',
    'compaction_window_size': 1
};
