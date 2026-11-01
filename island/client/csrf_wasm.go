//go:build js && wasm

package client

import "syscall/js"

// csrfToken is the token the server put in the page (<meta name="csrf-token">).
func csrfToken() string {
	meta := js.Global().Get("document").Call("querySelector", `meta[name="csrf-token"]`)
	if meta.IsNull() {
		return ""
	}
	return meta.Call("getAttribute", "content").String()
}
