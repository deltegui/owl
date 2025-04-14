package middleware

import (
	"log"
	"net/http"

	"slices"

	"github.com/deltegui/owl"
	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/session"
)

func Authorize(manager *session.Manager, url string) owl.Middleware {
	return func(next owl.Handler) owl.Handler {
		return func(ctx owl.Ctx) error {
			user, err := manager.ReadSessionCookie(ctx.Req)
			if err != nil {
				handleError(ctx, url)
				return err
			}
			ctx.Set(session.ContextKey, user)
			return next(ctx)
		}
	}
}

func AuthorizeRoles(manager *session.Manager, url string, roles []core.Role) owl.Middleware {
	return func(next owl.Handler) owl.Handler {
		return func(ctx owl.Ctx) error {
			user, err := manager.ReadSessionCookie(ctx.Req)
			if err != nil {
				handleError(ctx, url)
				return err
			}
			for _, userRole := range user.Roles {
				if slices.Contains(roles, userRole) {
					return next(ctx)
				}
			}
			handleError(ctx, url)
			return nil
		}
	}
}

func Admin(manager *session.Manager, url string) owl.Middleware {
	return func(next owl.Handler) owl.Handler {
		return func(ctx owl.Ctx) error {
			user, err := manager.ReadSessionCookie(ctx.Req)
			if err != nil {
				log.Println("Error while authenticating:", err)
				handleError(ctx, url)
				return err
			}
			if slices.Contains(user.Roles, core.RoleAdmin) {
				log.Println("User is not admin!")
				handleError(ctx, url)
				return err
			}
			ctx.Set(session.ContextKey, user)
			return next(ctx)
		}
	}
}

func handleError(ctx owl.Ctx, url string) {
	if len(url) > 0 {
		http.Redirect(ctx.Res, ctx.Req, url, http.StatusTemporaryRedirect)
		log.Printf("Authentication failed. Redirecting to url: %s", url)
	} else {
		ctx.Res.WriteHeader(http.StatusUnauthorized)
		log.Println("Authentication failed")
	}
}
