package owl

import (
	"io"
	"strings"

	"github.com/deltegui/hx"
	"github.com/deltegui/hx/ssr"
	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/localizer"
)

// HixView builds a page with Hix. It gets the Ctx, so it can read the model
// state, the user, the CSRF token or the localizer.
//
//	func index(ctx owl.Ctx) error {
//		return ctx.View(owl.HixView(func(ctx owl.Ctx) hx.INode {
//			return hx.El("html").Body(hx.El("body").Body(hx.H1().Text("Hello")))
//		}))
//	}
type HixView func(ctx Ctx) hx.INode

// View renders the view on the server with hx/ssr and writes it as the body of
// the response. If the view builds an <html> element it writes <!doctype html>
// first. Attributes such as href and src are checked by Hix (hx.SafeAttribute)
// and text is escaped.
func (ctx Ctx) View(view HixView) error {
	html := ssr.Render(func() hx.INode { return view(ctx) })
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

// FormatError translates a validation error: the name of the field and the
// message of the rule come from the localizer, and the error fills in its own
// values (a minimum, a maximum...). It is for code that builds its own answer,
// for example a JSON one; views get it from hxui.ViewData.
func FormatError(loc localizer.Localizer, err core.ValidationError) string {
	field := loc.Get(err.GetFieldName())
	return err.Format(loc.GetFormatted(string(err.GetIdentifier()), field))
}
