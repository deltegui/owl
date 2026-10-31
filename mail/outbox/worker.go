package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/textproto"
	"os"
	"time"

	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/logx"
	"github.com/deltegui/owl/v3/mail"
)

// Options configures a Worker. Store, Sender and Payloads are required.
type Options struct {
	Store Store
	// Sender sends the emails that were kept, usually a mail.Service.
	Sender   mail.Sender
	Payloads Payloads
	// PollInterval is the time between two looks at the Store when nothing wakes
	// the Worker. 30 seconds if it is zero.
	PollInterval time.Duration
	// BatchSize is how many emails are claimed at once. 50 if it is zero. If a
	// look finds that many, the next one is done without waiting.
	BatchSize int
	// StaleAfter is how long an email can be InProgress before the Worker that
	// claimed it is thought dead. 10 minutes if it is zero. It must be longer
	// than the time to send an email.
	StaleAfter time.Duration
	// MaxAttempts is how many times an email is tried before it is Failed. 5 if
	// it is zero.
	MaxAttempts int
	// Backoff is the wait before the next attempt, given how many attempts failed
	// (1 the first time). DefaultBackoff if nil.
	Backoff func(attempts int) time.Duration
	// Retry says if an error is worth another attempt. DefaultRetry if nil.
	Retry func(err error) bool
	// Logger is logx.Default{} if nil.
	Logger logx.Logger
	// Now is the clock, time.Now if nil.
	Now func() time.Time
	// ID names this Worker in the Store. "host:pid" if empty.
	ID string
}

// Worker sends the emails a Store keeps: it claims the ones that are due,
// gives them to the Sender and marks them as Sent, or as failed with the time of
// the next attempt. The delivery is at least once: if the program dies after the
// server accepted an email and before it is marked as Sent, it is sent again.
//
// A Worker does nothing until Run (or RunOnce) is called. Several Workers, in
// several programs, can share a Store.
//
// Deploy the Workers before the programs that save a new kind of email: an
// email whose type a Worker does not know is Failed.
type Worker struct {
	o    Options
	log  logx.Logger
	wake chan struct{}
}

// NewWorker creates a Worker.
func NewWorker(o Options) (*Worker, error) {
	if o.Store == nil || o.Sender == nil || o.Payloads == nil {
		return nil, errors.New("outbox: Store, Sender and Payloads are required")
	}
	if o.PollInterval == 0 {
		o.PollInterval = 30 * time.Second
	}
	if o.BatchSize == 0 {
		o.BatchSize = 50
	}
	if o.StaleAfter == 0 {
		o.StaleAfter = 10 * time.Minute
	}
	if o.MaxAttempts == 0 {
		o.MaxAttempts = 5
	}
	if o.Backoff == nil {
		o.Backoff = DefaultBackoff
	}
	if o.Retry == nil {
		o.Retry = DefaultRetry
	}
	if o.Logger == nil {
		o.Logger = logx.Default{}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.ID == "" {
		host, _ := os.Hostname()
		o.ID = fmt.Sprintf("%s:%d", host, os.Getpid())
	}
	return &Worker{o: o, log: o.Logger, wake: make(chan struct{}, 1)}, nil
}

// Wake makes the Worker look at the Store now, instead of waiting for the
// poll. It does not block and can be called from any goroutine; Sender calls
// it with WithWake.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default: // it is already going to look
	}
}

// Run sends emails until ctx is done. Call it in its own goroutine. It looks at
// the Store at once, then when it is woken, when the poll interval passes or
// right away if the last look filled a batch. What a stop leaves claimed and
// unsent is recovered by another look, after StaleAfter.
func (w *Worker) Run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-w.wake:
		}
		wait := w.o.PollInterval
		n, err := w.RunOnce(ctx)
		switch {
		case err != nil:
			w.log.ErrorContext(ctx, "outbox: cannot process the emails", "error", err)
		case n >= w.o.BatchSize:
			wait = 0 // there may be more
		}
		timer.Reset(wait)
	}
}

// RunOnce looks at the Store once and sends what is due. It returns how many
// emails it claimed. Use it instead of Run to send from a scheduled job.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	now := w.o.Now()
	if err := w.o.Store.ResetStale(ctx, now.Add(-w.o.StaleAfter)); err != nil {
		w.log.ErrorContext(ctx, "outbox: cannot reset the stale emails", "error", err)
	}
	items, err := w.o.Store.Claim(ctx, w.o.ID, w.o.BatchSize, now)
	if err != nil {
		return 0, fmt.Errorf("outbox: claiming: %w", err)
	}
	for i, item := range items {
		if ctx.Err() != nil {
			return i, nil
		}
		w.deliver(ctx, item)
	}
	return len(items), nil
}

func (w *Worker) deliver(ctx context.Context, item Item) {
	msg, err := w.message(item)
	if err == nil {
		err = w.o.Sender.Send(ctx, msg)
	}
	// A stop must not lose what was decided about an email that was already
	// sent: the Store is called without the cancellation, with its own limit.
	done, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	switch {
	case err == nil:
		if err := w.o.Store.MarkSent(done, item.ID); err != nil {
			w.log.ErrorContext(ctx, "outbox: the email was sent but cannot be marked", "id", item.ID, "error", err)
		}
	case ctx.Err() != nil:
		// Stopped while sending: not a failure of the email. It stays claimed.
	default:
		attempts := item.Attempts + 1
		var next *time.Time
		if w.o.Retry(err) && attempts < w.o.MaxAttempts {
			t := w.o.Now().Add(w.o.Backoff(attempts))
			next = &t
		}
		w.log.ErrorContext(ctx, "outbox: cannot send the email", "id", item.ID, "attempts", attempts, "will_retry", next != nil, "error", err)
		if err := w.o.Store.MarkFailed(done, item.ID, err.Error(), next); err != nil {
			w.log.ErrorContext(ctx, "outbox: cannot mark the email as failed", "id", item.ID, "error", err)
		}
	}
}

// message reads an Item back. What cannot be read is the same next time, so
// the errors are permanent.
func (w *Worker) message(item Item) (mail.Message, error) {
	var envelope mail.Envelope
	if err := json.Unmarshal(item.Envelope, &envelope); err != nil {
		return mail.Message{}, Permanent(fmt.Errorf("outbox: envelope of email %d: %w", item.ID, err))
	}
	payload, err := w.o.Payloads.DecodePayload(item.Type, func(into any) error {
		return json.Unmarshal(item.Payload, into)
	})
	if err != nil {
		return mail.Message{}, Permanent(fmt.Errorf("outbox: payload of email %d: %w", item.ID, err))
	}
	return mail.Message{Envelope: envelope, Lang: item.Lang, Payload: payload}, nil
}

// DefaultBackoff is the wait after each failed attempt: 30 seconds, 5
// minutes, 30 minutes and then 2 hours.
func DefaultBackoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return 30 * time.Second
	case 2:
		return 5 * time.Minute
	case 3:
		return 30 * time.Minute
	}
	return 2 * time.Hour
}

// Permanent marks an error as one that trying again does not fix: the
// DefaultRetry does not retry it. An error that has a Permanent() bool method
// that returns true is the same.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err}
}

type permanentError struct{ err error }

func (e permanentError) Error() string   { return e.err.Error() }
func (e permanentError) Unwrap() error   { return e.err }
func (e permanentError) Permanent() bool { return true }

// authCodes are the answers of a server to a login that fail because of how the
// program is set up, not because of the email: once fixed, the email goes out.
var authCodes = map[int]bool{530: true, 534: true, 535: true, 538: true}

// DefaultRetry says an error is worth another attempt unless it is permanent:
//   - an error marked with Permanent, or with a Permanent() bool method;
//   - a 5xx answer of the SMTP server, except the ones about logging in (535...);
//   - an invalid message (mail.ErrInvalid) or a missing or forbidden file.
//
// Everything else is retried: a 4xx answer, a timeout, a connection that fails.
func DefaultRetry(err error) bool {
	var p interface{ Permanent() bool }
	if errors.As(err, &p) && p.Permanent() {
		return false
	}
	var smtpErr *textproto.Error
	if errors.As(err, &smtpErr) && smtpErr.Code >= 500 && !authCodes[smtpErr.Code] {
		return false
	}
	return !errors.Is(err, mail.ErrInvalid) && !errors.Is(err, files.ErrNotFound) && !errors.Is(err, files.ErrAccessDenied)
}
