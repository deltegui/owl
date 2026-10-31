package hxmail

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"path"
	"reflect"
	"strings"

	"github.com/deltegui/hx"
	"github.com/deltegui/hx/ssr"
	"github.com/deltegui/owl/v3/files"
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

// Engine renders the emails with Hix and gives them to a mail.Driver. It is a
// mail.Sender: a service gives it a mail.Message and the type of the payload
// decides the template. It is safe for concurrent use once the templates are
// registered, which is done when the program starts.
type Engine struct {
	driver    mail.Driver
	tmp       files.Temp
	resources files.Directory
	lang      string
	cids      map[string]string
	templates map[reflect.Type]entry
	byName    map[string]entry
}

// Option changes how an Engine works.
type Option func(*Engine)

// WithDefaultLang sets the language of the emails that do not say one, and the
// one used when a template has no texts in the language of the email. It should
// be the default language of the application.
func WithDefaultLang(lang string) Option { return func(e *Engine) { e.lang = lang } }

// WithResources sets the directory where the resources of the emails (a
// logo...) are. See WithCID.
func WithResources(dir files.Directory) Option { return func(e *Engine) { e.resources = dir } }

// WithCID adds a resource used by all the emails, a logo for example: the
// views write it as <img src="cid:id">. The path is relative to the
// resources directory (WithResources). Without one, it is a path in the Temp
// directory. A template can add more in Template.CIDs.
func WithCID(id, path string) Option { return func(e *Engine) { e.cids[id] = path } }

// New creates an Engine. The driver sends what the engine renders; tmp is
// where the driver finds the attachments and the resources.
func New(driver mail.Driver, tmp files.Temp, opts ...Option) *Engine {
	if driver == nil {
		panic("hxmail: New with a nil driver")
	}
	e := &Engine{driver: driver, tmp: tmp, cids: map[string]string{}, templates: map[reflect.Type]entry{}, byName: map[string]entry{}}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Template is how to build one kind of email, the one whose payload has type P.
type Template[P any] struct {
	// Texts are the translations. Unless Subject is set, they must have the key
	// SubjectKey. The language is the one of the message, or the default one of
	// the Engine if there are no texts in it (see localizer.Pick).
	Texts localizer.Source
	// View builds the body of the email. If it builds an <html> element, the
	// engine writes <!doctype html> before it. Text is escaped and URLs are
	// checked by Hix, as in a page. Email clients support little CSS, so write
	// the styles in the elements with Style.
	View func(payload P, texts localizer.Localizer) hx.INode
	// Subject builds the subject when it is not just Texts[SubjectKey].
	Subject func(payload P, texts localizer.Localizer) string
	// CIDs are the resources of this email, with the same rules as WithCID.
	// They win over the ones of the Engine.
	CIDs map[string]string
	// Driver sends this email instead of the one of the Engine.
	Driver mail.Driver
	// Name identifies the payload when it is stored (see PayloadName). It is the
	// type, like "account.UserConfirmation", if it is empty. Set it if you want to
	// rename or move the type without leaving unsent emails that cannot be read.
	Name string
}

// Register adds a template to the Engine. The email is the one whose payload
// has type P (or *P: a pointer to it is also accepted). It panics if View is
// nil or the type already has a template: it is a mistake in the program, not
// something to find when sending.
func Register[P any](e *Engine, t Template[P]) {
	if t.View == nil {
		panic("hxmail: Register with a nil View")
	}
	key := reflect.TypeFor[P]()
	if _, ok := e.templates[key]; ok {
		panic(fmt.Sprintf("hxmail: %v already has a template", key))
	}
	name := cmp.Or(t.Name, key.String())
	if _, ok := e.byName[name]; ok {
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
	e.templates[key] = en
	e.byName[name] = en
}

type entry struct {
	name   string
	texts  localizer.Source
	cids   map[string]string
	driver mail.Driver
	render func(payload any, loc localizer.Localizer) (html, subject string, err error)
	decode func(into func(any) error) (any, error)
}

// Send renders the message and gives it to the driver. It returns what the
// driver returns, so a nil error means what it means for that driver.
func (e *Engine) Send(ctx context.Context, msg mail.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	tpl, ok := e.lookup(msg.Payload)
	if !ok {
		return fmt.Errorf("%w: %T", ErrNotRegistered, msg.Payload)
	}

	loc := localizer.Pick(tpl.texts, cmp.Or(msg.Lang, e.lang), e.lang)
	html, subject, err := tpl.render(msg.Payload, loc)
	if err != nil {
		return err
	}
	cids, err := e.resolveCIDs(ctx, tpl)
	if err != nil {
		return err
	}
	driver := e.driver
	if tpl.driver != nil {
		driver = tpl.driver
	}
	return driver.Send(ctx, mail.Request{
		Subject:  subject,
		HTML:     html,
		Envelope: msg.Envelope,
		CIDs:     cids,
		Payload:  msg.Payload,
	}, e.tmp)
}

func (e *Engine) lookup(payload any) (entry, bool) {
	t := reflect.TypeOf(payload)
	if t == nil {
		return entry{}, false
	}
	tpl, ok := e.templates[t]
	if !ok && t.Kind() == reflect.Pointer {
		tpl, ok = e.templates[t.Elem()]
	}
	return tpl, ok
}

// PayloadName is the name the payload is stored with (Template.Name). The
// bool is false if its type has no template. With DecodePayload, it is what
// package outbox needs to keep an email and send it later.
func (e *Engine) PayloadName(payload any) (string, bool) {
	tpl, ok := e.lookup(payload)
	return tpl.name, ok
}

// DecodePayload makes a payload of the type registered with that name. It
// calls decode with a pointer to the new value, so the caller chooses the
// format (JSON...) and hxmail does not import it; the payload is returned by
// value, as Template[P] has it. The error is ErrNotRegistered if there is no
// template with that name.
func (e *Engine) DecodePayload(name string, decode func(into any) error) (any, error) {
	tpl, ok := e.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotRegistered, name)
	}
	return tpl.decode(decode)
}

// resolveCIDs joins the resources of the engine and the template. If there is
// a resources directory they are copied to the Temp directory, where the
// driver reads them: a Temp that expires (files.Temporal) would delete a logo
// that was put there once.
func (e *Engine) resolveCIDs(ctx context.Context, tpl entry) (map[string]string, error) {
	if len(e.cids) == 0 && len(tpl.cids) == 0 {
		return nil, nil
	}
	cids := maps.Clone(e.cids)
	maps.Copy(cids, tpl.cids)
	if e.resources == nil {
		return cids, nil
	}
	for id, p := range cids {
		dest := rand.Text() + "/" + path.Base(p) // a folder of its own: sends do not share files
		stored, err := e.resources.CopyTo(ctx, p, dest, e.tmp)
		if err != nil {
			return nil, fmt.Errorf("hxmail: resource %q (cid %s): %w", p, id, err)
		}
		cids[id] = stored
	}
	return cids, nil
}

// render builds the view on the server. A panic in the view is an error: the
// engine usually runs where a panic would take the program down.
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
