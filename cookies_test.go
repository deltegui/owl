package owl

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/cipher"
)

func newCookieCtx(t *testing.T, req *http.Request) (*Ctx, *httptest.ResponseRecorder) {
	t.Helper()
	c, err := cipher.New()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	return &Ctx{Req: req, Res: rec, cipher: c}, rec
}

func TestCreatedCookiesAreSameSiteLax(t *testing.T) {
	ctx, rec := newCookieCtx(t, httptest.NewRequest("GET", "/", nil))
	if err := ctx.CreateCookieOptions(CookieOptions{Name: "theme", Value: "dark", Expires: time.Hour}); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("want one SameSite=Lax cookie, got %+v", cookies)
	}
}

func TestDeleteCookieExpiresItNow(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "x"})
	ctx, rec := newCookieCtx(t, req)
	if err := ctx.DeleteCookie("theme"); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want one cookie, got %+v", cookies)
	}
	c := cookies[0]
	// MaxAge < 0 is written as Max-Age=0: the browser drops it right away.
	if c.Name != "theme" || c.Value != "" || c.MaxAge != -1 || c.Path != "/" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected deletion cookie: %+v", c)
	}
	if got := rec.Header().Get("Set-Cookie"); !strings.Contains(got, "Max-Age=0") {
		t.Fatalf("Set-Cookie should carry Max-Age=0: %q", got)
	}
}

func TestDeleteCookieThatWasNotSentIsAnError(t *testing.T) {
	ctx, rec := newCookieCtx(t, httptest.NewRequest("GET", "/", nil))
	if err := ctx.DeleteCookie("theme"); err == nil {
		t.Fatal("want an error")
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("nothing should be written")
	}
}
