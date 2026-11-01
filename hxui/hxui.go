// Package hxui has the pieces of a page that OWL views repeat, written with Hix:
// the CSRF input, form errors, select lists, pagination and the document of a
// page. They are plain functions that return an hx.INode, to use inside a HixView.
//
// The markup is small and has few classes (form-error, phx-select, pagination);
// add yours with Class on the node they return.
package hxui

import (
	"net/url"
	"strconv"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/csrf"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/pagination"
)

// Page is the document of a page: doctype (written by ctx.View), charset,
// viewport and title in the head, and the body. The lang of the document is the
// language of the request (the cookie, see ctx.GetCurrentLanguage), or none if
// there is no localization.
func Page(ctx owl.Ctx, title string, head []hx.INode, body ...hx.INode) hx.INode {
	meta := []hx.INode{
		hx.El("meta").Attribute("charset", "utf-8"),
		hx.El("meta").Attribute("name", "viewport").Attribute("content", "width=device-width, initial-scale=1"),
		hx.El("title").Text(title),
	}
	html := hx.Html()
	if lang := ctx.GetCurrentLanguage(); lang != "" {
		html.Attribute("lang", lang)
	}
	return html.Body(
		hx.El("head").Body(append(meta, head...)...),
		hx.El("body").Body(body...),
	)
}

// Stylesheet is <link rel="stylesheet" href=...>.
func Stylesheet(href string) hx.INode {
	return hx.El("link").Attribute("rel", "stylesheet").Href(href)
}

// Script is <script src=... defer>.
func Script(src string) hx.INode {
	return hx.El("script").Src(src).Attribute("defer", "")
}

// View is what a view made with Hix works with: the translations of a component
// (a localizer.Source, usually a Dict, plus the shared ones of AddLocalization), and through Ctx
// the model state and the CSRF token. Make one at the start of the HixView.
//
//	func(ctx owl.Ctx) hx.INode {
//		v := hxui.NewView(ctx, texts)
//		return hx.Label().Text(v.T("Name"))...
type View struct {
	Ctx owl.Ctx
	loc localizer.Localizer
}

// NewView gets the translations of the component for the language of the request.
func NewView(ctx owl.Ctx, texts localizer.Source) View {
	return View{Ctx: ctx, loc: ctx.Texts(texts)}
}

// T is the key translated, with fmt arguments.
func (v View) T(key string, args ...any) string { return v.loc.GetFormatted(key, args...) }

// LocalizeError translates a DomainError with the errors file.
func (v View) LocalizeError(err core.DomainError) string { return v.Ctx.LocalizeError(err) }

// CsrfInput is the hidden input with the CSRF token of the request.
func (v View) CsrfInput() hx.INode {
	return hx.Input().Type("hidden").Attribute("name", csrf.FieldName).Value(v.Ctx.CsrfToken())
}

// errorsOf is the validation errors of a field, or nothing if it is valid.
func (v View) errorsOf(field string) []core.ValidationError {
	if v.Ctx.ModelState.Valid {
		return nil
	}
	return v.Ctx.ModelState.Errors[field]
}

// HaveFormError tells if the validation of the field failed (see ctx.Validate).
func (v View) HaveFormError(field string) bool { return len(v.errorsOf(field)) > 0 }

// FormError is the first error of the field, translated, or an empty string.
func (v View) FormError(field string) string {
	errs := v.errorsOf(field)
	if len(errs) == 0 {
		return ""
	}
	return owl.FormatError(v.loc, errs[0])
}

// FormErrors is the list of errors of a field, translated, or nothing if the
// field is valid. Each item has the class form-error.
func (v View) FormErrors(field string) hx.INode {
	errs := v.errorsOf(field)
	if len(errs) == 0 {
		return hx.Noop()
	}
	items := make([]hx.INode, len(errs))
	for i, err := range errs {
		items[i] = hx.Li().Class("form-error").Text(owl.FormatError(v.loc, err))
	}
	return hx.Span().Body(hx.Ul().Body(items...))
}

// PageLink returns the URL of the request with the page changed, keeping the
// rest of the query (filters, order). It is what Pagination uses by default.
func PageLink(ctx owl.Ctx) func(page int) string {
	return func(page int) string {
		query := url.Values{}
		for k, v := range ctx.Req.URL.Query() {
			query[k] = v
		}
		query.Set("pagination.currentPage", strconv.Itoa(page))
		return ctx.Req.URL.Path + "?" + query.Encode()
	}
}

// Pagination is a nav with previous, the numbers around the current page and
// next. href gives the URL of a page; nil means PageLink(ctx). It writes nothing
// if everything fits in one page.
func Pagination(ctx owl.Ctx, p pagination.Pagination, href func(page int) string) hx.INode {
	vm := pagination.ToVM(ctx, p)
	if !vm.Show {
		return hx.Noop()
	}
	if href == nil {
		href = PageLink(ctx)
	}
	item := func(page int, label string, active bool) hx.INode {
		return hx.Li().Class("page-item").ClassIf("active", active).Body(
			hx.A().Class("page-link").Href(href(page)).Text(label),
		)
	}
	var items []hx.INode
	if vm.ShowPreviousButton {
		items = append(items, item(vm.PreviousPage, vm.GetPreviousTag(), false))
	}
	for _, page := range vm.Sequence {
		items = append(items, item(page, strconv.Itoa(page), page == vm.CurrentPage))
	}
	if vm.ShowNextButton {
		items = append(items, item(vm.NextPage, vm.GetNextTag(), false))
	}
	return hx.Nav().Class("pagination").Body(
		hx.Span().Class("pagination-info").Text(vm.GetMessageOfElements()),
		hx.Ul().Class("pagination-list").Body(items...),
	)
}
