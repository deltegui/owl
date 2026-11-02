#!/bin/sh
# Pasa golangci-lint sobre owl, en nativo y en js/wasm (los paquetes que compilan a WASM).
# Uso: ./lint.sh   (desde owl/)
set -u
cd "$(dirname "$0")"
LINT="${GOLANGCI_LINT:-golangci-lint}"
command -v "$LINT" >/dev/null 2>&1 || LINT="$(go env GOPATH)/bin/golangci-lint"
fail=0
run() {
	echo "== $1"
	shift
	"$@" || fail=1
}
# En js/wasm solo se miran los paquetes que se compilan a WASM, y sin unused
# (en esa pasada dice que lo que solo usa el servidor no se usa).
run "nativo"  "$LINT" run ./...
run "js/wasm" env GOOS=js GOARCH=wasm "$LINT" run --disable unused ./island/... ./localizer ./pagination
run "formato" "$LINT" fmt --diff ./...
exit $fail
