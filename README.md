# owl

![Logo](https://raw.githubusercontent.com/deltegui/owl/refs/heads/main/logo.png)

Owl is a highly opinionated framework for Go.

## Purpose
Owl aims to be a web framework focused on building server-rendered websites with:

* Server-side rendering
* Localization (i18n)
* Automatic cookie encryption
* CORS support
* CSRF tokens
* Session management
* Form validation with error rendering
* Optional Dependency Injection Container

All features are tightly integrated with each other and implemented with a minimal number of external dependencies (currently only 3, one of which is developed in-house).

## Example

Here’s an example that demonstrates what the framework can do:

```go
// Define a struct for our login form.
type loginForm struct {
	Name               string `valtruc:"min=3, max=255, required"`  // Use valtruc to define rules for validation
	Password           string `valtruc:"min=3, max=255, required"`
	LangSelectList     owl.SelectList                               // A select list
}

func handleProcessLogin(service accountService, sessionManager *session.Manager) owl.Handler {
	templ := template.Must(...) // Parse your views using plain Go template.
	return func(ctx owl.Ctx) error {
        var form loginForm
		ctx.ParseForm(&form) // Reads form serializes as  and fill loginForm struct
		ctx.Validate(form) // Validate the struct now its filled.

		if !ctx.ModelState.Valid { // ModelState will tell you if the loginForm is valid or not.
			form.Password = ""
            ctx.Status(http.BadRequest)
		    return ctx.Render(templ, views.AccountLogin, form) // Render the view
		}

		dto := accountDto{
			Name:     form.Name,
			Password: form.Password,
		}

		account, _ = service.login(dto) // Call your service

		sessionManager.CreateSessionCookie(ctx.Res, session.User{
			Id:    account.IdUser,
			Name:  account.Name,
		})
		_ = ctx.ChangeLanguage(account.ProfileLang)

		return ctx.Redirect("/panel")
    }
}

func main() {
    cy := cypher.New()

    r := owl.NewWithInjector() // Create a new router with a dependency injection container. If you dont like / need a dependency injection container you can just use owl.New().
    r.StaticEmbedded(static.Files) // Create a mountpoint for static files.
	r.AddLocalization(locale.Files, locale.Shared, locale.Errors) // Adds loclaization. You can then use Localize(key) in your views.

    r.Add(newAccountService()) // Register your account service
    r.Add(func() *session.Manager {
        return session.NewInMemoryManager()
    }) // Register a session manager with in-memory persistence.

    sub := r.CreateSubMux("/account") // Creates a subrouter mounted in the URL /account
    sub.Post("/login", handleProcessLogin) // Automatically will wire your dependencies.

	ctx.Mux.Listen(conf.ListenURL) // Start the server.
}
```

Your view.html content:

```html
<html>
<body>

{{ template "select_list" (SelectList .Localizer .Model.LangSelectList) }}

<form action="/account/login" method="post">
    {{ .PlaceCsrfInput }} <!-- Create a hidden input with CSRF token. -->
    <div class="mb-3">
        <label for="input-name">{{ .Localize "Name" }}</label> <!-- This will search in view.html.json a localized string -->
        <input id="input-name" type="text" name="Name" value="{{ .Model.Name }}" /> <!-- Access to your model using .Model -->
        {{ PlaceErrorList . "Name" }} <!-- Place a list with validation errors for the struct field named "Name" -->
    </div>
    <div class="mb-3">
        <label for="input-password">{{ .Localize "Password" }}</label>
        <input id="input-password" type="password" name="Password" />
        {{ PlaceErrorList . "Password" }}
    </div>

    <button type="submit">
        {{ .Localize "Login" }}
    </button>

</form>
```

Store your localized strings for your view in view.html.json:

```json
{
    "es": {
        "Name": "Nombre",
        "Password": "Contraseña",
        "Login": "Inicia sesión"
    },
    "en": {
        "Name": "Name",
        "Password": "Password",
        "Login": "Login"
    }
}
```

# How to install

Just run this inside your project:

```
go get -u github.com/deltegui/owl
```

## Resources

- [Go reference](https://pkg.go.dev/github.com/deltegui/owl)
