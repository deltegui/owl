package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/logx"
	"github.com/deltegui/owl/v3/mail"
)

type payload struct{ N int }

func msg(n int) mail.Message {
	return mail.Message{Envelope: mail.Envelope{To: []string{fmt.Sprintf("u%d@example.com", n)}}, Payload: payload{n}}
}

// recorder is a mail.Sender that waits for a gate, if it has one, and keeps what it gets.
type recorder struct {
	gate    chan struct{} // closed to let the sends go on; nil: no waiting
	entered chan struct{} // gets a value each time a send starts
	delay   time.Duration
	fn      func(ctx context.Context, m mail.Message) error

	mu        sync.Mutex
	got       []mail.Message
	cur, peak int
}

func newRecorder() *recorder { return &recorder{entered: make(chan struct{}, 1000)} }

func (r *recorder) Send(ctx context.Context, m mail.Message) error {
	r.entered <- struct{}{}
	r.mu.Lock()
	r.cur++
	r.peak = max(r.peak, r.cur)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.cur--
		r.mu.Unlock()
	}()
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	if r.gate != nil {
		select {
		case <-r.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if r.fn != nil {
		if err := r.fn(ctx, m); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, m)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *recorder) waitEntered(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-r.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("the sender was not called")
		}
	}
}

type quiet struct{ logx.Default }

func (quiet) Error(string, ...any) {}

// errs collects what OnError receives.
type errs struct {
	mu   sync.Mutex
	list []error
	msgs []mail.Message
}

func (e *errs) on(m mail.Message, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list, e.msgs = append(e.list, err), append(e.msgs, m)
}

func (e *errs) all() []error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]error(nil), e.list...)
}

func short(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func TestSendDoesNotWaitForTheServer(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{})
	q := New(r, Options{})
	start := time.Now()
	if err := q.Send(context.Background(), msg(1)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second || r.count() != 0 {
		t.Fatal("Send waited for the sender")
	}
	close(r.gate)
	ctx, cancel := short(3 * time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil || r.count() != 1 {
		t.Fatalf("close: %v, sent %d", err, r.count())
	}
	if p := r.got[0].Payload.(payload); p.N != 1 {
		t.Fatalf("%+v", r.got[0])
	}
}

func TestInvalidMessagesFailInSend(t *testing.T) {
	r := newRecorder()
	q := New(r, Options{})
	err := q.Send(context.Background(), mail.Message{Payload: payload{}})
	if !errors.Is(err, mail.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
	ctx, cancel := short(time.Second)
	defer cancel()
	q.Close(ctx)
	if r.count() != 0 {
		t.Fatal("sent")
	}
}

func TestWorkersLimitTheConcurrency(t *testing.T) {
	r := newRecorder()
	r.delay = 20 * time.Millisecond
	q := New(r, Options{Workers: 3})
	for i := 0; i < 12; i++ {
		if err := q.Send(context.Background(), msg(i)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := short(5 * time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil || r.count() != 12 {
		t.Fatalf("close: %v, sent %d", err, r.count())
	}
	if r.peak < 2 || r.peak > 3 {
		t.Fatalf("peak concurrency %d with 3 workers", r.peak)
	}
}

func TestFullQueueMakesSendWaitAndNeverGrows(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{})
	q := New(r, Options{Buffer: 2, Workers: 1})
	ctx := context.Background()

	q.Send(ctx, msg(1))
	r.waitEntered(t, 1) // the worker has it: the queue is empty again
	q.Send(ctx, msg(2))
	q.Send(ctx, msg(3)) // full

	waiting, cancel := short(50 * time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := q.Send(waiting, msg(4)); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) < 40*time.Millisecond {
		t.Fatalf("a full queue must wait for the context: %v after %v", err, time.Since(start))
	}

	close(r.gate)
	done, cancel2 := short(3 * time.Second)
	defer cancel2()
	if err := q.Close(done); err != nil || r.count() != 3 {
		t.Fatalf("close: %v, sent %d (the 4th was never queued)", err, r.count())
	}
}

func TestRoomIsGivenBackAndSendGoesOn(t *testing.T) {
	r := newRecorder()
	q := New(r, Options{Buffer: 1})
	for i := 0; i < 50; i++ { // far more than the buffer
		ctx, cancel := short(3 * time.Second)
		if err := q.Send(ctx, msg(i)); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		cancel()
	}
	ctx, cancel := short(3 * time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil || r.count() != 50 {
		t.Fatalf("close: %v, sent %d", err, r.count())
	}
}

func TestTheContextOfSendDoesNotReachTheDelivery(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{})
	var ctxErr atomic.Value
	r.fn = func(ctx context.Context, m mail.Message) error {
		ctxErr.Store(fmt.Sprint(ctx.Err()))
		return nil
	}
	q := New(r, Options{})
	req, cancel := context.WithCancel(context.Background())
	q.Send(req, msg(1))
	cancel() // the request ends
	close(r.gate)
	done, cancel2 := short(3 * time.Second)
	defer cancel2()
	q.Close(done)
	if got := ctxErr.Load(); got != "<nil>" || r.count() != 1 {
		t.Fatalf("delivery context error %v, sent %d", got, r.count())
	}
}

func TestTheEnvelopeIsCopied(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{})
	q := New(r, Options{})
	to := []string{"a@example.com"}
	m := mail.Message{Envelope: mail.Envelope{To: to, Attachments: []string{"a.pdf"}}, Payload: payload{}}
	q.Send(context.Background(), m)
	to[0] = "evil@example.com" // the caller reuses its slices
	m.Envelope.Attachments[0] = "b.pdf"
	close(r.gate)
	done, cancel := short(3 * time.Second)
	defer cancel()
	q.Close(done)
	if got := r.got[0].Envelope; got.To[0] != "a@example.com" || got.Attachments[0] != "a.pdf" {
		t.Fatalf("the envelope changed after Send: %+v", got)
	}
}

func TestCloseSendsWhatIsWaiting(t *testing.T) {
	r := newRecorder()
	r.delay = 5 * time.Millisecond
	q := New(r, Options{Workers: 2})
	for i := 0; i < 20; i++ {
		q.Send(context.Background(), msg(i))
	}
	ctx, cancel := short(5 * time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil || r.count() != 20 {
		t.Fatalf("close: %v, sent %d", err, r.count())
	}
	if err := q.Send(context.Background(), msg(99)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send after Close: %v", err)
	}
	if err := q.Close(ctx); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
}

func TestCloseThatRunsOutOfTime(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{}) // the server never answers
	defer close(r.gate)
	var e errs
	q := New(r, Options{Buffer: 3, Workers: 1, OnError: e.on})
	for i := 1; i <= 4; i++ {
		q.Send(context.Background(), msg(i))
		if i == 1 {
			r.waitEntered(t, 1) // one is being sent, three wait
		}
	}
	ctx, cancel := short(50 * time.Millisecond)
	defer cancel()
	err := q.Close(ctx)
	if !errors.Is(err, ErrNotDrained) || err.Error() != "queue: emails left unsent: 4" {
		t.Fatalf("got %v", err)
	}

	// The waiting ones were handed over as discarded and the one being sent was cancelled.
	deadline := time.Now().Add(3 * time.Second)
	for len(e.all()) < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	discarded, canceled := 0, 0
	for _, err := range e.all() {
		switch {
		case errors.Is(err, ErrDiscarded):
			discarded++
		case errors.Is(err, context.Canceled):
			canceled++
		}
	}
	if discarded != 3 || canceled != 1 {
		t.Fatalf("discarded %d, canceled %d: %v", discarded, canceled, e.all())
	}
	if err := q.Send(context.Background(), msg(5)); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestACloseThatLeavesNothingIsNotAnError(t *testing.T) {
	r := newRecorder()
	q := New(r, Options{})
	ctx, cancel := short(time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSendThatWaitsForRoomGetsErrClosed(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{})
	defer close(r.gate)
	q := New(r, Options{Buffer: 1})
	q.Send(context.Background(), msg(1))
	r.waitEntered(t, 1)
	q.Send(context.Background(), msg(2)) // full

	result := make(chan error, 1)
	go func() { result <- q.Send(context.Background(), msg(3)) }()
	time.Sleep(50 * time.Millisecond) // it is waiting
	ctx, cancel := short(50 * time.Millisecond)
	defer cancel()
	q.Close(ctx)
	select {
	case err := <-result:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a Send that was waiting is not released by Close")
	}
}

func TestFailuresAndPanicsGoToOnError(t *testing.T) {
	r := newRecorder()
	boom := errors.New("550 mailbox unavailable")
	r.fn = func(_ context.Context, m mail.Message) error {
		switch m.Payload.(payload).N {
		case 1:
			return boom
		case 2:
			panic("bad template")
		}
		return nil
	}
	var e errs
	q := New(r, Options{OnError: e.on})
	for i := 1; i <= 3; i++ {
		q.Send(context.Background(), msg(i))
	}
	ctx, cancel := short(3 * time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil {
		t.Fatal(err)
	}
	got := e.all()
	if len(got) != 2 || !errors.Is(got[0], boom) || got[1] == nil || e.msgs[0].Payload.(payload).N != 1 {
		t.Fatalf("OnError got %v", got)
	}
	if r.count() != 1 || r.got[0].Payload.(payload).N != 3 {
		t.Fatalf("the queue did not go on after the failures: %+v", r.got)
	}
}

func TestEachDeliveryHasATimeout(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{}) // never opens
	defer close(r.gate)
	var e errs
	q := New(r, Options{Timeout: 50 * time.Millisecond, OnError: e.on})
	q.Send(context.Background(), msg(1))
	deadline := time.Now().Add(3 * time.Second)
	for len(e.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := e.all(); len(got) != 1 || !errors.Is(got[0], context.DeadlineExceeded) {
		t.Fatalf("got %v", got)
	}
}

// Nothing may be lost or sent twice when Send and Close run at the same time:
// every email that Send accepted is delivered once.
func TestConcurrentSendsAndClose(t *testing.T) {
	for round := 0; round < 20; round++ {
		r := newRecorder()
		q := New(r, Options{Buffer: 5, Workers: 3})
		var accepted atomic.Int32
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 30; i++ {
					ctx, cancel := short(2 * time.Second)
					err := q.Send(ctx, msg(g*100+i))
					cancel()
					switch {
					case err == nil:
						accepted.Add(1)
					case errors.Is(err, ErrClosed):
						return
					default:
						t.Errorf("Send: %v", err)
						return
					}
				}
			}()
		}
		time.Sleep(time.Duration(round%5) * time.Millisecond)
		ctx, cancel := short(5 * time.Second)
		if err := q.Close(ctx); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		cancel()
		wg.Wait()
		if int(accepted.Load()) != r.count() {
			t.Fatalf("round %d: accepted %d, delivered %d", round, accepted.Load(), r.count())
		}
		seen := map[int]bool{}
		for _, m := range r.got {
			n := m.Payload.(payload).N
			if seen[n] {
				t.Fatalf("round %d: %d delivered twice", round, n)
			}
			seen[n] = true
		}
	}
}

func TestDefaultsAndNilSender(t *testing.T) {
	q := New(newRecorder(), Options{Logger: quiet{}})
	if cap(q.ch) != 100 || q.timeout != time.Minute {
		t.Fatalf("defaults: buffer %d, timeout %v", cap(q.ch), q.timeout)
	}
	ctx, cancel := short(time.Second)
	defer cancel()
	q.Close(ctx)
	defer func() {
		if recover() == nil {
			t.Fatal("a nil sender must panic")
		}
	}()
	New(nil, Options{})
}
