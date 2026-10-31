package outbox_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/mail"
	"github.com/deltegui/owl/v3/mail/hxmail"
	"github.com/deltegui/owl/v3/mail/outbox"
)

type confirmation struct {
	Name string
	Link string
	note string // not exported: it does not travel through the store
}

type reset struct{ Code string }

type driver struct {
	mu  sync.Mutex
	got []mail.Request
}

func (d *driver) Send(_ context.Context, r mail.Request, _ files.Temp) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got = append(d.got, r)
	return nil
}

// The web part keeps the email with the Renderer as Payloads; the worker, which
// could be another program, renders and sends it with a Service over the same
// Renderer.
func TestOutboxWithTheHxmailRenderer(t *testing.T) {
	d := &driver{}
	renderer := hxmail.New(hxmail.WithDefaultLang("es"))
	service := mail.NewService(renderer, d, files.Temp{})
	texts := func(subject string) localizer.Dict {
		return localizer.Dict{"es": {"Subject": subject + " (es)"}, "en": {"Subject": subject + " (en)"}}
	}
	hxmail.Register(renderer, hxmail.Template[confirmation]{
		Texts: texts("Confirm"),
		View: func(p confirmation, t localizer.Localizer) hx.INode {
			return hx.P().Textf("Hi %s %s", p.Name, p.note).Body(hx.A().Href(p.Link).Text("go"))
		},
	})
	hxmail.Register(renderer, hxmail.Template[reset]{
		Name:  "account-reset", // does not depend on the Go name
		Texts: texts("Reset"),
		View:  func(p reset, t localizer.Localizer) hx.INode { return hx.P().Text(p.Code) },
	})

	store := outbox.NewMemoryStore(nil)
	worker, err := outbox.NewWorker(outbox.Options{Store: store, Sender: service, Payloads: renderer, PollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	sender := outbox.NewSender(store, renderer, outbox.WithWake(worker.Wake))
	ctx := context.Background()

	to := mail.Envelope{To: []string{"ana@example.com"}}
	if err := sender.Send(ctx, mail.Message{Envelope: to, Lang: "en", Payload: &confirmation{Name: "Ana", Link: "https://example.com/c", note: "lost"}}); err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(ctx, mail.Message{Envelope: to, Payload: reset{Code: "123456"}}); err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(ctx, mail.Message{Envelope: to, Payload: struct{}{}}); err == nil {
		t.Fatal("a payload without template must fail when it is saved")
	}
	if got := store.Snapshot(); len(got) != 2 || !strings.HasSuffix(got[0].Type, ".confirmation") || got[1].Type != "account-reset" {
		t.Fatalf("stored as %+v", got)
	}

	if n, err := worker.RunOnce(ctx); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	first, second := d.got[0], d.got[1]
	if first.Subject != "Confirm (en)" || !strings.Contains(first.HTML, `Hi Ana `) || !strings.Contains(first.HTML, `href="https://example.com/c"`) {
		t.Errorf("first: %q %q", first.Subject, first.HTML)
	}
	if strings.Contains(first.HTML, "lost") {
		t.Error("an unexported field went through the store")
	}
	if second.Subject != "Reset (es)" || second.HTML != "<p>123456</p>" {
		t.Errorf("second: %q %q", second.Subject, second.HTML)
	}
	for _, r := range store.Snapshot() {
		if r.Status != outbox.Sent {
			t.Errorf("%+v", r)
		}
	}
}
