# Localization

Translations live **in code**, as Go maps next to the component that uses them.
There are no JSON files to load, so a typo is a compile error, a repeated key is
a compile error, and the same dictionaries work in the browser (WASM).

Packages:

- `owl/localizer`: the data types. Imports only `fmt` and `slices`, so it compiles
  to WASM at almost no cost (about +2 KB with TinyGo).
- `owl/localizer/webstore`: the server side (the language cookie and the
  application-wide translations). You rarely use it directly: `owl.Mux` does.

## Dictionaries

```go
import "github.com/deltegui/owl/v3/localizer"

var productTexts = localizer.Dict{
	"es": {"Title": "Productos", "Name": "Nombre", "Price": "Precio"},
	"en": {"Title": "Products",  "Name": "Name",   "Price": "Price"},
}
```

A `localizer.Dict` is `map[language]Localizer` and a `Localizer` is
`map[key]translation`. Go refuses a key written twice in the same language.

```go
type Source interface{ Get(language string) Localizer }

func (d Dict) Get(language string) Localizer        // empty if the Dict has no such language
func (loc Localizer) Get(key string) string          // the key itself if it is missing
func (loc Localizer) GetFormatted(key string, args ...any) string // fmt.Sprintf on the translation
func Pick(src Source, language, fallback string) Localizer
func Check(d Dict, languages ...string) []string
```

- `Source` is anything that can give a `Localizer` for a language. `Dict` is
  one; you can write another (a JSON file, a database) and everything else
  accepts it.
- **A missing key shows as itself**, so a forgotten translation is visible and
  never a blank or a crash.
- What `Dict.Get` returns is the dictionary's own map: **read it, do not
  change it.**
- `Pick(src, language, fallback)` is the `Localizer` of the language, or the one of
  `fallback` if the source has *nothing* in that language. The fallback is by
  language, not by key: a language with some keys missing shows those keys as they
  are. Server and browser both use `Pick` with the same fallback, so they say the
  same.
- `localizer.LangSpanish` (`"es"`) and `localizer.LangEnglish` (`"en"`) are only
  names. They decide nothing: the languages of the application are the ones you
  give to `AddLocalization`.

### Checking dictionaries in a test

```go
func TestTexts(t *testing.T) {
	for _, problem := range localizer.Check(productTexts, "es", "en") {
		t.Error(problem)
	}
}
```

`Check` reports the languages the dictionary lacks entirely (when you list them)
and, for every language, the keys that another language has and it does not.

## Setting up the application

```go
r.AddLocalization(owl.Localization{
	Shared:    locale.Shared,               // localizer.Source every component can use
	Errors:    locale.Errors,               // translates core.DomainError by code
	Languages: []string{"es", "en"},        // at least one; the first is the default
})
```

- `Shared` has what is common: the words of the layout, buttons (`Shared.Save`),
  the names of the fields of forms, and the messages of validation rules (keyed by
  the rule identifier, like `requiredIdentifier`).
- `Errors` is keyed by the **code** of a `core.DomainError` as a string (`"7"`
  for `Code: 7`). A code with no translation falls back to the error's own
  `Message`.
- `Languages` is the list of languages the application speaks. It panics when
  empty. The first one is the default: the language of someone with no cookie,
  of a cookie that cannot be read, or whose language the application no longer
  speaks.
- `Shared` and `Errors` can be nil.

Call `AddLocalization` **before** creating sub-routers, which copy it.

## Language of a request

The language is kept in a cookie called `language`, sealed with the mux's
cipher for the purpose `language` (a day). A cookie that was tampered with, or
that names a language that is not in `Languages`, counts as the default.

```go
func (ctx *Ctx) GetCurrentLanguage() string
func (ctx *Ctx) ChangeLanguage(to string) error
```

`ChangeLanguage` saves the cookie (a language that is not in the list is saved
as the default); it returns `owl.ErrNoLocalization` if `AddLocalization` was not
called. Without localization `GetCurrentLanguage()` is `""`.

A language switcher that works without JavaScript is a form that posts to a
handler which calls `ChangeLanguage` and redirects back:

```go
r.Post("/language/:lang", func(ctx owl.Ctx) error {
	if err := ctx.ChangeLanguage(ctx.GetURLParam("lang")); err != nil {
		return err
	}
	return ctx.RedirectCode(ctx.Req.PostFormValue("Next"), http.StatusSeeOther)
})
```

(Validate the `Next` value before redirecting to it, or you have an open
redirect; accept only paths that start with a single `/`.)

## Using translations

### In handlers

```go
loc := ctx.Texts(productTexts)  // Localizer in the request's language, shared translations added
title := loc.Get("Title")
```

`ctx.Texts(src)` is `Pick(src, language, default)` merged over `Shared`: the
component's own keys win when a key is in both. It returns a **new map**, so you
can change it. Without localization it is empty, so every key shows as itself.

### In views

`hxui.NewViewData(ctx, texts)` does the same and gives you `vd.T(key, args...)`. See
[views](views.md).

### Domain errors

```go
ctx.LocalizeError(core.DomainError{Code: 7, Message: "fallback"})
```

Looks the code up in `Errors` in the language of the request.

### Validation errors

```go
owl.FormatError(loc, err)  // loc is a Localizer, err is a core.ValidationError
```

The name of the field is `loc.Get(err.GetFieldName())`; the message is
`loc.GetFormatted(string(err.GetIdentifier()), fieldName)`, and the error fills in
its own values (a minimum, a maximum). So the dictionary needs the field names
and the rule identifiers:

```go
var Shared = localizer.Dict{
	"en": {
		"Name":                    "Name",
		"requiredIdentifier":      "%s is required",
		"minStringLengthIdentifier": "%s is too short",
	},
}
```

Use it to build a JSON answer by hand; `ctx.JsonInvalid` already does.

### In islands

Keep the dictionary in the package shared by server and browser, and send only
the language in the state. See [islands](islands.md#translating-an-island).

## webstore (server internals)

```go
func New(shared, errors localizer.Source, languages []string, c core.Cipher) Store
func (ws Store) Default() string
func (ws Store) ReadCookie(req *http.Request) string
func (ws Store) CreateCookie(w http.ResponseWriter, language string) error
func (ws Store) Texts(src localizer.Source, req *http.Request) localizer.Localizer
func (ws Store) GetLocalizedError(err core.DomainError, req *http.Request) string
```

`New` panics without languages. Use it directly only if you build your own
router; with OWL, `AddLocalization` creates one for you.
