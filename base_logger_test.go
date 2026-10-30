package owl

import (
	"context"
	"fmt"

	"github.com/deltegui/owl/v3/logx"
)

type nopLogger struct{}

func (nopLogger) WithModuleName(string) logx.Logger            { return nopLogger{} }
func (nopLogger) Info(string, ...any)                          {}
func (nopLogger) InfoContext(context.Context, string, ...any)  {}
func (nopLogger) Warn(string, ...any)                          {}
func (nopLogger) WarnContext(context.Context, string, ...any)  {}
func (nopLogger) Error(string, ...any)                         {}
func (nopLogger) ErrorContext(context.Context, string, ...any) {}
func (nopLogger) Debug(string, ...any)                         {}
func (nopLogger) DebugContext(context.Context, string, ...any) {}

func (l *recordLogger) WithModuleName(string) logx.Logger { return l }
func (l *recordLogger) ErrorContext(_ context.Context, msg string, args ...any) {
	l.errors = append(l.errors, fmt.Sprint(append([]any{msg}, args...)...))
}
