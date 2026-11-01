package hxui

import (
	"fmt"
	"strings"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/island"
)

// AppOptions says what the page of a WASM program needs besides its content.
type AppOptions struct {
	Title string
	// Head is added to the head (stylesheets, meta...). The CSRF meta that the
	// islands read (see island/client) is already there when the request has a token.
	Head []hx.INode
	// Bundle is the name of the WASM the page loads: "full" loads
	// WasmDir+"full.wasm". Empty loads none, and the page is just HTML.
	Bundle string
	// WasmDir is where the bundle and wasm_exec.js are served from ("/js/" by default).
	WasmDir string
	// Scripts load before the program (a library its islands use, like a chart).
	// They must be plain script elements, not Script: a deferred one would run
	// after the program has started.
	Scripts []hx.INode
	// Islands are the ones the page has: their state goes after the body, where
	// the browser reads it when the WASM starts. Their elements go where
	// the page wants them, with island.Instance.Node.
	Islands []island.Instance
}

// App is Page for a page that carries a WASM program: the document, the CSRF
// meta, the body, the state of the islands and, at the end, the scripts that
// start the program. The server renders the body and the islands in it; the
// program hydrates them.
//
//	hxui.App(ctx, hxui.AppOptions{Title: "Products", Bundle: "full", Islands: ins},
//		hx.Main().Body(ins[0].Node(), ins[1].Node()))
func App(ctx owl.Ctx, o AppOptions, body ...hx.INode) hx.INode {
	head := o.Head
	if token := ctx.CsrfToken(); token != "" {
		head = append([]hx.INode{hx.El("meta").Attribute("name", "csrf-token").Attribute("content", token)}, head...)
	}
	body = append(body[:len(body):len(body)], o.tail()...)
	return Page(ctx, o.Title, head, body...)
}

// tail is what goes after the content: state, scripts and the loader.
func (o AppOptions) tail() []hx.INode {
	var out []hx.INode
	for _, i := range o.Islands {
		out = append(out, i.StateScript())
	}
	if o.Bundle == "" {
		return out
	}
	dir := o.WasmDir
	if dir == "" {
		dir = "/js/"
	}
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	out = append(out, o.Scripts...)
	return append(out,
		hx.El("script").Src(dir+"wasm_exec.js"),
		hx.Raw("<script>"+loader(dir+safeName(o.Bundle)+".wasm")+"</script>"))
}

func loader(url string) string {
	return fmt.Sprintf(`const go = new Go();
WebAssembly.instantiateStreaming(fetch(%q), go.importObject).then((r) => go.run(r.instance));`, url)
}

// safeName panics on a bundle name that is not a plain file name: it goes into a
// script, and it is the programmer's, never the user's.
func safeName(name string) string {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			panic(fmt.Sprintf("hxui: %q is not a valid bundle name", name))
		}
	}
	return name
}
