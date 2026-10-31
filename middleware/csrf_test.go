package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/csrf"
	"github.com/deltegui/owl/v3/middleware"
)

func serve(t *testing.T) (http.Handler, *int) {
	t.Helper()
	calls := new(int)
	c, err := cipher.New()
	if err != nil {
		t.Fatal(err)
	}
	r := owl.New(c)
	r.Use(middleware.Csrf(csrf.New(c, csrf.WithSecure(false))))
	r.Get("/", func(ctx owl.Ctx) error {
		_, _ = ctx.Res.Write([]byte(ctx.CsrfToken()))
		return nil
	})
	r.Post("/save", func(ctx owl.Ctx) error { *calls++; return nil })
	return r, calls
}

func TestFlow(t *testing.T) {
	h, calls := serve(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	token := rec.Body.String()
	if token == "" || len(rec.Result().Cookies()) != 1 {
		t.Fatalf("GET must give a token and a cookie: %q", token)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	cookie := rec.Result().Cookies()[0]

	bad := httptest.NewRecorder()
	h.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/save", nil))
	if bad.Code != http.StatusForbidden || *calls != 0 {
		t.Fatalf("POST without token: %d, handler calls %d", bad.Code, *calls)
	}

	form := url.Values{csrf.FieldName: {token}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, req)
	if ok.Code != http.StatusOK || *calls != 1 {
		t.Fatalf("POST with token: %d, handler calls %d", ok.Code, *calls)
	}
}
