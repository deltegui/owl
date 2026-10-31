package mail

import (
	"context"
	"errors"
	"testing"

	"github.com/deltegui/owl/v3/files"
)

type userConfirmation struct{ Link string }

// recorder is a Sender for the tests of the code that sends emails.
type recorder struct{ sent []Message }

func (r *recorder) Send(_ context.Context, m Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	r.sent = append(r.sent, m)
	return nil
}

func TestServicesOnlyNeedASender(t *testing.T) {
	var s Sender = &recorder{}
	err := s.Send(context.Background(), Message{
		Envelope: Envelope{To: []string{"ana@example.com"}, Attachments: []string{"tmp/20261031/report.pdf"}},
		Payload:  userConfirmation{Link: "https://example.com/confirm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.(*recorder).sent; len(got) != 1 || got[0].Payload.(userConfirmation).Link == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestValidate(t *testing.T) {
	ok := Message{Envelope: Envelope{To: []string{"Ana <ana@example.com>"}}, Payload: &userConfirmation{}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	withBcc := Message{Envelope: Envelope{Bcc: []string{"a@example.com"}}, Payload: 1}
	if err := withBcc.Validate(); err != nil {
		t.Fatalf("only Bcc is enough: %v", err)
	}

	bad := map[string]Message{
		"no payload":     {Envelope: Envelope{To: []string{"a@example.com"}}},
		"no recipients":  {Envelope: Envelope{ReplyTo: []string{"a@example.com"}}, Payload: 1},
		"not an address": {Envelope: Envelope{To: []string{"nope"}}, Payload: 1},
		"empty address":  {Envelope: Envelope{To: []string{""}}, Payload: 1},
		"bad cc":         {Envelope: Envelope{To: []string{"a@example.com"}, Cc: []string{"x"}}, Payload: 1},
		"bad reply-to":   {Envelope: Envelope{To: []string{"a@example.com"}, ReplyTo: []string{"x"}}, Payload: 1},
		"header injection": {
			Envelope: Envelope{To: []string{"a@example.com\r\nBcc: victim@example.com"}}, Payload: 1},
		"line break": {Envelope: Envelope{To: []string{"a@example.com\n"}}, Payload: 1},
	}
	for name, m := range bad {
		if err := m.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// fakeDriver shows what a driver receives: the request and the Temp to open
// the files of the envelope.
type fakeDriver struct{ got Request }

func (d *fakeDriver) Send(ctx context.Context, req Request, tmp files.Temp) error {
	d.got = req
	for _, path := range req.Envelope.Attachments {
		r, err := tmp.Open(ctx, path)
		if err != nil {
			return err
		}
		r.Close()
	}
	return nil
}

func TestDriverReadsAttachmentsFromTemp(t *testing.T) {
	ctx := context.Background()
	local, err := files.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	tmp := files.AsTemp(local)
	path, err := files.WriteString(ctx, tmp, "report.pdf", "pdf")
	if err != nil {
		t.Fatal(err)
	}

	var d Driver = &fakeDriver{}
	req := Request{Subject: "Hi", HTML: "<p>Hi</p>", Envelope: Envelope{To: []string{"a@example.com"}, Attachments: []string{path}}}
	if err := d.Send(ctx, req, tmp); err != nil {
		t.Fatal(err)
	}
	req.Envelope.Attachments = []string{"nope.pdf"}
	if err := d.Send(ctx, req, tmp); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	req.Envelope.Attachments = []string{"../secret"}
	if err := d.Send(ctx, req, tmp); !errors.Is(err, files.ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
}
