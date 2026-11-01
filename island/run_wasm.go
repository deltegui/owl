//go:build js && wasm

package island

import (
	"log"
	"syscall/js"

	"github.com/deltegui/hx"
	"github.com/deltegui/hx/wasm"
)

// Run hydrates each of the islands that the page has, and then keeps the program
// alive. An island in the page that is not in the list is a page that loaded the
// wrong binary, and it is reported.
func Run(islands ...Island) {
	known := map[string]Island{}
	for _, i := range islands {
		known[i.Name()] = i
	}
	document := js.Global().Get("document")
	found := document.Call("querySelectorAll", "[data-island]")
	for i := 0; i < found.Length(); i++ {
		if name := found.Index(i).Get("dataset").Get("island").String(); known[name] == nil {
			log.Printf("the page has the island %q, and this binary does not carry it", name)
		}
	}
	for name, i := range known {
		if document.Call("getElementById", ID(name)).IsNull() {
			continue // this page does not have it
		}
		state := wasm.ReadState(StateID(name))
		wasm.Hydrate(ID(name), func() hx.INode { return i.Hydrate(state) })
	}
	select {}
}
