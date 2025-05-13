package middleware

import (
	"log"
	"log/slog"

	"github.com/deltegui/owl"
)

// Logs HTTP requests using log
func Logger(next owl.Handler) owl.Handler {
	return func(ctx owl.Ctx) error {
		log.Printf(
			"[OWL] request from %s (%s) to (%s) %s",
			ctx.Req.RemoteAddr,
			ctx.Req.UserAgent(),
			ctx.Req.Method,
			ctx.Req.RequestURI)
		return next(ctx)
	}
}

// Logs HTTP request using slog
func SlogLogger(next owl.Handler, logger *slog.Logger) owl.Handler {
	return func(ctx owl.Ctx) error {
		logger.Info(
			"[OWL] request from %s (%s) to (%s) %s",
			ctx.Req.RemoteAddr,
			ctx.Req.UserAgent(),
			ctx.Req.Method,
			ctx.Req.RequestURI)
		return next(ctx)
	}
}
