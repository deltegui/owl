# Security

Packages: `owl/cipher` (encryption and sealed values), `owl/csrf` and
`owl/middleware` (CSRF and CORS). Sessions and passwords are in
[authentication](authentication.md).

## Cipher

Everything OWL stores in a cookie is sealed with a `core.Cipher`:

```go
type Cipher interface {
	Encrypt(data, aad []byte) ([]byte, error)
	Decrypt(data, aad []byte) ([]byte, error)
}
```

`aad` is *additional authenticated data*: it is not stored, but `Decrypt` only
opens what `Encrypt` sealed with the same one. OWL uses it to bind a value to its
purpose.

`cipher.AES256` implements it with AES-256-GCM:

```go
func New() (*AES256, error)                          // random key; lost on restart
func NewWithKeys(keys ...[]byte) (*AES256, error)    // 32-byte keys
func NewFromStrings(keys ...string) (*AES256, error) // keys in raw standard base64
func GenerateKey() ([]byte, error)
func GenerateKeyString() (string, error)             // what NewFromStrings reads
```

None of the constructors panic; they return an error.

### Keys and rotation

Keep the key in your configuration (generate one with `cipher.GenerateKeyString()`
once and store it as a secret). With **several keys** the first one encrypts
and every key opens (a key id byte goes in front of the data). To rotate a key,
put the new one first and keep the old one until what was sealed with it expires
(a session lasts a day by default):

```go
cy, err := cipher.NewFromStrings(newKey, oldKey)
```

### Seal and Open

```go
func Seal(c core.Cipher, purpose, data string) (string, error)
func Open(c core.Cipher, purpose, token string, maxAge time.Duration) (string, error)
var ErrExpired error
```

`Seal` encrypts `data`, adds the current time and returns base64, ready for a
cookie or a form field. `Open` reverses it and fails if:

- the value was sealed for another `purpose`,
- it was manipulated,
- it is older than `maxAge` (`0` means it never expires; the error is `ErrExpired`).

The **purpose** is any string that tells apart the uses of the key: what was
sealed for the session cookie cannot be opened as the language cookie even though
they share the key. OWL's own purposes are `session`, `language`, `csrf` and
`cookie:<name>` (for `ctx.CreateCookie`). Pick your own for your own tokens
(`"password-reset"`).

```go
token, _ := cipher.Seal(cy, "password-reset", strconv.Itoa(userID))
id, err := cipher.Open(cy, "password-reset", token, time.Hour)
```

## CSRF

```go
import (
	"github.com/deltegui/owl/v3/csrf"
	"github.com/deltegui/owl/v3/middleware"
)

r.Use(middleware.Csrf(csrf.New(cy)))
```

OWL's CSRF protection is a *signed double submit*:

1. Each browser gets a cookie `owl-csrf` (HttpOnly, SameSite=Lax, path `/`) with
   a random secret sealed by the cipher (purpose `csrf`).
2. The **token** that goes into forms, a `<meta>` or a header is that secret.
3. A request that is not safe (anything but GET, HEAD, OPTIONS and TRACE) must:
   - come from the same origin (checked with `Sec-Fetch-Site`, `Origin` and
     `Referer`), and
   - send a token equal to the secret in its own cookie, in the header
     `X-Csrf-Token` or, if there is none, in the form field `X-Csrf-Token`
     (**never** in the URL).

A token requested by an attacker belongs to the attacker's browser and does not
match the victim's cookie, and the attacker can neither read nor forge that
cookie. The token does not expire.

The middleware answers `403` to a request that fails, leaves the token in the
request context (`ctx.CsrfToken()`), and marks responses `Cache-Control: private,
no-store` and `Vary: Cookie`, since they carry the token.

### Using the token

| Where | How |
|---|---|
| A form in a view | `vd.CsrfInput()` of `hxui.ViewData`, a hidden input. |
| JavaScript or WASM | `hxui.Page`/`App`: put `<meta name="csrf-token" content="…">` (`App` does it) and send the value in `X-Csrf-Token`. `island/client.Send` does both. |
| Another client | Read the token from a GET response and send it in the header. |

The first request of a browser must be a GET (to receive the cookie): a POST
without the cookie is rejected.

### Options

```go
csrf.New(cy,
	csrf.WithSecure(false),                              // development over plain http
	csrf.WithTrustedOrigins("https://admin.example.com"), // other origins that may post
	csrf.WithMaxBody(1<<20),                             // bytes read from a body to find the field
)
```

- **`WithSecure`** is on by default. Browsers do not keep a `Secure` cookie that
  came over plain `http`, so turn it off for local development without TLS, and
  only there.
- **`WithTrustedOrigins`** takes `scheme://host[:port]` origins that are accepted
  besides the own host.
- **`WithMaxBody`** limits how much of a form body is read to look for the token
  (32 MB by default). A request with the header does not read the body.

### The Csrf type

```go
func (c *Csrf) Token(w, req) (string, error)  // token of this browser; creates the cookie if missing
func (c *Csrf) Check(w, req) error            // nil if safe, or same-origin with a matching token
func (c *Csrf) Rotate(w) (string, error)      // new secret and cookie; returns the new token
```

**Call `Rotate` after a login** (when who the browser is changes), so a secret
from before the login is no use (session fixation).

Constants: `csrf.HeaderName`, `csrf.FieldName` (`X-Csrf-Token`), `csrf.CookieName`
(`owl-csrf`), and `csrf.ContextKey`, where the middleware leaves the token.

## CORS

```go
r.Use(middleware.CorsDefault())          // allow everything
r.Use(middleware.Cors(middleware.CorsOptions{
	AllowOrigin:  "https://app.example.com",
	AllowMethods: []string{"GET", "POST"},
	AllowHeaders: []string{"Content-Type", "X-Csrf-Token"},
	MaxAge:       3600, // seconds the browser may cache the preflight
}))
```

| Field | Meaning |
|---|---|
| `AllowOrigin` | One origin, or `middleware.CorsAny` (`"*"`). Always set it. |
| `AllowMethods` | Allowed methods, or `["*"]` / empty for any. |
| `AllowHeaders` | Allowed request headers, or `["*"]` / empty for any. |
| `MaxAge` | Preflight cache time in seconds. |

`CorsDefault()` is `*` for origin, methods and headers with a 10-day max age.

How it behaves:

- For an `OPTIONS` request it answers the preflight (204) with the allow
  headers, or `403` if the requested method or headers are not allowed.
- For other requests it rejects (`403`) a method, an `Origin` or request headers
  that are not allowed, runs the handler and then sets `Access-Control-Allow-Origin`
  and `Access-Control-Allow-Credentials: true` on the response.
- **`*` together with credentials is not accepted by browsers**, so for an API
  that uses cookies, set `AllowOrigin` to the exact origin of your front end.
- `httprouter` answers `OPTIONS` itself for paths that have no `Options` route,
  so the middleware only sees a preflight where you registered an `Options`
  route for that path.
- Apply CORS **before** CSRF if browsers from another origin must reach the
  token-protected routes; a trusted origin also needs `csrf.WithTrustedOrigins`.

## Checklist for production

- A fixed key in configuration (`cipher.NewFromStrings`), not `cipher.New()`.
- HTTPS, and `Secure` cookies (the default) in `csrf` and `session`.
- `middleware.Csrf` globally, and `Rotate` after login.
- `Authorize` on private routes, `Argon2` for passwords.
- Never put user text in `hx.Raw`, `style`, `srcset` or `on*` attributes.
