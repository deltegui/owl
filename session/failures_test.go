package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/core"
)

var errBoom = errors.New("boom")

// failStore is a MemoryStore that fails the calls we ask it to.
type failStore struct {
	Store
	save, get, del, delUser error
}

func (s *failStore) Save(ctx context.Context, e Entry) error {
	if s.save != nil {
		return s.save
	}
	return s.Store.Save(ctx, e)
}

func (s *failStore) Get(ctx context.Context, k string) (Entry, error) {
	if s.get != nil {
		return Entry{}, s.get
	}
	return s.Store.Get(ctx, k)
}

func (s *failStore) Delete(ctx context.Context, k string) error {
	if s.del != nil {
		return s.del
	}
	return s.Store.Delete(ctx, k)
}

func (s *failStore) DeleteUser(ctx context.Context, id int64) error {
	if s.delUser != nil {
		return s.delUser
	}
	return s.Store.DeleteUser(ctx, id)
}

// flaky is a Cipher that cannot encrypt while fail is set.
type flaky struct {
	core.Cipher
	fail bool
}

func (f *flaky) Encrypt(data, aad []byte) ([]byte, error) {
	if f.fail {
		return nil, errBoom
	}
	return f.Cipher.Encrypt(data, aad)
}

func newFailing(t *testing.T, opts ...Option) (*Manager, *clock, *failStore, *flaky) {
	t.Helper()
	c, err := cipher.New()
	if err != nil {
		t.Fatal(err)
	}
	fc := &flaky{Cipher: c}
	fs := &failStore{Store: NewMemoryStore()}
	m := NewManager(fc, append([]Option{WithStore(fs)}, opts...)...)
	clk := &clock{t: time.Now()}
	m.now = clk.now
	return m, clk, fs, fc
}

func wantError(t *testing.T, what string, err error, contains string) {
	t.Helper()
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), contains) {
		t.Errorf("%s: want the cause wrapped with %q, got %v", what, contains, err)
	}
}

func TestStoreAndCipherFailuresAreReported(t *testing.T) {
	ctx := context.Background()

	m, _, fs, _ := newFailing(t)
	fs.save = errBoom
	rec := httptest.NewRecorder()
	wantError(t, "save on create", m.Create(ctx, rec, User{Id: 1}), "cannot save the session")
	if len(rec.Result().Cookies()) != 0 {
		t.Error("no cookie when the session could not be saved")
	}

	m, _, fs, _ = newFailing(t, WithSingleSession(true))
	fs.delUser = errBoom
	rec = httptest.NewRecorder()
	wantError(t, "close the others", m.Create(ctx, rec, User{Id: 1}), "cannot close the other sessions")
	if len(rec.Result().Cookies()) != 0 {
		t.Error("no cookie when the other sessions could not be closed")
	}

	m, _, _, fc := newFailing(t)
	fc.fail = true
	wantError(t, "seal on create", m.Create(ctx, httptest.NewRecorder(), User{Id: 1}), "cannot seal the session cookie")

	m, _, fs, _ = newFailing(t)
	fs.delUser = errBoom
	wantError(t, "revoke", m.Revoke(ctx, 1), "cannot revoke the sessions")

	m, _, fs, _ = newFailing(t)
	cookie := create(t, m, User{Id: 1})
	fs.del = errBoom
	wantError(t, "destroy", m.Destroy(httptest.NewRecorder(), request(cookie)), "cannot close the session")

	m, _, fs, _ = newFailing(t)
	cookie = create(t, m, User{Id: 1})
	fs.get = errBoom
	_, err := m.User(httptest.NewRecorder(), request(cookie))
	wantError(t, "read", err, "cannot read the session")
	if errors.Is(err, ErrNoSession) {
		t.Error("a store that fails is not 'no session': the user would be logged out by an outage")
	}
}

func TestRenewalFailuresAreReported(t *testing.T) {
	m, clk, fs, _ := newFailing(t, WithMaxAge(time.Hour))
	cookie := create(t, m, User{Id: 1})
	clk.advance(40 * time.Minute)
	fs.save = errBoom
	_, err := m.User(httptest.NewRecorder(), request(cookie))
	wantError(t, "save on renew", err, "cannot renew the session")

	m, clk, _, fc := newFailing(t, WithMaxAge(time.Hour))
	cookie = create(t, m, User{Id: 1})
	clk.advance(40 * time.Minute)
	fc.fail = true
	_, err = m.User(httptest.NewRecorder(), request(cookie))
	wantError(t, "seal on renew", err, "cannot seal the session cookie")
}

func TestNoSessionSaysWhy(t *testing.T) {
	m, _ := newManager(t)
	unknown, _ := cipher.Seal(m.cipher, cookiePurpose, "an id nobody made")
	_, err := m.User(httptest.NewRecorder(), request(nil))
	if !errors.Is(err, ErrNoSession) || !strings.Contains(err.Error(), "no cookie") {
		t.Errorf("no cookie: %v", err)
	}
	_, err = m.User(httptest.NewRecorder(), request(&http.Cookie{Name: m.cookieName, Value: "garbage"}))
	if !errors.Is(err, ErrNoSession) || strings.Contains(err.Error(), "unknown or closed") {
		t.Errorf("a cookie that does not open fails before the store is asked: %v", err)
	}
	_, err = m.User(httptest.NewRecorder(), request(&http.Cookie{Name: m.cookieName, Value: unknown}))
	if !errors.Is(err, ErrNoSession) || !strings.Contains(err.Error(), "unknown or closed") {
		t.Errorf("an id the store does not know: %v", err)
	}
	cookie := create(t, m, User{Id: 1})
	if err := m.Revoke(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	_, err = m.User(httptest.NewRecorder(), request(cookie))
	if !errors.Is(err, ErrNoSession) || !strings.Contains(err.Error(), "unknown or closed") {
		t.Errorf("a closed session: %v", err)
	}
}

func TestExpiredSessionSaysSoAndIsDeleted(t *testing.T) {
	m, clk := newManager(t, WithMaxAge(time.Hour))
	cookie := create(t, m, User{Id: 1})
	clk.advance(time.Hour) // exactly when it expires: it is not valid anymore
	_, err := m.User(httptest.NewRecorder(), request(cookie))
	if !errors.Is(err, ErrNoSession) || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("got %v", err)
	}
	if n := len(m.store.(*MemoryStore).values); n != 0 {
		t.Fatalf("the expired entry must be deleted from the store, %d left", n)
	}
}

func TestRenewsExactlyAtHalfTheTime(t *testing.T) {
	m, clk := newManager(t, WithMaxAge(time.Hour))
	cookie := create(t, m, User{Id: 1})
	clk.advance(30*time.Minute - time.Second)
	rec := httptest.NewRecorder()
	if _, err := m.User(rec, request(cookie)); err != nil || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("a second before half: no renewal (%v)", err)
	}
	clk.advance(time.Second) // exactly half
	rec = httptest.NewRecorder()
	if _, err := m.User(rec, request(cookie)); err != nil {
		t.Fatal(err)
	}
	if len(rec.Result().Cookies()) != 1 {
		t.Fatal("at half of the time it renews")
	}
}

func TestDoesNotRenewWhenTheAbsoluteLimitLeavesNothingToGain(t *testing.T) {
	m, clk := newManager(t, WithMaxAge(time.Hour), WithAbsoluteMaxAge(time.Hour))
	cookie := create(t, m, User{Id: 1})
	clk.advance(40 * time.Minute) // past half, but the limit is the same as the expiration
	rec := httptest.NewRecorder()
	if _, err := m.User(rec, request(cookie)); err != nil {
		t.Fatal(err)
	}
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("there is nothing to extend, it must not set a cookie: %+v", got)
	}
}

func TestTheCookieLivesAtLeastOneSecond(t *testing.T) {
	m, _ := newManager(t, WithMaxAge(300*time.Millisecond))
	if c := create(t, m, User{Id: 1}); c.MaxAge != 1 {
		t.Fatalf("MaxAge = %d: zero would make it a cookie of the browser session", c.MaxAge)
	}
}

func TestAnyPositiveAbsoluteLimitApplies(t *testing.T) {
	m, clk := newManager(t, WithMaxAge(time.Hour), WithAbsoluteMaxAge(time.Nanosecond))
	cookie := create(t, m, User{Id: 1})
	clk.advance(time.Second)
	if _, err := m.User(httptest.NewRecorder(), request(cookie)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("the limit counts since it was made: got %v", err)
	}
}
