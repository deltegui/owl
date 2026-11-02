package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/mail"
)

// capture keeps what the queue logs as an error.
type capture struct {
	quiet
	args chan []any
}

func (c capture) Error(_ string, args ...any) { c.args <- args }

func TestTheDefaultErrorLogCountsEveryRecipient(t *testing.T) {
	log := capture{args: make(chan []any, 1)}
	r := newRecorder()
	r.fn = func(context.Context, mail.Message) error { return errors.New("server down") }
	q := New(r, Options{Logger: log})
	m := mail.Message{
		Envelope: mail.Envelope{
			To:  []string{"a@example.com"},
			Cc:  []string{"b@example.com", "c@example.com"},
			Bcc: []string{"d@example.com", "e@example.com", "f@example.com"},
		},
		Payload: payload{1},
	}
	if err := q.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	select {
	case args := <-log.args:
		got := map[string]any{}
		for i := 0; i+1 < len(args); i += 2 {
			got[args[i].(string)] = args[i+1]
		}
		if got["recipients"] != 6 || got["payload"] != "queue.payload" {
			t.Fatalf("logged %v", got)
		}
		if _, leaks := got["to"]; leaks {
			t.Fatal("the addresses must not be logged")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nothing was logged")
	}
}

func TestInFlightGoesBackToZero(t *testing.T) {
	r := newRecorder()
	q := New(r, Options{Workers: 2})
	for i := 1; i <= 5; i++ {
		if err := q.Send(context.Background(), msg(i)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := short(3 * time.Second)
	defer cancel()
	if err := q.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if n := q.inflight.Load(); n != 0 {
		t.Fatalf("inflight = %d after everything was delivered", n)
	}
	if r.count() != 5 {
		t.Fatalf("delivered %d of 5", r.count())
	}
}

func TestCloseCountsExactlyWhatWasLeft(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{})
	defer close(r.gate)
	q := New(r, Options{Workers: 1})
	if err := q.Send(context.Background(), msg(1)); err != nil {
		t.Fatal(err)
	}
	r.waitEntered(t, 1) // one in flight, nothing waiting
	ctx, cancel := short(50 * time.Millisecond)
	defer cancel()
	err := q.Close(ctx)
	if !errors.Is(err, ErrNotDrained) || err.Error() != "queue: emails left unsent: 1" {
		t.Fatalf("got %v", err)
	}
}

func TestACloseThatRunsOutOfTimeStopsTheWorkersAndCanBeRepeated(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{})
	defer close(r.gate)
	q := New(r, Options{Workers: 1})
	if err := q.Send(context.Background(), msg(1)); err != nil {
		t.Fatal(err)
	}
	r.waitEntered(t, 1)
	ctx, cancel := short(50 * time.Millisecond)
	defer cancel()
	if err := q.Close(ctx); !errors.Is(err, ErrNotDrained) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-q.stop:
	default:
		t.Fatal("the workers were not told to stop")
	}
	// A second Close does not close the channel again.
	ctx2, cancel2 := short(50 * time.Millisecond)
	defer cancel2()
	_ = q.Close(ctx2)
}

func TestADeliveryTimeoutOfOneNanosecondIsKept(t *testing.T) {
	r := newRecorder()
	r.gate = make(chan struct{}) // never opens: only the timeout ends the send
	defer close(r.gate)
	var e errs
	q := New(r, Options{Timeout: time.Nanosecond, OnError: e.on})
	if err := q.Send(context.Background(), msg(1)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(e.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := e.all(); len(got) != 1 || !errors.Is(got[0], context.DeadlineExceeded) {
		t.Fatalf("a timeout of 1ns is not the default of a minute: %v", got)
	}
}
