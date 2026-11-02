#!/bin/sh
# Cobertura de owl: junta los tests nativos y los de js/wasm de island/client (si hay node).
# Los tests de island usan hx/ssr y en wasm chocarían con el motor del navegador, y
# lo que island hace en el navegador (run_wasm.go, id_wasm.go) solo se prueba con
# OWL_DEMO (make check): no cuenta aquí.
# Uso: ./cover.sh            resumen por paquete y total
#      ./cover.sh html       además escribe cover.html
#      COVER_MIN=60 ./cover.sh   falla si el total baja de 60%
set -eu
cd "$(dirname "$0")"
out=cover.out
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

go test -coverpkg=./... -coverprofile="$tmp/native.out" ./... >/dev/null
files="$tmp/native.out"
if command -v node >/dev/null 2>&1; then
	GOOS=js GOARCH=wasm go test -exec "$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
		-coverpkg=./island/client -coverprofile="$tmp/wasm.out" ./island/client >/dev/null
	files="$files $tmp/wasm.out"
else
	echo "aviso: sin node, no se mide js/wasm" >&2
fi

# Mismo bloque en varias pasadas: vale el que más veces se ejecutó.
{
	echo "mode: set"
	cat $files | grep -v '^mode:' |
		awk '{k=$1" "$2; v=$3+0; if(!(k in c)||v>c[k])c[k]=v} END{for(k in c)print k, c[k]}' | sort
} >"$out"

awk -v min="${COVER_MIN:-0}" '
NR==1 {next}
{
	split($1, a, ":"); f=a[1]; sub(/^github.com\/deltegui\/owl\/v3\/?/, "", f)
	pkg=f; if (sub(/\/[^\/]*$/, "", pkg) == 0) pkg="."
	n=$2; c=($3>0)?$2:0
	tot[pkg]+=n; cov[pkg]+=c; T+=n; C+=c
}
END {
	for (p in tot) printf "%6.1f%%  %-18s (%d/%d)\n", 100*cov[p]/tot[p], p, cov[p], tot[p] | "sort -n"
	close("sort -n")
	printf "\n%6.1f%%  total\n", 100*C/T
	if (100*C/T < min) { printf "FALLA: por debajo de COVER_MIN=%s\n", min; exit 1 }
}' "$out"

[ "${1:-}" = html ] && go tool cover -html="$out" -o cover.html && echo "cover.html escrito"
exit 0
