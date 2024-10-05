package middleware

import (
	"errors"
	"net/http"

	"github.com/deltegui/owl"
	"github.com/deltegui/owl/csrf"
)

func Csrf(cs *csrf.Csrf) owl.Middleware {
	return func(next owl.Handler) owl.Handler {
		return func(ctx owl.Ctx) error {
			if ctx.Req.Method == http.MethodGet || ctx.Req.Method == http.MethodOptions {
				ctx.Set(csrf.ContextKey, cs.Generate())
				return next(ctx)
			}
			if !cs.CheckRequest(ctx.Req) {
				ctx.Res.WriteHeader(http.StatusForbidden)
				return errors.New("expired csrf token")
			}
			ctx.Set(csrf.ContextKey, cs.Generate())
			return next(ctx)
		}
	}
}
