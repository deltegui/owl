package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/deltegui/owl/v3/mail"
)

// ErrUnknownPayload is returned by Sender.Send when the type of the payload is
// not known to Payloads: it is found when the email is saved, not later, when
// nobody is looking.
var ErrUnknownPayload = errors.New("outbox: unknown payload type")

// Sender is a mail.Sender that does not send: it keeps the email in a Store
// and a Worker sends it later. A nil error means the email was saved.
//
// The payload is kept as JSON, so only its exported fields arrive. The
// attachments are kept as paths, and the files must be there when the email is
// sent, which can be hours later if it is retried: the Temp directory must not
// expire them before that (see Worker).
type Sender struct {
	store    Store
	payloads Payloads
	wake     func()
}

var _ mail.Sender = (*Sender)(nil)

// SenderOption changes how a Sender works.
type SenderOption func(*Sender)

// WithWake sets a function called after an email is saved, usually Worker.Wake
// when the worker runs in the same program, so the email does not wait for the
// next poll.
func WithWake(wake func()) SenderOption { return func(s *Sender) { s.wake = wake } }

// NewSender creates a Sender.
func NewSender(store Store, payloads Payloads, opts ...SenderOption) *Sender {
	s := &Sender{store: store, payloads: payloads}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Sender) Send(ctx context.Context, msg mail.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	name, ok := s.payloads.PayloadName(msg.Payload)
	if !ok {
		return fmt.Errorf("%w: %T", ErrUnknownPayload, msg.Payload)
	}
	payload, err := json.Marshal(msg.Payload)
	if err != nil {
		return fmt.Errorf("outbox: payload %T: %w", msg.Payload, err)
	}
	envelope, err := json.Marshal(msg.Envelope)
	if err != nil {
		return fmt.Errorf("outbox: envelope: %w", err)
	}
	if err := s.store.Save(ctx, Entry{Type: name, Payload: payload, Envelope: envelope, Lang: msg.Lang}); err != nil {
		return fmt.Errorf("outbox: saving: %w", err)
	}
	if s.wake != nil {
		s.wake()
	}
	return nil
}
