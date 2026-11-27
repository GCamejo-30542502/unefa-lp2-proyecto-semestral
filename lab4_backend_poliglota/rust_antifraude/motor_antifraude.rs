use std::sync::{Arc, Mutex};
use std::thread;

const CANTIDAD_HILOS: usize = 4;
const LIMITE_RIESGO_CENTAVOS: u64 = 500_000;

#[derive(Clone, Copy)]
struct Transaccion<'a> {
    id_tx: &'a str,
    msisdn: &'a str,
    monto_centavos: u64,
}

struct AlertaFraude<'a> {
    id_tx: &'a str,
    msisdn: &'a str,
    motivo: &'a str,
    monto_centavos: u64,
}

#[derive(Default)]
struct EstadisticasRiesgo {
    transacciones_auditadas: u64,
    alertas_emitidas: u64,
    monto_en_alerta_centavos: u64,
}

const LOTE: [Transaccion<'static>; 12] = [
    Transaccion {
        id_tx: "tx-001",
        msisdn: "+584121000001",
        monto_centavos: 120_000,
    },
    Transaccion {
        id_tx: "tx-002",
        msisdn: "+584121000002",
        monto_centavos: 740_000,
    },
    Transaccion {
        id_tx: "tx-003",
        msisdn: "+584121000003",
        monto_centavos: 85_000,
    },
    Transaccion {
        id_tx: "tx-004",
        msisdn: "+584121000004",
        monto_centavos: 920_000,
    },
    Transaccion {
        id_tx: "tx-005",
        msisdn: "+584121000005",
        monto_centavos: 315_000,
    },
    Transaccion {
        id_tx: "tx-006",
        msisdn: "+584121000006",
        monto_centavos: 500_000,
    },
    Transaccion {
        id_tx: "tx-007",
        msisdn: "+584121000007",
        monto_centavos: 46_000,
    },
    Transaccion {
        id_tx: "tx-008",
        msisdn: "+584121000008",
        monto_centavos: 1_200_000,
    },
    Transaccion {
        id_tx: "tx-009",
        msisdn: "+584121000009",
        monto_centavos: 215_000,
    },
    Transaccion {
        id_tx: "tx-010",
        msisdn: "+584121000010",
        monto_centavos: 650_000,
    },
    Transaccion {
        id_tx: "tx-011",
        msisdn: "+584121000011",
        monto_centavos: 175_000,
    },
    Transaccion {
        id_tx: "tx-012",
        msisdn: "+584121000012",
        monto_centavos: 510_000,
    },
];

fn ejecutar() -> Result<(), String> {
    let estadisticas = Arc::new(Mutex::new(EstadisticasRiesgo::default()));
    let tamano_lote_por_hilo = LOTE.len().div_ceil(CANTIDAD_HILOS);
    let mut hilos = Vec::with_capacity(CANTIDAD_HILOS);

    for (indice, lote_hilo) in LOTE.chunks(tamano_lote_por_hilo).enumerate() {
        let estadisticas_compartidas = Arc::clone(&estadisticas);
        let transacciones = lote_hilo.to_vec();

        hilos.push(thread::spawn(move || -> Result<(), String> {
            let mut estadisticas_locales = EstadisticasRiesgo::default();

            for transaccion in transacciones {
                estadisticas_locales.transacciones_auditadas += 1;

                if transaccion.monto_centavos >= LIMITE_RIESGO_CENTAVOS {
                    let alerta = AlertaFraude {
                        id_tx: transaccion.id_tx,
                        msisdn: transaccion.msisdn,
                        motivo: "monto igual o superior al umbral de riesgo",
                        monto_centavos: transaccion.monto_centavos,
                    };

                    println!(
                        "[hilo {indice}] alerta {} para {}: {} centavos ({})",
                        alerta.id_tx,
                        alerta.msisdn,
                        alerta.monto_centavos,
                        alerta.motivo
                    );
                    estadisticas_locales.alertas_emitidas += 1;
                    estadisticas_locales.monto_en_alerta_centavos += alerta.monto_centavos;
                }
            }

            let mut compartidas = estadisticas_compartidas
                .lock()
                .map_err(|_| "mutex de estadisticas envenenado".to_string())?;
            compartidas.transacciones_auditadas += estadisticas_locales.transacciones_auditadas;
            compartidas.alertas_emitidas += estadisticas_locales.alertas_emitidas;
            compartidas.monto_en_alerta_centavos +=
                estadisticas_locales.monto_en_alerta_centavos;

            Ok(())
        }));
    }

    if hilos.len() != CANTIDAD_HILOS {
        return Err(format!(
            "se esperaban {CANTIDAD_HILOS} hilos, se lanzaron {}",
            hilos.len()
        ));
    }

    for hilo in hilos {
        hilo.join()
            .map_err(|_| "un hilo auditor entro en panico".to_string())??;
    }

    let estadisticas = Arc::try_unwrap(estadisticas)
        .map_err(|_| "quedaron referencias compartidas a las estadisticas".to_string())?
        .into_inner()
        .map_err(|_| "mutex de estadisticas envenenado".to_string())?;

    println!("\nResumen del lote:");
    println!(
        "  Transacciones auditadas: {}",
        estadisticas.transacciones_auditadas
    );
    println!("  Alertas emitidas: {}", estadisticas.alertas_emitidas);
    println!(
        "  Monto total en alerta: {} centavos",
        estadisticas.monto_en_alerta_centavos
    );

    Ok(())
}

fn main() {
    if let Err(error) = ejecutar() {
        eprintln!("Error en el motor antifraude: {error}");
        std::process::exit(1);
    }
}
