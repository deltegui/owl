package owl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/csrf"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/localizer/webstore"
	"github.com/deltegui/owl/v3/logx"
	"github.com/deltegui/owl/v3/session"
	"github.com/deltegui/valtruc"

	"github.com/julienschmidt/httprouter"
)

// ErrNoLocalization is what ChangeLanguage returns when AddLocalization was not called.
var ErrNoLocalization = errors.New("owl: localization is not configured, call AddLocalization")

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
	locstore   *webstore.Store
	cipher     core.Cipher

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
//
// Without arguments the text is written as it is, so a "%" in it (for example in
// text that comes from a user) is not taken for a format.
func (ctx Ctx) String(data string, a ...any) error {
	var err error
	if len(a) == 0 {
		_, err = io.WriteString(ctx.Res, data)
	} else {
		_, err = fmt.Fprintf(ctx.Res, data, a...)
	}
	return err
}

// Text writes the text as it is. Unlike String it is not a format, so it is what
// to use with text that comes from somewhere else (a "%" in it is only a "%").
func (ctx Ctx) Text(data string) error {
	_, err := io.WriteString(ctx.Res, data)
	return err
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
func (ctx Ctx) NoContent() error {
	ctx.Res.WriteHeader(http.StatusNoContent)
	return nil
}

// StringNotFound just writes a string with a not found (404) http status to http response. See String method.
func (ctx Ctx) StringNotFound(data string, a ...any) error {
	ctx.Status(http.StatusNotFound)
	return ctx.String(data, a...)
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

// Texts is the Localizer of a component in the language defined in http cookie,
// with the shared translations (see AddLocalization) added; it is a new map.
// A source with nothing in that language is read in the default one. Without
// localization it is empty, so every key shows as itself.
func (ctx Ctx) Texts(src localizer.Source) localizer.Localizer {
	if ctx.locstore == nil {
		return localizer.Localizer{}
	}
	return ctx.locstore.Texts(src, ctx.Req)
}

// Localizes a DomainError using the errors translations with the language
// defined in http cookie.
func (ctx Ctx) LocalizeError(err core.DomainError) string {
	if ctx.locstore == nil {
		return err.Message
	}
	return ctx.locstore.GetLocalizedError(err, ctx.Req)
}

// CsrfToken is the CSRF token of the request, or an empty string if the CSRF
// middleware is not used.
func (ctx Ctx) CsrfToken() string {
	token, _ := ctx.Get(csrf.ContextKey).(string)
	return token
}

// HaveSession tells if there is a logged user in this request (the session
// middleware left it in the context).
func (ctx Ctx) HaveSession() bool {
	_, ok := ctx.User()
	return ok
}

// User is the logged user, and false if there is none.
func (ctx Ctx) User() (session.User, bool) {
	return session.FromContext(ctx.ctx)
}

// GetCurrentLanguage get current cookie defined language.
// It is always one of the languages of AddLocalization, the first one if the
// request has no valid cookie. Without localization it is an empty string.
func (ctx *Ctx) GetCurrentLanguage() string {
	if ctx.locstore == nil {
		return ""
	}
	return ctx.locstore.ReadCookie(ctx.Req)
}

// ChangeLanguage changes current cookie defined language.
// It returns ErrNoLocalization if the multiplexer has none (see AddLocalization).
func (ctx *Ctx) ChangeLanguage(to string) error {
	if ctx.locstore == nil {
		return ErrNoLocalization
	}
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

// cookiePurpose ties what a cookie holds to its name: a value sealed for one cookie
// does not open as another.
func cookiePurpose(name string) string { return "cookie:" + name }

// Creates a cookie with provided options.
func (ctx *Ctx) CreateCookieOptions(opt CookieOptions) error {
	var data string
	var err error
	data, err = cipher.Seal(ctx.cipher, cookiePurpose(opt.Name), opt.Value)
	if err != nil {
		return fmt.Errorf("error sealing cookie: %w", err)
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
	data, err = cipher.Open(ctx.cipher, cookiePurpose(name), cookie.Value, 0)
	if err != nil {
		return "", fmt.Errorf("cannot open cookie: %w", err)
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
