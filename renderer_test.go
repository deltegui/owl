package owl

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deltegui/hx"
)

func get(mux *Mux, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHixIsTheDefaultRenderer(t *testing.T) {
	mux := New(nil)
	mux.Get("/", func(ctx Ctx) error {
		return ctx.View(HixView(func(ctx Ctx) hx.INode {
			return hx.El("html").Body(hx.El("body").Body(
				hx.H1().Text("<b>Hi</b>"),
				hx.A().Href("javascript:alert(1)").Text("x"),
			))
		}))
	})
	body := get(mux, "/").Body.String()
	for _, want := range []string{"<!doctype html><html", "&lt;b&gt;Hi&lt;/b&gt;", hx.UnsafeURL} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "javascript:") {
		t.Errorf("unsafe URL survived: %s", body)
	}
}

func TestRendererCanBeSwappedAndSubMuxInherits(t *testing.T) {
	mux := New(nil)
	mux.SetRenderer(TemplateRenderer{})
	templ := template.Must(template.New("v").Parse(`<p>{{ .Model.Name }}</p>`))
	h := func(ctx Ctx) error {
		return ctx.View(TemplateView{Templates: templ, Name: "v", Model: struct{ Name string }{"Ana"}})
	}
	mux.Get("/a", h)
	sub := mux.CreateSubMux("/s")
	sub.Get("/b", h)
	for _, path := range []string{"/a", "/s/b"} {
		if body := get(mux, path).Body.String(); body != "<p>Ana</p>" {
			t.Errorf("%s = %q", path, body)
		}
	}
}

func TestWrongViewForRenderer(t *testing.T) {
	mux := New(nil)
	var got error
	mux.Get("/", func(ctx Ctx) error { got = ctx.View("nope"); return nil })
	get(mux, "/")
	if got == nil || !strings.Contains(got.Error(), "HixRenderer") {
		t.Errorf("error = %v", got)
	}
}
