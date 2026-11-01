// Package island is a piece of a page that the server renders and the browser
// hydrates. The server builds it from a state (JSON in a <script> element next
// to it) and the browser builds it from that same state, so what it builds is
// what is already there and nothing is redrawn.
//
// An island is defined once, in a package that compiles on both sides:
//
//	var List = island.Define("products", func(s State) hx.INode { return Screen(s) })
//
// The server places it in a page with island.Mount(List, state) and the WASM of
// the page carries it with island.Run(List).
//
// This package imports only Hix and the standard library (no OWL root, no
// net/http), so a WASM binary can import it. Run is in a file that only
// compiles for js/wasm.
package island

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/deltegui/hx"
)

// ID is the id of the element of the island called name.
func ID(name string) string { return "island-" + name }

// StateID is the id of the script element that has the state of the island.
func StateID(name string) string { return "island-" + name + "-state" }

// Island is what Run hydrates: a name and a way to build from the text of the state.
type Island interface {
	Name() string
	// Hydrate builds the island from the state, the JSON the server wrote
	// (empty is the zero state).
	Hydrate(state string) hx.INode
}

// Def is an island whose state is a value of type S.
type Def[S any] struct {
	name  string
	build func(S) hx.INode
}

// Define makes the island called name. The name is what ties the element, the
// state and the code in the browser together, so it must be unique in a page.
func Define[S any](name string, build func(S) hx.INode) Def[S] {
	if name == "" || build == nil {
		panic("island: Define needs a name and a build function")
	}
	return Def[S]{name: name, build: build}
}

func (d Def[S]) Name() string { return d.name }

// Build is the island for a state that is already a value.
func (d Def[S]) Build(state S) hx.INode { return d.build(state) }

// Hydrate decodes the state and builds. A state that does not decode is reported
// and the island is built from what did.
func (d Def[S]) Hydrate(state string) hx.INode {
	var s S
	if state != "" {
		if err := json.Unmarshal([]byte(state), &s); err != nil {
			log.Printf("island %q: the state does not decode: %v", d.name, err)
		}
	}
	return d.build(s)
}

type fn struct {
	name  string
	build func(string) hx.INode
}

func (f fn) Name() string                  { return f.name }
func (f fn) Hydrate(state string) hx.INode { return f.build(state) }

// Func is an island whose state is plain text, not JSON (a label, say).
func Func(name string, build func(state string) hx.INode) Island {
	return fn{name: name, build: build}
}

// Instance is an island with the state it is placed with in one page.
type Instance struct {
	Island Island
	State  string
}

// Mount places the island with a state. The state is encoded as JSON.
func Mount[S any](d Def[S], state S) Instance {
	text, err := json.Marshal(state)
	if err != nil {
		panic(fmt.Sprintf("island %q: the state does not encode: %v", d.name, err))
	}
	return Instance{Island: d, State: string(text)}
}

// Name is the name of the island.
func (i Instance) Name() string { return i.Island.Name() }

// Build is the island built from its state.
func (i Instance) Build(state string) hx.INode { return i.Island.Hydrate(state) }

// Node is the element of the island with the island built inside. Put it where
// the island goes.
func (i Instance) Node() hx.INode {
	return hx.Div().Id(ID(i.Name())).Attribute("data-island", i.Name()).Body(i.Island.Hydrate(i.State))
}

// StateScript is the script element with the state. Put it in the page after
// the HTML of the islands (the browser reads it when the WASM starts).
func (i Instance) StateScript() hx.INode {
	if i.State == "" {
		return hx.Noop()
	}
	return hx.Raw(hx.StateScript(StateID(i.Name()), i.State))
}
