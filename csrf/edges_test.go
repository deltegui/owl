package csrf

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/core"
)

type brokenCipher struct{ core.Cipher }

var errBroken = errors.New("broken")

func (brokenCipher) Encrypt(_, _ []byte) ([]byte, error) { return nil, errBroken }

func TestRotateFailsWhenTheCookieCannotBeSealed(t *testing.T) {
	c := New(brokenCipher{})
	rec := httptest.NewRecorder()
	token, err := c.Rotate(rec)
	if !errors.Is(err, errBroken) || !strings.Contains(err.Error(), "cannot seal csrf cookie") || token != "" {
		t.Fatalf("got %q, %v", token, err)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("no cookie when it cannot be sealed")
	}
}

func TestSecretErrorsSayWhy(t *testing.T) {
	c := newCsrf()
	if _, err := c.secret(httptest.NewRequest(http.MethodGet, "/", nil)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("no cookie: %v", err)
	}
	garbage := httptest.NewRequest(http.MethodGet, "/", nil)
	garbage.AddCookie(&http.Cookie{Name: CookieName, Value: "garbage"})
	if _, err := c.secret(garbage); err == nil || !strings.Contains(err.Error(), "invalid csrf cookie") {
		t.Errorf("garbage: %v", err)
	}
	// A cookie that opens fine but holds nothing is no secret: anyone could match it with an empty token.
	empty, _ := cipher.Seal(c.cipher, purpose, "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: empty})
	if secret, err := c.secret(req); err == nil {
		t.Errorf("an empty secret is accepted: %q", secret)
	}
	rec := httptest.NewRecorder()
	if token, err := c.Token(rec, req); err != nil || token == "" || len(rec.Result().Cookies()) != 1 {
		t.Errorf("an empty secret is replaced by a new one: %q %v", token, err)
	}
}

func TestCheckErrorsSayWhy(t *testing.T) {
	c := newCsrf()
	cookie, token := browser(t, c)
	if err := c.Check(httptest.NewRecorder(), post(nil, withHeader(token))); err == nil || !strings.Contains(err.Error(), "cookie") {
		t.Errorf("no cookie: %v", err)
	}
	if err := c.Check(httptest.NewRecorder(), post(cookie, nil)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("no token: %v", err)
	}
	if err := c.Check(httptest.NewRecorder(), post(cookie, withHeader("other"))); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("wrong token: %v", err)
	}
}

func TestOriginOfARequestWithoutOrigin(t *testing.T) {
	c := newCsrf()
	cookie, token := browser(t, c)
	withReferer := func(referer string) func(*http.Request) {
		return func(r *http.Request) {
			withHeader(token)(r)
			r.Header.Set("Referer", referer)
		}
	}
	for _, bad := range []string{"::not a url", "example.com/save", "//example.com/save", "http:///save", "/save"} {
		err := c.Check(httptest.NewRecorder(), post(cookie, withReferer(bad)))
		if err == nil || !strings.Contains(err.Error(), "invalid referer") {
			t.Errorf("Referer %q: %v", bad, err)
		}
	}
	if err := c.Check(httptest.NewRecorder(), post(cookie, withReferer("http://example.com/form"))); err != nil {
		t.Errorf("a referer of the same host: %v", err)
	}
	if err := c.Check(httptest.NewRecorder(), post(cookie, withReferer("http://evil.test/form"))); err == nil {
		t.Error("a referer of another host")
	}
}

func TestSecFetchSite(t *testing.T) {
	c := newCsrf()
	cookie, token := browser(t, c)
	site := func(v string) func(*http.Request) {
		return func(r *http.Request) {
			withHeader(token)(r)
			r.Header.Set("Sec-Fetch-Site", v)
		}
	}
	for _, ok := range []string{"same-origin", "none"} {
		if err := c.Check(httptest.NewRecorder(), post(cookie, site(ok))); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"cross-site", "same-site"} {
		if err := c.Check(httptest.NewRecorder(), post(cookie, site(bad))); err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestTheDefaultBodyLimitIsThirtyTwoMiB(t *testing.T) {
	if got := newCsrf().maxBody; got != 32<<20 {
		t.Fatalf("maxBody = %d", got)
	}
	if got := newCsrf(WithMaxBody(10)).maxBody; got != 10 {
		t.Fatalf("WithMaxBody = %d", got)
	}
}
