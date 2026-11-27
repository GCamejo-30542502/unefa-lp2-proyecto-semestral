from __future__ import annotations

import asyncio
import re
from dataclasses import dataclass


@dataclass(slots=True, frozen=True)
class Transaccion:
    id_tx: str
    msisdn: str
    monto_centavos: int


@dataclass(slots=True, frozen=True)
class ResultadoValidacion:
    id_tx: str
    valida: bool
    errores: tuple[str, ...]


PATRON_MSISDN = re.compile(r"\+[1-9]\d{7,14}")


async def validar_transaccion(
    transaccion: Transaccion,
) -> ResultadoValidacion:
    # Punto de cesion para representar validaciones asincronas de I/O externas.
    await asyncio.sleep(0)
    errores: list[str] = []

    if not transaccion.id_tx.strip():
        errores.append("id_tx vacio")
    if PATRON_MSISDN.fullmatch(transaccion.msisdn) is None:
        errores.append("msisdn debe estar en formato internacional E.164")
    if transaccion.monto_centavos <= 0:
        errores.append("monto_centavos debe ser positivo")

    return ResultadoValidacion(
        id_tx=transaccion.id_tx,
        valida=not errores,
        errores=tuple(errores),
    )


async def validar_lote(
    transacciones: list[Transaccion],
) -> list[ResultadoValidacion]:
    return list(
        await asyncio.gather(
            *(validar_transaccion(transaccion) for transaccion in transacciones)
        )
    )


async def main() -> None:
    lote = [
        Transaccion("tx-001", "+584121000001", 120_000),
        Transaccion("tx-002", "+584121000002", 740_000),
        Transaccion("", "+584121000003", 85_000),
        Transaccion("tx-004", "04121000004", 920_000),
        Transaccion("tx-005", "+584121000005", 0),
    ]
    resultados = await validar_lote(lote)

    for resultado in resultados:
        estado = "VALIDA" if resultado.valida else "INVALIDA"
        detalle = ", ".join(resultado.errores) if resultado.errores else "sin errores"
        print(f"{resultado.id_tx or '<sin id>'}: {estado} ({detalle})")

    print(
        f"\nResumen: {sum(resultado.valida for resultado in resultados)} "
        f"validas de {len(resultados)}"
    )
    print(
        "asyncio permite concurrencia cooperativa para tareas de I/O; "
        "no paraleliza por si solo trabajo CPU-bound debido al GIL de CPython."
    )


if __name__ == "__main__":
    asyncio.run(main())
