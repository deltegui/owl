package outbox

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by a Store when an id does not exist.
var ErrNotFound = errors.New("outbox: not found")

// Entry is an email kept to be sent later. The payload and the envelope are
// JSON, written and read by this package: a Store only keeps them (in two text
// or bytes columns, for example).
type Entry struct {
	// Type is the name of the payload, see Payloads.
	Type     string
	Payload  []byte
	Envelope []byte
	Lang     string
}

// Item is an Entry a Store has given to a Worker.
type Item struct {
	ID int64
	// Attempts is how many times it was tried and failed.
	Attempts int
	Entry
}

// Store keeps the emails until they are sent. The application implements it,
// usually over a table; MemoryStore is one for tests. An email is Pending until
// a worker claims it (InProgress) and then Sent or Failed, or Pending again to
// be retried.
//
// More than one Worker can use the same Store, in different processes: Claim
// must give each Item to only one of them.
type Store interface {
	// Save keeps a new email, Pending and due now. Its ctx is the one of the code
	// that sends the email, so a Store can use the transaction it carries (if the
	// application puts one there) and the email is saved, or not, together with
	// what caused it.
	Save(ctx context.Context, e Entry) error

	// ResetStale makes Pending again the emails that were claimed before the given
	// time and are still InProgress: their worker died.
	ResetStale(ctx context.Context, before time.Time) error

	// Claim takes up to limit Pending emails whose time to try has come (it is
	// not after now), oldest first, and marks them InProgress for the worker. It
	// must be atomic: in Postgres, an UPDATE ... WHERE id IN (SELECT ... FOR UPDATE
	// SKIP LOCKED) ... RETURNING.
	Claim(ctx context.Context, worker string, limit int, now time.Time) ([]Item, error)

	// MarkSent marks the email as Sent.
	MarkSent(ctx context.Context, id int64) error

	// MarkFailed counts a failed attempt and keeps the cause. If next is nil the
	// email is Failed and nobody tries it again; otherwise it is Pending and will
	// be claimed from that time.
	MarkFailed(ctx context.Context, id int64, cause string, next *time.Time) error
}

// Payloads tells names and values of the payloads, so an email can be kept with
// the name of its type and read back as the same type. hxmail.Renderer implements
// it: the types are registered once, with its templates.
type Payloads interface {
	// PayloadName is the name of the type of the payload. The bool is false if the
	// type is not known.
	PayloadName(payload any) (string, bool)
	// DecodePayload makes a value of the type with that name. It calls decode with
	// a pointer to it, to be filled (from JSON), and returns the value.
	DecodePayload(name string, decode func(into any) error) (any, error)
}
