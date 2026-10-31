package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	netsmtp "net/smtp"
	"net/textproto"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/mail"
)

// ---- a fake SMTP server ----------------------------------------------------

type received struct {
	from, user, pass string
	rcpts            []string
	data             string
	tls              bool
}

type testServer struct {
	startTLS   bool
	implicit   bool
	auth       string // mechanisms to offer: "PLAIN", "LOGIN", "PLAIN LOGIN" or ""
	rejectRcpt string
	hang       bool // accepts the connection and says nothing

	cert  tls.Certificate
	pool  *x509.CertPool
	ln    net.Listener
	done  chan struct{}
	conns atomic.Int32

	mu    sync.Mutex
	mails []received
}

func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func newServer(t *testing.T, set func(*testServer)) *testServer {
	t.Helper()
	s := &testServer{done: make(chan struct{})}
	if set != nil {
		set(s)
	}
	s.cert, s.pool = selfSigned(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	t.Cleanup(func() { close(s.done); ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(c)
		}
	}()
	return s
}

func (s *testServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *testServer) tlsConf() *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{s.cert}}
}

func (s *testServer) last(t *testing.T) received {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.mails) == 0 {
		t.Fatal("the server received no email")
	}
	return s.mails[len(s.mails)-1]
}

func (s *testServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mails)
}

func (s *testServer) handle(conn net.Conn) {
	defer func() { conn.Close() }()
	s.conns.Add(1)
	if s.hang {
		<-s.done
		return
	}
	tlsOn := false
	if s.implicit {
		conn = tls.Server(conn, s.tlsConf())
		tlsOn = true
	}
	r := textproto.NewReader(bufio.NewReader(conn))
	send := func(format string, a ...any) { fmt.Fprintf(conn, format+"\r\n", a...) }
	decode := func(b64 string) string {
		b, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		return string(b)
	}
	mechanism := func(m string) bool { return slices.Contains(strings.Fields(s.auth), m) }

	send("220 localhost ready")
	var cur received
	for {
		line, err := r.ReadLine()
		if err != nil {
			return
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			exts := []string{"8BITMIME"}
			if s.startTLS && !tlsOn {
				exts = append(exts, "STARTTLS")
			}
			if s.auth != "" {
				exts = append(exts, "AUTH "+s.auth)
			}
			send("250-localhost")
			for i, e := range exts {
				if i < len(exts)-1 {
					send("250-%s", e)
				} else {
					send("250 %s", e)
				}
			}
		case upper == "STARTTLS":
			send("220 go ahead")
			conn = tls.Server(conn, s.tlsConf())
			r = textproto.NewReader(bufio.NewReader(conn))
			tlsOn = true
			cur = received{}
		case strings.HasPrefix(upper, "AUTH PLAIN") && mechanism("PLAIN"):
			arg := strings.TrimSpace(line[len("AUTH PLAIN"):])
			if arg == "" {
				send("334 ")
				arg, _ = r.ReadLine()
			}
			parts := strings.Split(decode(arg), "\x00")
			if len(parts) == 3 {
				cur.user, cur.pass = parts[1], parts[2]
			}
			send("235 ok")
		case upper == "AUTH LOGIN" && mechanism("LOGIN"):
			send("334 VXNlcm5hbWU6")
			u, _ := r.ReadLine()
			send("334 UGFzc3dvcmQ6")
			p, _ := r.ReadLine()
			cur.user, cur.pass = decode(u), decode(p)
			send("235 ok")
		case strings.HasPrefix(upper, "AUTH"):
			send("504 mechanism not supported")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			cur.from = between(line)
			send("250 ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			to := between(line)
			if to == s.rejectRcpt {
				send("550 mailbox unavailable")
				continue
			}
			cur.rcpts = append(cur.rcpts, to)
			send("250 ok")
		case upper == "DATA":
			send("354 go on")
			var b strings.Builder
			for {
				l, err := r.ReadLine()
				if err != nil {
					return
				}
				if l == "." {
					break
				}
				b.WriteString(strings.TrimPrefix(l, ".") + "\r\n") // dot-unstuffing
			}
			cur.data, cur.tls = b.String(), tlsOn
			s.mu.Lock()
			s.mails = append(s.mails, cur)
			s.mu.Unlock()
			cur = received{user: cur.user, pass: cur.pass}
			send("250 queued")
		case upper == "QUIT":
			send("221 bye")
			return
		default:
			send("250 ok")
		}
	}
}

func between(line string) string {
	i, j := strings.Index(line, "<"), strings.LastIndex(line, ">")
	if i < 0 || j < i {
		return ""
	}
	return line[i+1 : j]
}

// ---- helpers ---------------------------------------------------------------

func newDriver(t *testing.T, s *testServer, set func(*Options)) *Driver {
	t.Helper()
	o := Options{
		Host:      "127.0.0.1",
		Port:      s.port(),
		Sender:    "Little Cash <no-reply@littlecash.example>",
		Security:  NoTLS,
		TLSConfig: &tls.Config{RootCAs: s.pool},
		Timeout:   5 * time.Second,
		Now:       func() time.Time { return time.Date(2026, 10, 31, 15, 30, 0, 0, time.UTC) },
	}
	if set != nil {
		set(&o)
	}
	d, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newTemp(t *testing.T) files.Temp {
	t.Helper()
	l, err := files.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return files.AsTemp(l)
}

func simple() mail.Request {
	return mail.Request{
		Subject:  "Hola",
		HTML:     "<p>Hola</p>",
		Envelope: mail.Envelope{To: []string{"ana@example.com"}},
	}
}

// entityOf is a part of the received message, decoded.
type entityOf struct {
	ctype  string
	header textproto.MIMEHeader
	body   []byte
	parts  []entityOf
}

func parseEntity(t *testing.T, h textproto.MIMEHeader, body io.Reader) entityOf {
	t.Helper()
	e := entityOf{header: h}
	mt, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		t.Fatalf("Content-Type %q: %v", h.Get("Content-Type"), err)
	}
	e.ctype = mt
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextPart() // it decodes quoted-printable by itself
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			e.parts = append(e.parts, parseEntity(t, p.Header, p))
		}
		return e
	}
	switch strings.ToLower(h.Get("Content-Transfer-Encoding")) {
	case "base64":
		body = base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		body = quotedprintable.NewReader(body)
	}
	e.body, err = io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func parseMessage(t *testing.T, data string) (netmail.Header, entityOf) {
	t.Helper()
	m, err := netmail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return m.Header, parseEntity(t, textproto.MIMEHeader(m.Header), m.Body)
}

func contains(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("%v does not contain %q", got, w)
		}
	}
}

// ---- tests -----------------------------------------------------------------

func TestSendSimpleEmail(t *testing.T) {
	s := newServer(t, nil)
	d := newDriver(t, s, nil)
	req := simple()
	req.HTML = "<p>Línea con acento: ñ</p>\n.punto al inicio\n<p>fin</p>"
	if err := d.Send(context.Background(), req, files.Temp{}); err != nil {
		t.Fatal(err)
	}
	got := s.last(t)
	if got.from != "no-reply@littlecash.example" {
		t.Errorf("envelope sender %q", got.from)
	}
	if len(got.rcpts) != 1 || got.rcpts[0] != "ana@example.com" {
		t.Errorf("rcpts %v", got.rcpts)
	}

	h, e := parseMessage(t, got.data)
	if e.ctype != "text/html" || len(e.parts) != 0 {
		t.Fatalf("a plain email must be a single html part: %s, %d parts", e.ctype, len(e.parts))
	}
	// SMTP ends the last line itself: the server sees a CRLF more.
	if want := "<p>Línea con acento: ñ</p>\r\n.punto al inicio\r\n<p>fin</p>"; strings.TrimSuffix(string(e.body), "\r\n") != want {
		t.Errorf("body %q, want %q", e.body, want)
	}
	if h.Get("From") != `"Little Cash" <no-reply@littlecash.example>` {
		t.Errorf("From %q", h.Get("From"))
	}
	if h.Get("To") != "<ana@example.com>" || h.Get("Mime-Version") != "1.0" {
		t.Errorf("headers %v", h)
	}
	if h.Get("Date") != "Sat, 31 Oct 2026 15:30:00 +0000" {
		t.Errorf("Date %q", h.Get("Date"))
	}
	if id := h.Get("Message-Id"); !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@littlecash.example>") {
		t.Errorf("Message-ID %q", id)
	}
	if h.Get("Cc") != "" || h.Get("Reply-To") != "" {
		t.Errorf("empty headers must not be written: %v", h)
	}
	for _, line := range strings.Split(got.data, "\r\n") {
		if len(line) > 998 {
			t.Errorf("line too long: %d", len(line))
		}
	}
}

func TestMessageIDsAreUnique(t *testing.T) {
	s := newServer(t, nil)
	d := newDriver(t, s, nil)
	ids := map[string]bool{}
	for i := 0; i < 3; i++ {
		if err := d.Send(context.Background(), simple(), files.Temp{}); err != nil {
			t.Fatal(err)
		}
		h, _ := parseMessage(t, s.last(t).data)
		ids[h.Get("Message-Id")] = true
	}
	if len(ids) != 3 {
		t.Fatalf("repeated ids: %v", ids)
	}
}

func TestSendWithRecipientsAndSubject(t *testing.T) {
	s := newServer(t, nil)
	d := newDriver(t, s, nil)
	req := mail.Request{
		Subject: "Confirmación de tu cuenta ñandú — " + strings.Repeat("largo ", 30),
		HTML:    "<p>Hola</p>",
		Envelope: mail.Envelope{
			To:      []string{"Ana García <ana@example.com>", "b@example.com"},
			Cc:      []string{"c@example.com"},
			Bcc:     []string{"hidden@example.com"},
			ReplyTo: []string{"reply@example.com"},
		},
	}
	if err := d.Send(context.Background(), req, files.Temp{}); err != nil {
		t.Fatal(err)
	}
	got := s.last(t)
	contains(t, got.rcpts, "ana@example.com", "b@example.com", "c@example.com", "hidden@example.com")

	h, _ := parseMessage(t, got.data)
	if strings.Contains(strings.ToLower(got.data), "hidden@example.com") {
		t.Error("the Bcc address is in the message")
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(h.Get("Subject"))
	if err != nil || subject != req.Subject {
		t.Errorf("subject %q (%v)", subject, err)
	}
	to, err := h.AddressList("To")
	if err != nil || len(to) != 2 || to[0].Name != "Ana García" || to[1].Address != "b@example.com" {
		t.Errorf("To %v (%v)", to, err)
	}
	if h.Get("Cc") != "<c@example.com>" || h.Get("Reply-To") != "<reply@example.com>" {
		t.Errorf("Cc %q, Reply-To %q", h.Get("Cc"), h.Get("Reply-To"))
	}
}

func TestSendWithAttachmentsAndInlineResources(t *testing.T) {
	s := newServer(t, nil)
	d := newDriver(t, s, nil)
	tmp := newTemp(t)
	ctx := context.Background()

	pdf := make([]byte, 5000) // every byte value, and more than a few lines of base64
	for i := range pdf {
		pdf[i] = byte(i)
	}
	pdfPath, err := files.WriteBytes(ctx, tmp, "docs/Factura ñ.pdf", pdf)
	if err != nil {
		t.Fatal(err)
	}
	txtPath, _ := files.WriteString(ctx, tmp, "notas.txt", "notas")
	logoPath, _ := files.WriteString(ctx, tmp, "logo.png", "PNGDATA")

	req := simple()
	req.HTML = `<img src="cid:logo">`
	req.Envelope.Attachments = []string{pdfPath, txtPath}
	req.CIDs = map[string]string{"logo": logoPath}
	if err := d.Send(ctx, req, tmp); err != nil {
		t.Fatal(err)
	}

	_, e := parseMessage(t, s.last(t).data)
	if e.ctype != "multipart/mixed" || len(e.parts) != 3 {
		t.Fatalf("want multipart/mixed with 3 parts, got %s with %d", e.ctype, len(e.parts))
	}
	related := e.parts[0]
	if related.ctype != "multipart/related" || len(related.parts) != 2 {
		t.Fatalf("want multipart/related with 2 parts, got %s with %d", related.ctype, len(related.parts))
	}
	if related.parts[0].ctype != "text/html" || string(related.parts[0].body) != `<img src="cid:logo">` {
		t.Errorf("html part: %s %q", related.parts[0].ctype, related.parts[0].body)
	}
	logo := related.parts[1]
	if logo.header.Get("Content-ID") != "<logo>" || logo.ctype != "image/png" || string(logo.body) != "PNGDATA" {
		t.Errorf("inline: %v %q", logo.header, logo.body)
	}
	if d, _, _ := mime.ParseMediaType(logo.header.Get("Content-Disposition")); d != "inline" {
		t.Errorf("disposition %q", d)
	}

	att := e.parts[1]
	disp, params, err := mime.ParseMediaType(att.header.Get("Content-Disposition"))
	if err != nil || disp != "attachment" || params["filename"] != "Factura ñ.pdf" {
		t.Errorf("disposition %q %v (%v)", disp, params, err)
	}
	if att.ctype != "application/pdf" || !bytes.Equal(att.body, pdf) {
		t.Errorf("pdf: %s, %d bytes (want %d)", att.ctype, len(att.body), len(pdf))
	}
	if e.parts[2].ctype != "text/plain" || string(e.parts[2].body) != "notas" {
		t.Errorf("txt: %s %q", e.parts[2].ctype, e.parts[2].body)
	}
}

func TestMissingFilesDoNotConnect(t *testing.T) {
	s := newServer(t, nil)
	d := newDriver(t, s, nil)
	tmp := newTemp(t)
	ctx := context.Background()

	req := simple()
	req.Envelope.Attachments = []string{"nope.pdf"}
	if err := d.Send(ctx, req, tmp); !errors.Is(err, files.ErrNotFound) {
		t.Errorf("attachment: %v", err)
	}
	req.Envelope.Attachments = []string{"../secret"}
	if err := d.Send(ctx, req, tmp); !errors.Is(err, files.ErrAccessDenied) {
		t.Errorf("climbing path: %v", err)
	}
	req.Envelope.Attachments = nil
	req.CIDs = map[string]string{"logo": "nope.png"}
	if err := d.Send(ctx, req, tmp); !errors.Is(err, files.ErrNotFound) {
		t.Errorf("resource: %v", err)
	}
	req.Envelope.Attachments = []string{"x"}
	if err := d.Send(ctx, req, files.Temp{}); err == nil {
		t.Error("files without a Temp must fail")
	}
	if s.conns.Load() != 0 {
		t.Fatalf("the server was called %d times", s.conns.Load())
	}
}

func TestHeaderInjectionIsRejected(t *testing.T) {
	s := newServer(t, nil)
	d := newDriver(t, s, nil)
	bad := map[string]func(*mail.Request){
		"subject":  func(r *mail.Request) { r.Subject = "Hi\r\nBcc: victim@example.com" },
		"to":       func(r *mail.Request) { r.Envelope.To = []string{"a@example.com\r\nBcc: v@example.com"} },
		"cc":       func(r *mail.Request) { r.Envelope.Cc = []string{"a@example.com\n"} },
		"reply-to": func(r *mail.Request) { r.Envelope.ReplyTo = []string{"a@example.com\r\nX: y"} },
		"cid":      func(r *mail.Request) { r.CIDs = map[string]string{"a>\r\nX: y": "f"} },
		"none":     func(r *mail.Request) { r.Envelope.To = nil },
		"invalid":  func(r *mail.Request) { r.Envelope.To = []string{"not an address"} },
	}
	for name, mutate := range bad {
		req := simple()
		mutate(&req)
		if err := d.Send(context.Background(), req, newTemp(t)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if s.conns.Load() != 0 {
		t.Fatalf("the server was called %d times", s.conns.Load())
	}
	if _, err := New(Options{Host: "h", Sender: "a@example.com\r\nBcc: v@example.com"}); err == nil {
		t.Error("Sender with a line break")
	}
}

func TestAuthPlain(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.auth = "PLAIN LOGIN" })
	d := newDriver(t, s, func(o *Options) { o.Username, o.Password = "user", "p@ss" })
	if err := d.Send(context.Background(), simple(), files.Temp{}); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t); got.user != "user" || got.pass != "p@ss" {
		t.Fatalf("credentials %q %q", got.user, got.pass)
	}
}

func TestAuthLoginWhenThereIsNoPlain(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.auth = "LOGIN" })
	d := newDriver(t, s, func(o *Options) { o.Username, o.Password = "user", "p@ss" })
	if err := d.Send(context.Background(), simple(), files.Temp{}); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t); got.user != "user" || got.pass != "p@ss" {
		t.Fatalf("credentials %q %q", got.user, got.pass)
	}
}

func TestAuthFailures(t *testing.T) {
	// Credentials but the server offers no authentication: do not send without.
	s := newServer(t, nil)
	d := newDriver(t, s, func(o *Options) { o.Username, o.Password = "user", "x" })
	if err := d.Send(context.Background(), simple(), files.Temp{}); err == nil {
		t.Error("no AUTH offered")
	}
	// A mechanism we do not know.
	s2 := newServer(t, func(s *testServer) { s.auth = "XOAUTH2" })
	d2 := newDriver(t, s2, func(o *Options) { o.Username, o.Password = "user", "x" })
	if err := d2.Send(context.Background(), simple(), files.Temp{}); err == nil {
		t.Error("unknown mechanism")
	}
	if s.count()+s2.count() != 0 {
		t.Fatal("something was sent")
	}
}

type fakeAuth struct{ started atomic.Bool }

func (a *fakeAuth) Start(*netsmtp.ServerInfo) (string, []byte, error) {
	a.started.Store(true)
	return "PLAIN", []byte("\x00custom\x00secret"), nil
}
func (a *fakeAuth) Next([]byte, bool) ([]byte, error) { return nil, nil }

func TestCustomAuth(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.auth = "PLAIN" })
	a := &fakeAuth{}
	d := newDriver(t, s, func(o *Options) { o.Auth = a })
	if err := d.Send(context.Background(), simple(), files.Temp{}); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t); !a.started.Load() || got.user != "custom" {
		t.Fatalf("custom auth not used: %+v", got)
	}
}

func TestLoginAuthRefusesUnencryptedConnections(t *testing.T) {
	a := loginAuth{"u", "p"}
	if _, _, err := a.Start(&netsmtp.ServerInfo{Name: "mail.example.com", TLS: false}); err == nil {
		t.Error("must refuse")
	}
	for _, ok := range []*netsmtp.ServerInfo{{Name: "mail.example.com", TLS: true}, {Name: "localhost"}, {Name: "127.0.0.1"}} {
		if _, _, err := a.Start(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
}

func TestStartTLS(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.startTLS = true; s.auth = "PLAIN" })
	d := newDriver(t, s, func(o *Options) {
		o.Security = StartTLS
		o.Username, o.Password = "user", "pw"
	})
	if err := d.Send(context.Background(), simple(), files.Temp{}); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t); !got.tls || got.user != "user" {
		t.Fatalf("the email did not go through TLS: %+v", got)
	}
}

func TestStartTLSIsNeverSkipped(t *testing.T) {
	s := newServer(t, nil) // does not offer STARTTLS
	d := newDriver(t, s, func(o *Options) { o.Security = StartTLS })
	err := d.Send(context.Background(), simple(), files.Temp{})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("got %v", err)
	}
	if s.count() != 0 {
		t.Fatal("sent without encryption")
	}
}

func TestImplicitTLS(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.implicit = true })
	d := newDriver(t, s, func(o *Options) { o.Security = ImplicitTLS })
	if err := d.Send(context.Background(), simple(), files.Temp{}); err != nil {
		t.Fatal(err)
	}
	if !s.last(t).tls {
		t.Fatal("not encrypted")
	}
}

func TestTLSCertificateIsChecked(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.implicit = true })
	d := newDriver(t, s, func(o *Options) {
		o.Security = ImplicitTLS
		o.TLSConfig = nil // the certificate of the test server is not trusted
	})
	if err := d.Send(context.Background(), simple(), files.Temp{}); err == nil {
		t.Fatal("an untrusted certificate must fail")
	}
	if s.count() != 0 {
		t.Fatal("sent")
	}
}

func TestRejectedRecipientFailsWithTheServerCode(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.rejectRcpt = "bad@example.com" })
	d := newDriver(t, s, nil)
	req := simple()
	req.Envelope.To = []string{"ana@example.com", "bad@example.com"}
	err := d.Send(context.Background(), req, files.Temp{})
	var perr *textproto.Error
	if !errors.As(err, &perr) || perr.Code != 550 {
		t.Fatalf("got %v", err)
	}
	if s.count() != 0 {
		t.Fatal("the email was sent to the others")
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	s := newServer(t, func(s *testServer) { s.hang = true })

	d := newDriver(t, s, func(o *Options) { o.Timeout = 200 * time.Millisecond })
	start := time.Now()
	err := d.Send(context.Background(), simple(), files.Temp{})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout: %v after %v", err, time.Since(start))
	}

	d = newDriver(t, s, nil)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start = time.Now()
	err = d.Send(ctx, simple(), files.Temp{})
	if !errors.Is(err, context.Canceled) || time.Since(start) > 3*time.Second {
		t.Fatalf("cancel: %v after %v", err, time.Since(start))
	}
}

func TestConnectionRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	d, _ := New(Options{Host: "127.0.0.1", Port: port, Sender: "a@example.com", Security: NoTLS, Timeout: time.Second})
	if err := d.Send(context.Background(), simple(), files.Temp{}); err == nil {
		t.Fatal("expected error")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk failure") }
func (failingReader) Close() error             { return nil }

// A file that fails while it is written must not leave the server with an
// email that looks complete.
func TestAFailureWhileWritingDoesNotSendTheEmail(t *testing.T) {
	s := newServer(t, nil)
	d := newDriver(t, s, nil)
	out, err := d.prepare(context.Background(), simple(), files.Temp{})
	if err != nil {
		t.Fatal(err)
	}
	out.files = append(out.files, attachment{name: "x.bin", ctype: "application/octet-stream", r: failingReader{}})
	defer out.close()
	if err := d.deliver(context.Background(), out); err == nil || !strings.Contains(err.Error(), "disk failure") {
		t.Fatalf("got %v", err)
	}
	time.Sleep(100 * time.Millisecond) // let the server see the closed connection
	if s.count() != 0 {
		t.Fatalf("the server accepted an incomplete email: %q", s.last(t).data)
	}
}

func TestNewChecksOptions(t *testing.T) {
	for name, o := range map[string]Options{
		"no host":    {Sender: "a@example.com"},
		"no sender":  {Host: "h"},
		"bad sender": {Host: "h", Sender: "nope"},
	} {
		if _, err := New(o); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	for sec, port := range map[Security]int{StartTLS: 587, ImplicitTLS: 465, NoTLS: 25} {
		d, err := New(Options{Host: "h", Sender: "a@example.com", Security: sec})
		if err != nil || d.o.Port != port {
			t.Errorf("security %d: port %d (%v)", sec, d.o.Port, err)
		}
	}
}

func TestLineWriter(t *testing.T) {
	var b bytes.Buffer
	lw := &lineWriter{w: &b}
	lw.Write([]byte(strings.Repeat("a", 100)))
	lw.Write([]byte(strings.Repeat("b", 60)))
	if err := lw.finish(); err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("a", 76) + "\r\n" + strings.Repeat("a", 24) + strings.Repeat("b", 52) + "\r\n" + strings.Repeat("b", 8) + "\r\n"
	if b.String() != want {
		t.Fatalf("got %q", b.String())
	}
	b.Reset()
	lw = &lineWriter{w: &b}
	lw.Write([]byte(strings.Repeat("a", 76)))
	lw.finish()
	if b.String() != strings.Repeat("a", 76)+"\r\n" {
		t.Fatalf("exact line: %q", b.String())
	}
}
