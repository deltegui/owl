# Views

Pages are plain Go code written with [Hix](https://github.com/deltegui/hx). There
are no template files: a view is a function that returns an `hx.INode`.

Packages: `owl` (`HixView`, `ctx.View`), `owl/hxui` (the pieces views repeat),
`owl/pagination` (the data).

## HixView and ctx.View

```go
type HixView func(ctx Ctx) hx.INode
func (ctx Ctx) View(view HixView) error
```

`ctx.View` renders the tree on the server (with `hx/ssr`) and writes it as the
response body. If the root is an `<html>` element it writes `<!doctype html>`
first. A view gets the `Ctx`, so it can read the model state, the user, the CSRF
token or the translations.

```go
func showIndex(ctx owl.Ctx) error {
	return ctx.View(func(ctx owl.Ctx) hx.INode {
		return hx.El("html").Body(hx.El("body").Body(hx.H1().Text("Hello")))
	})
}
```

Hix escapes all text, and checks URL attributes (`href`, `src`, `action`...):
a scheme that is not `http`, `https`, `mailto` or `tel` (such as `javascript:`)
becomes `about:invalid#hx-unsafe-url`. `style`, `srcset` and `on*` attributes are
**not** checked: never put user text in them. `hx.Raw(html)` writes HTML as is;
never use it with user text.

Server rendering does not track signals: a `Signal` is a plain value there, so
the same component can be used on the server and in a WASM island.

## hxui

```go
import "github.com/deltegui/owl/v3/hxui"
```

### Page

```go
func Page(ctx owl.Ctx, title string, head []hx.INode, body ...hx.INode) hx.INode
```

The document of a page: `<html lang>` (the language of the request, omitted
when there is no localization), charset and viewport meta, the title, whatever
you put in `head`, and the body.

```go
hxui.Page(ctx, "Products",
	[]hx.INode{hxui.Stylesheet("/css/site.css"), hxui.Script("/js/site.js")},
	hx.Main().Body(...))
```

| Function | Writes |
|---|---|
| `Stylesheet(href)` | `<link rel="stylesheet" href=...>` |
| `Script(src)` | `<script src=... defer>` |

For a page that loads a WASM program use [`hxui.App`](islands.md#hxuiapp).

### ViewData: translations, forms and errors

```go
func NewViewData(ctx owl.Ctx, texts localizer.Source) ViewData
```

Make one at the start of the view: it is everything the view needs besides your
own data. It holds the translations of the component
(`texts`, usually a [`localizer.Dict`](localization.md)) in the language of the
request, plus the shared ones from `AddLocalization`.

| Method | Returns |
|---|---|
| `T(key, args...)` | The translation, formatted with `fmt.Sprintf` when there are args. A missing key shows as itself. |
| `CsrfInput()` | `<input type="hidden">` with the CSRF token. Put it in every form that is not GET. |
| `HaveFormError(field)` | `true` if validation failed on that field. |
| `FormError(field)` | The first error of the field, translated, or `""`. |
| `FormErrors(field)` | `<span><ul><li class="form-error">…</li></ul></span>` with all of them, or nothing. |
| `LocalizeError(core.DomainError)` | The translated message of a domain error. |
| `Ctx` | The `owl.Ctx`, for anything else. |

```go
vd := hxui.NewViewData(ctx, productTexts)
hx.Div().ClassIf("has-error", vd.HaveFormError("Name")).Body(
	hx.Label().Text(vd.T("Name")),
	hx.Input().Type("text").Attribute("name", "Name"),
	vd.FormErrors("Name"),
)
```

The field name that `FormErrors` looks up is the key in `ctx.ModelState.Errors`
(the Go field name, `Lines[0].Account` for nested ones). Error messages come
from the translations: the field name by its key, and the rule message by the
rule identifier (`requiredIdentifier`, `minStringLengthIdentifier`...), so put
those keys in the shared dictionary.

### Pagination

```go
func Pagination(ctx owl.Ctx, p pagination.Pagination, href func(page int) string) hx.INode
func PageLink(ctx owl.Ctx) func(page int) string
```

A `<nav class="pagination">` with the "x to y of z" text, previous, the page
numbers around the current one (four on each side) and next. It writes nothing
if everything fits in one page. `href` gives the URL of a page; `nil` means
`PageLink(ctx)`, which keeps the rest of the query string (filters, order) and
changes `pagination.currentPage`.

```go
page, _ := strconv.Atoi(ctx.GetQueryParam("pagination.currentPage"))
list, _ := service.Find(filter, pagination.Pagination{CurrentPage: max(page, 1), ElementsPerPage: 20})
// in the view:
hxui.Pagination(ctx, list.Pagination, nil)
```

The previous/next/“of” words are in `pagination.Texts` (Spanish and English). To
use another language, replace or extend it at startup, see
[utilities](utilities.md#pagination).

## Writing the markup

The markup of `hxui` is small and has few classes (`form-error`,
`pagination`, `pagination-info`, `pagination-list`, `page-item`, `page-link`,
`active`). Add your own with `Class` on the node a function returns. For a CSS
framework as components (Bootstrap, Bulma, Basecoat) see the `hxbs`, `hxbulma` and
`hxcoat` modules, which work on the server and in WASM.
