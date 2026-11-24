#!/usr/bin/env bash
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
LAB_DIR="$ROOT_DIR/lab1_memoria_y_complejidad"
CC="${CC:-gcc}"

if ! command -v "$CC" >/dev/null 2>&1; then
    printf 'Error: no se encontro el compilador C "%s".\n' "$CC" >&2
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    printf 'Error: no se encontro Go en PATH.\n' >&2
    exit 1
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

printf '%s\n' '== Compilacion estricta y ejecucion C =='
"$CC" -std=c11 -Wall -Wextra -Werror -O0 \
    "$LAB_DIR/cdr_memory_profiler.c" -o "$TMP_DIR/cdr_profiler"
"$TMP_DIR/cdr_profiler"

printf '\n%s\n' '== Escape analysis de Go =='
go build -gcflags="-m" -o "$TMP_DIR/escape_analyzer" \
    "$LAB_DIR/escape_analyzer.go"

printf '\n%s\n' '== Benchmark Go =='
go run "$LAB_DIR/escape_analyzer.go"
