package mail

import (
	"context"

	"github.com/deltegui/owl/v3/files"
)

// Driver transports an email that is already rendered: it does not decide
// what it says, only how it goes out (SMTP, an HTTP API of a provider...).
// The engine that renders the emails calls it; the drivers live in their own
// packages (see mail/smtp).
//
// The driver reads the attachments and the inline resources from tmp, with
// the relative paths of the Request: it does not touch the file system.
type Driver interface {
	Send(ctx context.Context, req Request, tmp files.Temp) error
}

// Request is an email ready to be transported.
type Request struct {
	Subject string
	// HTML is the body.
	HTML     string
	Envelope Envelope
	// CIDs are the resources the HTML uses with "cid:" (a logo, for example):
	// the key is the content id and the value is the relative path in tmp, the
	// same as the attachments.
	CIDs map[string]string
	// Payload is the data the email was rendered from, for the drivers that
	// need it. Most do not.
	Payload Payload
}
