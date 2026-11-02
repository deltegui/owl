#!/bin/bash
# Mutation testing de owl con go-mutesting (solo código nativo, sin -race).
#
#   ./mutate.sh                 todos los ficheros de la lista de abajo
#   ./mutate.sh url.go signal.go   solo esos (rutas desde owl/)
#   MUTATE_MIN=80 ./mutate.sh      sale con 1 si algún fichero baja de 80 %
#   MUTATE_TIMEOUT=10 ./mutate.sh  segundos por mutante (10 por defecto)
#
# Un mutante que "sobrevive" es un cambio en el código que ningún test nota: o
# falta un test, o el cambio no altera el comportamiento (mutante equivalente).
# Los supervivientes de cada fichero quedan en mutation-report/<fichero>.txt.
#
# go-mutesting escribe cada mutante en el fichero real y lo restaura al acabar.
# Si lo interrumpes a mitad, podría dejar uno puesto: este script guarda una copia
# de cada fichero antes de empezar y la devuelve siempre, también con Ctrl+C.
# Cada mutante tarda lo que tarden los tests; por eso no uses -race aquí.
#
# Solo se mutan los ficheros con tests de su paquete. Quedan fuera los que casi no
# tienen (ctx.go, middleware/cors.go, core/, logx/, pagination/): mutarlos solo daría
# supervivientes. Tampoco los _wasm.go (island/run_wasm.go): necesitan un navegador.
set -u
cd "$(dirname "$0")"

DEFAULT_FILES="cipher/seal.go cipher/aes.go csrf/csrf.go session/session.go session/store.go hash/argon2.go hash/bcrypt.go middleware/session.go middleware/csrf.go files/path.go files/policies.go files/naming.go files/chain.go files/local.go files/temporal.go excel/cell.go excel/sheet.go excel/excel.go mail/mail.go mail/service.go mail/queue/queue.go mail/outbox/sender.go mail/outbox/worker.go mail/smtp/message.go mail/smtp/smtp.go mail/hxmail/hxmail.go pdf/pdf.go localizer/dict.go localizer/webstore/webstore.go parseform.go"
files="${*:-$DEFAULT_FILES}"
timeout="${MUTATE_TIMEOUT:-10}"
min="${MUTATE_MIN:-0}"
out=mutation-report

tool=$(command -v go-mutesting || true)
[ -z "$tool" ] && [ -x "$(go env GOPATH)/bin/go-mutesting" ] && tool="$(go env GOPATH)/bin/go-mutesting"
if [ -z "$tool" ]; then
	echo "falta go-mutesting: go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest" >&2
	exit 2
fi

backup=$(mktemp -d)
# Devuelve los ficheros tal como estaban y avisa si hubo que hacerlo.
restore() {
	for f in $files; do
		if [ -f "$backup/$f" ] && ! cmp -s "$backup/$f" "$f"; then
			cp "$backup/$f" "$f"
			echo "AVISO: $f había quedado con un mutante; restaurado." >&2
		fi
	done
	rm -f report.json
	rm -rf "$backup"
}
trap restore EXIT
trap 'echo; echo "interrumpido"; exit 130' INT TERM

for f in $files; do
	[ -f "$f" ] || { echo "no existe: $f" >&2; exit 2; }
	mkdir -p "$backup/$(dirname "$f")"
	cp "$f" "$backup/$f"
done

# Si los tests ya fallan, todos los mutantes "mueren" y el resultado no vale nada.
if ! go test -count=1 ./... >/dev/null 2>&1; then
	echo "los tests fallan sin mutar nada: arréglalos antes de medir" >&2
	exit 2
fi

mkdir -p "$out"
status=0
printf '%-20s %8s %8s %9s %6s\n' fichero puntos muertos sobreviven tiempo
for f in $files; do
	name=$(echo "$f" | tr / _)
	start=$(date +%s)
	"$tool" --exec-timeout="$timeout" "./$f" >"$out/$name.raw" 2>&1
	secs=$(( $(date +%s) - start ))
	line=$(grep 'mutation score' "$out/$name.raw" || true)
	if [ -z "$line" ]; then
		printf '%-20s %s\n' "$f" "sin resultado (mira $out/$name.raw)"
		status=1
		continue
	fi
	# "The mutation score is 0.95 (19 passed, 1 failed, 0 duplicated, ..."
	score=$(echo "$line" | sed -E 's/.*score is ([0-9.]+).*/\1/')
	killed=$(echo "$line" | sed -E 's/.*\(([0-9]+) passed.*/\1/')
	survived=$(echo "$line" | sed -E 's/.*passed, ([0-9]+) failed.*/\1/')
	pct=$(awk -v s="$score" 'BEGIN{printf "%.0f", s*100}')
	printf '%-20s %7s%% %8s %9s %5ss\n' "$f" "$pct" "$killed" "$survived" "$secs"
	# Los supervivientes: el diff de cada mutante que no mató ningún test.
	awk '
		/^@@/ {hunk=$0; next}
		/^[-+]/ && !/^(---|\+\+\+)/ {hunk=hunk "\n" $0; next}
		/^PASS "/ {hunk=""}
		/^FAIL "/ {if (hunk != "") print hunk "\n"; hunk=""}
	' "$out/$name.raw" >"$out/$name.txt"
	rm -f "$out/$name.raw"
	[ "$survived" -eq 0 ] && rm -f "$out/$name.txt"
	[ "$pct" -lt "$min" ] && status=1
done
echo
echo "supervivientes en $out/ (un fichero por cada .go con alguno)"
[ "$status" -ne 0 ] && echo "por debajo de MUTATE_MIN=$min o con errores"
exit $status
