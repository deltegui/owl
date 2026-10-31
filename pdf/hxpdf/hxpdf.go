// Package hxpdf renders the HTML of the PDF documents with Hix: for each kind
// of payload it registers a view (written with Hix and rendered with hx/ssr)
// and its texts (a localizer.Dict). It is a pdf.Renderer; it does not make the
// PDF, pdf.Service does with a driver.
//
// It is apart from package pdf so a program that only needs the driver does not
// import hx/ssr.
package hxpdf

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/deltegui/hx"
	"github.com/deltegui/hx/ssr"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/pdf"
)

// ErrNotRegistered is returned when the type of the payload has no template.
var ErrNotRegistered = errors.New("hxpdf: payload type not registered")

// Renderer renders the documents with Hix: the type of the payload decides the
// template. It is safe for concurrent use once the templates are registered,
// which is done when the program starts.
type Renderer struct {
	lang      string
	templates map[reflect.Type]entry
}

var _ pdf.Renderer = (*Renderer)(nil)

// Option changes how a Renderer works.
type Option func(*Renderer)

// WithDefaultLang sets the language of the documents that do not say one, and
// the one used when a template has no texts in the requested language. It
// should be the default language of the application.
func WithDefaultLang(lang string) Option { return func(r *Renderer) { r.lang = lang } }

// New creates a Renderer with no templates. Add them with Register.
func New(opts ...Option) *Renderer {
	r := &Renderer{templates: map[reflect.Type]entry{}}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Template is how to build one kind of document, the one whose payload has type P.
type Template[P any] struct {
	// Texts are the translations. The language is the requested one, or the
	// default one of the Renderer if there are no texts in it (see localizer.Pick).
	Texts localizer.Source
	// View builds the document. If it builds an <html> element, the renderer
	// writes <!doctype html> before it. Text is escaped and URLs are checked by Hix.
	View func(payload P, texts localizer.Localizer) hx.INode
	// Options are the paper, margins... of this document.
	Options pdf.Options
	// Driver makes this document instead of the one of the pdf.Service.
	Driver pdf.Driver
}

type entry struct {
	options pdf.Options
	driver  pdf.Driver
	texts   localizer.Source
	render  func(payload any, loc localizer.Localizer) (string, error)
}

// Register adds a template to the Renderer. The document is the one whose
// payload has type P (or *P). It panics if View is nil or the type already has
// a template: it is a mistake in the program, not something to find when rendering.
func Register[P any](r *Renderer, t Template[P]) {
	if t.View == nil {
		panic("hxpdf: Register with a nil View")
	}
	key := reflect.TypeFor[P]()
	if _, ok := r.templates[key]; ok {
		panic(fmt.Sprintf("hxpdf: %v already has a template", key))
	}
	r.templates[key] = entry{
		options: t.Options,
		driver:  t.Driver,
		texts:   t.Texts,
		render: func(payload any, loc localizer.Localizer) (string, error) {
			p, ok := payload.(P)
			if !ok { // registered by value and given a pointer
				if rv := reflect.ValueOf(payload); rv.Kind() == reflect.Pointer && !rv.IsNil() {
					p, ok = rv.Elem().Interface().(P)
				}
			}
			if !ok {
				return "", fmt.Errorf("%w: %T", ErrNotRegistered, payload)
			}
			return render(func() hx.INode { return t.View(p, loc) })
		},
	}
}

// Render builds the HTML of the payload, in the language lang.
func (r *Renderer) Render(_ context.Context, payload any, lang string) (pdf.Rendered, error) {
	t := reflect.TypeOf(payload)
	if t == nil {
		return pdf.Rendered{}, pdf.ErrNoPayload
	}
	tpl, ok := r.templates[t]
	if !ok && t.Kind() == reflect.Pointer {
		tpl, ok = r.templates[t.Elem()]
	}
	if !ok {
		return pdf.Rendered{}, fmt.Errorf("%w: %T", ErrNotRegistered, payload)
	}
	var loc localizer.Localizer
	if tpl.texts != nil {
		loc = localizer.Pick(tpl.texts, cmp.Or(lang, r.lang), r.lang)
	}
	html, err := tpl.render(payload, loc)
	if err != nil {
		return pdf.Rendered{}, err
	}
	return pdf.Rendered{HTML: html, Options: tpl.options, Driver: tpl.driver}, nil
}

// render builds the view on the server. A panic in the view is an error.
func render(build func() hx.INode) (html string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hxpdf: the view panicked: %v", r)
		}
	}()
	html = ssr.Render(build)
	if html == "" {
		return "", errors.New("hxpdf: the view built nothing")
	}
	if strings.HasPrefix(html, "<html") {
		html = "<!doctype html>" + html
	}
	return html, nil
}
