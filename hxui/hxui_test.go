package hxui

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/csrf"
	"github.com/deltegui/owl/v3/pagination"
)

//go:embed testdata/*.json common/*.json
var files embed.FS

type form struct {
	Name string `valtruc:"required"`
}

func serve(t *testing.T, target string, view func(ctx owl.Ctx) hx.INode) string {
	t.Helper()
	mux := owl.New(nil)
	mux.AddLocalization(files, "testdata/shared", "testdata/errors")
	mux.Handle(http.MethodGet, "/list", func(ctx owl.Ctx) error {
		ctx.Set(csrf.ContextKey, `tok"<en`)
		return ctx.View(owl.HixView(view))
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Body.String()
}

func TestPageCsrfAndErrors(t *testing.T) {
	body := serve(t, "/list", func(ctx owl.Ctx) hx.INode {
		ctx.Validate(form{})
		v := NewView(ctx, "testdata/view")
		return Page("T <1>", []hx.INode{Stylesheet("/a.css")},
			hx.Form().Body(v.CsrfInput(), v.FormErrors("Name"), v.FormErrors("Other"),
				hx.P().Text(v.T("Hello", "<Ana>"))),
		)
	})
	for _, want := range []string{
		"<!doctype html><html", `<title>T &lt;1&gt;</title>`, `href="/a.css"`,
		`name="X-Csrf-Token"`, `value="tok&#34;&lt;en"`, `form-error`,
		"Hola &lt;Ana&gt;", "Nombre",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
	if strings.Count(body, "<ul>") != 1 {
		t.Errorf("only the invalid field has a list:\n%s", body)
	}
}

func TestSelectIsTranslated(t *testing.T) {
	body := serve(t, "/list", func(ctx owl.Ctx) hx.INode {
		return NewView(ctx, "testdata/view").Select(owl.SelectList{Name: "kind", Items: []owl.SelectItem{
			{Value: "", Tag: "shared.choose"}, {Value: "1", Tag: "kind.one", Selected: true},
		}})
	})
	for _, want := range []string{`name="kind"`, `id="kind"`, ">Elige<", ">Uno<", "selected"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestPagination(t *testing.T) {
	p := pagination.Pagination{CurrentPage: 3, ElementsPerPage: 10, TotalElements: 100}
	body := serve(t, "/list?q=a+b&pagination.currentPage=3", func(ctx owl.Ctx) hx.INode { return Pagination(ctx, p, nil) })
	for _, want := range []string{"active page-item", "pagination.currentPage=4", "q=a+b", ">Siguiente<", "20 a 30 de 100"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	one := pagination.Pagination{CurrentPage: 1, ElementsPerPage: 10, TotalElements: 5}
	if got := serve(t, "/list", func(ctx owl.Ctx) hx.INode { return Pagination(ctx, one, nil) }); got != "" {
		t.Errorf("one page should write nothing: %q", got)
	}
}
