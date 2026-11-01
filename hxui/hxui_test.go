package hxui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/csrf"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/pagination"
)

var view = localizer.Dict{
	"es": {"Name": "Nombre", "Hello": "Hola %s", "kind.one": "Uno"},
	"en": {"Name": "Name"},
}

var shared = localizer.Dict{
	"es": {"shared.choose": "Elige"},
	"en": {"shared.choose": "Choose"},
}

type form struct {
	Name string `valtruc:"required"`
}

func serve(t *testing.T, target string, view func(ctx owl.Ctx) hx.INode) string {
	t.Helper()
	mux := owl.New(nil)
	mux.AddLocalization(owl.Localization{Shared: shared, Languages: []string{"es", "en"}})
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
		vd := NewViewData(ctx, view)
		return Page(ctx, "T <1>", []hx.INode{Stylesheet("/a.css")},
			hx.Form().Body(vd.CsrfInput(), vd.FormErrors("Name"), vd.FormErrors("Other"),
				hx.P().Text(vd.T("Hello", "<Ana>"))),
		)
	})
	for _, want := range []string{
		"<!doctype html><html lang=\"es\"", `<title>T &lt;1&gt;</title>`, `href="/a.css"`,
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

func TestPageLangIsTheLanguageOfTheRequest(t *testing.T) {
	c, err := cipher.NewWithKeys([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	mux := owl.New(c)
	mux.AddLocalization(owl.Localization{Shared: shared, Languages: []string{"es", "en"}})
	mux.Handle(http.MethodGet, "/set", func(ctx owl.Ctx) error { return ctx.ChangeLanguage("en") })
	mux.Handle(http.MethodGet, "/", func(ctx owl.Ctx) error {
		return ctx.View(owl.HixView(func(ctx owl.Ctx) hx.INode { return Page(ctx, "T", nil) }))
	})
	set := httptest.NewRecorder()
	mux.ServeHTTP(set, httptest.NewRequest(http.MethodGet, "/set", nil))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range set.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, `<html lang="en"`) {
		t.Errorf("want lang=en after choosing English, got %s", body)
	}
}

func TestPageHasNoLangWithoutLocalization(t *testing.T) {
	mux := owl.New(nil)
	mux.Handle(http.MethodGet, "/", func(ctx owl.Ctx) error {
		return ctx.View(owl.HixView(func(ctx owl.Ctx) hx.INode { return Page(ctx, "T", nil) }))
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if body := rec.Body.String(); strings.Contains(body, "lang=") || !strings.Contains(body, "<html>") {
		t.Errorf("no localization, no lang attribute: %s", body)
	}
}
