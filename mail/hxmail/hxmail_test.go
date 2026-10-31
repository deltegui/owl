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

// recorder is a driver that keeps what it receives and reads the resources
// from the Temp it is given.
type recorder struct {
	mu        sync.Mutex
	got       []mail.Request
	resources map[string]string // content of the cids, read from the Temp
}

func (r *recorder) Send(ctx context.Context, req mail.Request, tmp files.Temp) error {
	res := map[string]string{}
	for id, p := range req.CIDs {
		if tmp.Directory == nil {
			continue
		}
		if s, err := files.ReadString(ctx, tmp, p); err == nil {
			res[id] = s
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, req)
	r.resources = res
	return nil
}

func (r *recorder) last(t *testing.T) mail.Request {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) == 0 {
		t.Fatal("the driver received nothing")
	}
	return r.got[len(r.got)-1]
}

func msg(p any) mail.Message {
	return mail.Message{Envelope: mail.Envelope{To: []string{"ana@example.com"}}, Payload: p}
}

func newEngine(t *testing.T, opts ...Option) (*Engine, *recorder) {
	t.Helper()
	d := &recorder{}
	e := New(d, files.Temp{}, append([]Option{WithDefaultLang("es")}, opts...)...)
	Register(e, Template[welcome]{Texts: welcomeTexts, View: welcomeView})
	return e, d
}

func TestRendersTheViewInTheLanguageOfTheMessage(t *testing.T) {
	e, d := newEngine(t)
	var s mail.Sender = e
	ctx := context.Background()

	m := msg(welcome{Name: "Ana", Link: "https://example.com/confirm?a=1&b=2"})
	m.Envelope.Attachments = []string{"tmp/report.pdf"}
	if err := s.Send(ctx, m); err != nil {
		t.Fatal(err)
	}
	req := d.last(t)
	if req.Subject != "Bienvenido" {
		t.Errorf("subject %q", req.Subject)
	}
	if !strings.HasPrefix(req.HTML, "<!doctype html><html>") {
		t.Errorf("html %q", req.HTML)
	}
	for _, want := range []string{"Hola, Ana", "Confirmar", `href="https://example.com/confirm?a=1&amp;b=2"`, `<meta charset="utf-8"/>`, `src="cid:logo"`} {
		if !strings.Contains(req.HTML, want) {
			t.Errorf("html does not contain %q:\n%s", want, req.HTML)
		}
	}
	if req.Envelope.To[0] != "ana@example.com" || req.Envelope.Attachments[0] != "tmp/report.pdf" {
		t.Errorf("envelope %+v", req.Envelope)
	}
	if p, ok := req.Payload.(welcome); !ok || p.Name != "Ana" {
		t.Errorf("payload %#v", req.Payload)
	}

	m.Lang = "en"
	s.Send(ctx, m)
	if req := d.last(t); req.Subject != "Welcome" || !strings.Contains(req.HTML, "Hi, Ana") {
		t.Errorf("en: %q %q", req.Subject, req.HTML)
	}

	m.Lang = "fr" // no texts: the default language
	s.Send(ctx, m)
	if req := d.last(t); req.Subject != "Bienvenido" {
		t.Errorf("fr: %q", req.Subject)
	}
}

func TestTextAndLinksAreSafe(t *testing.T) {
	e, d := newEngine(t)
	err := e.Send(context.Background(), msg(welcome{Name: `<script>alert(1)</script>`, Link: "javascript:alert(1)"}))
	if err != nil {
		t.Fatal(err)
	}
	html := d.last(t).HTML
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("text not escaped: %s", html)
	}
	if strings.Contains(html, "javascript:") {
		t.Errorf("unsafe url: %s", html)
	}
}

func TestPayloadPointersAndUnregisteredTypes(t *testing.T) {
	e, d := newEngine(t)
	ctx := context.Background()
	if err := e.Send(ctx, msg(&welcome{Name: "Ana", Link: "https://example.com"})); err != nil {
		t.Fatalf("pointer to a registered type: %v", err)
	}
	if !strings.Contains(d.last(t).HTML, "Hola, Ana") {
		t.Error("the pointer was not read")
	}

	n := len(d.got)
	for _, p := range []any{struct{ X int }{1}, "text", 3} {
		if err := e.Send(ctx, msg(p)); !errors.Is(err, ErrNotRegistered) {
			t.Errorf("%T: %v", p, err)
		}
	}
	// Registered with a pointer: only the pointer is accepted.
	e2 := New(&recorder{}, files.Temp{})
	Register(e2, Template[*welcome]{Texts: welcomeTexts, View: func(p *welcome, t localizer.Localizer) hx.INode { return hx.P().Text("x") }})
	if err := e2.Send(ctx, msg(welcome{})); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("value for a pointer template: %v", err)
	}
	if len(d.got) != n {
		t.Error("the driver was called")
	}
}

func TestInvalidMessagesDoNotReachTheDriver(t *testing.T) {
	e, d := newEngine(t)
	err := e.Send(context.Background(), mail.Message{Payload: welcome{}})
	if !errors.Is(err, mail.ErrInvalid) || len(d.got) != 0 {
		t.Fatalf("got %v (%d calls)", err, len(d.got))
	}
}

func TestSubject(t *testing.T) {
	ctx := context.Background()
	d := &recorder{}
	e := New(d, files.Temp{}, WithDefaultLang("es"))
	// No subject in the texts.
	Register(e, Template[welcome]{
		Texts: localizer.Dict{"es": {"Greeting": "Hola, %s"}},
		View:  welcomeView,
	})
	if err := e.Send(ctx, msg(welcome{})); !errors.Is(err, ErrNoSubject) {
		t.Fatalf("got %v", err)
	}
	// A subject that depends on the payload.
	type code struct{ Value string }
	Register(e, Template[code]{
		Texts:   localizer.Dict{"es": {"Subject": "Tu código"}},
		Subject: func(p code, t localizer.Localizer) string { return t.Get("Subject") + ": " + p.Value },
		View:    func(p code, t localizer.Localizer) hx.INode { return hx.P().Text(p.Value) },
	})
	if err := e.Send(ctx, msg(code{"123456"})); err != nil {
		t.Fatal(err)
	}
	if got := d.last(t); got.Subject != "Tu código: 123456" || got.HTML != "<p>123456</p>" {
		t.Errorf("got %q %q", got.Subject, got.HTML)
	}
}

func TestAViewThatFailsIsAnError(t *testing.T) {
	ctx := context.Background()
	d := &recorder{}
	e := New(d, files.Temp{})
	type boom struct{}
	type empty struct{}
	texts := localizer.Dict{"": {"Subject": "s"}}
	Register(e, Template[boom]{Texts: texts, View: func(boom, localizer.Localizer) hx.INode { panic("bad view") }})
	Register(e, Template[empty]{Texts: texts, View: func(empty, localizer.Localizer) hx.INode { return nil }})
	if err := e.Send(ctx, msg(boom{})); err == nil || !strings.Contains(err.Error(), "bad view") {
		t.Errorf("panic: %v", err)
	}
	if err := e.Send(ctx, msg(empty{})); err == nil {
		t.Error("nil view")
	}
	if len(d.got) != 0 {
		t.Error("the driver was called")
	}
}

func TestRegisterMistakesPanic(t *testing.T) {
	e := New(&recorder{}, files.Temp{})
	Register(e, Template[welcome]{Texts: welcomeTexts, View: welcomeView})
	for name, f := range map[string]func(){
		"twice":      func() { Register(e, Template[welcome]{Texts: welcomeTexts, View: welcomeView}) },
		"nil view":   func() { Register(e, Template[int]{Texts: welcomeTexts}) },
		"nil driver": func() { New(nil, files.Temp{}) },
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

func TestTemplateDriver(t *testing.T) {
	engineDriver, own := &recorder{}, &recorder{}
	e := New(engineDriver, files.Temp{}, WithDefaultLang("es"))
	Register(e, Template[welcome]{Texts: welcomeTexts, View: welcomeView, Driver: own})
	if err := e.Send(context.Background(), msg(welcome{Link: "https://example.com"})); err != nil {
		t.Fatal(err)
	}
	if len(own.got) != 1 || len(engineDriver.got) != 0 {
		t.Fatalf("own %d, engine %d", len(own.got), len(engineDriver.got))
	}
}

func local(t *testing.T) *files.Local {
	t.Helper()
	l, err := files.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestResourcesAreCopiedToTemp(t *testing.T) {
	ctx := context.Background()
	resources, tmpDir := local(t), local(t)
	files.WriteString(ctx, resources, "img/logo.png", "LOGO")
	files.WriteString(ctx, resources, "img/other.png", "OTHER")
	files.WriteString(ctx, resources, "img/special.png", "SPECIAL")
	// The Temp renames what it stores: the engine must use the path it gives back.
	tmp := files.AsTemp(files.Of(tmpDir).Add(files.Folder("mail")).Naming(files.GUID()).Build())

	d := &recorder{}
	e := New(d, tmp, WithDefaultLang("es"), WithResources(resources),
		WithCID("logo", "img/logo.png"), WithCID("other", "img/other.png"))
	Register(e, Template[welcome]{
		Texts: welcomeTexts, View: welcomeView,
		CIDs: map[string]string{"logo": "img/special.png"}, // wins over the one of the engine
	})
	if err := e.Send(ctx, msg(welcome{Link: "https://example.com"})); err != nil {
		t.Fatal(err)
	}
	req := d.last(t)
	if len(req.CIDs) != 2 || !strings.HasPrefix(req.CIDs["logo"], "mail/") || strings.Contains(req.CIDs["logo"], "special") {
		t.Fatalf("cids %v", req.CIDs)
	}
	if d.resources["logo"] != "SPECIAL" || d.resources["other"] != "OTHER" {
		t.Fatalf("content in the Temp: %v", d.resources)
	}
	// The next email gets its own copy.
	e.Send(ctx, msg(welcome{Link: "https://example.com"}))
	if d.last(t).CIDs["other"] == req.CIDs["other"] {
		t.Error("two emails share a file in the Temp")
	}
	// A resource that does not exist is an error and nothing is sent.
	n := len(d.got)
	bad := New(d, tmp, WithDefaultLang("es"), WithResources(resources), WithCID("x", "img/nope.png"))
	Register(bad, Template[welcome]{Texts: welcomeTexts, View: welcomeView})
	if err := bad.Send(ctx, msg(welcome{})); !errors.Is(err, files.ErrNotFound) || len(d.got) != n {
		t.Fatalf("got %v", err)
	}
}

func TestCIDsAreTempPathsWithoutResources(t *testing.T) {
	ctx := context.Background()
	tmpDir := local(t)
	files.WriteString(ctx, tmpDir, "logo.png", "LOGO")
	d := &recorder{}
	e := New(d, files.AsTemp(tmpDir), WithDefaultLang("es"), WithCID("logo", "logo.png"))
	Register(e, Template[welcome]{Texts: welcomeTexts, View: welcomeView})
	if err := e.Send(ctx, msg(welcome{Link: "https://example.com"})); err != nil {
		t.Fatal(err)
	}
	if got := d.last(t).CIDs; len(got) != 1 || got["logo"] != "logo.png" || d.resources["logo"] != "LOGO" {
		t.Fatalf("cids %v, %v", got, d.resources)
	}
}

func TestSendIsSafeForConcurrentUse(t *testing.T) {
	e, d := newEngine(t)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := msg(welcome{Name: "Ana", Link: "https://example.com"})
			m.Lang = []string{"es", "en"}[i%2]
			if err := e.Send(context.Background(), m); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(d.got) != 30 {
		t.Fatalf("%d emails", len(d.got))
	}
	for _, r := range d.got {
		if (r.Subject == "Welcome") != strings.Contains(r.HTML, "Hi, Ana") {
			t.Fatalf("subject %q does not match the body", r.Subject)
		}
	}
}

func TestPayloadNamesAndDecoding(t *testing.T) {
	e := New(&recorder{}, files.Temp{})
	Register(e, Template[welcome]{Texts: welcomeTexts, View: welcomeView})
	type code struct{ Value string }
	Register(e, Template[code]{
		Name:  "two-factor", // does not depend on the Go name
		Texts: welcomeTexts,
		View:  func(code, localizer.Localizer) hx.INode { return hx.P() },
	})

	if n, ok := e.PayloadName(welcome{}); !ok || n != "hxmail.welcome" {
		t.Errorf("default name %q, %v", n, ok)
	}
	if n, ok := e.PayloadName(&welcome{}); !ok || n != "hxmail.welcome" {
		t.Errorf("pointer: %q, %v", n, ok)
	}
	if n, _ := e.PayloadName(code{}); n != "two-factor" {
		t.Errorf("custom name %q", n)
	}
	for _, p := range []any{nil, 1, struct{}{}} {
		if _, ok := e.PayloadName(p); ok {
			t.Errorf("%v is not registered", p)
		}
	}

	// A payload goes out as JSON and comes back by value, as the template has it.
	data, _ := json.Marshal(welcome{Name: "Ana", Link: "l"})
	got, err := e.DecodePayload("hxmail.welcome", func(into any) error { return json.Unmarshal(data, into) })
	if err != nil || got != (welcome{Name: "Ana", Link: "l"}) {
		t.Fatalf("got %#v, %v", got, err)
	}
	if _, err := e.DecodePayload("two-factor", func(into any) error { return json.Unmarshal([]byte(`{"Value":"7"}`), into) }); err != nil {
		t.Fatal(err)
	}
	// The format error is the caller's.
	boom := errors.New("bad json")
	if _, err := e.DecodePayload("hxmail.welcome", func(any) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("got %v", err)
	}
	// A name nobody registered: not registered, and trying again does not fix it.
	_, err = e.DecodePayload("nope", func(any) error { return nil })
	var p interface{ Permanent() bool }
	if !errors.Is(err, ErrNotRegistered) || !errors.As(err, &p) || !p.Permanent() {
		t.Errorf("got %v", err)
	}
	if !errors.As(ErrNoSubject, &p) || !p.Permanent() {
		t.Error("ErrNoSubject must be permanent")
	}

	// A template registered with a pointer decodes to a pointer.
	e2 := New(&recorder{}, files.Temp{})
	Register(e2, Template[*welcome]{Texts: welcomeTexts, View: func(*welcome, localizer.Localizer) hx.INode { return hx.P() }})
	got, err = e2.DecodePayload("*hxmail.welcome", func(into any) error { return json.Unmarshal(data, into) })
	if w, ok := got.(*welcome); err != nil || !ok || w.Name != "Ana" {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestNamesCannotRepeat(t *testing.T) {
	e := New(&recorder{}, files.Temp{})
	Register(e, Template[welcome]{Name: "same", Texts: welcomeTexts, View: welcomeView})
	defer func() {
		if recover() == nil {
			t.Fatal("two templates with the same name must panic")
		}
	}()
	Register(e, Template[int]{Name: "same", Texts: welcomeTexts, View: func(int, localizer.Localizer) hx.INode { return hx.P() }})
}
