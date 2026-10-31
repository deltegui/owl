package middleware

import (
	"fmt"
	"net/http"

	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/csrf"
)

// Csrf rejects with 403 the requests that are not safe and do not pass cs.Check,
// and leaves the token of the request in the context (see Ctx.CsrfToken). The
// responses are private and not cacheable because they carry that token.
func Csrf(cs *csrf.Csrf) owl.Middleware {
	return func(next owl.Handler) owl.Handler {
		return func(ctx owl.Ctx) error {
			if err := cs.Check(ctx.Res, ctx.Req); err != nil {
				ctx.Res.WriteHeader(http.StatusForbidden)
				return fmt.Errorf("invalid csrf: %w", err)
			}
			token, err := cs.Token(ctx.Res, ctx.Req)
			if err != nil {
				wrapped := fmt.Errorf("error while generating CSRF token: %w", err)
				ctx.Logger.ErrorContext(ctx.Context(), wrapped.Error())
				return wrapped
			}
			header := ctx.Res.Header()
			header.Add("Vary", "Cookie")
			header.Set("Cache-Control", "private, no-store")
			ctx.Set(csrf.ContextKey, token)
			return next(ctx)
		}
	}
}
