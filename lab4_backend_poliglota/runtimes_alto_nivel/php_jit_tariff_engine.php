<?php
declare(strict_types=1);

enum TipoRed: string
{
    case G2 = '2G';
    case G4 = '4G';
    case G5 = '5G';
}

final readonly class CalculadorTarifas
{
    public function calcular(int $duracionSegundos, TipoRed $tipoRed): int
    {
        if ($duracionSegundos < 0) {
            throw new InvalidArgumentException('La duracion no puede ser negativa.');
        }

        $centavosPorSegundo = match ($tipoRed) {
            TipoRed::G2 => 3,
            TipoRed::G4 => 5,
            TipoRed::G5 => 8,
        };

        if ($duracionSegundos > intdiv(PHP_INT_MAX, $centavosPorSegundo)) {
            throw new OverflowException('El costo excede el rango entero disponible.');
        }

        return $duracionSegundos * $centavosPorSegundo;
    }
}

$calculador = new CalculadorTarifas();
foreach (TipoRed::cases() as $tipoRed) {
    $costo = $calculador->calcular(120, $tipoRed);
    printf("%s: %d centavos por 120 segundos\n", $tipoRed->value, $costo);
}
