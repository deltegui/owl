package csrf

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/deltegui/owl/v3/cipher"
)

func newCsrf(opts ...Option) *Csrf {
	c, err := cipher.New()
	if err != nil {
		panic(err)
	}
	return New(c, opts...)
}

// browser asks for a page and keeps the cookie and the token it gets.
func browser(t *testing.T, c *Csrf) (*http.Cookie, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	token, err := c.Token(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want one cookie, got %d", len(cookies))
	}
	return cookies[0], token
}

func post(cookie *http.Cookie, mutate func(*http.Request)) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/save", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if mutate != nil {
		mutate(req)
	}
	return req
}

func withHeader(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set(HeaderName, token) }
}

func TestCookieAttributes(t *testing.T) {
	cookie, _ := browser(t, newCsrf())
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("bad cookie: %+v", cookie)
	}
	cookie, _ = browser(t, newCsrf(WithSecure(false)))
	if cookie.Secure {
		t.Fatal("WithSecure(false) must clear Secure")
	}
}

func TestTokenIsStableAndCookieIsReused(t *testing.T) {
	c := newCsrf()
	cookie, token := browser(t, c)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	again, err := c.Token(rec, req)
	if err != nil || again != token {
		t.Fatalf("token changed: %q %q %v", token, again, err)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("a valid cookie must not be replaced")
	}
}

func TestSafeMethodsPass(t *testing.T) {
	c := newCsrf()
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		if err := c.Check(httptest.NewRecorder(), httptest.NewRequest(m, "/", nil)); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
}

func TestHeaderToken(t *testing.T) {
	c := newCsrf()
	cookie, token := browser(t, c)
	if err := c.Check(httptest.NewRecorder(), post(cookie, withHeader(token))); err != nil {
		t.Fatal(err)
	}
}

func TestFormToken(t *testing.T) {
	c := newCsrf()
	cookie, token := browser(t, c)
	req := post(cookie, func(r *http.Request) {
		r.Body = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{FieldName: {token}}.Encode())).Body
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	if err := c.Check(httptest.NewRecorder(), req); err != nil {
		t.Fatal(err)
	}
}

func TestRejects(t *testing.T) {
	c := newCsrf()
	cookie, token := browser(t, c)
	_, attackerToken := browser(t, c)
	tampered := *cookie
	tampered.Value = cookie.Value[:len(cookie.Value)-2] + "AA"
	// The same cipher seals other things: what was sealed for another purpose is no secret.
	other, _ := cipher.Seal(c.cipher, "session", "some session data")
	foreign := *cookie
	foreign.Value = other

	cases := map[string]*http.Request{
		"no cookie":              post(nil, withHeader(token)),
		"no token":               post(cookie, nil),
		"token of another agent": post(cookie, withHeader(attackerToken)),
		"tampered token":         post(cookie, withHeader(token+"x")),
		"tampered cookie":        post(&tampered, withHeader(token)),
		"cookie of other use":    post(&foreign, withHeader(token)),
		"token in the url": post(cookie, func(r *http.Request) {
			r.URL.RawQuery = url.Values{FieldName: {token}}.Encode()
		}),
	}
	for name, req := range cases {
		if err := c.Check(httptest.NewRecorder(), req); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestOrigin(t *testing.T) {
	c := newCsrf(WithTrustedOrigins("https://app.example.org"))
	cookie, token := browser(t, c)
	cases := []struct {
		name   string
		header map[string]string
		ok     bool
	}{
		{"same origin fetch", map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"cross site fetch", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"same site fetch", map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		{"origin of the host", map[string]string{"Origin": "http://example.com"}, true},
		{"other origin", map[string]string{"Origin": "http://evil.test"}, false},
		{"null origin", map[string]string{"Origin": "null"}, false},
		{"trusted origin", map[string]string{"Origin": "https://app.example.org"}, true},
		{"referer of the host", map[string]string{"Referer": "http://example.com/page"}, true},
		{"other referer", map[string]string{"Referer": "http://evil.test/page"}, false},
		{"no headers (not a browser)", nil, true},
	}
	for _, tc := range cases {
		req := post(cookie, withHeader(token))
		for k, v := range tc.header {
			req.Header.Set(k, v)
		}
		if err := c.Check(httptest.NewRecorder(), req); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok = %v", tc.name, err, tc.ok)
		}
	}
}

func TestMaxBody(t *testing.T) {
	c := newCsrf(WithMaxBody(16))
	cookie, token := browser(t, c)
	body := url.Values{"pad": {strings.Repeat("a", 64)}, FieldName: {token}}.Encode()
	req := post(cookie, func(r *http.Request) {
		r.Body = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).Body
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	})
	if err := c.Check(httptest.NewRecorder(), req); err == nil {
		t.Fatal("a body over the limit must not be accepted")
	}
}

func TestCookieIsPerKey(t *testing.T) {
	cookie, token := browser(t, newCsrf())
	if err := newCsrf().Check(httptest.NewRecorder(), post(cookie, withHeader(token))); err == nil {
		t.Fatal("a cookie made with another key must fail")
	}
}

func TestRotate(t *testing.T) {
	c := newCsrf()
	cookie, token := browser(t, c)
	rec := httptest.NewRecorder()
	fresh, err := c.Rotate(rec)
	if err != nil || fresh == token || len(rec.Result().Cookies()) != 1 {
		t.Fatalf("Rotate must make a new secret and cookie: %v", err)
	}
	if err := c.Check(httptest.NewRecorder(), post(cookie, withHeader(token))); err != nil {
		t.Fatalf("the old pair is still a pair: %v", err)
	}
	if err := c.Check(httptest.NewRecorder(), post(rec.Result().Cookies()[0], withHeader(token))); err == nil {
		t.Fatal("the old token must not work with the new cookie")
	}
}
