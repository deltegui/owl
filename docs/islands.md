# Islands

An **island** is a piece of a page that the server renders and the browser
brings to life. The server builds it from a *state* (JSON in a `<script>` next
to it); the browser, compiled to WASM with Hix, builds it from that same state
and *hydrates* the HTML that is already there. Nothing is redrawn, and the page
works (as static HTML) before, or without, the WASM.

Packages:

| Package | Imports `owl` root? | Compiles to WASM? | Purpose |
|---|---|---|---|
| `owl/island` | no | yes (`Run` only for `js/wasm`) | Define, place and start islands. |
| `owl/island/client` | no | yes | The island's HTTP client (CSRF), error shapes, `Escape`. |
| `owl/hxui` (`App`) | yes | no | The page around islands with the WASM loader. |
| `owl` (`JsonInvalid`, `JsonFailure`) | — | no | Server answers that `client` understands. |

The packages that must run in the browser never import the `owl` root: it would
pull `net/http`, cryptography and the router into the WASM (hundreds of KB).

## The shape of an island app

```
feature/product/web/            compiles on both sides (no owl import)
    islands.go                  var List = island.Define("products", ...)
feature/product/product_handler.go   server: island.Mount(List, state), hxui.App(...)
client/full/main.go             browser: island.Run(productweb.List, ...)
```

### 1. Define the island (shared code)

```go
package productweb

type State struct {
	Items []Product `json:"items"`
	Lang  string    `json:"l"`
}

var List = island.Define("products", func(s State) hx.INode {
	return screen(s) // Hix tree built from the state
})
```

`Define[S](name, build)` ties a **name**, a **state type** and a **builder**
together. The name must be unique in a page. The state is encoded as JSON, so
only exported fields with JSON tags travel.

The builder is called twice with equal input: once on the server (to render the
HTML) and once in the browser (to hydrate it). It must be deterministic from the
state: do not read the clock, random numbers or `window` while building. Read
the browser-only things in `hx.OnMount` instead.

### 2. Place it in a page (server)

```go
func showProducts(ctx owl.Ctx) error {
	in := island.Mount(productweb.List, productweb.State{Items: items, Lang: ctx.GetCurrentLanguage()})
	return ctx.View(func(ctx owl.Ctx) hx.INode {
		return hxui.App(ctx, hxui.AppOptions{
			Title:   "Products",
			Bundle:  "full",
			Islands: []island.Instance{in},
		}, hx.Main().Body(in.Node()))
	})
}
```

`island.Mount(def, state)` returns an `island.Instance` (the island plus its
JSON state). Use:

- `in.Node()` where the island goes: a `<div id="island-NAME" data-island="NAME">`
  with the island already built inside.
- `in.StateScript()` after the HTML of the islands (`hxui.App` does it for the
  islands in `AppOptions.Islands`).

### 3. Carry it in the WASM (browser)

```go
//go:build js && wasm

package main

import (
	"github.com/deltegui/owl/v3/island"
	productweb "myapp/feature/product/web"
)

func main() {
	island.Run(productweb.List, productweb.Stats)
}
```

`island.Run(islands...)` finds the islands the page has (by id), reads each
state, hydrates it and then blocks forever so the program stays alive. It logs
`the page has the island "x", and this binary does not carry it` if the page
holds an island the binary does not know: the page loaded the wrong bundle.

Build it with `GOOS=js GOARCH=wasm go build -o static/js/full.wasm ./client/full`
(or TinyGo for much smaller files) and copy `wasm_exec.js` from your Go or
TinyGo installation next to it. One binary per kind of page keeps the heavy
islands out of the light pages.

## island package reference

```go
func Define[S any](name string, build func(S) hx.INode) Def[S]
func Mount[S any](d Def[S], state S) Instance
func Func(name string, build func(state string) hx.INode) Island
func Run(islands ...Island)           // js/wasm only
func ID(name string) string           // "island-" + name
func StateID(name string) string      // "island-" + name + "-state"
```

| Type | Methods |
|---|---|
| `Def[S]` | `Name()`, `Build(state S) hx.INode` (from a value), `Hydrate(state string) hx.INode` (from JSON text; empty text is the zero state; a state that does not decode is logged and what decoded is used). |
| `Island` (interface) | `Name() string`, `Hydrate(state string) hx.INode`. `Def[S]` implements it. |
| `Instance{Island, State}` | `Name()`, `Build(state string)`, `Node()`, `StateScript()`. |

`island.Func(name, build)` is an island whose state is plain text, not JSON (a
label, a theme name). Use it for tiny islands, or to adapt code that was written
as a registry of functions.

The state is read from `<script type="application/json" id="island-NAME-state">`;
`hx.StateScript` escapes `<`, `>`, `&` and the line separators so the JSON can
never close the script element.

## hxui.App

```go
func App(ctx owl.Ctx, o AppOptions, body ...hx.INode) hx.INode

type AppOptions struct {
	Title   string
	Head    []hx.INode        // stylesheets, meta... (added to the head)
	Bundle  string            // "full" loads WasmDir + "full.wasm"; empty loads none
	WasmDir string            // default "/js/"
	Scripts []hx.INode        // plain <script> elements to load before the program
	Islands []island.Instance // the islands of the page: their state goes after the body
}
```

`App` is `Page` plus what a WASM page needs, in this order:

1. In the head: `<meta name="csrf-token">` (when the request has a token, which
   `island/client` reads), then your `Head`.
2. Your `body`.
3. The state `<script>` of every island in `Islands`.
4. With a `Bundle`: your `Scripts`, then `<WasmDir>wasm_exec.js`, then an inline
   loader that fetches and starts `<WasmDir><Bundle>.wasm`.

Notes:

- `Scripts` must be plain `<script>` elements (`hx.El("script").Src(...)`), not
  `hxui.Script`, which has `defer`: a deferred script would run after the
  program started. Use it for libraries the islands call (a charting library).
- A bundle name that is not a plain file name (letters, digits, `-`, `_`, `.`)
  panics: it is written inside a script.
- Without a `Bundle`, the page is only HTML and the state scripts.
- `Islands` only places the *state*. The elements go where you put `Node()`.

## island/client: talking to the server

```go
import "github.com/deltegui/owl/v3/island/client"

func Send[T any](ctx context.Context, method, url string, body any) (T, error)
func FieldErrors(err error) (map[string][]string, bool)
func ErrorMessage(err error) (string, bool)
func Escape(s string) string

type Errors struct {
	Errors  map[string][]string `json:"errors,omitempty"`
	Message string              `json:"error,omitempty"`
}
const CsrfHeader = "X-Csrf-Token"
```

`Send` is `hxjson.Send` plus the CSRF header. It reads the token from
`<meta name="csrf-token">` (which `hxui.App` writes), sends `Accept:
application/json`, encodes `body` as JSON (nil sends none) and decodes the
answer into `T` (an empty answer is the zero `T`; use `struct{}` when there is
nothing to read). A non-2xx answer is an `*hxhttp.HTTPError` with `Status` and
`Body`. Outside a browser it fails with `hxhttp.ErrNotInBrowser`, so code that
uses it still compiles and tests natively.

```go
saved, err := client.Send[Product](ctx, "PUT", "/api/products/"+strconv.Itoa(p.ID), p)
if byField, ok := client.FieldErrors(err); ok {
	// map of field -> messages, from a 422 (JsonInvalid)
} else if msg, ok := client.ErrorMessage(err); ok {
	// the message of a rule of the domain, from a 400 (JsonFailure)
}
```

`Escape` percent-encodes a query value without importing `net/url` (which adds
size to the WASM): `"?q=" + client.Escape(text)`.

**Do not wait for a response inside an event handler.** `hxhttp.Do` (and so
`Send`) returns `hxhttp.ErrInCallback` if called from a handler or an animation
frame, because waiting there would deadlock the single-threaded runtime. Start
the request in a goroutine, or use Hix's `Action` and `Resource`, which do that
and keep the state in signals.

### Answering errors

On the server, a JSON API that islands call answers errors with:

```go
func (ctx Ctx) JsonInvalid(texts localizer.Source) error
func (ctx Ctx) JsonFailure(err core.DomainError) error
```

- `JsonInvalid(texts)` answers **422** with `{"errors": {"field": ["message"]}}`
  from `ctx.ModelState`, translated with `texts` (the field names come from
  there). Field names follow the JSON of the model: the Go name with the first
  letter of each part in lower case (`Lines[0].Account` → `lines[0].account`).
  A model whose JSON tags say something else needs its own answer. If a field
  has a *required* error, only that is said; the other rules would repeat it.
- `JsonFailure(err)` answers **400** with `{"error": "message"}`: the message of
  a `core.DomainError`, translated by its code with the `Errors` of
  `AddLocalization`.

```go
func handleSave(service productService) owl.Handler {
	return func(ctx owl.Ctx) error {
		var item productweb.Product
		if err := ctx.ParseJson(&item); err != nil {
			return ctx.StringBadRequest("Invalid JSON")
		}
		ctx.Validate(item)
		if !ctx.ModelState.Valid {
			return ctx.JsonInvalid(productweb.Texts)
		}
		saved, err := service.Save(item)
		var domain core.DomainError
		if errors.As(err, &domain) {
			return ctx.JsonFailure(domain) // a rule of the domain, translated by its code
		}
		if err != nil {
			return err // 500
		}
		return ctx.Json(saved)
	}
}
```

These bodies are the `client.Errors` type, so `FieldErrors` and `ErrorMessage`
read exactly what these two write.

## Translating an island

Do not put the translations in the state. Put the `localizer.Dict` in the shared
package (`productweb`), put only the language in the state (`Lang`), and have the
builder call `localizer.Pick(dict, lang, defaultLang)`: server and browser pick
the same words, and the state stays small. The `localizer` package compiles to
WASM (it imports only `fmt` and `slices`). See [localization](localization.md).

## Hydration rules to keep in mind

These come from Hix:

- If a tag or the number of children differs from the server's HTML, that part
  is rebuilt; different text, attributes, classes or ids are corrected in place.
- The browser inserts a `<tbody>` in a `<table>` that has none. Write the
  `TBody` yourself so the trees match.
- `Body(...)` *replaces* all children; it does not append.
- Do not use `Html`, `Head` or `Body` elements inside an island.
- State changes the island makes after hydration are local to the browser; the
  server does not know about them. Persist them with `client.Send`.

## Security

- The state is JSON visible to the user: put in it only what they may see.
- Every non-GET request an island makes must carry the CSRF token (`Send` does)
  and the route must be behind `middleware.Csrf`. See [security](security.md#csrf).
- `hx.Raw` must never receive user text.
