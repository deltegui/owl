# Request context (`owl.Ctx`)

`Ctx` is what every handler receives. It wraps the request and the response and
carries the validation state, the localization and the logger.

```go
type Ctx struct {
	Req        *http.Request
	Res        http.ResponseWriter
	ModelState core.ModelState // filled by Validate
	Logger     logx.Logger
	// unexported: URL params, request context, localization, validator, cipher
}
```

`Ctx` is passed **by value**. A few methods have a pointer receiver because they
change it (`Validate`, `Set`, cookies, language); they work on the `ctx`
variable of the handler, which is addressable. Pass `ctx` on to a helper and
changes it makes through those methods are not seen by the caller, except for
what they write to the response.

## Status

The status is kept until the first byte is written (or the handler ends), so
you can call `ctx.Status(...)` and then set headers or write the body in any
order.

```go
ctx.Status(http.StatusBadRequest)
return ctx.View(loginView(form))
```

## Reading the request

| Method | Returns |
|---|---|
| `GetURLParam(name)` | The `:name` path parameter, or `""`. |
| `GetQueryParam(name)` | The query-string value, or `""`. |
| `ParseForm(&dst)` | Fills a struct from the form (see below). |
| `ParseJson(&dst) error` | Decodes the JSON body into `dst`. |
| `Context()` | The request's `context.Context`. |
| `Set(key, value)` / `Get(key)` | Store and read a value in the request context (middleware use this to pass data). |
| `User() (session.User, bool)` / `HaveSession()` | The logged user, if the [session middleware](authentication.md) ran. |
| `CsrfToken()` | The CSRF token of the request, or `""` without the CSRF middleware. |

### ParseForm

`ParseForm` reads `req.Form` (multipart too) and fills a struct through
reflection. A field is read by its `html` tag, or by its name if it has none. It
only supports flat structs and these types: all `int`/`uint` sizes, `float32`,
`float64`, `bool`, `string` and `time.Time`.

```go
type search struct {
	Query string
	Page  int  `html:"pagination.currentPage"`
	Done  bool
}

var s search
ctx.ParseForm(&s) // pass a pointer
```

## Validation

Rules are `valtruc` tags on the struct. `Validate` applies them and fills
`ctx.ModelState`:

```go
type product struct {
	Name  string  `valtruc:"min=3, max=60, required"`
	Price float64 `valtruc:"min=0.01"`
}

var p product
ctx.ParseForm(&p)
ctx.Validate(p)
if !ctx.ModelState.Valid {
	// ctx.ModelState.Errors is map[field][]core.ValidationError
}
```

- The field key is the Go field name; nested fields are joined with dots
  (`Lines[0].Account`).
- Each `core.ValidationError` has the field name, the rule identifier and the
  values needed to build the message.
- Show the errors in a view with [`hxui.View.FormErrors`](views.md#view-translations-forms-and-errors),
  or translate them yourself with `owl.FormatError(localizer, err)`.
- For JSON answers use [`ctx.JsonInvalid`](islands.md#answering-errors).

## Responses

### Text and status

| Method | Writes |
|---|---|
| `Text(s)` | `s` as it is (use for text that comes from a user). |
| `String(format, args...)` | `fmt.Sprintf`-style text; with no args it is written as is, so a `%` is safe. |
| `StringOk`, `StringBadRequest`, `StringForbidden`, `StringNotFound`, `StringInternalServerError` | `String` with the status 200, 400, 403, 404, 500. |
| `Ok()`, `BadRequest()`, `Forbidden()`, `NotFound()`, `InternalServerError()`, `NoContent()` | Only the status (200, 400, 403, 404, 500, 204). |
| `Status(code)` | Sets the status. |

### JSON

| Method | Writes |
|---|---|
| `Json(data) error` | `data` as JSON with `Content-Type: application/json`, with the current status (200 by default). |
| `JsonOk(data) error` | Sets 200 and writes `data`. |
| `JsonInvalid(texts) error` | 422 with the validation errors by field. |
| `JsonFailure(domainError) error` | 400 with the translated message of a domain error. |

### Pages

| Method | Does |
|---|---|
| `View(owl.HixView)` | Renders a Hix tree on the server. See [views](views.md). |

### Redirects

| Method | Does |
|---|---|
| `Redirect(to)` | 307 (temporary, keeps the method). |
| `RedirectCode(to, code)` | Redirect with the status you choose. After a form POST use `http.StatusSeeOther` (303). |

## Cookies

Cookies written with these methods are **sealed** (encrypted and authenticated)
with the mux's cipher, for a purpose derived from the cookie name
(`cookie:<name>`): a value sealed for one cookie cannot be replayed as another.

```go
ctx.CreateCookie("theme", "dark")           // 1 day, HttpOnly, not Secure
v, err := ctx.ReadCookie("theme")           // error if missing or tampered with
ctx.DeleteCookie("theme")                   // error if the request did not have it
```

For more control:

```go
ctx.CreateCookieOptions(owl.CookieOptions{
	Name:     "remember",
	Value:    "42",
	Expires:  30 * 24 * time.Hour,
	HttpOnly: true,
	Secure:   true,
})
```

Path is always `/`. Sessions and the language use their own cookies, see
[authentication](authentication.md) and [localization](localization.md).

## Localization helpers

| Method | Returns |
|---|---|
| `GetCurrentLanguage()` | The language of the request (always one of the app's languages, the first if the cookie is missing; `""` without localization). |
| `ChangeLanguage(to) error` | Saves the language in the cookie. `owl.ErrNoLocalization` if `AddLocalization` was not called. |
| `Texts(source) Localizer` | The translations of a component in the language of the request, with the shared ones added. A new map. |
| `LocalizeError(core.DomainError) string` | The translated message of a domain error. |

## Logging helpers

`owl.PrintLogo(file)` and `owl.PrintLogoEmbedded(fs, path)` print an ASCII logo
at startup.
