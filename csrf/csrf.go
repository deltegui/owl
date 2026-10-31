// Package csrf protects against cross-site request forgery with a signed
// double-submit cookie.
//
// Each browser gets a cookie (HttpOnly, SameSite=Lax) with a random secret
// sealed by the application's cipher. The token that goes into forms and
// requests is the secret itself. A request that is not safe (anything but GET,
// HEAD, OPTIONS and TRACE) must send a token equal to the secret in its own
// cookie and come from the same origin. A token taken from another browser
// (an attacker asking for the page) does not match the victim's cookie, and
// the attacker can neither read nor forge that cookie.
package csrf

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/core"
)

const (
	// HeaderName is the request header that carries the token.
	HeaderName = "X-Csrf-Token"
	// FieldName is the form field that carries the token when there is no header.
	FieldName = "X-Csrf-Token"
	// CookieName is the cookie that holds the sealed secret.
	CookieName = "owl-csrf"
)

type contextKey struct{}

// ContextKey is where the middleware leaves the token in the request context.
var ContextKey = contextKey{}

// purpose is what the cookie is sealed for (see cipher.Seal), so a value made by
// the same cipher for something else (a session cookie) is not a secret.
const purpose = "csrf"

const (
	secretBytes    = core.Size32
	defaultMaxBody = 32 << 20
)

// Csrf creates and checks tokens.
type Csrf struct {
	cipher  core.Cipher
	secure  bool
	trusted []string
	maxBody int64
}

// Option changes how Csrf works.
type Option func(*Csrf)

// WithSecure sets the Secure attribute of the cookie. It is on by default, and
// browsers do not keep a Secure cookie sent over plain http, so turn it off
// for local development without TLS.
func WithSecure(secure bool) Option { return func(c *Csrf) { c.secure = secure } }

// WithTrustedOrigins accepts requests whose Origin is one of these, written as
// scheme://host[:port], besides the ones to the own host.
func WithTrustedOrigins(origins ...string) Option {
	return func(c *Csrf) { c.trusted = append(c.trusted, origins...) }
}

// WithMaxBody limits the bytes read from a body to look for the token in a
// form (32 MB by default). A request with a header does not read the body.
func WithMaxBody(n int64) Option { return func(c *Csrf) { c.maxBody = n } }

// New creates a Csrf that seals the cookie with sealer.
func New(sealer core.Cipher, opts ...Option) *Csrf {
	c := &Csrf{cipher: sealer, secure: true, maxBody: defaultMaxBody}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Token returns the token of the browser that makes req. If the request has no
// valid cookie, it makes a secret and sets the cookie in w.
func (c *Csrf) Token(w http.ResponseWriter, req *http.Request) (string, error) {
	if secret, err := c.secret(req); err == nil {
		return secret, nil
	}
	return c.Rotate(w)
}

// Rotate makes a new secret, sets its cookie in w and returns its token. Call it when
// who the browser is changes (after a login), so a secret from before is no use.
func (c *Csrf) Rotate(w http.ResponseWriter) (string, error) {
	secret := core.GenerateToken(secretBytes)
	value, err := cipher.Seal(c.cipher, purpose, secret)
	if err != nil {
		return "", fmt.Errorf("cannot seal csrf cookie: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return secret, nil
}

func (c *Csrf) secret(req *http.Request) (string, error) {
	cookie, err := req.Cookie(CookieName)
	if err != nil {
		return "", errors.New("csrf cookie not found")
	}
	secret, err := cipher.Open(c.cipher, purpose, cookie.Value, 0)
	if err != nil || secret == "" {
		return "", fmt.Errorf("invalid csrf cookie: %w", err)
	}
	return secret, nil
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// Check returns nil if req is safe or comes from the same origin with a token
// that matches its cookie. Otherwise the error says why not. The token is read
// from the header HeaderName and, if there is none, from the form field
// FieldName (never from the URL); w is used to limit the size of that body.
func (c *Csrf) Check(w http.ResponseWriter, req *http.Request) error {
	if safeMethod(req.Method) {
		return nil
	}
	if err := c.checkOrigin(req); err != nil {
		return err
	}
	secret, err := c.secret(req)
	if err != nil {
		return err
	}
	token := req.Header.Get(HeaderName)
	if token == "" {
		req.Body = http.MaxBytesReader(w, req.Body, c.maxBody)
		token = req.PostFormValue(FieldName)
	}
	if token == "" {
		return fmt.Errorf("csrf token (%s) not found", HeaderName)
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		return errors.New("csrf token does not match")
	}
	return nil
}

func (c *Csrf) checkOrigin(req *http.Request) error {
	site := req.Header.Get("Sec-Fetch-Site")
	if site == "same-origin" || site == "none" {
		return nil
	}
	origin := req.Header.Get("Origin")
	if origin == "" {
		if referer := req.Header.Get("Referer"); referer != "" {
			u, err := url.Parse(referer)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return errors.New("invalid referer")
			}
			origin = u.Scheme + "://" + u.Host
		}
	}
	if origin == "" {
		if site != "" {
			return fmt.Errorf("cross-origin request (Sec-Fetch-Site: %s)", site)
		}
		return nil // not a browser: the token and the cookie still have to match
	}
	u, err := url.Parse(origin)
	if err == nil && u.Host == req.Host {
		return nil
	}
	if slices.Contains(c.trusted, origin) {
		return nil
	}
	return fmt.Errorf("origin %q is not allowed", origin)
}
