#!/usr/bin/env bash
# Elimina los ficheros «:Zone.Identifier» que Windows deja al copiar archivos
# descargados hacia WSL o hacia un recurso compartido.
#
# Uso:
#   ./scripts/clean-zone-identifier.sh                 # limpia el directorio actual
#   ./scripts/clean-zone-identifier.sh /ruta [/ruta2]  # limpia las rutas indicadas
#   ./scripts/clean-zone-identifier.sh -n              # simulacro, no borra nada
#
# Opciones:
#   -n, --dry-run   Muestra qué se borraría sin tocar el disco
#   -q, --quiet     No lista los ficheros, solo el total
#   -h, --help      Esta ayuda
set -euo pipefail

dry_run=0
quiet=0
paths=()

usage() {
	sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		-n|--dry-run) dry_run=1 ;;
		-q|--quiet)   quiet=1 ;;
		-h|--help)    usage; exit 0 ;;
		--)           shift; paths+=("$@"); break ;;
		-*)           echo "Opción desconocida: $1" >&2; usage >&2; exit 2 ;;
		*)            paths+=("$1") ;;
	esac
	shift
done

[[ ${#paths[@]} -eq 0 ]] && paths=(".")

total=0
for root in "${paths[@]}"; do
	if [[ ! -d "$root" ]]; then
		echo "No es un directorio: $root" >&2
		exit 1
	fi

	# -name sin los dos puntos: cubre tanto «fichero:Zone.Identifier» (WSL/Linux)
	# como el nombre normalizado que exponen algunos montajes de red.
	while IFS= read -r -d '' file; do
		total=$((total + 1))
		[[ $quiet -eq 0 ]] && echo "$file"
		[[ $dry_run -eq 0 ]] && rm -f -- "$file"
	done < <(find "$root" \
		\( -name .git -o -name node_modules -o -name vendor \) -prune -o \
		-type f -name '*Zone.Identifier' -print0)
done

if [[ $dry_run -eq 1 ]]; then
	echo "→ $total fichero(s) Zone.Identifier encontrados (simulacro, no se ha borrado nada)"
else
	echo "→ $total fichero(s) Zone.Identifier eliminados"
fi
