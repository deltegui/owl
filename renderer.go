package owl

import (
	"fmt"
	"html/template"
	"io"
	"strings"

	"github.com/deltegui/hx"
	"github.com/deltegui/hx/ssr"
)

// Renderer turns a view into the body of the response. ctx.View calls the one of
// the Mux (see SetRenderer), and Hix is the default.
//
// The view is what each renderer understands: HixRenderer takes a HixView and
// TemplateRenderer takes a TemplateView. Another renderer can take anything.
type Renderer interface {
	Render(ctx Ctx, view any) error
}

// DefaultRenderer is what a Mux uses until SetRenderer is called.
var DefaultRenderer Renderer = HixRenderer{}

// HixView builds a page with Hix. It gets the Ctx, so it can read the model
// state, the user, the CSRF token or the localizer.
//
//	func index(ctx owl.Ctx) error {
//		return ctx.View(owl.HixView(func(ctx owl.Ctx) hx.INode {
//			return hx.El("html").Body(hx.El("body").Body(hx.H1().Text("Hello")))
//		}))
//	}
type HixView func(ctx Ctx) hx.INode

// HixRenderer renders a HixView on the server with hx/ssr. If the view builds an
// <html> element it writes <!doctype html> first. Attributes such as href and src
// are checked by Hix (hx.SafeAttribute) and text is escaped.
type HixRenderer struct{}

func (HixRenderer) Render(ctx Ctx, view any) error {
	var build func(Ctx) hx.INode
	switch v := view.(type) {
	case HixView:
		build = v
	case func(Ctx) hx.INode:
		build = v
	default:
		return fmt.Errorf("owl: HixRenderer cannot render a %T, use an owl.HixView", view)
	}
	html := ssr.Render(func() hx.INode { return build(ctx) })
	return writePage(ctx.Res, html)
}

func writePage(w io.Writer, html string) error {
	if strings.HasPrefix(html, "<html") {
		if _, err := io.WriteString(w, "<!doctype html>"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, html)
	return err
}

// TemplateView is a view for html/template: the parsed templates, the name to
// execute and the model, like the arguments of Ctx.Render.
type TemplateView struct {
	Templates *template.Template
	Name      string
	Model     any
}

// TemplateRenderer renders a TemplateView with html/template and the ViewModel
// of Ctx.Render, so existing views work unchanged.
type TemplateRenderer struct{}

func (TemplateRenderer) Render(ctx Ctx, view any) error {
	v, ok := view.(TemplateView)
	if !ok {
		return fmt.Errorf("owl: TemplateRenderer cannot render a %T, use an owl.TemplateView", view)
	}
	return ctx.Render(v.Templates, v.Name, v.Model)
}

// View renders the view with the renderer of the Mux, or DefaultRenderer.
func (ctx Ctx) View(view any) error {
	if ctx.renderer != nil {
		return ctx.renderer.Render(ctx, view)
	}
	return DefaultRenderer.Render(ctx, view)
}

// ViewModel returns what a template gets as its dot: the model, the localizer of
// the file, the model state and the CSRF token. Views made with Hix use it to
// reuse the form error and localization logic.
func (ctx Ctx) ViewModel(file string, model any) ViewModel {
	return createViewModel(ctx, file, model)
}
