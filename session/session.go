// Package session keeps who is logged in. The browser holds only a cookie with a
// random id (sealed, see package cipher); the user lives in a Store on the server.
package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/core"
)

// Claim is the name of a piece of data kept with the user (their name, an image...).
type Claim string

// User is who is logged in. It is a copy taken when the session was made: a change in
// the roles or the claims shows up in the next session, or after Revoke.
type User struct {
	Id     int64
	Roles  []core.Role
	Claims map[Claim]string
}

func (u User) clone() User {
	u.Roles = slices.Clone(u.Roles)
	u.Claims = maps.Clone(u.Claims)
	return u
}

// HasRole tells if the user has the role.
func (u User) HasRole(role core.Role) bool { return slices.Contains(u.Roles, role) }

// ErrNoSession is what the Manager returns when the request has no valid session: no
// cookie, a cookie that does not open, or a session that expired or was closed.
var ErrNoSession = errors.New("no valid session")

const (
	cookiePurpose     = "session"
	defaultCookieName = "owl_session"
	defaultMaxAge     = core.OneDayDuration
)

// Manager makes, reads and closes sessions.
type Manager struct {
	store         Store
	cipher        core.Cipher
	cookieName    string
	maxAge        time.Duration
	absoluteMax   time.Duration
	secure        bool
	singleSession bool
	now           func() time.Time // a test can change it
}

// Option changes how a Manager works.
type Option func(*Manager)

// WithStore keeps the sessions in store (in memory by default, so they do not survive a restart).
func WithStore(store Store) Option { return func(m *Manager) { m.store = store } }

// WithMaxAge sets how long a session lasts without being used (a day by default). Using it
// renews it, in the store and in the cookie.
func WithMaxAge(d time.Duration) Option { return func(m *Manager) { m.maxAge = d } }

// WithAbsoluteMaxAge sets how long a session lasts since it was made, however much it is
// used. Zero (the default) means no limit.
func WithAbsoluteMaxAge(d time.Duration) Option { return func(m *Manager) { m.absoluteMax = d } }

// WithSecure sets the Secure attribute of the cookie, on by default. A browser does
// not keep a Secure cookie that came over plain http, so turn it off for local development.
func WithSecure(secure bool) Option { return func(m *Manager) { m.secure = secure } }

// WithCookieName sets the name of the cookie ("owl_session" by default).
func WithCookieName(name string) Option { return func(m *Manager) { m.cookieName = name } }

// WithSingleSession makes a new session of a user close their other sessions: a login in
// the phone logs out the laptop. Off by default; see also Revoke.
func WithSingleSession(single bool) Option { return func(m *Manager) { m.singleSession = single } }

// NewManager creates a Manager that seals the cookie with c.
func NewManager(c core.Cipher, opts ...Option) *Manager {
	m := &Manager{
		cipher:     c,
		store:      NewMemoryStore(),
		cookieName: defaultCookieName,
		maxAge:     defaultMaxAge,
		secure:     true,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// key is what the store knows a session by: a hash of the id, so whoever reads the
// store cannot use its sessions.
func key(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) limit(created time.Time) time.Time {
	expires := m.now().Add(m.maxAge)
	if m.absoluteMax > 0 {
		if last := created.Add(m.absoluteMax); last.Before(expires) {
			return last
		}
	}
	return expires
}

// Create makes a session for user and sets its cookie in w.
func (m *Manager) Create(ctx context.Context, w http.ResponseWriter, user User) error {
	if m.singleSession {
		if err := m.store.DeleteUser(ctx, user.Id); err != nil {
			return fmt.Errorf("cannot close the other sessions: %w", err)
		}
	}
	id := core.GenerateTokenDefaultLength()
	now := m.now()
	entry := Entry{Key: key(id), User: user.clone(), Created: now, Expires: m.limit(now)}
	if err := m.store.Save(ctx, entry); err != nil {
		return fmt.Errorf("cannot save the session: %w", err)
	}
	return m.setCookie(w, id, entry.Expires)
}

func (m *Manager) setCookie(w http.ResponseWriter, id string, expires time.Time) error {
	sealed, err := cipher.Seal(m.cipher, cookiePurpose, id)
	if err != nil {
		return fmt.Errorf("cannot seal the session cookie: %w", err)
	}
	maxAge := max(int(expires.Sub(m.now()).Seconds()), 1) // zero would make it a cookie of the browser session
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    sealed,
		Expires:  expires,
		MaxAge:   maxAge,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
		Secure:   m.secure,
	})
	return nil
}

func (m *Manager) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Path:     "/",
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
		Secure:   m.secure,
	})
}

func (m *Manager) readId(req *http.Request) (string, error) {
	cookie, err := req.Cookie(m.cookieName)
	if err != nil {
		return "", fmt.Errorf("%w: no cookie", ErrNoSession)
	}
	id, err := cipher.Open(m.cipher, cookiePurpose, cookie.Value, 0)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoSession, err)
	}
	return id, nil
}

// User is who is logged in in req, or an error that wraps ErrNoSession. A session that
// is used renews itself: when half of its time has gone, its expiration and its cookie
// (set in w) are extended.
func (m *Manager) User(w http.ResponseWriter, req *http.Request) (User, error) {
	id, err := m.readId(req)
	if err != nil {
		return User{}, err
	}
	ctx := req.Context()
	entry, err := m.store.Get(ctx, key(id))
	if errors.Is(err, ErrNotFound) {
		return User{}, fmt.Errorf("%w: unknown or closed", ErrNoSession)
	}
	if err != nil {
		return User{}, fmt.Errorf("cannot read the session: %w", err)
	}
	now := m.now()
	if !now.Before(entry.Expires) {
		_ = m.store.Delete(ctx, entry.Key)
		return User{}, fmt.Errorf("%w: expired", ErrNoSession)
	}
	// Half of the time since the last renewal has gone, and the limit lets it go further.
	if renewed := m.limit(entry.Created); renewed.After(entry.Expires) && now.Sub(entry.Expires.Add(-m.maxAge)) >= m.maxAge/2 {
		entry.Expires = renewed
		if err := m.store.Save(ctx, entry); err != nil {
			return User{}, fmt.Errorf("cannot renew the session: %w", err)
		}
		if err := m.setCookie(w, id, renewed); err != nil {
			return User{}, err
		}
	}
	return entry.User.clone(), nil
}

// Destroy closes the session of req and removes its cookie. The cookie is removed
// even if it is not valid, so a broken one does not stay in the browser.
func (m *Manager) Destroy(w http.ResponseWriter, req *http.Request) error {
	m.clearCookie(w)
	id, err := m.readId(req)
	if err != nil {
		return nil // nothing to close in the server
	}
	if err := m.store.Delete(req.Context(), key(id)); err != nil {
		return fmt.Errorf("cannot close the session: %w", err)
	}
	return nil
}

// Revoke closes every session of the user, in every browser: for "log out everywhere"
// or after a password change.
func (m *Manager) Revoke(ctx context.Context, userId int64) error {
	if err := m.store.DeleteUser(ctx, userId); err != nil {
		return fmt.Errorf("cannot revoke the sessions: %w", err)
	}
	return nil
}
