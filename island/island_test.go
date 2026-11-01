package island_test

import (
	"strings"
	"testing"

	"github.com/deltegui/hx"
	"github.com/deltegui/hx/ssr"
	"github.com/deltegui/owl/v3/island"
)

type state struct {
	Name string `json:"name"`
}

var greeting = island.Define("greeting", func(s state) hx.INode { return hx.P().Text("hi " + s.Name) })

func TestMountRendersHostAndState(t *testing.T) {
	inst := island.Mount(greeting, state{Name: "ana"})
	html := ssr.Render(inst.Node)
	for _, want := range []string{`id="island-greeting"`, `data-island="greeting"`, "hi ana"} {
		if !strings.Contains(html, want) {
			t.Errorf("host %q lacks %q", html, want)
		}
	}
	script := ssr.Render(inst.StateScript)
	if !strings.Contains(script, `id="island-greeting-state"`) || !strings.Contains(script, `{"name":"ana"}`) {
		t.Errorf("state script = %q", script)
	}
}

func TestHydrateBuildsTheSameFromTheState(t *testing.T) {
	inst := island.Mount(greeting, state{Name: "ana"})
	a := ssr.Render(func() hx.INode { return greeting.Build(state{Name: "ana"}) })
	b := ssr.Render(func() hx.INode { return greeting.Hydrate(inst.State) })
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
	if got := ssr.Render(func() hx.INode { return greeting.Hydrate("") }); !strings.Contains(got, "hi ") {
		t.Fatalf("empty state: %q", got)
	}
}

func TestFuncIslandAndEmptyState(t *testing.T) {
	label := island.Func("label", func(s string) hx.INode { return hx.Span().Text(s) })
	inst := island.Instance{Island: label}
	if got := ssr.Render(inst.StateScript); strings.Contains(got, "script") {
		t.Errorf("no state, no script: %q", got)
	}
}
