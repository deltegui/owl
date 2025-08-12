package middleware

import (
	"github.com/deltegui/owl"
)

// Logs HTTP requests using log
func Logger(next owl.Handler) owl.Handler {
	return func(ctx owl.Ctx) error {
		ctx.Logger.Info(
			"Request info",
			"address",
			ctx.Req.RemoteAddr,
			"agent",
			ctx.Req.UserAgent(),
			"method",
			ctx.Req.Method,
			"uri",
			ctx.Req.RequestURI)
		return next(ctx)
	}
}
