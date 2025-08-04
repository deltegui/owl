package middleware

import (
	"github.com/deltegui/owl"
)

// Logs HTTP requests using log
func Logger(next owl.Handler) owl.Handler {
	return func(ctx owl.Ctx) error {
		ctx.Logger.Info(
			"[OWL] request from %s (%s) to (%s) %s",
			ctx.Req.RemoteAddr,
			ctx.Req.UserAgent(),
			ctx.Req.Method,
			ctx.Req.RequestURI)
		return next(ctx)
	}
}
