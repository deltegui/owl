package middleware

import (
	"fmt"
	"net/http"

	"github.com/deltegui/owl"
	"github.com/deltegui/owl/csrf"
)

func Csrf(cs *csrf.Csrf) owl.Middleware {
	return func(next owl.Handler) owl.Handler {
		return func(ctx owl.Ctx) error {
			if ctx.Req.Method != http.MethodGet && ctx.Req.Method != http.MethodOptions {
				if err := cs.CheckRequest(ctx.Req); err != nil {
					ctx.Res.WriteHeader(http.StatusForbidden)
					return fmt.Errorf("invalid csrf: %w", err)
				}
			}

			token, err := cs.Generate()
			if err != nil {
				wrapped := fmt.Errorf("error while generating CSRF token: %w", err)
				ctx.Logger.ErrorContext(ctx.Context(), wrapped.Error())
				return wrapped
			}
			ctx.Set(csrf.ContextKey, token)
			return next(ctx)
		}
	}
}
