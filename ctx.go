package owl

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/deltegui/owl/localizer"
	"github.com/deltegui/owl/session"

	"github.com/julienschmidt/httprouter"
)

type Ctx struct {
	Req    *http.Request
	Res    http.ResponseWriter
	params httprouter.Params
	ctx    context.Context

	locstore *localizer.Store
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
	fmt.Fprintf(ctx.Res, data, a...)
	ctx.Res.WriteHeader(status)
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
	return ctx.locstore.GetUsingRequest(file, ctx.Req)
}

func (ctx Ctx) Localize(file, key string) string {
	return ctx.locstore.GetUsingRequest(file, ctx.Req).Get(key)
}

func (ctx Ctx) LocalizeWithoutShared(file, key string) string {
	return ctx.locstore.GetUsingRequestWithoutShared(file, ctx.Req).Get(key)
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
