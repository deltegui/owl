// Package mail is the contract between the code that needs to send an email
// and the code that sends it. A service builds a Message and gives it to a
// Sender; it does not know if the email goes out right away, through a queue
// or through a database table, nor how it is rendered or transported.
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
)

// ErrInvalid is wrapped by the errors of Message.Validate.
var ErrInvalid = errors.New("mail: invalid message")

// Sender sends a Message. It is the only thing a service must know about
// mailing. What a nil error means depends on the implementation: the email
// was sent, or it was accepted to be sent later.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Message is an email to send.
type Message struct {
	Envelope Envelope
	// Lang is the language of the email, as the names of package localizer
	// (for example localizer.LangSpanish). Empty is the default language.
	Lang string
	// Payload has the data of the email. Its type decides which email it is
	// (which template and subject): for example a UserConfirmation struct with
	// the user and the link. It must be a pointer or a struct, never nil.
	Payload Payload
}

// Payload is the data of one kind of email. Any value can be one, the
// implementations that render the email register what they do for each type.
type Payload any

// Envelope says who receives the email and what goes with it.
type Envelope struct {
	To      []string
	Cc      []string
	Bcc     []string
	ReplyTo []string
	// Attachments are relative paths in the files.Temp directory. The one that
	// sends the email only receives the paths, not the content.
	Attachments []string
}

// Validate checks what every Sender needs: a payload, at least one
// recipient and addresses that are well formed. Besides the syntax, this
// rejects line breaks in the addresses, so they cannot add headers to the
// email. The attachments are checked when they are opened.
func (m Message) Validate() error {
	if m.Payload == nil {
		return fmt.Errorf("%w: no payload", ErrInvalid)
	}
	e := m.Envelope
	if len(e.To)+len(e.Cc)+len(e.Bcc) == 0 {
		return fmt.Errorf("%w: no recipients", ErrInvalid)
	}
	for _, list := range []struct {
		name      string
		addresses []string
	}{{"To", e.To}, {"Cc", e.Cc}, {"Bcc", e.Bcc}, {"ReplyTo", e.ReplyTo}} {
		for _, a := range list.addresses {
			if err := validAddress(a); err != nil {
				return fmt.Errorf("%w: %s %q: %v", ErrInvalid, list.name, a, err)
			}
		}
	}
	return nil
}

func validAddress(a string) error {
	for _, r := range a {
		if r == '\r' || r == '\n' {
			return errors.New("line break")
		}
	}
	_, err := mail.ParseAddress(a)
	return err
}
