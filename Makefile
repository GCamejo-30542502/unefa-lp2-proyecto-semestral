CC ?= gcc
GO ?= go
RUSTC ?= rustc
NODE ?= node
PHP ?= php
# Vacio: run_all_checks.sh detecta "python3" o "python" segun el sistema.
PYTHON ?=

.PHONY: all check

all: check

check:
	@CC="$(CC)" GO="$(GO)" RUSTC="$(RUSTC)" NODE="$(NODE)" PYTHON="$(PYTHON)" PHP="$(PHP)" bash run_all_checks.sh