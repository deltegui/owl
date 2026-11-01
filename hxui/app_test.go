package hxui

import (
	"strings"
	"testing"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/island"
)

type appState struct {
	N int `json:"n"`
}

var counter = island.Define("counter", func(s appState) hx.INode { return hx.P().Text("n") })

func TestAppHasMetaStateAndLoader(t *testing.T) {
	in := island.Mount(counter, appState{N: 3})
	body := serve(t, "/list", func(ctx owl.Ctx) hx.INode {
		return App(ctx, AppOptions{Title: "T", Bundle: "full", Islands: []island.Instance{in},
			Scripts: []hx.INode{hx.El("script").Src("/chart.js")}}, hx.Main().Body(in.Node()))
	})
	order := []string{
		`window.__hxq`, // the recorder is first
		`content="tok&#34;&lt;en" name="csrf-token"`, // escaped by Hix
		`data-island="counter"`,
		`type="application/json" id="counter-`,
		`/chart.js`,
		`src="/js/wasm_exec.js"`,
		`fetch("/js/full.wasm")`,
	}
	at := 0
	for _, want := range order {
		i := strings.Index(body[at:], want)
		if i < 0 {
			t.Fatalf("missing or out of order %q in %s", want, body)
		}
		at += i
	}
}

func TestAppWithoutBundleIsHTML(t *testing.T) {
	body := serve(t, "/list", func(ctx owl.Ctx) hx.INode {
		return App(ctx, AppOptions{Title: "T"}, hx.P().Text("hi"))
	})
	if strings.Contains(body, "wasm") || strings.Contains(body, "Go()") || strings.Contains(body, "__hxq") {
		t.Errorf("no bundle, no loader: %s", body)
	}
}

func TestAppDirAndBadBundle(t *testing.T) {
	body := serve(t, "/list", func(ctx owl.Ctx) hx.INode {
		return App(ctx, AppOptions{Bundle: "x", WasmDir: "/static"}, hx.P())
	})
	if !strings.Contains(body, `"/static/x.wasm"`) || !strings.Contains(body, `src="/static/wasm_exec.js"`) {
		t.Errorf("dir: %s", body)
	}
	defer func() {
		if recover() == nil {
			t.Error("a bundle name with a quote must panic")
		}
	}()
	safeName(`a"b`)
}
