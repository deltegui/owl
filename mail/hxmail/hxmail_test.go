package hxmail

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/mail"
)

type welcome struct{ Name, Link string }

var welcomeTexts = localizer.Dict{
	"es": {"Subject": "Bienvenido", "Greeting": "Hola, %s", "Button": "Confirmar"},
	"en": {"Subject": "Welcome", "Greeting": "Hi, %s", "Button": "Confirm"},
}

func welcomeView(p welcome, t localizer.Localizer) hx.INode {
	return hx.El("html").Body(
		hx.El("head").Body(hx.El("meta").Attribute("charset", "utf-8")),
		hx.El("body").Body(
			hx.H1().Textf(t.Get("Greeting"), p.Name),
			hx.A().Href(p.Link).Style("color", "#fff").Text(t.Get("Button")),
			hx.Img().Attribute("src", "cid:logo"),
		),
	)
}

func msg(p any) mail.Message {
	return mail.Message{Envelope: mail.Envelope{To: []string{"ana@example.com"}}, Payload: p}
}

func newRenderer() *Renderer {
	r := New(WithDefaultLang("es"))
	Register(r, Template[welcome]{Texts: welcomeTexts, View: welcomeView})
	return r
}

func mustRender(t *testing.T, r *Renderer, m mail.Message) mail.Rendered {
	t.Helper()
	got, err := r.Render(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRendersTheViewInTheLanguageOfTheMessage(t *testing.T) {
	r := newRenderer()
	var _ mail.Renderer = r

	m := msg(welcome{Name: "Ana", Link: "https://example.com/confirm?a=1&b=2"})
	got := mustRender(t, r, m)
	if got.Subject != "Bienvenido" {
		t.Errorf("subject %q", got.Subject)
	}
	if !strings.HasPrefix(got.HTML, "<!doctype html><html>") {
		t.Errorf("html %q", got.HTML)
	}
	for _, want := range []string{"Hola, Ana", "Confirmar", `href="https://example.com/confirm?a=1&amp;b=2"`, `<meta charset="utf-8"/>`, `src="cid:logo"`} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("html does not contain %q:\n%s", want, got.HTML)
		}
	}
	if got.CIDs != nil || got.Driver != nil {
		t.Errorf("a template with no resources or driver: %+v", got)
	}

	m.Lang = "en"
	if got := mustRender(t, r, m); got.Subject != "Welcome" || !strings.Contains(got.HTML, "Hi, Ana") {
		t.Errorf("en: %q %q", got.Subject, got.HTML)
	}
	m.Lang = "fr" // no texts: the default language
	if got := mustRender(t, r, m); got.Subject != "Bienvenido" {
		t.Errorf("fr: %q", got.Subject)
	}
}

func TestTextAndLinksAreSafe(t *testing.T) {
	got := mustRender(t, newRenderer(), msg(welcome{Name: `<script>alert(1)</script>`, Link: "javascript:alert(1)"}))
	if strings.Contains(got.HTML, "<script>") || !strings.Contains(got.HTML, "&lt;script&gt;") {
		t.Errorf("text not escaped: %s", got.HTML)
	}
	if strings.Contains(got.HTML, "javascript:") {
		t.Errorf("unsafe url: %s", got.HTML)
	}
}

func TestPayloadPointersAndUnregisteredTypes(t *testing.T) {
	r := newRenderer()
	ctx := context.Background()
	if got := mustRender(t, r, msg(&welcome{Name: "Ana", Link: "https://example.com"})); !strings.Contains(got.HTML, "Hola, Ana") {
		t.Error("a pointer to a registered type was not read")
	}
	for _, p := range []any{struct{ X int }{1}, "text", 3, nil} {
		if _, err := r.Render(ctx, msg(p)); !errors.Is(err, ErrNotRegistered) {
			t.Errorf("%T: %v", p, err)
		}
	}
	// Registered with a pointer: only the pointer is accepted.
	r2 := New()
	Register(r2, Template[*welcome]{Texts: welcomeTexts, View: func(p *welcome, t localizer.Localizer) hx.INode { return hx.P().Text("x") }})
	if _, err := r2.Render(ctx, msg(welcome{})); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("value for a pointer template: %v", err)
	}
}

func TestSubject(t *testing.T) {
	ctx := context.Background()
	r := New(WithDefaultLang("es"))
	// No subject in the texts.
	Register(r, Template[welcome]{
		Texts: localizer.Dict{"es": {"Greeting": "Hola, %s"}},
		View:  welcomeView,
	})
	if _, err := r.Render(ctx, msg(welcome{})); !errors.Is(err, ErrNoSubject) {
		t.Fatalf("got %v", err)
	}
	// A subject that depends on the payload.
	type code struct{ Value string }
	Register(r, Template[code]{
		Texts:   localizer.Dict{"es": {"Subject": "Tu código"}},
		Subject: func(p code, t localizer.Localizer) string { return t.Get("Subject") + ": " + p.Value },
		View:    func(p code, t localizer.Localizer) hx.INode { return hx.P().Text(p.Value) },
	})
	if got := mustRender(t, r, msg(code{"123456"})); got.Subject != "Tu código: 123456" || got.HTML != "<p>123456</p>" {
		t.Errorf("got %q %q", got.Subject, got.HTML)
	}
}

func TestAViewThatFailsIsAnError(t *testing.T) {
	ctx := context.Background()
	r := New()
	type boom struct{}
	type empty struct{}
	texts := localizer.Dict{"": {"Subject": "s"}}
	Register(r, Template[boom]{Texts: texts, View: func(boom, localizer.Localizer) hx.INode { panic("bad view") }})
	Register(r, Template[empty]{Texts: texts, View: func(empty, localizer.Localizer) hx.INode { return nil }})
	if _, err := r.Render(ctx, msg(boom{})); err == nil || !strings.Contains(err.Error(), "bad view") {
		t.Errorf("panic: %v", err)
	}
	if _, err := r.Render(ctx, msg(empty{})); err == nil {
		t.Error("nil view")
	}
}

func TestRegisterMistakesPanic(t *testing.T) {
	r := New()
	Register(r, Template[welcome]{Texts: welcomeTexts, View: welcomeView})
	for name, f := range map[string]func(){
		"twice":    func() { Register(r, Template[welcome]{Texts: welcomeTexts, View: welcomeView}) },
		"nil view": func() { Register(r, Template[int]{Texts: welcomeTexts}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			f()
		}()
	}
}

type fixedDriver struct{}

func (fixedDriver) Send(context.Context, mail.Request, files.Temp) error { return nil }

// What is only of one template, its resources and its driver, comes with the render.
func TestTemplateResourcesAndDriverComeWithTheRender(t *testing.T) {
	r := New(WithDefaultLang("es"))
	own := fixedDriver{}
	Register(r, Template[welcome]{
		Texts: welcomeTexts, View: welcomeView,
		CIDs:   map[string]string{"logo": "img/special.png"},
		Driver: own,
	})
	got := mustRender(t, r, msg(welcome{Link: "https://example.com"}))
	if got.CIDs["logo"] != "img/special.png" || got.Driver != own {
		t.Fatalf("%+v", got)
	}
	got.CIDs["logo"] = "changed" // what the caller gets is its own copy
	if again := mustRender(t, r, msg(welcome{Link: "https://example.com"})); again.CIDs["logo"] != "img/special.png" {
		t.Error("the template shares its map")
	}
}

func TestPayloadNamesAndDecoding(t *testing.T) {
	r := New()
	Register(r, Template[welcome]{Texts: welcomeTexts, View: welcomeView})
	type code struct{ Value string }
	Register(r, Template[code]{
		Name:  "two-factor", // does not depend on the Go name
		Texts: welcomeTexts,
		View:  func(code, localizer.Localizer) hx.INode { return hx.P() },
	})

	if n, ok := r.PayloadName(welcome{}); !ok || n != "hxmail.welcome" {
		t.Errorf("default name %q, %v", n, ok)
	}
	if n, ok := r.PayloadName(&welcome{}); !ok || n != "hxmail.welcome" {
		t.Errorf("pointer: %q, %v", n, ok)
	}
	if n, _ := r.PayloadName(code{}); n != "two-factor" {
		t.Errorf("custom name %q", n)
	}
	for _, p := range []any{nil, 1, struct{}{}} {
		if _, ok := r.PayloadName(p); ok {
			t.Errorf("%v is not registered", p)
		}
	}

	// A payload goes out as JSON and comes back by value, as the template has it.
	data, _ := json.Marshal(welcome{Name: "Ana", Link: "l"})
	got, err := r.DecodePayload("hxmail.welcome", func(into any) error { return json.Unmarshal(data, into) })
	if err != nil || got != (welcome{Name: "Ana", Link: "l"}) {
		t.Fatalf("got %#v, %v", got, err)
	}
	if _, err := r.DecodePayload("two-factor", func(into any) error { return json.Unmarshal([]byte(`{"Value":"7"}`), into) }); err != nil {
		t.Fatal(err)
	}
	// The format error is the caller's.
	boom := errors.New("bad json")
	if _, err := r.DecodePayload("hxmail.welcome", func(any) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("got %v", err)
	}
	// A name nobody registered: not registered, and trying again does not fix it.
	_, err = r.DecodePayload("nope", func(any) error { return nil })
	var p interface{ Permanent() bool }
	if !errors.Is(err, ErrNotRegistered) || !errors.As(err, &p) || !p.Permanent() {
		t.Errorf("got %v", err)
	}
	if !errors.As(ErrNoSubject, &p) || !p.Permanent() {
		t.Error("ErrNoSubject must be permanent")
	}

	// A template registered with a pointer decodes to a pointer.
	r2 := New()
	Register(r2, Template[*welcome]{Texts: welcomeTexts, View: func(*welcome, localizer.Localizer) hx.INode { return hx.P() }})
	got, err = r2.DecodePayload("*hxmail.welcome", func(into any) error { return json.Unmarshal(data, into) })
	if w, ok := got.(*welcome); err != nil || !ok || w.Name != "Ana" {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestNamesCannotRepeat(t *testing.T) {
	r := New()
	Register(r, Template[welcome]{Name: "same", Texts: welcomeTexts, View: welcomeView})
	defer func() {
		if recover() == nil {
			t.Fatal("two templates with the same name must panic")
		}
	}()
	Register(r, Template[int]{Name: "same", Texts: welcomeTexts, View: func(int, localizer.Localizer) hx.INode { return hx.P() }})
}

func TestRenderIsSafeForConcurrentUse(t *testing.T) {
	r := newRenderer()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := msg(welcome{Name: "Ana", Link: "https://example.com"})
			m.Lang = []string{"es", "en"}[i%2]
			got, err := r.Render(context.Background(), m)
			if err != nil {
				t.Error(err)
				return
			}
			if (got.Subject == "Welcome") != strings.Contains(got.HTML, "Hi, Ana") {
				t.Errorf("subject %q does not match the body", got.Subject)
			}
		}()
	}
	wg.Wait()
}
