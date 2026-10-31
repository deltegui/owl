// Package hxmail renders emails with Hix: for each kind of payload it registers
// a view (written with Hix and rendered with hx/ssr) and its texts (a
// localizer.Dict), and gives back the subject and the HTML. It is a
// mail.Renderer; it does not send anything: mail.Service does, with a driver.
//
// It is apart from package mail so a service that only sends emails does not
// import hx/ssr.
package hxmail

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/deltegui/hx"
	"github.com/deltegui/hx/ssr"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/mail"
)

// Hix replaces the URLs with a scheme it does not know (see hx.SafeAttribute),
// and the views of an email need "cid:" for their inline resources. It is
// allowed here, in init, because hx asks for it before anything is rendered.
// In a web page it is harmless: a browser cannot resolve it.
func init() { hx.AllowURLScheme("cid") }

// SubjectKey is the key of the texts of a template with the subject of the email.
const SubjectKey = "Subject"

var (
	// ErrNotRegistered is returned when the type of the payload has no template.
	ErrNotRegistered error = permanent("hxmail: payload type not registered")
	// ErrNoSubject is returned when a template has no subject in the language of the email.
	ErrNoSubject error = permanent("hxmail: no subject")
)

// permanent is an error that trying again does not fix: it says so with a
// Permanent method, which package outbox looks for, so hxmail does not need to
// import it.
type permanent string

func (e permanent) Error() string   { return string(e) }
func (e permanent) Permanent() bool { return true }

// Renderer renders the emails with Hix: the type of the payload of a message
// decides the template. It is a mail.Renderer, and with its PayloadName and
// DecodePayload it is the outbox.Payloads that package outbox needs, so the
// types are registered only once. It is safe for concurrent use once the
// templates are registered, which is done when the program starts.
type Renderer struct {
	lang      string
	templates map[reflect.Type]entry
	byName    map[string]entry
}

var _ mail.Renderer = (*Renderer)(nil)

// Option changes how a Renderer works.
type Option func(*Renderer)

// WithDefaultLang sets the language of the emails that do not say one, and the
// one used when a template has no texts in the language of the email. It should
// be the default language of the application.
func WithDefaultLang(lang string) Option { return func(r *Renderer) { r.lang = lang } }

// New creates a Renderer with no templates. Add them with Register.
func New(opts ...Option) *Renderer {
	r := &Renderer{templates: map[reflect.Type]entry{}, byName: map[string]entry{}}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Template is how to build one kind of email, the one whose payload has type P.
type Template[P any] struct {
	// Texts are the translations. Unless Subject is set, they must have the key
	// SubjectKey. The language is the one of the message, or the default one of
	// the Renderer if there are no texts in it (see localizer.Pick).
	Texts localizer.Source
	// View builds the body of the email. If it builds an <html> element, the
	// renderer writes <!doctype html> before it. Text is escaped and URLs are
	// checked by Hix, as in a page. Email clients support little CSS, so write
	// the styles in the elements with Style.
	View func(payload P, texts localizer.Localizer) hx.INode
	// Subject builds the subject when it is not just Texts[SubjectKey].
	Subject func(payload P, texts localizer.Localizer) string
	// CIDs are the resources of this email (see mail.WithCID). They win over the
	// ones of the mail.Service.
	CIDs map[string]string
	// Driver sends this email instead of the one of the mail.Service.
	Driver mail.Driver
	// Name identifies the payload when it is stored (see PayloadName). It is the
	// type, like "account.UserConfirmation", if it is empty. Set it if you want to
	// rename or move the type without leaving unsent emails that cannot be read.
	Name string
}

// Register adds a template to the Renderer. The email is the one whose payload
// has type P (or *P: a pointer to it is also accepted). It panics if View is
// nil or the type, or the name, already has a template: it is a mistake in the
// program, not something to find when sending.
func Register[P any](r *Renderer, t Template[P]) {
	if t.View == nil {
		panic("hxmail: Register with a nil View")
	}
	key := reflect.TypeFor[P]()
	if _, ok := r.templates[key]; ok {
		panic(fmt.Sprintf("hxmail: %v already has a template", key))
	}
	name := cmp.Or(t.Name, key.String())
	if _, ok := r.byName[name]; ok {
		panic(fmt.Sprintf("hxmail: the name %q is used by another template", name))
	}
	en := entry{
		name:   name,
		texts:  t.Texts,
		cids:   maps.Clone(t.CIDs),
		driver: t.Driver,
		render: func(payload any, loc localizer.Localizer) (html, subject string, err error) {
			p, ok := payload.(P)
			if !ok { // registered by value and given a pointer
				if rv := reflect.ValueOf(payload); rv.Kind() == reflect.Pointer && !rv.IsNil() {
					p, ok = rv.Elem().Interface().(P)
				}
			}
			if !ok {
				return "", "", fmt.Errorf("%w: %T", ErrNotRegistered, payload)
			}
			if t.Subject != nil {
				subject = t.Subject(p, loc)
			} else {
				subject = loc[SubjectKey]
			}
			if strings.TrimSpace(subject) == "" {
				return "", "", fmt.Errorf("%w for %T", ErrNoSubject, payload)
			}
			html, err = render(func() hx.INode { return t.View(p, loc) })
			return html, subject, err
		},
		decode: func(into func(any) error) (any, error) {
			var p P
			if err := into(&p); err != nil {
				return nil, err
			}
			return p, nil
		},
	}
	r.templates[key] = en
	r.byName[name] = en
}

type entry struct {
	name   string
	texts  localizer.Source
	cids   map[string]string
	driver mail.Driver
	render func(payload any, loc localizer.Localizer) (html, subject string, err error)
	decode func(into func(any) error) (any, error)
}

// Render builds the subject and the HTML of the message, in its language. It
// does not check the message: mail.Service does that before it calls a renderer.
func (r *Renderer) Render(_ context.Context, msg mail.Message) (mail.Rendered, error) {
	tpl, ok := r.lookup(msg.Payload)
	if !ok {
		return mail.Rendered{}, fmt.Errorf("%w: %T", ErrNotRegistered, msg.Payload)
	}
	loc := localizer.Pick(tpl.texts, cmp.Or(msg.Lang, r.lang), r.lang)
	html, subject, err := tpl.render(msg.Payload, loc)
	if err != nil {
		return mail.Rendered{}, err
	}
	return mail.Rendered{Subject: subject, HTML: html, CIDs: maps.Clone(tpl.cids), Driver: tpl.driver}, nil
}

func (r *Renderer) lookup(payload any) (entry, bool) {
	t := reflect.TypeOf(payload)
	if t == nil {
		return entry{}, false
	}
	tpl, ok := r.templates[t]
	if !ok && t.Kind() == reflect.Pointer {
		tpl, ok = r.templates[t.Elem()]
	}
	return tpl, ok
}

// PayloadName is the name the payload is stored with (Template.Name). The
// bool is false if its type has no template. With DecodePayload, it is what
// package outbox needs to keep an email and send it later.
func (r *Renderer) PayloadName(payload any) (string, bool) {
	tpl, ok := r.lookup(payload)
	return tpl.name, ok
}

// DecodePayload makes a payload of the type registered with that name. It
// calls decode with a pointer to the new value, so the caller chooses the
// format (JSON...) and hxmail does not import it; the payload is returned by
// value, as Template[P] has it. The error is ErrNotRegistered if there is no
// template with that name.
func (r *Renderer) DecodePayload(name string, decode func(into any) error) (any, error) {
	tpl, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotRegistered, name)
	}
	return tpl.decode(decode)
}

// render builds the view on the server. A panic in the view is an error: the
// renderer usually runs where a panic would take the program down.
func render(build func() hx.INode) (html string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hxmail: the view panicked: %v", r)
		}
	}()
	html = ssr.Render(build)
	if html == "" {
		return "", errors.New("hxmail: the view built nothing")
	}
	if strings.HasPrefix(html, "<html") {
		html = "<!doctype html>" + html
	}
	return html, nil
}
