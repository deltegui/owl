package logx

import (
	"context"
	"log"
	"log/slog"
)

type Level int

const (
	LevelDebug Level = -4
	LevelInfo  Level = 0
	LevelWarn  Level = 4
	LevelError Level = 8
)

type Logger interface {
	WithModuleName(name string) Logger

	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)

	Warn(msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)

	Error(msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)

	Debug(msg string, args ...any)
	DebugContext(ctx context.Context, msg string, args ...any)
}

type SlogLogger struct {
	slog *slog.Logger
}

func NewWithSlog(slog *slog.Logger) SlogLogger {
	return SlogLogger{slog}
}

func (logger SlogLogger) WithModuleName(module string) Logger {
	return NewWithSlog(logger.slog.With("module", module))
}

func (logger SlogLogger) Info(msg string, args ...any) {
	logger.slog.Info(msg, args...)
}

func (logger SlogLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	logger.slog.InfoContext(ctx, msg, args...)
}

func (logger SlogLogger) Warn(msg string, args ...any) {
	logger.slog.Warn(msg, args...)
}

func (logger SlogLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	logger.slog.WarnContext(ctx, msg, args...)
}

func (logger SlogLogger) Error(msg string, args ...any) {
	logger.slog.Error(msg, args...)
}

func (logger SlogLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	logger.slog.ErrorContext(ctx, msg, args...)
}

func (logger SlogLogger) Debug(msg string, args ...any) {
	logger.slog.Debug(msg, args...)
}

func (logger SlogLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	logger.slog.DebugContext(ctx, msg, args...)
}

type Default struct {
	Level  Level
	module string
}

func (logger Default) WithModuleName(module string) Logger {
	return Default{
		Level:  logger.Level,
		module: module,
	}
}

func (logger Default) Info(msg string, args ...any) {
	if logger.Level > LevelInfo {
		return
	}
	log.Printf("[INFO] "+msg, args...)
}

func (logger Default) InfoContext(ctx context.Context, msg string, args ...any) {
	logger.Info(msg, args...)
}

func (logger Default) Warn(msg string, args ...any) {
	if logger.Level > LevelWarn {
		return
	}
	log.Printf("[WARN] "+msg, args...)
}

func (logger Default) WarnContext(ctx context.Context, msg string, args ...any) {
	logger.Warn(msg, args...)
}

func (logger Default) Error(msg string, args ...any) {
	if logger.Level > LevelError {
		return
	}
	log.Printf("[ERROR] "+msg, args...)
}

func (logger Default) ErrorContext(ctx context.Context, msg string, args ...any) {
	logger.Error(msg, args...)
}

func (logger Default) Debug(msg string, args ...any) {
	if logger.Level > LevelDebug {
		return
	}
	log.Printf("[DEBUG] "+msg, args...)
}

func (logger Default) DebugContext(ctx context.Context, msg string, args ...any) {
	logger.Debug(msg, args...)
}

func Err(err error) slog.Attr {
	return slog.Any("err", err)
}

type MultiHandler struct {
	handlers []slog.Handler
}

func NewSlogMultiHandler(handlers ...slog.Handler) slog.Handler {
	return &MultiHandler{handlers: handlers}
}

func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *MultiHandler) Handle(ctx context.Context, record slog.Record) error {
	var err error
	for _, h := range m.handlers {
		if h.Enabled(ctx, record.Level) {
			if e := h.Handle(ctx, record); e != nil && err == nil {
				err = e
			}
		}
	}
	return err
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var with []slog.Handler
	for _, h := range m.handlers {
		with = append(with, h.WithAttrs(attrs))
	}
	return &MultiHandler{handlers: with}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	var with []slog.Handler
	for _, h := range m.handlers {
		with = append(with, h.WithGroup(name))
	}
	return &MultiHandler{handlers: with}
}
