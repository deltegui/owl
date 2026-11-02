package outbox

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/logx"
	"github.com/deltegui/owl/v3/mail"
)

// logged keeps the errors the worker logs.
type logged struct {
	quiet
	mu   sync.Mutex
	msgs []string
	args map[string][]any
}

func newLogged() *logged { return &logged{args: map[string][]any{}} }

func (l *logged) ErrorContext(_ context.Context, msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, msg)
	l.args[msg] = args
}

func (l *logged) WithModuleName(string) logx.Logger { return l }

func (l *logged) has(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.msgs {
		if strings.Contains(m, msg) {
			return true
		}
	}
	return false
}

func (l *logged) field(msg, key string) any {
	l.mu.Lock()
	defer l.mu.Unlock()
	for m, args := range l.args {
		if !strings.Contains(m, msg) {
			continue
		}
		for i := 0; i+1 < len(args); i += 2 {
			if args[i] == key {
				return args[i+1]
			}
		}
	}
	return nil
}

// brokenStore fails the calls it is told to.
type brokenStore struct {
	Store
	reset, claim, sent, failed error
	markCtx                    context.Context
}

func (s *brokenStore) ResetStale(ctx context.Context, before time.Time) error {
	if s.reset != nil {
		return s.reset
	}
	return s.Store.ResetStale(ctx, before)
}

func (s *brokenStore) Claim(ctx context.Context, w string, n int, now time.Time) ([]Item, error) {
	if s.claim != nil {
		return nil, s.claim
	}
	return s.Store.Claim(ctx, w, n, now)
}

func (s *brokenStore) MarkSent(ctx context.Context, id int64) error {
	s.markCtx = ctx
	if s.sent != nil {
		return s.sent
	}
	return s.Store.MarkSent(ctx, id)
}

func (s *brokenStore) MarkFailed(ctx context.Context, id int64, cause string, next *time.Time) error {
	s.markCtx = ctx
	if s.failed != nil {
		return s.failed
	}
	return s.Store.MarkFailed(ctx, id, cause, next)
}

func workerWith(t *testing.T, store Store, sender mail.Sender, log logx.Logger) *Worker {
	t.Helper()
	w, err := NewWorker(Options{Store: store, Sender: sender, Payloads: payloads{}, Now: time.Now, Logger: log, ID: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestNewWorkerNeedsStoreSenderAndPayloads(t *testing.T) {
	store, sender := NewMemoryStore(time.Now), &sink{}
	for name, o := range map[string]Options{
		"nothing":      {},
		"no store":     {Sender: sender, Payloads: payloads{}},
		"no sender":    {Store: store, Payloads: payloads{}},
		"no payloads":  {Store: store, Sender: sender},
		"only a store": {Store: store},
	} {
		if _, err := NewWorker(o); err == nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("%s: %v", name, err)
		}
	}
	w, err := NewWorker(Options{Store: store, Sender: sender, Payloads: payloads{}})
	if err != nil {
		t.Fatal(err)
	}
	if w.log == nil || w.o.Now == nil || w.o.Backoff == nil || w.o.Retry == nil || w.o.ID == "" {
		t.Fatalf("the defaults were not set: %+v", w.o)
	}
	if w.o.PollInterval != 30*time.Second || w.o.BatchSize != 50 || w.o.StaleAfter != 10*time.Minute || w.o.MaxAttempts != 5 {
		t.Fatalf("defaults changed: %+v", w.o)
	}
}

func TestWakeNeverQueuesMoreThanOne(t *testing.T) {
	w := workerWith(t, NewMemoryStore(time.Now), &sink{}, quiet{})
	w.Wake()
	w.Wake()
	w.Wake()
	if n := len(w.wake); n != 1 {
		t.Fatalf("%d wakes queued: one look at the store is enough", n)
	}
}

func TestRunOnceSaysWhenItCannotClaim(t *testing.T) {
	boom := errors.New("db down")
	store := &brokenStore{Store: NewMemoryStore(time.Now), claim: boom}
	w := workerWith(t, store, &sink{}, quiet{})
	n, err := w.RunOnce(context.Background())
	if n != 0 || !errors.Is(err, boom) || !strings.Contains(err.Error(), "outbox: claiming") {
		t.Fatalf("got %d, %v", n, err)
	}
}

func TestRunLogsWhatItCannotProcessAndGoesOn(t *testing.T) {
	log := newLogged()
	store := &brokenStore{Store: NewMemoryStore(time.Now), claim: errors.New("db down")}
	w := workerWith(t, store, &sink{}, log)
	w.o.PollInterval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	eventually(t, "the failure being logged", func() bool { return log.has("cannot process the emails") })
	cancel()
	<-done
}

func TestAStaleResetThatFailsIsLoggedAndTheWorkGoesOn(t *testing.T) {
	log := newLogged()
	mem := NewMemoryStore(time.Now)
	store := &brokenStore{Store: mem, reset: errors.New("db hiccup")}
	sender := &sink{}
	if err := NewSender(mem, payloads{}).Send(context.Background(), message("Ana")); err != nil {
		t.Fatal(err)
	}
	w := workerWith(t, store, sender, log)
	n, err := w.RunOnce(context.Background())
	if err != nil || n != 1 || sender.count() != 1 {
		t.Fatalf("got %d, %v, sent %d", n, err, sender.count())
	}
	if !log.has("cannot reset the stale emails") {
		t.Fatal("the failure was not logged")
	}
}

func TestAnEmailThatWasSentButCannotBeMarkedIsLogged(t *testing.T) {
	log := newLogged()
	mem := NewMemoryStore(time.Now)
	store := &brokenStore{Store: mem, sent: errors.New("db down")}
	if err := NewSender(mem, payloads{}).Send(context.Background(), message("Ana")); err != nil {
		t.Fatal(err)
	}
	w := workerWith(t, store, &sink{}, log)
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !log.has("sent but cannot be marked") || log.field("sent but cannot be marked", "id") != int64(1) {
		t.Fatalf("logged: %v", log.msgs)
	}
}

func TestAFailureIsLoggedWithWhatTheWorkerDecided(t *testing.T) {
	log := newLogged()
	mem := NewMemoryStore(time.Now)
	store := &brokenStore{Store: mem, failed: errors.New("db down")}
	if err := NewSender(mem, payloads{}).Send(context.Background(), message("Ana")); err != nil {
		t.Fatal(err)
	}
	w := workerWith(t, store, &sink{errs: []error{errors.New("timeout")}}, log)
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !log.has("cannot send the email") {
		t.Fatalf("logged: %v", log.msgs)
	}
	if log.field("cannot send the email", "attempts") != 1 || log.field("cannot send the email", "will_retry") != true {
		t.Fatalf("fields: %v", log.args)
	}
	if !log.has("cannot mark the email as failed") {
		t.Fatal("the failure to mark it was not logged")
	}
}

func TestTheMarksHaveTenSecondsAndSurviveACancel(t *testing.T) {
	mem := NewMemoryStore(time.Now)
	store := &brokenStore{Store: mem}
	if err := NewSender(mem, payloads{}).Send(context.Background(), message("Ana")); err != nil {
		t.Fatal(err)
	}
	w := workerWith(t, store, &sink{}, quiet{})
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	dl, ok := store.markCtx.Deadline()
	if left := time.Until(dl); !ok || left > 10*time.Second || left < 9500*time.Millisecond {
		t.Fatalf("the mark has %v (deadline %v)", left, ok)
	}
}

type softError struct{}

func (softError) Error() string   { return "soft" }
func (softError) Permanent() bool { return false }

func TestDefaultRetry(t *testing.T) {
	smtpErr := func(code int) error { return fmt.Errorf("smtp: %w", &textproto.Error{Code: code, Msg: "x"}) }
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"a network failure":            {errors.New("connection reset"), true},
		"a temporary smtp error 450":   {smtpErr(450), true},
		"the last temporary code 499":  {smtpErr(499), true},
		"the first permanent code 500": {smtpErr(500), false},
		"a permanent smtp error 501":   {smtpErr(501), false},
		"a permanent smtp error 550":   {smtpErr(550), false},
		"login 530 is configuration":   {smtpErr(530), true},
		"login 534 is configuration":   {smtpErr(534), true},
		"login 535 is configuration":   {smtpErr(535), true},
		"login 538 is configuration":   {smtpErr(538), true},
		"533 is not a login code":      {smtpErr(533), false},
		"537 is not a login code":      {smtpErr(537), false},
		"539 is not a login code":      {smtpErr(539), false},
		"marked permanent":             {Permanent(errors.New("bad")), false},
		"marked permanent and wrapped": {fmt.Errorf("x: %w", Permanent(errors.New("bad"))), false},
		"says it is not permanent":     {softError{}, true},
		"an invalid message":           {fmt.Errorf("x: %w", mail.ErrInvalid), false},
		"a missing file":               {fmt.Errorf("x: %w", files.ErrNotFound), false},
		"a denied file":                {fmt.Errorf("x: %w", files.ErrAccessDenied), false},
	} {
		if got := DefaultRetry(tc.err); got != tc.want {
			t.Errorf("%s: retry = %v, want %v", name, got, tc.want)
		}
	}
}
