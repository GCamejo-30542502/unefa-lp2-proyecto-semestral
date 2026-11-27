#!/usr/bin/env bash
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
CC="${CC:-gcc}"
GO="${GO:-go}"
RUSTC="${RUSTC:-rustc}"
NODE="${NODE:-node}"
PHP="${PHP:-php}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

# En Windows "python3" suele ser un alias de Microsoft Store; se prefiere
# "python" cuando el primero no esta disponible.
if [ -z "${PYTHON:-}" ] || ! command -v "$PYTHON" >/dev/null 2>&1; then
    if command -v python3 >/dev/null 2>&1; then
        PYTHON="python3"
    elif command -v python >/dev/null 2>&1; then
        PYTHON="python"
    else
        printf 'Error: no se encontro Python 3 en PATH.\n' >&2
        exit 1
    fi
fi

for tool in "$CC" "$GO" "$RUSTC" "$NODE" "$PYTHON" "$PHP"; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        printf 'Error: no se encontro la herramienta "%s" en PATH.\n' "$tool" >&2
        exit 1
    fi
done

cd "$ROOT_DIR"

printf '%s\n' '== C: compilacion estricta y ejecucion =='
"$CC" -std=c11 -Wall -Wextra -Werror -O0 \
    lab1_memoria_y_complejidad/cdr_memory_profiler.c \
    -o "$TMP_DIR/cdr_profiler.exe"
"$TMP_DIR/cdr_profiler.exe"

printf '\n%s\n' '== Go: escape analysis del Lab 1 =='
"$GO" build -gcflags="-m" \
    -o "$TMP_DIR/escape_analyzer.exe" \
    lab1_memoria_y_complejidad/escape_analyzer.go

printf '\n%s\n' '== Go: go vet del Lab 2 =='
(
    cd lab2_arquitectura_hexagonal
    "$GO" vet ./...
)

printf '\n%s\n' '== Rust: compilacion optimizada y ejecucion =='
"$RUSTC" -O \
    lab4_backend_poliglota/rust_antifraude/motor_antifraude.rs \
    -o "$TMP_DIR/motor_antifraude.exe"
"$TMP_DIR/motor_antifraude.exe"

printf '\n%s\n' '== Go: worker pool con race detector =='
"$GO" run -race lab4_backend_poliglota/go_worker_pool/worker_pool.go

printf '\n%s\n' '== Node.js: worker criptografico =='
"$NODE" lab4_backend_poliglota/runtimes_alto_nivel/node_crypto_worker.mjs

printf '\n%s\n' '== Python: validador asincrono =='
"$PYTHON" lab4_backend_poliglota/runtimes_alto_nivel/python_async_validator.py

printf '\n%s\n' '== PHP: sintaxis y motor de tarifas =='
"$PHP" -l lab4_backend_poliglota/runtimes_alto_nivel/php_jit_tariff_engine.php
"$PHP" lab4_backend_poliglota/runtimes_alto_nivel/php_jit_tariff_engine.php
