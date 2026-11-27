UNEFA — Lenguaje de Programación II ("unefa-1p2-proyecto-semestral")
Universidad Nacional Experimental Politécnica de la Fuerza Armada Nacional Bolivariana (UNEFA)  
Carrera: Ingeniería de Sistemas | Semestre: 5° Semestre — Período 2-2026  
Sección SICEU: 05S-2629-D1 

Integrantes del Equipo

Integrante 1 |Requena Efren	        |V-31.312.056         |@ERequena-31312056|
Integrante 2 |Camejo Genesis   	  |V-30.542.502	        |@GCamejo-30542502

## Validar los ejercicios

Desde la raíz del repositorio, ejecuta todas las comprobaciones en secuencia:

```bash
make check
```

Si GNU Make no está instalado, puedes ejecutar directamente el script:

```bash
bash run_all_checks.sh
```

La validación requiere GCC, Go, Rust (`rustc`), Node.js, Python 3 y PHP.
El script compila C con advertencias estrictas, ejecuta el análisis de escape
de Go y `go vet` del Lab 2, compila Rust optimizado, ejecuta el worker pool
de Go con el detector de carreras, y prueba los módulos de Node.js, Python y
PHP. Para satisfacer las versiones solicitadas por los ejercicios, usa Go
1.22+, Rust 1.78+, Node.js 20+, Python 3.12+ y PHP 8.3+.

### Windows (MSYS2)

Si `make` o `bash` no están en el `PATH`, usa la instalación de MSYS2:

```powershell
# Instalar make una sola vez (si falta)
C:\msys64\usr\bin\pacman.exe -S --needed make

# Agregar rustc y las herramientas de MSYS2 al PATH de la sesión actual
$env:PATH = "C:\msys64\usr\bin;$env:USERPROFILE\.cargo\bin;$env:PATH"
& "C:\msys64\usr\bin\bash.exe" -c "make check"
```

### Traer los cambios del compañero

```bash
git fetch origin
git pull --rebase origin main
```

Si tienes cambios locales sin confirmar, guárdalos antes con
`git stash push -u` y recupéralos después con `git stash pop`.

