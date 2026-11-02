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
	for _, want := range []string{`data-island="greeting"`, `data-island-state="greeting-`, "hi ana"} {
		if !strings.Contains(html, want) {
			t.Errorf("host %q lacks %q", html, want)
		}
	}
	if strings.Contains(html, ` id=`) {
		t.Errorf("the host has no id of its own: %q", html)
	}
	id := stateIDOf(t, html)
	script := ssr.Render(inst.StateScript)
	if !strings.Contains(script, `id="`+id+`"`) || !strings.Contains(script, `{"name":"ana"}`) {
		t.Errorf("state script = %q, want id %q", script, id)
	}
}

func stateIDOf(t *testing.T, html string) string {
	t.Helper()
	const key = `data-island-state="`
	i := strings.Index(html, key)
	if i < 0 {
		t.Fatalf("no %s in %q", key, html)
	}
	id, _, found := strings.Cut(html[i+len(key):], `"`)
	if !found {
		t.Fatalf("the value of %s is not closed in %q", key, html)
	}
	return id
}

func TestTheSameIslandManyTimes(t *testing.T) {
	a := island.Mount(greeting, state{Name: "ana"})
	b := island.Mount(greeting, state{Name: "bob"})
	ha, hb := ssr.Render(a.Node), ssr.Render(b.Node)
	ida, idb := stateIDOf(t, ha), stateIDOf(t, hb)
	if ida == idb {
		t.Fatalf("two instances share the state id %q", ida)
	}
	sa, sb := ssr.Render(a.StateScript), ssr.Render(b.StateScript)
	if !strings.Contains(sa, `id="`+ida+`"`) || !strings.Contains(sa, `"ana"`) || strings.Contains(sa, "bob") {
		t.Errorf("first state = %q", sa)
	}
	if !strings.Contains(sb, `id="`+idb+`"`) || !strings.Contains(sb, `"bob"`) || strings.Contains(sb, "ana") {
		t.Errorf("second state = %q", sb)
	}
	// Asking the same instance twice gives the same ids.
	if again := stateIDOf(t, ssr.Render(a.Node)); again != ida {
		t.Errorf("the id of an instance changed: %q != %q", again, ida)
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
	if got := ssr.Render(inst.Node); strings.Contains(got, "data-island-state") {
		t.Errorf("no state, no reference to it: %q", got)
	}
}

func TestMountTextAndInstanceWithoutID(t *testing.T) {
	label := island.Func("label", func(s string) hx.INode { return hx.Span().Text(s) })
	a, b := island.MountText(label, "one"), island.MountText(label, "two")
	if stateIDOf(t, ssr.Render(a.Node)) == stateIDOf(t, ssr.Render(b.Node)) {
		t.Error("two text instances share the state id")
	}
	if got := ssr.Render(a.StateScript); !strings.Contains(got, ">one<") {
		t.Errorf("state script = %q", got)
	}
	// A hand-made instance still works, with one id per name.
	by := island.Instance{Island: label, State: "x"}
	if id := stateIDOf(t, ssr.Render(by.Node)); id != "label-state" || !strings.Contains(ssr.Render(by.StateScript), `id="label-state"`) {
		t.Errorf("hand-made instance id = %q", id)
	}
}
