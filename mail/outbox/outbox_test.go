package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/textproto"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/logx"
	"github.com/deltegui/owl/v3/mail"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// clock is a clock that only moves when the test says so.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type welcome struct{ Name, Link string }

// payloads is a Payloads with one type.
type payloads struct{}

func (payloads) PayloadName(p any) (string, bool) {
	switch p.(type) {
	case welcome, *welcome:
		return "welcome", true
	}
	return "", false
}

func (payloads) DecodePayload(name string, decode func(any) error) (any, error) {
	if name != "welcome" {
		return nil, fmt.Errorf("no payload %q", name)
	}
	var w welcome
	if err := decode(&w); err != nil {
		return nil, err
	}
	return w, nil
}

// sink is a mail.Sender that keeps the messages and fails as it is told.
type sink struct {
	mu   sync.Mutex
	got  []mail.Message
	errs []error // answers in order; then nil
}

func (s *sink) Send(_ context.Context, m mail.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, m)
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return err
	}
	return nil
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

func message(name string) mail.Message {
	return mail.Message{
		Envelope: mail.Envelope{To: []string{"ana@example.com"}, Attachments: []string{"tmp/a.pdf"}},
		Lang:     "en",
		Payload:  welcome{Name: name, Link: "https://example.com"},
	}
}

type fixture struct {
	clock  *clock
	store  *MemoryStore
	sink   *sink
	worker *Worker
	sender *Sender
}

func newFixture(t *testing.T, tweak func(*Options)) *fixture {
	t.Helper()
	c := &clock{t: t0}
	f := &fixture{clock: c, store: NewMemoryStore(c.Now), sink: &sink{}}
	o := Options{Store: f.store, Sender: f.sink, Payloads: payloads{}, Now: c.Now, Logger: quiet{}, ID: "w1"}
	if tweak != nil {
		tweak(&o)
	}
	var err error
	if f.worker, err = NewWorker(o); err != nil {
		t.Fatal(err)
	}
	f.sender = NewSender(f.store, payloads{}, WithWake(f.worker.Wake))
	return f
}

type quiet struct{ logx.Default }

func (quiet) Error(string, ...any)                         {}
func (quiet) ErrorContext(context.Context, string, ...any) {}
func (q quiet) WithModuleName(string) logx.Logger          { return q }

func (f *fixture) status(t *testing.T, id int) Record {
	t.Helper()
	return f.store.Snapshot()[id-1]
}

// ---- MemoryStore -----------------------------------------------------------

func TestMemoryStoreClaim(t *testing.T) {
	ctx := context.Background()
	c := &clock{t: t0}
	s := NewMemoryStore(c.Now)
	for i := 0; i < 5; i++ {
		s.Save(ctx, Entry{Type: "x", Payload: []byte{byte(i)}})
		c.Add(time.Second)
	}
	// Not due yet: saved at t0..t0+4s, now is t0+2s: only the first three.
	items, err := s.Claim(ctx, "w", 10, t0.Add(2*time.Second))
	if err != nil || len(items) != 3 || items[0].ID != 1 || items[2].ID != 3 {
		t.Fatalf("got %+v, %v", items, err)
	}
	// What is claimed is not claimed again; the limit is respected, oldest first.
	items, _ = s.Claim(ctx, "w", 1, t0.Add(time.Hour))
	if len(items) != 1 || items[0].ID != 4 {
		t.Fatalf("got %+v", items)
	}
	if got := s.Snapshot()[0]; got.Status != InProgress || got.ClaimedBy != "w" {
		t.Fatalf("%+v", got)
	}
	// The items are copies.
	items[0].Payload[0] = 99
	if s.Snapshot()[3].Payload[0] == 99 {
		t.Fatal("the store shares its bytes")
	}
}

func TestMemoryStoreConcurrentClaimsNeverRepeat(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(func() time.Time { return t0 })
	for i := 0; i < 200; i++ {
		s.Save(ctx, Entry{Type: "x"})
	}
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				items, _ := s.Claim(ctx, fmt.Sprint(w), 7, t0)
				if len(items) == 0 {
					return
				}
				mu.Lock()
				for _, it := range items {
					seen[it.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 200 {
		t.Fatalf("claimed %d of 200", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("email %d claimed %d times", id, n)
		}
	}
}

func TestMemoryStoreMarkAndReset(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore(func() time.Time { return t0 })
	for i := 0; i < 3; i++ {
		s.Save(ctx, Entry{})
	}
	s.Claim(ctx, "w", 3, t0)
	next := t0.Add(time.Minute)
	s.MarkSent(ctx, 1)
	s.MarkFailed(ctx, 2, "boom", &next)
	s.MarkFailed(ctx, 3, "bad", nil)
	snap := s.Snapshot()
	if snap[0].Status != Sent ||
		snap[1].Status != Pending || snap[1].Attempts != 1 || !snap[1].NextAttemptAt.Equal(next) || snap[1].LastError != "boom" ||
		snap[2].Status != Failed || snap[2].Attempts != 1 {
		t.Fatalf("%+v", snap)
	}
	if err := s.MarkSent(ctx, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}

	// Reset only gives back what is InProgress and old.
	s.Save(ctx, Entry{})
	s.Claim(ctx, "dead", 1, t0.Add(time.Hour))
	s.ResetStale(ctx, t0.Add(30*time.Minute)) // claimed at +1h: not stale
	if s.Snapshot()[3].Status != InProgress {
		t.Fatal("reset a recent claim")
	}
	s.ResetStale(ctx, t0.Add(2*time.Hour))
	if got := s.Snapshot()[3]; got.Status != Pending || got.ClaimedBy != "" {
		t.Fatalf("%+v", got)
	}
}

// ---- Sender ----------------------------------------------------------------

func TestSenderKeepsTheEmail(t *testing.T) {
	f := newFixture(t, nil)
	if err := f.sender.Send(context.Background(), message("Ana")); err != nil {
		t.Fatal(err)
	}
	r := f.status(t, 1)
	if r.Status != Pending || r.Type != "welcome" || r.Lang != "en" {
		t.Fatalf("%+v", r)
	}
	var p welcome
	var env mail.Envelope
	if json.Unmarshal(r.Payload, &p) != nil || p.Name != "Ana" || json.Unmarshal(r.Envelope, &env) != nil ||
		env.To[0] != "ana@example.com" || env.Attachments[0] != "tmp/a.pdf" {
		t.Fatalf("payload %s, envelope %s", r.Payload, r.Envelope)
	}
	select {
	case <-f.worker.wake:
	default:
		t.Fatal("the worker was not woken")
	}
}

func TestSenderRejectsBeforeSaving(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	if err := f.sender.Send(ctx, mail.Message{Payload: welcome{}}); !errors.Is(err, mail.ErrInvalid) {
		t.Errorf("invalid: %v", err)
	}
	m := message("x")
	m.Payload = struct{ X int }{1}
	if err := f.sender.Send(ctx, m); !errors.Is(err, ErrUnknownPayload) {
		t.Errorf("unknown: %v", err)
	}
	m.Payload = make(chan int) // cannot be JSON, and is not known
	if err := f.sender.Send(ctx, m); err == nil {
		t.Error("channel")
	}
	if len(f.store.Snapshot()) != 0 {
		t.Fatal("something was saved")
	}
	// A store that fails: the caller must know the email was not kept.
	failing := NewSender(failingStore{}, payloads{})
	if err := failing.Send(ctx, message("x")); err == nil {
		t.Fatal("expected error")
	}
}

type failingStore struct{ Store }

func (failingStore) Save(context.Context, Entry) error { return errors.New("db down") }

// ---- Worker ----------------------------------------------------------------

func TestWorkerSendsWhatWasKept(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.sender.Send(ctx, message("Ana"))
	f.sender.Send(ctx, message("Bea"))

	n, err := f.worker.RunOnce(ctx)
	if err != nil || n != 2 || f.sink.count() != 2 {
		t.Fatalf("n=%d err=%v sent=%d", n, err, f.sink.count())
	}
	got := f.sink.got[0]
	if !reflect.DeepEqual(got, message("Ana")) {
		t.Errorf("the message changed on the way:\n got %#v\nwant %#v", got, message("Ana"))
	}
	if f.status(t, 1).Status != Sent || f.status(t, 2).Status != Sent {
		t.Fatalf("%+v", f.store.Snapshot())
	}
	if n, _ := f.worker.RunOnce(ctx); n != 0 || f.sink.count() != 2 {
		t.Fatal("sent twice")
	}
}

func TestWorkerRetriesWithBackoffAndGivesUp(t *testing.T) {
	boom := errors.New("connection refused")
	f := newFixture(t, func(o *Options) { o.MaxAttempts = 4 })
	f.sink.errs = []error{boom, boom, boom, boom}
	ctx := context.Background()
	f.sender.Send(ctx, message("Ana"))

	for i, wait := range []time.Duration{30 * time.Second, 5 * time.Minute, 30 * time.Minute} {
		f.worker.RunOnce(ctx)
		r := f.status(t, 1)
		if r.Status != Pending || r.Attempts != i+1 || !r.NextAttemptAt.Equal(f.clock.Now().Add(wait)) || r.LastError != boom.Error() {
			t.Fatalf("attempt %d: %+v (want next in %v)", i+1, r, wait)
		}
		// Not due yet: nothing happens.
		before := f.sink.count()
		f.clock.Add(wait - time.Second)
		if n, _ := f.worker.RunOnce(ctx); n != 0 || f.sink.count() != before {
			t.Fatalf("attempt %d: tried before its time", i+1)
		}
		f.clock.Add(time.Second)
	}
	f.worker.RunOnce(ctx) // the 4th is the last
	if r := f.status(t, 1); r.Status != Failed || r.Attempts != 4 {
		t.Fatalf("%+v", r)
	}
	f.clock.Add(24 * time.Hour)
	if n, _ := f.worker.RunOnce(ctx); n != 0 {
		t.Fatal("a failed email was tried again")
	}
}

func TestWorkerEventuallySends(t *testing.T) {
	f := newFixture(t, nil)
	f.sink.errs = []error{errors.New("timeout")}
	ctx := context.Background()
	f.sender.Send(ctx, message("Ana"))
	f.worker.RunOnce(ctx)
	f.clock.Add(time.Minute)
	f.worker.RunOnce(ctx)
	if r := f.status(t, 1); r.Status != Sent || r.Attempts != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestPermanentErrorsAreNotRetried(t *testing.T) {
	cases := map[string]struct {
		err   error
		retry bool
	}{
		"550 mailbox":      {&textproto.Error{Code: 550}, false},
		"554 rejected":     {fmt.Errorf("smtp: data: %w", &textproto.Error{Code: 554}), false},
		"451 try later":    {&textproto.Error{Code: 451}, true},
		"421 busy":         {&textproto.Error{Code: 421}, true},
		"535 bad password": {fmt.Errorf("smtp: authentication: %w", &textproto.Error{Code: 535}), true},
		"530 auth":         {&textproto.Error{Code: 530}, true},
		"timeout":          {context.DeadlineExceeded, true},
		"network":          {errors.New("connection reset"), true},
		"invalid message":  {fmt.Errorf("x: %w", mail.ErrInvalid), false},
		"missing file":     {fmt.Errorf("smtp: attachment: %w", files.ErrNotFound), false},
		"forbidden file":   {files.ErrAccessDenied, false},
		"marked":           {Permanent(errors.New("nope")), false},
		"method":           {methodPermanent{}, false},
	}
	for name, c := range cases {
		if got := DefaultRetry(c.err); got != c.retry {
			t.Errorf("%s: retry = %v, want %v", name, got, c.retry)
		}
	}
	// And the worker acts on it: failed at the first attempt.
	f := newFixture(t, nil)
	f.sink.errs = []error{&textproto.Error{Code: 550, Msg: "no such user"}}
	f.sender.Send(context.Background(), message("Ana"))
	f.worker.RunOnce(context.Background())
	if r := f.status(t, 1); r.Status != Failed || r.Attempts != 1 || r.LastError == "" {
		t.Fatalf("%+v", r)
	}
	if Permanent(nil) != nil {
		t.Fatal("Permanent(nil)")
	}
}

type methodPermanent struct{}

func (methodPermanent) Error() string   { return "x" }
func (methodPermanent) Permanent() bool { return true }

func TestCustomRetryBackoffAndMaxAttempts(t *testing.T) {
	f := newFixture(t, func(o *Options) {
		o.Retry = func(error) bool { return true } // even 5xx
		o.Backoff = func(n int) time.Duration { return time.Duration(n) * time.Hour }
		o.MaxAttempts = 2
	})
	f.sink.errs = []error{&textproto.Error{Code: 550}, &textproto.Error{Code: 550}}
	ctx := context.Background()
	f.sender.Send(ctx, message("Ana"))
	f.worker.RunOnce(ctx)
	if r := f.status(t, 1); r.Status != Pending || !r.NextAttemptAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("%+v", r)
	}
	f.clock.Add(time.Hour)
	f.worker.RunOnce(ctx)
	if r := f.status(t, 1); r.Status != Failed {
		t.Fatalf("MaxAttempts 2: %+v", r)
	}
}

func TestWorkerFailsWhatCannotBeRead(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.store.Save(ctx, Entry{Type: "gone", Payload: []byte(`{}`), Envelope: []byte(`{}`)})
	f.store.Save(ctx, Entry{Type: "welcome", Payload: []byte(`{not json`), Envelope: []byte(`{}`)})
	f.store.Save(ctx, Entry{Type: "welcome", Payload: []byte(`{}`), Envelope: []byte(`[`)})
	f.worker.RunOnce(ctx)
	for id := 1; id <= 3; id++ {
		if r := f.status(t, id); r.Status != Failed || r.Attempts != 1 {
			t.Errorf("email %d: %+v", id, r)
		}
	}
	if f.sink.count() != 0 {
		t.Fatal("the sender was called")
	}
}

func TestStaleClaimsAreRecovered(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.sender.Send(ctx, message("Ana"))
	f.store.Claim(ctx, "dead-worker", 10, f.clock.Now()) // claimed and never finished
	if n, _ := f.worker.RunOnce(ctx); n != 0 {
		t.Fatal("took an email claimed by another worker")
	}
	f.clock.Add(11 * time.Minute)
	if n, _ := f.worker.RunOnce(ctx); n != 1 || f.status(t, 1).Status != Sent {
		t.Fatalf("not recovered: %+v", f.status(t, 1))
	}
}

// ctxStore is a store like a database: it refuses to work with a cancelled context.
type ctxStore struct{ *MemoryStore }

func (s ctxStore) MarkSent(ctx context.Context, id int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.MemoryStore.MarkSent(ctx, id)
}

func (s ctxStore) MarkFailed(ctx context.Context, id int64, cause string, next *time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.MemoryStore.MarkFailed(ctx, id, cause, next)
}

func TestStopMarksWhatWasSentAndLeavesTheRest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newFixture(t, nil)
	f.worker.o.Store = ctxStore{f.store}
	f.worker.o.Sender = senderFunc(func(ctx context.Context, m mail.Message) error {
		cancel() // the program stops while the email is being sent
		return nil
	})
	f.sender.Send(context.Background(), message("Ana"))
	f.sender.Send(context.Background(), message("Bea"))
	f.worker.RunOnce(ctx)
	if f.status(t, 1).Status != Sent {
		t.Fatalf("the sent one was lost: %+v", f.status(t, 1))
	}
	if r := f.status(t, 2); r.Status != InProgress || r.Attempts != 0 {
		t.Fatalf("the rest stays claimed, without a failed attempt: %+v", r)
	}

	// A failure caused by the stop is not a failure of the email.
	f2 := newFixture(t, nil)
	ctx2, cancel2 := context.WithCancel(context.Background())
	f2.worker.o.Sender = senderFunc(func(ctx context.Context, m mail.Message) error {
		cancel2()
		return ctx.Err()
	})
	f2.sender.Send(context.Background(), message("Ana"))
	f2.worker.RunOnce(ctx2)
	if r := f2.status(t, 1); r.Status != InProgress || r.Attempts != 0 {
		t.Fatalf("%+v", r)
	}
}

type senderFunc func(context.Context, mail.Message) error

func (f senderFunc) Send(ctx context.Context, m mail.Message) error { return f(ctx, m) }

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunIsWokenAndStops(t *testing.T) {
	f := newFixture(t, func(o *Options) { o.PollInterval = time.Hour })
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { f.worker.Run(ctx); close(stopped) }()

	// It looks at once when it starts: nothing there, and now it waits an hour.
	time.Sleep(50 * time.Millisecond)
	f.sender.Send(ctx, message("Ana")) // WithWake wakes it
	eventually(t, "the woken worker to send", func() bool { return f.sink.count() == 1 })

	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestRunGoesOnWhileBatchesAreFull(t *testing.T) {
	f := newFixture(t, func(o *Options) { o.PollInterval = time.Hour; o.BatchSize = 2 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 5; i++ {
		f.store.Save(ctx, Entry{Type: "welcome", Payload: []byte(`{}`), Envelope: []byte(`{"To":["a@example.com"]}`)})
	}
	go f.worker.Run(ctx)
	eventually(t, "all the batches without waiting for the poll", func() bool { return f.sink.count() == 5 })
}

func TestTwoWorkersNeverSendTheSameEmail(t *testing.T) {
	c := &clock{t: t0}
	store := NewMemoryStore(c.Now)
	var sent atomic.Int32
	count := senderFunc(func(context.Context, mail.Message) error { sent.Add(1); return nil })
	for i := 0; i < 100; i++ {
		store.Save(context.Background(), Entry{Type: "welcome", Payload: []byte(`{}`), Envelope: []byte(`{"To":["a@example.com"]}`)})
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		w, _ := NewWorker(Options{Store: store, Sender: count, Payloads: payloads{}, Now: c.Now, Logger: quiet{}, ID: fmt.Sprint(i), BatchSize: 10})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n, _ := w.RunOnce(context.Background()); n > 0; n, _ = w.RunOnce(context.Background()) {
			}
		}()
	}
	wg.Wait()
	if sent.Load() != 100 {
		t.Fatalf("sent %d of 100", sent.Load())
	}
}

func TestNewWorkerChecksOptions(t *testing.T) {
	if _, err := NewWorker(Options{}); err == nil {
		t.Fatal("expected error")
	}
	w, err := NewWorker(Options{Store: NewMemoryStore(nil), Sender: &sink{}, Payloads: payloads{}})
	if err != nil || w.o.PollInterval != 30*time.Second || w.o.BatchSize != 50 || w.o.MaxAttempts != 5 ||
		w.o.StaleAfter != 10*time.Minute || w.o.ID == "" {
		t.Fatalf("defaults: %+v (%v)", w.o, err)
	}
	for n, want := range map[int]time.Duration{1: 30 * time.Second, 2: 5 * time.Minute, 3: 30 * time.Minute, 4: 2 * time.Hour, 9: 2 * time.Hour} {
		if got := DefaultBackoff(n); got != want {
			t.Errorf("backoff(%d) = %v", n, got)
		}
	}
}
