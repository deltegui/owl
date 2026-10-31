// Package queue is a mail.Sender that sends in the background, from memory: Send
// puts the email in a bounded queue and some goroutines give it to another
// mail.Sender (usually a mail.Service), so the caller does not wait for the
// mail server.
//
// Nothing is kept anywhere. A nil error from Send means "queued": if the
// program dies the emails that were waiting are lost, and nobody retries the
// ones that fail. It is for notices that can be lost. For what a person waits
// for and something depends on (confirming an account, a new password, a
// verification code) use package outbox, which keeps the email in a database
// and retries it.
package queue

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/deltegui/owl/v3/logx"
	"github.com/deltegui/owl/v3/mail"
)

var (
	// ErrClosed is returned by Send once Close was called.
	ErrClosed = errors.New("queue: closed")
	// ErrNotDrained is returned by Close when it could not send everything before
	// the deadline. It is wrapped with how many emails were left.
	ErrNotDrained = errors.New("queue: emails left unsent")
	// ErrDiscarded is what OnError receives for an email that was queued and was
	// dropped because Close ran out of time.
	ErrDiscarded = errors.New("queue: discarded when closing")
)

// Options configures a Queue.
type Options struct {
	// Buffer is how many emails can wait. 100 if it is zero. When it is full,
	// Send waits for room.
	Buffer int
	// Workers is how many emails are sent at the same time. 1 if it is zero.
	Workers int
	// Timeout is the limit for sending one email. 1 minute if it is zero.
	Timeout time.Duration
	// OnError is called, from a worker, with each email that could not be sent
	// and why. By default it logs the error, with the number of recipients and
	// not their addresses. It must not block for long.
	OnError func(msg mail.Message, err error)
	// Logger is used by the default OnError. logx.Default{} if nil.
	Logger logx.Logger
}

// Queue sends the emails in the background. Create it with New and call
// Close when the program stops. It is safe for concurrent use.
type Queue struct {
	sender  mail.Sender
	timeout time.Duration
	onError func(mail.Message, error)

	ch    chan mail.Message
	slots chan struct{} // one per email that is waiting: it limits the queue

	ctx    context.Context // of the deliveries, not of Send
	cancel context.CancelFunc

	mu      sync.Mutex
	closed  bool
	closing chan struct{} // closed with the queue
	stop    chan struct{} // closed when Close gives up waiting
	stopped sync.Once

	workers  sync.WaitGroup
	inflight atomic.Int32
}

var _ mail.Sender = (*Queue)(nil)

// New creates a Queue and starts its workers. It panics if sender is nil.
func New(sender mail.Sender, o Options) *Queue {
	if sender == nil {
		panic("queue: New with a nil sender")
	}
	if o.Buffer <= 0 {
		o.Buffer = 100
	}
	if o.Workers <= 0 {
		o.Workers = 1
	}
	if o.Timeout <= 0 {
		o.Timeout = time.Minute
	}
	if o.Logger == nil {
		o.Logger = logx.Default{}
	}
	if o.OnError == nil {
		log := o.Logger
		o.OnError = func(msg mail.Message, err error) {
			log.Error("queue: cannot send the email", "payload", fmt.Sprintf("%T", msg.Payload),
				"recipients", len(msg.Envelope.To)+len(msg.Envelope.Cc)+len(msg.Envelope.Bcc), "error", err)
		}
	}
	q := &Queue{
		sender:  sender,
		timeout: o.Timeout,
		onError: o.OnError,
		ch:      make(chan mail.Message, o.Buffer),
		slots:   make(chan struct{}, o.Buffer),
		closing: make(chan struct{}),
		stop:    make(chan struct{}),
	}
	q.ctx, q.cancel = context.WithCancel(context.Background())
	q.workers.Add(o.Workers)
	for i := 0; i < o.Workers; i++ {
		go q.work()
	}
	return q
}

// Send checks the message and queues it. The error of a message that is not
// valid (mail.ErrInvalid) comes back here, to the caller, not later. If the
// queue is full it waits for room until ctx is done, so give it a context with a
// limit. The ctx only covers that wait: the email is sent with the context of
// the queue, so a request that ends does not cancel it.
//
// The message is copied, but its payload is not: if it holds a pointer, do not
// change what it points to after Send.
func (q *Queue) Send(ctx context.Context, msg mail.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	msg.Envelope = cloneEnvelope(msg.Envelope)

	// A slot first, without the lock: waiting for room must not stop Close.
	select {
	case q.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-q.closing:
		return ErrClosed
	}
	// There is room in ch for every slot, so this does not block, and checking
	// that the queue is open and queuing is one step: nothing gets in after Close.
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		<-q.slots
		return ErrClosed
	}
	q.ch <- msg
	return nil
}

func cloneEnvelope(e mail.Envelope) mail.Envelope {
	e.To, e.Cc, e.Bcc = slices.Clone(e.To), slices.Clone(e.Cc), slices.Clone(e.Bcc)
	e.ReplyTo, e.Attachments = slices.Clone(e.ReplyTo), slices.Clone(e.Attachments)
	return e
}

// Close stops accepting emails (Send returns ErrClosed) and sends the ones that
// are waiting, until ctx is done. If it runs out of time it cancels the
// emails being sent, gives the ones that were still waiting to OnError with
// ErrDiscarded and returns ErrNotDrained, wrapped with how many were left. The
// sender must respect the context it receives, or those sends are not
// stopped. It can be called more than once.
func (q *Queue) Close(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.closing)
	}
	q.mu.Unlock()

	done := make(chan struct{})
	go func() { q.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}

	q.stopped.Do(func() { close(q.stop) })
	q.cancel()
	left := int(q.inflight.Load())
	for {
		select {
		case msg := <-q.ch:
			<-q.slots
			q.onError(msg, ErrDiscarded)
			left++
			continue
		default:
		}
		break
	}
	if left == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d", ErrNotDrained, left)
}

func (q *Queue) work() {
	defer q.workers.Done()
	for {
		select {
		case <-q.stop:
			return
		default:
		}
		select {
		case msg := <-q.ch:
			<-q.slots
			q.deliver(msg)
		case <-q.stop:
			return
		case <-q.closing:
			// Nothing can be queued anymore: take what is left, then stop.
			select {
			case msg := <-q.ch:
				<-q.slots
				q.deliver(msg)
			default:
				return
			}
		}
	}
}

// deliver sends one email. A panic of the sender is an error of that email: it
// must not take the program down from a goroutine nobody is watching.
func (q *Queue) deliver(msg mail.Message) {
	q.inflight.Add(1)
	defer q.inflight.Add(-1)
	defer func() {
		if r := recover(); r != nil {
			q.onError(msg, fmt.Errorf("queue: the sender panicked: %v", r))
		}
	}()
	ctx, cancel := context.WithTimeout(q.ctx, q.timeout)
	defer cancel()
	if err := q.sender.Send(ctx, msg); err != nil {
		q.onError(msg, err)
	}
}
