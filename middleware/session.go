package middleware

import (
	"net/http"
	"slices"
	"strings"

	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/session"
)

// Authorize lets through the requests with a session and leaves the user in the
// context (see Ctx.User). Without one, a browser (it accepts text/html) is sent to
// loginURL with a 303, and anything else, or every request if loginURL is empty,
// gets a 401.
func Authorize(manager *session.Manager, loginURL string) owl.Middleware {
	return AuthorizeRoles(manager, loginURL)
}

// AuthorizeRoles is Authorize and, besides, the user must have one of the roles, or
// the answer is a 403 (they are logged in: sending them to the login would not help).
// With no roles, any user passes.
func AuthorizeRoles(manager *session.Manager, loginURL string, roles ...core.Role) owl.Middleware {
	return func(next owl.Handler) owl.Handler {
		return func(ctx owl.Ctx) error {
			user, err := manager.User(ctx.Res, ctx.Req)
			if err != nil {
				ctx.Logger.InfoContext(ctx.Context(), "authentication failed", "error", err.Error())
				deny(ctx, loginURL)
				return nil
			}
			if len(roles) > 0 && !slices.ContainsFunc(roles, user.HasRole) {
				ctx.Logger.InfoContext(ctx.Context(), "user has none of the roles", "user", user.Id)
				http.Error(ctx.Res, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return nil
			}
			ctx.Set(session.ContextKey, user)
			return next(ctx)
		}
	}
}

// Admin is AuthorizeRoles for core.RoleAdmin.
func Admin(manager *session.Manager, loginURL string) owl.Middleware {
	return AuthorizeRoles(manager, loginURL, core.RoleAdmin)
}

func deny(ctx owl.Ctx, loginURL string) {
	if loginURL != "" && strings.Contains(ctx.Req.Header.Get("Accept"), "text/html") {
		http.Redirect(ctx.Res, ctx.Req, loginURL, http.StatusSeeOther)
		return
	}
	http.Error(ctx.Res, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
}
