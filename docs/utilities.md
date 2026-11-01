# Utilities

Packages `owl/core`, `owl/logx` and `owl/pagination`.

## core

Shared types and small helpers. `core` has no dependency on the rest of OWL, so
every other package can use it.

### Interfaces

```go
type Cipher interface {            // see security.md
	Encrypt(data, aad []byte) ([]byte, error)
	Decrypt(data, aad []byte) ([]byte, error)
}

type Hasher interface {            // see authentication.md
	Hash(password string) (string, error)
	Check(encoded, password string) (bool, error)
	NeedsRehash(encoded string) bool
	Mimic(password string)
}
```

### DomainError

An error produced by the business logic of your application, with a numeric
**code** that OWL uses to translate it.

```go
type DomainError struct {
	Code    int
	Message string // fallback text when the code has no translation
}

func (err DomainError) Error() string
func (err DomainError) Wrap(other error) DomainError
func (err DomainError) Wrapf(format string, a ...any) DomainError
func (err DomainError) IsPresent() bool
```

```go
var ErrNoStock = core.DomainError{Code: 1001, Message: "not enough stock"}

return ErrNoStock.Wrapf("product %d has %d left", id, left) // keeps the code, adds the cause
```

- `Wrap`/`Wrapf` return a new error with the **same code and message** and the cause
  attached (`Error()` prints both: `message: cause`).
- The code is the key in the `Errors` dictionary of `AddLocalization` (as a string:
  `"1001"`). `ctx.LocalizeError(err)` and `ctx.JsonFailure(err)` translate it by the
  language of the request, falling back to `Message`. See
  [localization](localization.md#domain-errors).
- Detect one with `errors.As(err, &domainError)`.

### ModelState and ValidationError

```go
type ModelState struct {
	Valid  bool
	Errors map[string][]ValidationError // by field
}

type ValidationError interface {
	Error() string
	Format(f string) string
	GetStructName() string
	GetFieldName() string
	GetFieldTypeName() string
	GetFieldValue() string
	GetIdentifier() valtruc.ValidatorIdentifier
}
```

`ctx.Validate` fills a `ModelState` from the `valtruc` tags. To add an error of
your own (a rule that tags cannot express), use `core.CustomValidationError`:

```go
ctx.ModelState.Valid = false
ctx.ModelState.Errors["Email"] = append(ctx.ModelState.Errors["Email"], core.CustomValidationError{
	ErrorMessage: "that email is already registered",
	FieldName:    "Email",
	Identifier:   "emailTaken", // key for translated messages
})
```

Its `Format` returns the message as it is.

### Roles

```go
type Role int64
const (
	RoleAdmin Role = 1
	RoleUser  Role = 2
)
```

Define your own roles as other `core.Role` values. Used by `session.User` and
`middleware.AuthorizeRoles`.

### Tokens and passwords

```go
core.GenerateToken(numberBytes int) string   // crypto-random, URL-safe base64
core.GenerateTokenDefaultLength() string     // 32 bytes
core.GenerateRandomPassword(length int) string
```

All use `crypto/rand`. `GenerateRandomPassword` panics only if the system's random
source fails.

Constants: `core.DefaultTokenBytes` (32), `core.OneDayDuration`, and
`Size8`/`Size16`/`Size32`/`Size64` and `IntBase10` for parsing.

## logx

A small logging interface over `log/slog`, with a default implementation that needs
no setup.

```go
type Logger interface {
	WithModuleName(name string) Logger
	Info(msg string, args ...any);  InfoContext(ctx, msg, args...)
	Warn(...);  WarnContext(...)
	Error(...); ErrorContext(...)
	Debug(...); DebugContext(...)
}
```

`args` are key-value pairs, as in `slog`: `ctx.Logger.Info("saved", "id", 7)`.

| Implementation | Use |
|---|---|
| `logx.Default{}` | Writes `[INFO] (MODULE: /prefix) message args` with the standard `log` package. The zero value logs `Info` and above; set `Level` (`logx.LevelDebug`, `LevelInfo`, `LevelWarn`, `LevelError`) to change it. |
| `logx.NewWithSlog(*slog.Logger)` | Wraps an `slog.Logger`; `WithModuleName` adds a `module` attribute. |

```go
mux := owl.New(cy)
mux.Logger = logx.NewWithSlog(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
```

Set `mux.Logger` **before** creating sub-routers: each sub-router gets
`Logger.WithModuleName(prefix)`. Handlers read it as `ctx.Logger`, and the mail
queue, outbox and middleware log through the same interface.

Helpers: `logx.Err(err)` makes the attribute `slog.Any("err", err)`, and
`logx.NewSlogMultiHandler(handlers...)` is an `slog.Handler` that sends each
record to several handlers (a console and a file, say).

## pagination

Data types for a paged list. The *rendering* is `hxui.Pagination`; see
[views](views.md#pagination).

```go
type Pagination struct {
	CurrentPage     int
	ElementsPerPage int
	TotalElements   int
	Order           Order   // OrderDescending (0) or OrderAscending (1)
	OrderBy         string
	Enabeld         bool    // (sic) true when the pagination is active
}

type List[T any] struct {
	Pagination Pagination
	Items      []T
}
```

A service returns a `List[T]` with the items of the page and the `Pagination`
filled in (page, size and total). A handler reads the query, builds the request,
and the view calls `hxui.Pagination(ctx, list.Pagination, nil)`.

### ViewModel

```go
func ToVM(ctx owl.Ctx, p Pagination) ViewModel
func (vm ViewModel) ToDto() Pagination
func (vm ViewModel) GetMessageOfElements() string
func (vm ViewModel) GetNextTag() string
func (vm ViewModel) GetPreviousTag() string
```

`ToVM` computes what a pagination bar needs: first and last element, the sequence
of page numbers (up to four on each side of the current one), previous and next
pages, whether to show the bar (`Show`: only if more than one page) and the
buttons, plus translated texts. Fields that map to a form or the query use `html`
tags: `CurrentPage` is `pagination.currentPage`, `Order` is `pagination.order`
and `OrderBy` is `pagination.orderBy`, so
`ctx.ParseForm(&vm)` reads them directly. `ToDto()` turns the view model back into a
`Pagination` (enabled, with page and order).

### Texts

```go
var pagination.Texts localizer.Dict // "PaginationNextButton", "PaginationPreviousButton", "PaginationMessageOfElements"
```

Spanish and English by default. For another language, extend it at startup:

```go
pagination.Texts["fr"] = localizer.Localizer{
	"PaginationNextButton":        "Suivant",
	"PaginationPreviousButton":    "Précédent",
	"PaginationMessageOfElements": "%d à %d sur %d",
}
```

A language it does not have is shown with the application's default language; if
that one is missing too, the bar shows the keys.
