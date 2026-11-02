package smtp

import (
	"context"
	"errors"
	"net/textproto"
	"strings"
	"testing"
	"time"

	netsmtp "net/smtp"

	"github.com/deltegui/owl/v3/files"
)

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not happen", what)
}

// Each step of the dialogue that the server refuses says which one it was and
// keeps the code of the server, and no mail is kept.
func TestEveryRefusedStepNamesItselfAndKeepsTheCode(t *testing.T) {
	// For "hello" both EHLO and HELO are refused: net/smtp falls back to HELO.
	cases := map[string]struct {
		set     func(*testServer)
		options func(*Options)
		prefix  string
		code    int
	}{
		"hello": {func(s *testServer) { s.refuse = map[string]string{"EHLO": "554 no service", "HELO": "554 no service"} }, nil, "smtp: hello", 554},
		"starttls": {func(s *testServer) {
			s.startTLS = true
			s.refuse = map[string]string{"STARTTLS": "454 tls not available"}
		}, func(o *Options) { o.Security = StartTLS }, "smtp: starttls", 454},
		"sender":  {func(s *testServer) { s.refuse = map[string]string{"MAIL": "550 sender refused"} }, nil, "smtp: sender", 550},
		"data":    {func(s *testServer) { s.refuse = map[string]string{"DATA": "554 no data"} }, nil, "smtp: data", 554},
		"the end": {func(s *testServer) { s.refuseEnd = "554 message rejected" }, nil, "smtp: the server did not accept the message", 554},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newServer(t, tc.set)
			d := newDriver(t, s, tc.options)
			err := d.Send(context.Background(), simple(), files.Temp{})
			var perr *textproto.Error
			if err == nil || !strings.Contains(err.Error(), tc.prefix) || !errors.As(err, &perr) || perr.Code != tc.code {
				t.Fatalf("want %q with code %d, got %v", tc.prefix, tc.code, err)
			}
			if s.count() != 0 {
				t.Fatal("a mail was kept")
			}
		})
	}
}

func TestTheRecipientErrorNamesTheAddress(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.rejectRcpt = "bad@example.com" })
	d := newDriver(t, s, nil)
	req := simple()
	req.Envelope.To = []string{"bad@example.com"}
	if err := d.Send(context.Background(), req, files.Temp{}); err == nil || !strings.Contains(err.Error(), "recipient bad@example.com") {
		t.Fatalf("got %v", err)
	}
}

func TestAcceptedMailEndsWithQuit(t *testing.T) {
	s := newServer(t, nil)
	d := newDriver(t, s, nil)
	if err := d.Send(context.Background(), simple(), files.Temp{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the server getting QUIT", func() bool { return s.quits.Load() == 1 })
}

func TestAConnectionThatCannotBeUsedIsClosed(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.greeting = "554 no service for you" })
	d := newDriver(t, s, nil)
	err := d.Send(context.Background(), simple(), files.Temp{})
	if err == nil || !strings.Contains(err.Error(), "smtp: greeting") {
		t.Fatalf("got %v", err)
	}
	eventually(t, "the server seeing the connection closed", func() bool { return s.ended.Load() == 1 })
}

func TestAuthenticationWhenTheServerOffersNone(t *testing.T) {
	s := newServer(t, nil) // no AUTH extension
	d := newDriver(t, s, func(o *Options) { o.Username, o.Password = "ana", "secret" })
	err := d.Send(context.Background(), simple(), files.Temp{})
	if err == nil || !strings.Contains(err.Error(), "smtp: authentication") || !strings.Contains(err.Error(), "does not offer authentication") {
		t.Fatalf("with a user but no AUTH it must not send without authenticating: %v", err)
	}
	if s.count() != 0 {
		t.Fatal("a mail was sent without authenticating")
	}
}

func TestAuthenticationWithAMechanismWeDoNotSpeak(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.auth = "CRAM-MD5 XOAUTH2" })
	d := newDriver(t, s, func(o *Options) { o.Username, o.Password = "ana", "secret" })
	err := d.Send(context.Background(), simple(), files.Temp{})
	if err == nil || !strings.Contains(err.Error(), "PLAIN or LOGIN are needed") {
		t.Fatalf("got %v", err)
	}
}

func TestLoginAuthOnlyOverEncryptionOrLocalhost(t *testing.T) {
	a := loginAuth{"ana", "secret"}
	for name, want := range map[string]bool{
		"localhost":   true,
		"127.0.0.1":   true,
		"::1":         true,
		"example.com": false,
		"127.0.0.2":   false,
		"":            false,
	} {
		_, _, err := a.Start(&netsmtp.ServerInfo{Name: name, TLS: false})
		if (err == nil) != want {
			t.Errorf("host %q without TLS: error %v, want ok=%v", name, err, want)
		}
		if _, _, err := a.Start(&netsmtp.ServerInfo{Name: name, TLS: true}); err != nil {
			t.Errorf("host %q with TLS: %v", name, err)
		}
	}
}

func TestLoginAuthAnswersItsChallenges(t *testing.T) {
	a := loginAuth{"ana", "secret"}
	for challenge, want := range map[string]string{"Username:": "ana", "username": "ana", "PASSWORD:": "secret"} {
		got, err := a.Next([]byte(challenge), true)
		if err != nil || string(got) != want {
			t.Errorf("%q: %q, %v", challenge, got, err)
		}
	}
	if _, err := a.Next([]byte("Surprise:"), true); err == nil {
		t.Error("an unknown challenge must fail")
	}
	if got, err := a.Next(nil, false); got != nil || err != nil {
		t.Errorf("nothing more to answer: %q, %v", got, err)
	}
}

func TestNewFillsInTheDefaults(t *testing.T) {
	before := time.Now()
	d, err := New(Options{Host: "h", Sender: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if d.o.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v", d.o.Timeout)
	}
	if d.o.Hostname != "localhost" {
		t.Errorf("Hostname = %q", d.o.Hostname)
	}
	if d.o.Now == nil || d.o.Now().Before(before) || d.o.Now().After(time.Now()) {
		t.Error("Now is the clock by default")
	}
	d, err = New(Options{Host: "h", Sender: "a@example.com", Timeout: time.Second, Hostname: "mail.example.com"})
	if err != nil || d.o.Timeout != time.Second || d.o.Hostname != "mail.example.com" {
		t.Errorf("what is set stays: %+v %v", d.o, err)
	}
}

func TestNewRefusesALineBreakInTheSender(t *testing.T) {
	_, err := New(Options{Host: "h", Sender: "a@example.com\r\nBcc: victim@example.com"})
	if err == nil || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("got %v", err)
	}
}
