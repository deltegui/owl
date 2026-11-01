//go:build js && wasm

package island

import (
	"strconv"
	"sync/atomic"
)

// Only the server mounts islands; this keeps crypto/rand out of the WASM
// binary and still gives a different id each time.
var lastID atomic.Uint64

func newID() string { return strconv.FormatUint(lastID.Add(1), 36) }
