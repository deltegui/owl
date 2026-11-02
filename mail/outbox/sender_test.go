package outbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/mail"
)

// knowsEverything names any payload, so what fails is turning it into JSON.
type knowsEverything struct{}

func (knowsEverything) PayloadName(any) (string, bool) { return "anything", true }
func (knowsEverything) DecodePayload(string, func(any) error) (any, error) {
	return nil, nil
}

func TestSenderSaysWhenThePayloadCannotBeJSON(t *testing.T) {
	store := NewMemoryStore(time.Now)
	s := NewSender(store, knowsEverything{})
	msg := mail.Message{
		Envelope: mail.Envelope{To: []string{"ana@example.com"}},
		Payload:  struct{ Callback func() }{Callback: func() {}},
	}
	err := s.Send(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), "outbox: payload") {
		t.Fatalf("got %v", err)
	}
	if len(store.Snapshot()) != 0 {
		t.Fatal("something was saved")
	}
}
