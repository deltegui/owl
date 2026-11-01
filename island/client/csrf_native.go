//go:build !(js && wasm)

package client

func csrfToken() string { return "" }
