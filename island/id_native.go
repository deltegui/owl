//go:build !(js && wasm)

package island

import "crypto/rand"

// newID is a random text, unique enough to name the state of an instance in a page.
func newID() string { return rand.Text() }
