package hxpdf

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/pdf"
)

type invoice struct{ Number string }

var texts = localizer.Dict{
	"es": {"Title": "Factura"},
	"en": {"Title": "Invoice"},
}

func newRenderer() *Renderer {
	r := New(WithDefaultLang("es"))
	Register(r, Template[invoice]{
		Texts:   texts,
		Options: pdf.Options{Format: "A4"},
		View: func(p invoice, loc localizer.Localizer) hx.INode {
			return hx.El("html").Body(hx.El("body").Body(hx.H1().Text(loc["Title"]+" "+p.Number), hx.P().Text("<b>")))
		},
	})
	return r
}

func TestRenderByTypeAndLanguage(t *testing.T) {
	r := newRenderer()
	for _, tc := range []struct {
		payload any
		lang    string
		want    string
	}{
		{invoice{"1"}, "en", "Invoice 1"},
		{&invoice{"2"}, "es", "Factura 2"},
		{invoice{"3"}, "", "Factura 3"},
		{invoice{"4"}, "fr", "Factura 4"},
	} {
		got, err := r.Render(context.Background(), tc.payload, tc.lang)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got.HTML, "<!doctype html><html") || !strings.Contains(got.HTML, tc.want) {
			t.Errorf("%v: %s", tc.payload, got.HTML)
		}
		if !strings.Contains(got.HTML, "&lt;b&gt;") {
			t.Errorf("text was not escaped: %s", got.HTML)
		}
		if got.Options.Format != "A4" {
			t.Errorf("options: %+v", got.Options)
		}
	}
}

func TestErrors(t *testing.T) {
	r := newRenderer()
	if _, err := r.Render(context.Background(), 5, ""); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("%v", err)
	}
	if _, err := r.Render(context.Background(), nil, ""); !errors.Is(err, pdf.ErrNoPayload) {
		t.Fatalf("%v", err)
	}
	Register(r, Template[int]{View: func(int, localizer.Localizer) hx.INode { panic("bad") }})
	if _, err := r.Render(context.Background(), 5, ""); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("%v", err)
	}
}

func TestRegisterPanics(t *testing.T) {
	for name, fn := range map[string]func(){
		"nil view": func() { Register(New(), Template[invoice]{}) },
		"twice": func() {
			r := newRenderer()
			Register(r, Template[invoice]{View: func(invoice, localizer.Localizer) hx.INode { return hx.Div() }})
		},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			fn()
		}()
	}
}
