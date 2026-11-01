//go:build js && wasm

package island

import (
	"log"
	"syscall/js"

	"github.com/deltegui/hx"
	"github.com/deltegui/hx/wasm"
)

// Run hydrates each of the islands that the page has, once for every element
// with data-island, and then keeps the program alive. The same island can be in
// the page many times; each element reads its state from the script named by
// its data-island-state. It also stops the recording of events that hxui.App
// starts in the head. An island in the page that is not in the list is a page
// that loaded the wrong binary, and it is reported.
func Run(islands ...Island) {
	known := map[string]Island{}
	for _, i := range islands {
		known[i.Name()] = i
	}
	reported := map[string]bool{}
	found := js.Global().Get("document").Call("querySelectorAll", "[data-island]")
	for n := 0; n < found.Length(); n++ {
		host := found.Index(n)
		dataset := host.Get("dataset")
		name := dataset.Get("island").String()
		i := known[name]
		if i == nil {
			if !reported[name] {
				reported[name] = true
				log.Printf("the page has the island %q, and this binary does not carry it", name)
			}
			continue
		}
		state := ""
		if id := dataset.Get("islandState"); !id.IsUndefined() {
			if state = wasm.ReadState(id.String()); state == "" {
				log.Printf("island %q: there is no state with id %q", name, id.String())
			}
		}
		wasm.HydrateElement(host, func() hx.INode { return i.Hydrate(state) })
	}
	wasm.StopReplay() // every island is hydrated and has replayed what the user did
	select {}
}
