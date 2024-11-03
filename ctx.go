package owl

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/cypher"
	"github.com/deltegui/owl/localizer"
	"github.com/deltegui/owl/session"
	"github.com/deltegui/valtruc"

	"github.com/julienschmidt/httprouter"
)

type Ctx struct {
	Req    *http.Request
	Res    http.ResponseWriter
	params httprouter.Params
	ctx    context.Context

	ModelState core.ModelState
	validator  valtruc.Valtruc
	locstore   *localizer.Store
	cypher     core.Cypher
}

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
		state.Errors[fieldname] = append(state.Errors[fieldname], valtrucErr)
	}

	ctx.ModelState = state
}

func (ctx *Ctx) Set(key, value any) {
	ctx.ctx = context.WithValue(ctx.ctx, key, value)
}

func (ctx Ctx) Get(key any) any {
	return ctx.ctx.Value(key)
}

func (ctx Ctx) Redirect(to string) error {
	http.Redirect(ctx.Res, ctx.Req, to, http.StatusTemporaryRedirect)
	return nil
}

func (ctx Ctx) RedirectCode(to string, code int) error {
	http.Redirect(ctx.Res, ctx.Req, to, code)
	return nil
}

func (ctx Ctx) GetURLParam(name string) string {
	return ctx.params.ByName(name)
}

func (ctx Ctx) GetQueryParam(name string) string {
	return ctx.Req.URL.Query().Get(name)
}

func (ctx Ctx) Json(status int, data any) error {
	response, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling data: %w", err)
	}
	ctx.Res.WriteHeader(status)
	ctx.Res.Header().Set("Content-Type", "application/json")
	_, err = ctx.Res.Write(response)
	return err
}

func (ctx Ctx) JsonOk(data any) error {
	return ctx.Json(http.StatusOK, data)
}

func (ctx Ctx) String(status int, data string, a ...any) error {
	ctx.Res.WriteHeader(status)
	fmt.Fprintf(ctx.Res, data, a...)
	return nil
}

func (ctx Ctx) BadRequest(data string, a ...any) error {
	return ctx.String(http.StatusBadRequest, data, a...)
}

func (ctx Ctx) NotFound(data string, a ...any) error {
	return ctx.String(http.StatusNotFound, data, a...)
}

func (ctx Ctx) Ok(data string, a ...any) error {
	return ctx.String(http.StatusOK, data, a...)
}

func (ctx Ctx) InternalServerError(data string, a ...any) error {
	return ctx.String(http.StatusInternalServerError, data, a...)
}

func (ctx Ctx) NotContent() error {
	ctx.Res.WriteHeader(http.StatusNoContent)
	return nil
}

func (ctx Ctx) Forbidden(data string, a ...any) error {
	return ctx.String(http.StatusForbidden, data, a...)
}

func (ctx Ctx) ParseJson(dst any) error {
	return json.NewDecoder(ctx.Req.Body).Decode(dst)
}

func (ctx Ctx) Render(templ *template.Template, name string, m any) error {
	return templ.Execute(ctx.Res, createViewModel(ctx, name, m))
}

func (ctx Ctx) GetLocalizer(file string) localizer.Localizer {
	if ctx.locstore == nil {
		return localizer.Localizer{}
	}
	return ctx.locstore.GetUsingRequest(file, ctx.Req)
}

func (ctx Ctx) Localize(file, key string) string {
	if ctx.locstore == nil {
		return key
	}
	return ctx.locstore.GetUsingRequest(file, ctx.Req).Get(key)
}

func (ctx Ctx) LocalizeWithoutShared(file, key string) string {
	if ctx.locstore == nil {
		return key
	}
	return ctx.locstore.GetUsingRequestWithoutShared(file, ctx.Req).Get(key)
}

func (ctx Ctx) LocalizeError(err core.DomainError) string {
	if ctx.locstore == nil {
		return err.Message
	}
	return ctx.locstore.GetLocalizedError(err, ctx.Req)
}

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

func (ctx Ctx) GetUser() session.User {
	return ctx.Get(session.ContextKey).(session.User)
}

func (ctx *Ctx) GetCurrentLanguage() string {
	return ctx.locstore.ReadCookie(ctx.Req)
}

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

func (ctx *Ctx) CreateCookie(name, data string) error {
	return ctx.CreateCookieOptions(CookieOptions{
		Name:     name,
		Expires:  core.OneDayDuration,
		Value:    data,
		HttpOnly: true,
		Secure:   false,
	})
}

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
