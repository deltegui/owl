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
	"github.com/deltegui/owl/v3/pagination"
)

// Page is the document of a page: doctype (written by HixRenderer), charset,
// viewport and title in the head, and the body.
func Page(title string, head []hx.INode, body ...hx.INode) hx.INode {
	meta := []hx.INode{
		hx.El("meta").Attribute("charset", "utf-8"),
		hx.El("meta").Attribute("name", "viewport").Attribute("content", "width=device-width, initial-scale=1"),
		hx.El("title").Text(title),
	}
	return hx.El("html").Attribute("lang", "en").Body(
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

// View is what a template gets as its dot, for a view made with Hix: the model
// state, the CSRF token and the translations of the view's file (the name you
// would give ctx.Render, for example "usr_tag/usr_tag_index.html", plus the shared
// file of AddLocalization). Make one at the start of the HixView.
//
//	func(ctx owl.Ctx) hx.INode {
//		v := hxui.NewView(ctx, "usr_tag/usr_tag_edit.html")
//		return hx.Label().Text(v.T("Name"))...
type View struct {
	Ctx owl.Ctx
	vm  owl.ViewModel
}

// NewView loads the translations of the file for the language of the request.
func NewView(ctx owl.Ctx, file string) View {
	return View{Ctx: ctx, vm: ctx.ViewModel(file, nil)}
}

// T is Localize in templates: the key translated, with fmt arguments.
func (v View) T(key string, args ...any) string { return v.vm.Localize(key, args...) }

// LocalizeError translates a DomainError with the errors file.
func (v View) LocalizeError(err core.DomainError) string { return v.vm.LocalizeError(err) }

// CsrfInput is the hidden input with the CSRF token of the request, the same
// that PlaceCsrfInput writes in templates.
func (v View) CsrfInput() hx.INode {
	return hx.Input().Type("hidden").Attribute("name", csrf.CsrfHeaderName).Value(v.vm.CsrfToken)
}

// HaveFormError tells if the validation of the field failed (see ctx.Validate).
func (v View) HaveFormError(field string) bool { return v.vm.HaveFormError(field) }

// FormError is the first error of the field, translated, or an empty string.
func (v View) FormError(field string) string { return v.vm.GetFormError(field) }

// FormErrors is the list of errors of a field, translated, or nothing if the
// field is valid. Each item has the class form-error (PlaceErrorList).
func (v View) FormErrors(field string) hx.INode {
	errs := v.vm.GetAllFormErrors(field)
	if len(errs) == 0 {
		return hx.Noop()
	}
	items := make([]hx.INode, len(errs))
	for i, err := range errs {
		items[i] = hx.Li().Class("form-error").Text(err)
	}
	return hx.Span().Body(hx.Ul().Body(items...))
}

// Select is a <select> from an owl.SelectList. The text of each option is its
// Tag translated.
func (v View) Select(list owl.SelectList) hx.INode {
	sel := hx.Select().Class("form-control", "phx-select").
		Attribute("name", list.Name).Id(list.Name)
	if list.Multiple {
		sel.Attribute("multiple", "")
	}
	if list.Filtered {
		sel.Attribute("filtered", "")
	}
	options := make([]hx.INode, len(list.Items))
	for i, item := range list.Items {
		option := hx.Option().Value(item.Value).Text(v.T(item.Tag))
		if item.Selected {
			option.Selected()
		}
		options[i] = option
	}
	return sel.Body(options...)
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
