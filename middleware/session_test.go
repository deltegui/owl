package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/middleware"
	"github.com/deltegui/owl/v3/session"
)

type app struct {
	mux     *owl.Mux
	manager *session.Manager
	reached map[string]bool
}

func newApp(t *testing.T) *app {
	t.Helper()
	c, err := cipher.New()
	if err != nil {
		t.Fatal(err)
	}
	a := &app{mux: owl.New(c), manager: session.NewManager(c, session.WithSecure(false)), reached: map[string]bool{}}
	handler := func(name string) owl.Handler {
		return func(ctx owl.Ctx) error {
			user, ok := ctx.User()
			a.reached[name] = ok && user.Id != 0 && ctx.HaveSession()
			return nil
		}
	}
	a.mux.Get("/any", handler("any"), middleware.Authorize(a.manager, "/login"))
	a.mux.Get("/api", handler("api"), middleware.Authorize(a.manager, ""))
	a.mux.Get("/roles", handler("roles"), middleware.AuthorizeRoles(a.manager, "/login", core.RoleAdmin, 99))
	a.mux.Get("/admin", handler("admin"), middleware.Admin(a.manager, "/login"))
	a.mux.Post("/form", handler("form"), middleware.Authorize(a.manager, "/login"))
	return a
}

func (a *app) login(t *testing.T, roles ...core.Role) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := a.manager.Create(t.Context(), rec, session.User{Id: 1, Roles: roles}); err != nil {
		t.Fatal(err)
	}
	return rec.Result().Cookies()[0]
}

func (a *app) get(method, path, accept string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	return rec
}

func TestAuthorize(t *testing.T) {
	a := newApp(t)
	cookie := a.login(t)
	if rec := a.get("GET", "/any", "", cookie); rec.Code != http.StatusOK || !a.reached["any"] {
		t.Fatalf("a user with a session must reach the handler and be in the context: %d", rec.Code)
	}
}

func TestWithoutSession(t *testing.T) {
	a := newApp(t)
	html := a.get("GET", "/any", "text/html,application/xhtml+xml", nil)
	if html.Code != http.StatusSeeOther || html.Header().Get("Location") != "/login" {
		t.Errorf("a browser goes to the login with a 303: %d %q", html.Code, html.Header().Get("Location"))
	}
	if rec := a.get("POST", "/form", "text/html", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("a form POST also goes to the login with a 303 (never a 307): %d", rec.Code)
	}
	if rec := a.get("GET", "/any", "application/json", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("JSON gets a 401: %d", rec.Code)
	}
	if rec := a.get("GET", "/api", "text/html", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a login URL it is always a 401: %d", rec.Code)
	}
	if len(a.reached) != 0 {
		t.Errorf("no handler may run: %v", a.reached)
	}
}

func TestRoles(t *testing.T) {
	a := newApp(t)
	plain, admin := a.login(t, 5), a.login(t, core.RoleAdmin)
	if rec := a.get("GET", "/roles", "text/html", plain); rec.Code != http.StatusForbidden {
		t.Errorf("a logged user without the role gets a 403, not the login: %d", rec.Code)
	}
	if rec := a.get("GET", "/roles", "", admin); rec.Code != http.StatusOK || !a.reached["roles"] {
		t.Errorf("with a role it passes and the user is in the context (it was not before): %d", rec.Code)
	}
	if rec := a.get("GET", "/admin", "", plain); rec.Code != http.StatusForbidden {
		t.Errorf("Admin: %d", rec.Code)
	}
	if rec := a.get("GET", "/admin", "", admin); rec.Code != http.StatusOK || !a.reached["admin"] {
		t.Errorf("Admin: %d", rec.Code)
	}
	if rec := a.get("GET", "/roles", "text/html", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("without a session it is the login: %d", rec.Code)
	}
}

func TestARecentSessionIsNotSetAgain(t *testing.T) {
	a := newApp(t)
	cookie := a.login(t)
	rec := a.get("GET", "/any", "", cookie)
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("a recent session must not be set again")
	}
}
