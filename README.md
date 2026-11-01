# owl
<img src="https://raw.githubusercontent.com/deltegui/owl/refs/heads/main/owl.svg" alt="logo" width="200"/>

Owl is a highly opinionated web framework for Go, for server-rendered sites that
can grow *islands* of interactivity. Pages are written in plain Go with
[Hix](https://github.com/deltegui/hx): there are no template files.

```
go get github.com/deltegui/owl/v3
```

## What you get

| Area | What it does | Docs |
|---|---|---|
| Routing | Router with sub-routers, middleware, static files, graceful shutdown. Optional dependency injection container. | [routing](docs/routing.md) |
| Request context | `owl.Ctx`: params, forms, JSON, cookies (encrypted), validation, responses. | [context](docs/context.md) |
| Views | `ctx.View` renders a Hix tree on the server. `hxui` has the repeated pieces: page, forms, errors, pagination. | [views](docs/views.md) |
| Islands | Pieces of a page that the server renders and the browser (WASM) hydrates from the same state. | [islands](docs/islands.md) |
| Localization | Translations in code (`localizer.Dict`), language cookie, translated validation errors. | [localization](docs/localization.md) |
| Security | AES-256-GCM cipher, CSRF (signed double submit), CORS. | [security](docs/security.md) |
| Authentication | Server-side sessions, `Authorize` middleware, Argon2id / bcrypt passwords. | [authentication](docs/authentication.md) |
| Files | Directories with policies (per user, per date, GUID names), local and temporal backends. | [files](docs/files.md) |
| Mail | Message, renderer, SMTP driver, in-memory queue, retrying outbox. | [mail](docs/mail.md) |
| PDF and Excel | The mechanism (renderer, service, driver contracts); the application plugs in Chrome or an xlsx writer. | [pdf-excel](docs/pdf-excel.md) |
| Utilities | `core` (errors, tokens), `logx`, `pagination`. | [utilities](docs/utilities.md) |

Everything is tightly integrated and has few dependencies: `httprouter`,
`valtruc` (validation, by the same author), `golang.org/x/crypto` (Argon2 and
bcrypt) and Hix.

The whole API reference is in [docs/](docs/README.md).

## A page, a form and a login

```go
// The form. valtruc reads the tags to validate it.
type loginForm struct {
	Name     string `valtruc:"min=3, max=255, required"`
	Password string `valtruc:"min=3, max=255, required"`
}

// The translations of the view live in code, next to it.
var loginTexts = localizer.Dict{
	"es": {"Name": "Nombre", "Password": "Contraseña", "Login": "Inicia sesión"},
	"en": {"Name": "Name", "Password": "Password", "Login": "Login"},
}

// The view builds the page with Hix.
func loginView(form loginForm) owl.HixView {
	return func(ctx owl.Ctx) hx.INode {
		vd := hxui.NewViewData(ctx, loginTexts) // translations in the language of the request
		return hxui.Page(ctx, vd.T("Login"), nil,
			hx.Form().Attribute("action", "/account/login").Attribute("method", "post").Body(
				vd.CsrfInput(), // hidden input with the CSRF token
				hx.Label().Text(vd.T("Name")),
				hx.Input().Type("text").Attribute("name", "Name").Value(form.Name),
				vd.FormErrors("Name"), // validation errors of the field "Name"
				hx.Label().Text(vd.T("Password")),
				hx.Input().Type("password").Attribute("name", "Password"),
				vd.FormErrors("Password"),
				hx.Button().Type("submit").Text(vd.T("Login")),
			),
		)
	}
}

func handleShowLogin() owl.Handler {
	return func(ctx owl.Ctx) error { return ctx.View(loginView(loginForm{})) }
}

func handleProcessLogin(service accountService, sessions *session.Manager) owl.Handler {
	return func(ctx owl.Ctx) error {
		var form loginForm
		ctx.ParseForm(&form) // fills the struct from the form
		ctx.Validate(form)   // applies the valtruc rules and fills ctx.ModelState

		if !ctx.ModelState.Valid {
			form.Password = ""
			ctx.Status(http.StatusBadRequest)
			return ctx.View(loginView(form))
		}

		account, err := service.login(form.Name, form.Password)
		if err != nil {
			return ctx.Forbidden()
		}
		err = sessions.Create(ctx.Context(), ctx.Res, session.User{
			Id:     account.ID,
			Claims: map[session.Claim]string{"Name": account.Name},
		})
		if err != nil {
			return err
		}
		_ = ctx.ChangeLanguage(account.Lang)
		return ctx.Redirect("/panel")
	}
}

func main() {
	cy, err := cipher.New() // random key; cipher.NewFromStrings(key) keeps it across restarts
	if err != nil {
		log.Fatal(err)
	}
	manager := session.NewManager(cy) // one Manager for the whole program (see docs/routing.md)

	r := owl.NewWithInjector(cy) // owl.New(cy) if you do not want dependency injection
	r.StaticEmbedded(static.Files)
	r.AddLocalization(owl.Localization{
		Shared:    locale.Shared,
		Errors:    locale.Errors,
		Languages: []string{"es", "en"}, // the first one is the default
	})
	r.Use(middleware.Logger)
	r.Use(middleware.Csrf(csrf.New(cy))) // Use goes before the routes it must cover

	r.Add(newAccountService)                             // a builder: func(...) accountService
	r.Add(func() *session.Manager { return manager })    // builders run on every request for the type: share the instance yourself

	account := r.CreateSubMuxi("/account") // routes under /account
	account.Get("/login", handleShowLogin)
	account.Post("/login", handleProcessLogin) // arguments are filled by the container

	r.Listen(":8080") // stops on SIGINT / SIGTERM
}
```

`localizer.Dict` refuses repeated keys at compile time, and `localizer.Check`
in a test tells you which keys a language is missing.

## Islands

A page is HTML from the server. Where it needs behavior, put an *island*: a
piece the server renders from a JSON state and a WASM program hydrates from the
same state, so nothing is redrawn. It is defined once, in a package that
compiles on both sides:

```go
// shared by the server and the WASM
var Counter = island.Define("counter", func(s State) hx.INode { return counter(s) })

// server: place it in the page
in := island.Mount(Counter, State{N: 3})
return hxui.App(ctx, hxui.AppOptions{Title: "Counter", Bundle: "app", Islands: []island.Instance{in}},
	hx.Main().Body(in.Node()))

// browser (the main of app.wasm)
func main() { island.Run(Counter) }
```

The island talks to its server with `island/client` (CSRF token included) and
the server answers errors with `ctx.JsonInvalid` / `ctx.JsonFailure`. See
[islands](docs/islands.md). A complete application is in `OWL_DEMO`.

## Packages

```
owl                      Mux, Muxi, Ctx, HixView, Injector, Localization
owl/core                 Cipher, Hasher, DomainError, ModelState, tokens
owl/cipher               AES-256-GCM, Seal / Open
owl/csrf                 signed double-submit CSRF
owl/session              server-side sessions
owl/middleware           Logger, Cors, Csrf, Authorize, AuthorizeRoles, Admin
owl/hash                 Argon2id and bcrypt
owl/localizer            Dict, Localizer, Pick, Check   (compiles to WASM)
owl/localizer/webstore   language cookie and application translations
owl/hxui                 Page, App, ViewData, Pagination
owl/island               Define, Mount, Run            (compiles to WASM)
owl/island/client        Send, Errors, Escape          (compiles to WASM)
owl/pagination           List, Pagination, ViewModel
owl/files                Directory, policies, Local, Temporal, Temp
owl/mail                 Message, Sender, Service, Driver
owl/mail/hxmail          renderer with Hix
owl/mail/smtp            SMTP driver
owl/mail/queue           in-memory background sender
owl/mail/outbox          stored emails with retries
owl/pdf, owl/pdf/hxpdf   PDF mechanism and Hix renderer
owl/excel                spreadsheet contract
owl/logx                 Logger over log/slog
```

## Resources

- [Documentation](docs/README.md)
- [Go reference](https://pkg.go.dev/github.com/deltegui/owl/v3)
