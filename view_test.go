package owl

import (
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

func TestViewRendersHixPage(t *testing.T) {
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
