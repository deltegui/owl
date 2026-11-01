# Authentication

Packages: `owl/session` (who is logged in), `owl/middleware` (protecting routes)
and `owl/hash` (passwords). Users, registration and the login form are yours;
OWL gives you the mechanisms.

## How sessions work

The browser only holds a cookie, `owl_session`, with a **random id**. The cookie
is sealed with the cipher (purpose `session`), HttpOnly, `SameSite=Lax` and
`Secure` by default. The user lives in a `Store` on the server under the **hash
of the id**, never the id itself, so a leaked store does not give sessions away.

A session lasts a day *without use*; using it renews it (in the store and the
cookie) once half of that time has passed.

## Manager

```go
func NewManager(c core.Cipher, opts ...Option) *Manager

func (m *Manager) Create(ctx context.Context, w http.ResponseWriter, user User) error
func (m *Manager) User(w http.ResponseWriter, req *http.Request) (User, error)
func (m *Manager) Destroy(w http.ResponseWriter, req *http.Request) error
func (m *Manager) Revoke(ctx context.Context, userId int64) error
```

| Method | Does |
|---|---|
| `Create` | Makes a session for the user and sets the cookie in `w`. |
| `User` | The user of the request, or an error wrapping `session.ErrNoSession` (no cookie, a cookie that does not open, an expired or closed session). Renews a session in use. |
| `Destroy` | Closes the session and removes the cookie, **even if the cookie is not valid**, so a broken one does not stay in the browser. Use it for logout. |
| `Revoke(userId)` | Closes every session of the user, in every browser: "log out everywhere", or after a password change. |

Options:

| Option | Meaning |
|---|---|
| `WithStore(store)` | Where sessions are kept. Default: in memory (lost on restart). |
| `WithMaxAge(d)` | Time without use before it expires. Default a day. |
| `WithAbsoluteMaxAge(d)` | Maximum life since it was made, however much it is used. Default: none. |
| `WithSecure(bool)` | `Secure` attribute. On by default; browsers drop a `Secure` cookie that came over plain http, so turn it off **only** for local development. |
| `WithCookieName(name)` | Cookie name. Default `owl_session`. |
| `WithSingleSession(bool)` | A new session of a user closes their other ones. Off by default. |

Create **one** `Manager` for the program. With `Muxi`, a builder that creates one
runs on every request for the type and would give each handler its own store;
return a shared instance instead (see [routing](routing.md#dependency-injection)).

### User

```go
type User struct {
	Id     int64
	Roles  []core.Role
	Claims map[Claim]string // data kept with the user: name, avatar...
}
func (u User) HasRole(role core.Role) bool
func FromContext(ctx context.Context) (User, bool)
```

The user is a **copy taken when the session was created**, and a copy is made on
every read: a change in roles or claims shows up in the next session, or after
`Revoke`. `core.RoleAdmin` (1) and `core.RoleUser` (2) exist; define more roles as
`core.Role` values.

### Store

Implement `session.Store` to keep sessions in a database or Redis:

```go
type Store interface {
	Save(ctx context.Context, entry Entry) error          // create or replace by Key
	Get(ctx context.Context, key string) (Entry, error)   // return session.ErrNotFound if missing
	Delete(ctx context.Context, key string) error         // not an error if missing
	DeleteUser(ctx context.Context, userId int64) error   // every entry of the user
}

type Entry struct {
	Key     string // hash of the session id
	User    User
	Created time.Time
	Expires time.Time
}
```

The `Manager` checks the expiration itself, so a store may keep an expired entry
for a while and may drop it whenever it wants. `session.NewMemoryStore()` is the
default and is safe for concurrent use.

## Protecting routes

```go
import "github.com/deltegui/owl/v3/middleware"

r.Get("/panel", showPanel, middleware.Authorize(manager, "/login"))
r.Get("/admin", showAdmin, middleware.Admin(manager, "/login"))
r.Get("/reports", showReports, middleware.AuthorizeRoles(manager, "/login", RoleAccountant, core.RoleAdmin))
```

| Middleware | Behavior |
|---|---|
| `Authorize(m, loginURL)` | Lets through a request with a session and leaves the user in the context. Without one, a **browser** (the request accepts `text/html`) is redirected to `loginURL` with `303`; anything else gets `401`. If `loginURL` is empty, everyone gets `401`. |
| `AuthorizeRoles(m, loginURL, roles...)` | The same, and the user must have **one of** the roles or the answer is `403` (they are logged in, so the login would not help). No roles means any user. |
| `Admin(m, loginURL)` | `AuthorizeRoles` with `core.RoleAdmin`. |

In a handler:

```go
user, ok := ctx.User()        // session.User, false if there is none
ctx.HaveSession()
user, ok := session.FromContext(ctx.Context())
```

`session.ContextKey` is where the middleware leaves the user.

## A login and a logout

```go
func handleLogin(users userRepository, hasher core.Hasher, sessions *session.Manager, cs *csrf.Csrf) owl.Handler {
	return func(ctx owl.Ctx) error {
		var form loginForm
		ctx.ParseForm(&form)
		ctx.Validate(form)
		if !ctx.ModelState.Valid {
			ctx.Status(http.StatusBadRequest)
			return ctx.View(loginView(form))
		}

		user, found := users.ByName(form.Name)
		if !found {
			hasher.Mimic(form.Password) // spend the time of a real check, so the answer does not tell
			return invalidLogin(ctx, form)
		}
		ok, err := hasher.Check(user.PasswordHash, form.Password)
		if err != nil {
			return err // the stored hash is unreadable
		}
		if !ok {
			return invalidLogin(ctx, form)
		}
		if hasher.NeedsRehash(user.PasswordHash) {
			if fresh, err := hasher.Hash(form.Password); err == nil {
				users.SetPasswordHash(user.ID, fresh)
			}
		}

		err = sessions.Create(ctx.Context(), ctx.Res, session.User{
			Id:     user.ID,
			Roles:  user.Roles,
			Claims: map[session.Claim]string{"Name": user.Name},
		})
		if err != nil {
			return err
		}
		if _, err := cs.Rotate(ctx.Res); err != nil { // the browser is somebody else now
			return err
		}
		return ctx.RedirectCode("/panel", http.StatusSeeOther)
	}
}

func handleLogout(sessions *session.Manager) owl.Handler {
	return func(ctx owl.Ctx) error {
		if err := sessions.Destroy(ctx.Res, ctx.Req); err != nil {
			return err
		}
		return ctx.RedirectCode("/login", http.StatusSeeOther)
	}
}
```

Good habits shown there: the same answer for "no such user" and "wrong
password"; `Mimic` so timing does not tell either; `Rotate` after a login;
logout as a POST (so CSRF applies).

## Passwords: package hash

```go
type Hasher interface {                    // core.Hasher
	Hash(password string) (string, error)
	Check(encoded, password string) (bool, error)
	NeedsRehash(encoded string) bool
	Mimic(password string)
}
```

| Method | Meaning |
|---|---|
| `Hash` | Hash with a new random salt. The result holds the algorithm, parameters and salt, so it can be checked later even if parameters change. |
| `Check` | `(true, nil)` if the password made the hash, `(false, nil)` for a wrong password. An error means the stored value is not a hash this hasher can read (`hash.ErrInvalidHash`). |
| `NeedsRehash` | The hash was made with other parameters than this hasher's (or cannot be read): hash again after a successful login. |
| `Mimic` | Does the work of a `Check` with no hash, for when the user does not exist. |

### Argon2 (use this one)

```go
hasher := hash.NewArgon2()           // 64 MiB, 3 passes, 2 threads, 16-byte salt, 32-byte key
hasher = hash.Argon2{Memory: 128 * 1024, Time: 4, Threads: 4} // a zero field takes its default
```

Argon2id in the standard text form `$argon2id$v=19$m=65536,t=3,p=2$salt$key`.
Every hash takes `Memory` of RAM while it runs: if many logins can happen at once
**limit them** (a semaphore, a rate limit), or a flood of requests uses up the
memory.

### Bcrypt

```go
hasher := hash.NewBcrypt()         // cost 12
hasher = hash.Bcrypt{Cost: 14}     // out of range is an error
```

bcrypt only reads **72 bytes**: `Hash` returns `hash.ErrPasswordTooLong` for more
and `Check` rejects more, instead of silently truncating. Prefer Argon2 if long
passphrases are allowed.

Defaults: `hash.DefaultArgon2Memory`, `DefaultArgon2Time`, `DefaultArgon2Threads`,
`DefaultBcryptCost`.

## Other pieces

- `core.GenerateToken(bytes)`, `GenerateTokenDefaultLength()` and
  `GenerateRandomPassword(length)` give random URL-safe tokens for password-reset
  links and such. See [utilities](utilities.md#core).
- For signed, expiring tokens bound to a purpose, use `cipher.Seal` and
  `cipher.Open`. See [security](security.md#seal-and-open).
