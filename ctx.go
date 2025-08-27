package owl

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/cypher"
	"github.com/deltegui/owl/localizer"
	"github.com/deltegui/owl/logx"
	"github.com/deltegui/owl/session"
	"github.com/deltegui/valtruc"

	"github.com/julienschmidt/httprouter"
)

// Ctx represents the request Context. This includes:
// - http.Request
// - http.ResponseWriter
// - URL params
// - request context.Context
// - ModelState validation
//
// This is the main API to interact with a request.
type Ctx struct {
	Req    *http.Request
	Res    http.ResponseWriter
	params httprouter.Params
	ctx    context.Context

	ModelState core.ModelState
	validator  valtruc.Valtruc
	locstore   *localizer.WebStore
	cypher     core.Cypher

	Logger logx.Logger
}

// Validates a struct. Will populate ModelState telling if
// struct is valid and the errors found.
func (ctx *Ctx) Validate(target any) {
	errs := ctx.validator.Validate(target)
	state := core.ModelState{
		Errors: map[string][]core.ValidationError{},
	}

	if len(errs) == 0 {
		state.Valid = true
		ctx.ModelState = state
		return
	}

	state.Valid = false
	for _, e := range errs {
		valtrucErr := e.(valtruc.ValidationError)
		fieldname := valtrucErr.GetFieldName()
		if len(valtrucErr.Path()) > 0 {
			path := strings.Join(valtrucErr.Path(), ".")
			fieldname = path + "." + valtrucErr.GetFieldName()
		}
		state.Errors[fieldname] = append(state.Errors[fieldname], valtrucErr)
	}

	ctx.ModelState = state
}

// Set a variable in the context.Context.
func (ctx *Ctx) Set(key, value any) {
	ctx.ctx = context.WithValue(ctx.ctx, key, value)
}

// Get a value identified by key in context.Context.
func (ctx Ctx) Get(key any) any {
	return ctx.ctx.Value(key)
}

// Return current request context.Context
func (ctx Ctx) Context() context.Context {
	return ctx.ctx
}

// Redirects to other URL with HTTP code 307 (temporary redirect).
func (ctx Ctx) Redirect(to string) error {
	http.Redirect(ctx.Res, ctx.Req, to, http.StatusTemporaryRedirect)
	return nil
}

// Redirect to other URL with the provided status code.
func (ctx Ctx) RedirectCode(to string, code int) error {
	http.Redirect(ctx.Res, ctx.Req, to, code)
	return nil
}

// Get URL param. You can define an URL param adding a colon in front of
// it:
//
// /index/:param
//
// Then just read it:
//
// param := ctx.GetURLParam("param")
//
// This function will always return something. If no url param is found
// will return empty string.
func (ctx Ctx) GetURLParam(name string) string {
	return ctx.params.ByName(name)
}

// Get query URL param. For example:
//
// /index?first=hello&second=hola
//
// Then, you can access to URL params this way:
//
//	a := ctx.GetQueryParam("first") // returns hello
//	b := ctx.GetQueryParam("second") // returns hola
func (ctx Ctx) GetQueryParam(name string) string {
	return ctx.Req.URL.Query().Get(name)
}

// Status set the response status. Example:
//
// ctx.Status(http.StatusOk)
//
// Is an alias of ctx.Res.WriteHeader(status)
func (ctx Ctx) Status(status int) {
	ctx.Res.WriteHeader(status)
}

// Json writes to http respond a Json with the data in the struct 'data'. You
// should pass an http status. Example:
//
//	ctx.Json(http.StatusOk, struct{Name string}{"Manolito"}) // returns the Json '{ A: "Manolito" }'
func (ctx Ctx) Json(data any) error {
	response, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling data: %w", err)
	}
	ctx.Res.Header().Set("Content-Type", "application/json")
	_, err = ctx.Res.Write(response)
	return err
}

// Json writes to http respond a Json with the data in the struct 'data' with a HTTP Ok status (200)
func (ctx Ctx) JsonOk(data any) error {
	ctx.Status(http.StatusOK)
	return ctx.Json(data)
}

// String just writes a string with a status to http response. Supports string format. Example:
//
//	ctx.String("Hello %s for %d time", "Manolito", 3)
func (ctx Ctx) String(data string, a ...any) error {
	fmt.Fprintf(ctx.Res, data, a...)
	return nil
}

// Ok just writes a string with a ok (200) http status to http response. See String method.
func (ctx Ctx) StringOk(data string, a ...any) error {
	ctx.Status(http.StatusOK)
	return ctx.String(data, a...)
}

// Forbidden just writes a string with a forbidden (403) http status to http response. See String method.
func (ctx Ctx) StringForbidden(data string, a ...any) error {
	ctx.Status(http.StatusForbidden)
	return ctx.String(data, a...)
}

// BadRequest just writes a string with a bad request (400) http status to http response. See String method.
func (ctx Ctx) StringBadRequest(data string, a ...any) error {
	ctx.Status(http.StatusBadRequest)
	return ctx.String(data, a...)
}

// InternalServerError just writes a string with a internal server error (500) http status to http response. See String method.
func (ctx Ctx) StringInternalServerError(data string, a ...any) error {
	ctx.Status(http.StatusInternalServerError)
	return ctx.String(data, a...)
}

// Ok sets OK (200) http status to http response. See Status method.
func (ctx Ctx) Ok() error {
	ctx.Status(http.StatusOK)
	return nil
}

// BadRequest sets bad request (400) http status to http response. See Status method.
func (ctx Ctx) BadRequest() error {
	ctx.Status(http.StatusBadRequest)
	return nil
}

// NotFound sets not found (404) http status to http response. See Status method.
func (ctx Ctx) NotFound() error {
	ctx.Status(http.StatusNotFound)
	return nil
}

// InternalServerError sets internal server error (500) http status to http response. See Status method.
func (ctx Ctx) InternalServerError() error {
	ctx.Status(http.StatusInternalServerError)
	return nil
}

// NoContent just writes no content (204) status.
func (ctx Ctx) NotContent() error {
	ctx.Res.WriteHeader(http.StatusNoContent)
	return nil
}

// Forbidden just writes a string with a forbidden (403) http status to http response. See String method.
func (ctx Ctx) Forbidden() error {
	ctx.Status(http.StatusForbidden)
	return nil
}

// ParseJson reads http request body, decode it as a Json and stores it in the struct pointed by dst.
func (ctx Ctx) ParseJson(dst any) error {
	return json.NewDecoder(ctx.Req.Body).Decode(dst)
}

// Render writes to http response a rendered template. You must pass the following arguments:
//
// - templ: Templates parsed needed to render the view.
// - name: Name of the view you want to render and is parsed in templ
// - m: A model you pass to the view.
//
// Example of use:
//
//	func indexHandler(service demoService) owl.Handler {
//		templ := views.Parse("DemoLayout.html", "DemoMainView.html")
//		return func(ctx owl.Ctx) error {
//			return ctx.Render(templ, "DemoMainView", viewModel{
//				Name: "Manolito",
//			})
//		}
//	}
//
// If you define a DemoLayout.html like this:
//
//	<html>
//	<head>
//		<title>¡Hello!</title>
//	</head>
//	<body>
//		{{ block "Content" }}
//			<!-- Content -->
//		{{ end }}
//	</body>
//
// </html>
//
// And you fill other file named DemoMainView.html this way:
//
//	{{ define "Content" }}
//		<h1>Hello {{ .Model.Name }}</h1>
//	{{ end }}
//
// The above code will render this html:
//
//	<html>
//	<head>
//		<title>¡Hello!</title>
//	</head>
//	<body>
//		<h1>Hello Manolito</h1>
//	</body>
//	</html>
//
// For more information how ViewModels works see ViewModel struct type.
func (ctx Ctx) Render(templ *template.Template, name string, m any) error {
	return templ.Execute(ctx.Res, createViewModel(ctx, name, m))
}

/*
func (ctx Ctx) Render(templ *template.Template, m any) error {
	definedTemplates := templ.DefinedTemplates()
	lastIndex := len(definedTemplates) - 1
	if lastIndex < 0 {
		panic("Call to render with a template.Template with no parsed templates!")
	}
	ctx.RenderLocalized(templ, definedTemplates[lastIndex], m)
}
*/

// GetLocalizer creates a Localizer from Json file with the language defined
// in http cookie.
func (ctx Ctx) GetLocalizer(file string) localizer.Localizer {
	if ctx.locstore == nil {
		return localizer.Localizer{}
	}
	return ctx.locstore.GetUsingRequest(file, ctx.Req)
}

// Localizes a key using the Json file you provided with the language defined
// in http cookie.
func (ctx Ctx) Localize(file, key string) string {
	if ctx.locstore == nil {
		return key
	}
	return ctx.locstore.GetUsingRequest(file, ctx.Req).Get(key)
}

// Localizes a key using the Json file you provided with the language defined
// in http cookie. Ignores Shared translations.
func (ctx Ctx) LocalizeWithoutShared(file, key string) string {
	if ctx.locstore == nil {
		return key
	}
	return ctx.locstore.GetUsingRequestWithoutShared(file, ctx.Req).Get(key)
}

// Localizes a DomainError using the error Json file with the language defined
// in http cookie.
func (ctx Ctx) LocalizeError(err core.DomainError) string {
	if ctx.locstore == nil {
		return err.Message
	}
	return ctx.locstore.GetLocalizedError(err, ctx.Req)
}

// HaveSession tells if a session is created for this http request.
func (ctx Ctx) HaveSession() bool {
	instance := ctx.Get(session.ContextKey)
	if instance == nil {
		return false
	}
	if _, ok := instance.(session.User); !ok {
		return false
	}
	return true
}

// GetUser get current logged user.
func (ctx Ctx) GetUser() session.User {
	return ctx.Get(session.ContextKey).(session.User)
}

// GetCurrentLanguage get current cookie defined language.
func (ctx *Ctx) GetCurrentLanguage() string {
	return ctx.locstore.ReadCookie(ctx.Req)
}

// ChangeLanguage changes current cookie defined language.
func (ctx *Ctx) ChangeLanguage(to string) error {
	return ctx.locstore.CreateCookie(ctx.Res, to)
}

type CookieOptions struct {
	Name    string
	Expires time.Duration
	Value   string

	// Set if front scripts can access to cookie.
	// If its true, front script cannot access.
	// By default true.
	HttpOnly bool

	// Sets if cookies are only send through https.
	// If its true means https only. By default false.
	Secure bool
}

// Creates a cookie with provided options.
func (ctx *Ctx) CreateCookieOptions(opt CookieOptions) error {
	var data string
	var err error
	data, err = cypher.EncodeCookie(ctx.cypher, opt.Value)
	if err != nil {
		return fmt.Errorf("error encoding cookie: %w", err)
	}

	http.SetCookie(ctx.Res, &http.Cookie{
		Name:     opt.Name,
		Value:    data,
		Expires:  time.Now().Add(opt.Expires),
		MaxAge:   int(opt.Expires.Seconds()),
		Path:     "/",
		SameSite: http.SameSiteDefaultMode,
		HttpOnly: opt.HttpOnly,
		Secure:   opt.Secure,
	})
	return nil
}

// Creates a cookie with name and data. By default uses a one day duration expiration,
// HttpOnly enabled, and Secure enabled.
func (ctx *Ctx) CreateCookie(name, data string) error {
	return ctx.CreateCookieOptions(CookieOptions{
		Name:     name,
		Expires:  core.OneDayDuration,
		Value:    data,
		HttpOnly: true,
		Secure:   false,
	})
}

// Reads a cookie identified by name.
func (ctx *Ctx) ReadCookie(name string) (string, error) {
	cookie, err := ctx.Req.Cookie(name)
	if err != nil {
		return "", fmt.Errorf("error while reading cookie with key: '%s': %w", name, err)
	}
	var data string
	data, err = cypher.DecodeCookie(ctx.cypher, cookie.Value)
	if err != nil {
		return "", fmt.Errorf("cannot decode cookie: %w", err)
	}
	return data, nil
}

// Deletes a cookie identified by name.
func (ctx *Ctx) DeleteCookie(name string) error {
	_, err := ctx.Req.Cookie(name)
	if err != nil {
		return fmt.Errorf("error while reading cookie with key: '%s': %w", name, err)
	}
	http.SetCookie(ctx.Res, &http.Cookie{
		Name:     name,
		Value:    "",
		Expires:  time.Unix(0, 0),
		MaxAge:   0,
		Path:     "/",
		SameSite: http.SameSiteDefaultMode,
	})
	return nil
}
