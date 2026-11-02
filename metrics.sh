#!/bin/bash
# Métricas de código de owl: tamaño y complejidad. Solo mide, no cambia nada.
#
#   ./metrics.sh            informe: tamaño y las 10 funciones peores de cada métrica
#   ./metrics.sh check      falla (1) si una función pasa un límite y no está en EXCEPTIONS
#   TOP=20 ./metrics.sh     cuántas funciones enseña por métrica (10 por defecto)
#
# Mide el código nativo y el de js/wasm (island), sin tests. Las métricas son las de golangci-lint:
#   gocyclo   caminos independientes de la función (cada if, for, case, &&, ||)
#   gocognit  lo que cuesta entenderla: el anidamiento pesa más que lo plano
#   funlen    sentencias de la función
#   maintidx  índice de mantenibilidad, de 0 a 100 (más bajo = peor)
#   nestif    complejidad de los if anidados
#   dupl      bloques duplicados
#
# Límites del modo check (se pueden cambiar con variables de entorno):
CYCLO_MAX=${CYCLO_MAX:-15}   # ciclomática por encima de esto falla
COGNIT_MAX=${COGNIT_MAX:-25} # cognitiva
STMTS_MAX=${STMTS_MAX:-40}   # sentencias
MAINT_MIN=${MAINT_MIN:-40}   # mantenibilidad por debajo de esto falla
NESTIF_MAX=${NESTIF_MAX:-4}  # if anidados
DUPL_TOKENS=${DUPL_TOKENS:-100}

# Funciones que ya eran complejas cuando se pusieron los límites y lo son por lo
# que hacen. Se escriben como fichero:nombre (nestif: fichero:`condición del if`).
# Una función nueva que pase un límite falla; estas no. Si arreglas una, quítala.
EXCEPTIONS="
parseform.go:setValue
hash/argon2.go:parse
mail/smtp/smtp.go:deliver
mail/smtp/message.go:fill
mail/hxmail/hxmail.go:Register
"

set -u
cd "$(dirname "$0")"
mode="${1:-report}"
top="${TOP:-10}"

lint=$(command -v golangci-lint || true)
[ -z "$lint" ] && [ -x "$(go env GOPATH)/bin/golangci-lint" ] && lint="$(go env GOPATH)/bin/golangci-lint"
[ -z "$lint" ] && { echo "falta golangci-lint (v2)" >&2; exit 2; }

# La configuración va dentro de owl/ para que las rutas salgan relativas a owl/.
cfg=$(mktemp ./.metrics-XXXXXX.yml)
raw=$(mktemp)
trap 'rm -f "$cfg" "$raw"' EXIT
cat >"$cfg" <<YML
version: "2"
run:
  tests: false
linters:
  default: none
  enable: [gocyclo, gocognit, funlen, nestif, maintidx, dupl]
  settings:
    gocyclo: {min-complexity: 1}
    gocognit: {min-complexity: 1}
    nestif: {min-complexity: 1}
    funlen: {lines: -1, statements: 1}
    maintidx: {under: 101}
    dupl: {threshold: $DUPL_TOKENS}
issues:
  max-issues-per-linter: 0
  max-same-issues: 0
  uniq-by-line: false
output:
  formats:
    text:
      path: stdout
      print-issued-lines: false
      colors: false
YML

echo "== tamaño"
if command -v scc >/dev/null 2>&1; then
	scc --exclude-dir docs --no-cocomo --include-ext go . | sed -n '1,4p;/^Total/p'
fi
printf '\n%-20s %8s %8s   (líneas sin blancos ni comentarios, aproximado)\n' paquete código tests
for d in $(go list -f '{{.Dir}}' ./... | sed "s|^$PWD/\?||; s|^\$|.|; s|^$|.|"); do
	code=$(cat /dev/null $(ls $d/*.go | grep -v _test.go) 2>/dev/null | grep -vcE '^\s*(//|$)')
	tests=$(cat /dev/null $(ls $d/*_test.go 2>/dev/null) 2>/dev/null | grep -vcE '^\s*(//|$)')
	printf '%-20s %8s %8s\n' "$d" "$code" "$tests"
done

# Las dos compilaciones: lo que el compilador nativo no ve (js/wasm) también cuenta.
{ "$lint" run -c "$cfg" ./... 2>/dev/null; GOOS=js GOARCH=wasm "$lint" run -c "$cfg" ./island/... 2>/dev/null; } >"$raw"

# Una fila por función y métrica: métrica, valor, fichero:línea, nombre, fichero.
rows=$(awk '
function emit(m, v, f, l, n) { sub(/^\([^)]*\)\./, "", n); printf "%s\t%d\t%s:%s\t%s\t%s\n", m, v, f, l, n, f }
function num(s, re,   a) { match(s, re); a = substr(s, RSTART, RLENGTH); gsub(/[^0-9]/, "", a); return a }
function between(s, from, to,   i, j) { i = index(s, from); if (!i) return "?"; s = substr(s, i + length(from)); j = index(s, to); return j ? substr(s, 1, j - 1) : s }
/^[^ :]+:[0-9]+:[0-9]+: / {
	split($0, p, ":"); f = p[1]; l = p[2]
	t = substr($0, length(p[1] p[2] p[3]) + 4)
	if (t ~ /\(gocyclo\)$/)       emit("gocyclo",  num(t, "complexity [0-9]+"), f, l, between(t, "func `", "`"))
	else if (t ~ /\(gocognit\)$/) emit("gocognit", num(t, "complexity [0-9]+"), f, l, between(t, "func `", "`"))
	else if (t ~ /\(funlen\)$/)   emit("funlen",   num(t, "[(][0-9]+ >"), f, l, between(t, "Function \047", "\047"))
	else if (t ~ /\(maintidx\)$/) emit("maintidx", num(t, "Maintainability Index: [0-9]+"), f, l, between(t, "Function name: ", ","))
	else if (t ~ /\(nestif\)$/)   emit("nestif",   num(t, "complexity: [0-9]+"), f, l, "`" between(t, "`", "`") "`")
	else if (t ~ /\(dupl\)$/)     emit("dupl",     0, f, l, t)
}' "$raw" | sort -u)

names=(gocyclo gocognit funlen maintidx nestif)
titles=("ciclomática (gocyclo)" "cognitiva (gocognit)" "sentencias (funlen)" "mantenibilidad (maintidx, 0-100, más bajo = peor)" "if anidados (nestif)")
echo
for i in 0 1 2 3 4; do
	m=${names[$i]}
	if [ "$m" = maintidx ]; then order=-k2,2n; else order=-k2,2nr; fi
	all=$(awk -F'\t' -v m="$m" '$1 == m' <<<"$rows" | sort -t$'\t' $order)
	n=$(grep -c . <<<"$all")
	if [ "$m" != maintidx ] && [ "$n" -gt 0 ]; then
		med=$(cut -f2 <<<"$all" | sort -n | awk '{a[NR]=$1} END{print a[int((NR+1)/2)]}')
		echo "== ${titles[$i]}: $n funciones, mediana $med"
	else
		echo "== ${titles[$i]}: $n funciones"
	fi
	[ "$n" -gt 0 ] && head -n "$top" <<<"$all" | awk -F'\t' '{printf "  %4d  %-28s %s\n", $2, $3, $4}'
	echo
done
echo "== duplicados (dupl, $DUPL_TOKENS tokens)"
dup=$(awk -F'\t' '$1 == "dupl" {printf "  %s  %s\n", $3, $4}' <<<"$rows")
[ -z "$dup" ] && echo "  ninguno" || echo "$dup"

[ "$mode" = check ] || exit 0

echo
echo "== check (límites: ciclomática $CYCLO_MAX, cognitiva $COGNIT_MAX, sentencias $STMTS_MAX, mantenibilidad $MAINT_MIN, if anidados $NESTIF_MAX)"
bad=$(awk -F'\t' -v c="$CYCLO_MAX" -v g="$COGNIT_MAX" -v s="$STMTS_MAX" -v mi="$MAINT_MIN" -v n="$NESTIF_MAX" -v exc="$EXCEPTIONS" '
BEGIN { k = split(exc, e, "\n"); for (i = 1; i <= k; i++) if (e[i] != "") skip[e[i]] = 1 }
{
	over = ($1 == "gocyclo" && $2 > c) || ($1 == "gocognit" && $2 > g) || ($1 == "funlen" && $2 > s) \
		|| ($1 == "maintidx" && $2 < mi) || ($1 == "nestif" && $2 > n) || ($1 == "dupl")
	key = $5 ":" $4
	if (over && !(key in skip)) printf "  %-9s %4d  %-28s %s\n", $1, $2, $3, $4
}' <<<"$rows")
if [ -n "$bad" ]; then
	echo "$bad"
	echo
	echo "Pasan un límite y no están en EXCEPTIONS: simplifícalas o, si son complejas por lo que hacen, añádelas con un comentario."
	exit 1
fi
echo "  ninguna función nueva pasa los límites"
