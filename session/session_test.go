package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/core"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newManager(t *testing.T, opts ...Option) (*Manager, *clock) {
	t.Helper()
	c, err := cipher.New()
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(c, opts...)
	clk := &clock{t: time.Now()}
	m.now = clk.now
	return m, clk
}

func create(t *testing.T, m *Manager, user User) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := m.Create(context.Background(), rec, user); err != nil {
		t.Fatal(err)
	}
	return cookieOf(t, rec, m.cookieName)
}

func cookieOf(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no cookie %q", name)
	return nil
}

func request(cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func TestCreateAndRead(t *testing.T) {
	m, _ := newManager(t)
	cookie := create(t, m, User{Id: 7, Roles: []core.Role{core.RoleAdmin}, Claims: map[Claim]string{"Name": "ana"}})
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 {
		t.Fatalf("bad cookie: %+v", cookie)
	}
	user, err := m.User(httptest.NewRecorder(), request(cookie))
	if err != nil || user.Id != 7 || !user.HasRole(core.RoleAdmin) || user.Claims["Name"] != "ana" {
		t.Fatalf("got %+v, %v", user, err)
	}
}

func TestOptions(t *testing.T) {
	m, _ := newManager(t, WithSecure(false), WithCookieName("sid"), WithMaxAge(time.Hour))
	cookie := create(t, m, User{Id: 1})
	if cookie.Name != "sid" || cookie.Secure || cookie.MaxAge != 3600 {
		t.Fatalf("bad cookie: %+v", cookie)
	}
}

func TestTheStoreNeverSeesTheId(t *testing.T) {
	store := NewMemoryStore()
	m, _ := newManager(t, WithStore(store))
	cookie := create(t, m, User{Id: 1})
	id, err := cipher.Open(m.cipher, cookiePurpose, cookie.Value, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.values[id]; ok {
		t.Fatal("the store is keyed by the id itself")
	}
	if _, ok := store.values[key(id)]; !ok {
		t.Fatal("the store must be keyed by the hash of the id")
	}
}

func TestNoSession(t *testing.T) {
	m, _ := newManager(t)
	// Something the application sealed for another purpose, with the same key.
	other, _ := cipher.Seal(m.cipher, "cookie:theme", "anything")
	unknown, _ := cipher.Seal(m.cipher, cookiePurpose, "an id nobody made")
	cases := map[string]*http.Cookie{
		"no cookie":         nil,
		"garbage":           {Name: m.cookieName, Value: "garbage"},
		"other purpose":     {Name: m.cookieName, Value: other},
		"unknown id":        {Name: m.cookieName, Value: unknown},
		"another manager's": create(t, mustManager(t), User{Id: 1}),
	}
	for name, cookie := range cases {
		if _, err := m.User(httptest.NewRecorder(), request(cookie)); !errors.Is(err, ErrNoSession) {
			t.Errorf("%s: want ErrNoSession, got %v", name, err)
		}
	}
}

func mustManager(t *testing.T) *Manager {
	m, _ := newManager(t)
	return m
}

func TestExpires(t *testing.T) {
	m, clk := newManager(t, WithMaxAge(time.Hour))
	cookie := create(t, m, User{Id: 1})
	clk.advance(61 * time.Minute)
	if _, err := m.User(httptest.NewRecorder(), request(cookie)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession, got %v", err)
	}
	clk.advance(-61 * time.Minute) // even if time went back, the expired one is gone
	if _, err := m.User(httptest.NewRecorder(), request(cookie)); !errors.Is(err, ErrNoSession) {
		t.Fatal("an expired session must be deleted")
	}
}

func TestSlidingRenewal(t *testing.T) {
	m, clk := newManager(t, WithMaxAge(time.Hour))
	cookie := create(t, m, User{Id: 1})

	clk.advance(20 * time.Minute) // less than half: nothing to renew
	rec := httptest.NewRecorder()
	if _, err := m.User(rec, request(cookie)); err != nil {
		t.Fatal(err)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("it must not renew before half of the time")
	}

	clk.advance(20 * time.Minute) // 40 minutes: past half
	rec = httptest.NewRecorder()
	if _, err := m.User(rec, request(cookie)); err != nil {
		t.Fatal(err)
	}
	renewed := cookieOf(t, rec, m.cookieName)
	if renewed.MaxAge != 3600 {
		t.Fatalf("the cookie must be renewed for the whole time, MaxAge = %d", renewed.MaxAge)
	}

	clk.advance(50 * time.Minute) // 90 minutes from the start: alive only because it was renewed
	if _, err := m.User(httptest.NewRecorder(), request(renewed)); err != nil {
		t.Fatalf("a renewed session must last: %v", err)
	}
}

func TestAbsoluteMaxAge(t *testing.T) {
	m, clk := newManager(t, WithMaxAge(time.Hour), WithAbsoluteMaxAge(90*time.Minute))
	cookie := create(t, m, User{Id: 1})
	clk.advance(50 * time.Minute)
	rec := httptest.NewRecorder()
	if _, err := m.User(rec, request(cookie)); err != nil {
		t.Fatal(err)
	}
	renewed := cookieOf(t, rec, m.cookieName)
	if renewed.MaxAge != 40*60 {
		t.Fatalf("it can be renewed only up to the limit, MaxAge = %d", renewed.MaxAge)
	}
	clk.advance(41 * time.Minute)
	if _, err := m.User(httptest.NewRecorder(), request(renewed)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("past the absolute limit it must close, got %v", err)
	}
}

func TestDestroy(t *testing.T) {
	m, _ := newManager(t)
	cookie := create(t, m, User{Id: 1})
	rec := httptest.NewRecorder()
	if err := m.Destroy(rec, request(cookie)); err != nil {
		t.Fatal(err)
	}
	if cleared := cookieOf(t, rec, m.cookieName); cleared.MaxAge != -1 {
		t.Fatalf("the cookie must be removed (MaxAge -1), got %+v", cleared)
	}
	if _, err := m.User(httptest.NewRecorder(), request(cookie)); !errors.Is(err, ErrNoSession) {
		t.Fatal("the session must be closed in the server too")
	}
	for name, c := range map[string]*http.Cookie{"garbage": {Name: m.cookieName, Value: "garbage"}, "none": nil} {
		rec := httptest.NewRecorder()
		if err := m.Destroy(rec, request(c)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cookieOf(t, rec, m.cookieName).MaxAge != -1 {
			t.Fatalf("%s: the cookie must be removed anyway", name)
		}
	}
}

func TestSeveralSessionsAndRevoke(t *testing.T) {
	m, _ := newManager(t)
	phone, laptop, other := create(t, m, User{Id: 1}), create(t, m, User{Id: 1}), create(t, m, User{Id: 2})
	for name, c := range map[string]*http.Cookie{"phone": phone, "laptop": laptop} {
		if _, err := m.User(httptest.NewRecorder(), request(c)); err != nil {
			t.Fatalf("%s must stay open: %v", name, err)
		}
	}
	if err := m.Revoke(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*http.Cookie{"phone": phone, "laptop": laptop} {
		if _, err := m.User(httptest.NewRecorder(), request(c)); !errors.Is(err, ErrNoSession) {
			t.Fatalf("%s must be closed", name)
		}
	}
	if _, err := m.User(httptest.NewRecorder(), request(other)); err != nil {
		t.Fatalf("another user is not touched: %v", err)
	}
}

func TestSingleSession(t *testing.T) {
	m, _ := newManager(t, WithSingleSession(true))
	first := create(t, m, User{Id: 1})
	other := create(t, m, User{Id: 2})
	second := create(t, m, User{Id: 1})
	if _, err := m.User(httptest.NewRecorder(), request(first)); !errors.Is(err, ErrNoSession) {
		t.Fatal("the first session must be closed by the second")
	}
	for name, c := range map[string]*http.Cookie{"second": second, "other user": other} {
		if _, err := m.User(httptest.NewRecorder(), request(c)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestTheUserIsACopy(t *testing.T) {
	m, _ := newManager(t)
	roles := []core.Role{core.RoleAdmin}
	claims := map[Claim]string{"Name": "ana"}
	cookie := create(t, m, User{Id: 1, Roles: roles, Claims: claims})
	roles[0], claims["Name"] = 0, "changed" // the caller changes what it gave

	user, _ := m.User(httptest.NewRecorder(), request(cookie))
	if !user.HasRole(core.RoleAdmin) || user.Claims["Name"] != "ana" {
		t.Fatalf("the session must keep its own copy: %+v", user)
	}
	user.Roles[0], user.Claims["Name"] = 0, "changed" // and a handler changes what it got
	again, _ := m.User(httptest.NewRecorder(), request(cookie))
	if !again.HasRole(core.RoleAdmin) || again.Claims["Name"] != "ana" {
		t.Fatalf("a handler must not change the stored session: %+v", again)
	}
}

func TestMemoryStoreSweepsExpired(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.Save(ctx, Entry{Key: "old", Expires: time.Now().Add(-time.Hour)})
	s.lastSweep = time.Now().Add(-2 * sweepEvery)
	_ = s.Save(ctx, Entry{Key: "new", Expires: time.Now().Add(time.Hour)})
	if _, err := s.Get(ctx, "old"); !errors.Is(err, ErrNotFound) {
		t.Fatal("the expired entry must be swept")
	}
	if _, err := s.Get(ctx, "new"); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentUse(t *testing.T) {
	m, _ := newManager(t, WithSingleSession(true))
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			if err := m.Create(context.Background(), rec, User{Id: int64(i % 4)}); err != nil {
				t.Error(err)
				return
			}
			for _, c := range rec.Result().Cookies() {
				_, _ = m.User(httptest.NewRecorder(), request(c))
			}
			_ = m.Revoke(context.Background(), int64(i%4))
		}()
	}
	wg.Wait()
}
