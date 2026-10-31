package session

import "context"

type contextKey struct{}

// ContextKey is where the middleware leaves the User in the request context.
var ContextKey = contextKey{}

// FromContext is the user that the middleware left in ctx.
func FromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(ContextKey).(User)
	return user, ok
}
