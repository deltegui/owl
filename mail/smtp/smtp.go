package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	netsmtp "net/smtp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/mail"
)

// Security says how the connection to the server is protected.
type Security int

const (
	// StartTLS connects without encryption and upgrades with the STARTTLS
	// command (port 587). It is an error if the server does not offer it: it
	// never falls back to sending without encryption. It is the default.
	StartTLS Security = iota
	// ImplicitTLS is encrypted from the first byte (port 465).
	ImplicitTLS
	// NoTLS never encrypts (port 25). Only for a server in the same machine
	// or network, or for development: the password is refused over an
	// unencrypted connection unless the host is localhost.
	NoTLS
)

// Options configures a Driver. Host and Sender are required.
type Options struct {
	Host string
	// Port is 587 with StartTLS, 465 with ImplicitTLS and 25 with NoTLS if it is zero.
	Port int
	// Sender is the From of the emails and the sender of the envelope: an
	// address, with or without name ("Little Cash <no-reply@example.com>").
	Sender string
	// Username and Password log in with PLAIN, or LOGIN if the server does
	// not offer PLAIN. If Username is empty there is no authentication.
	Username, Password string
	// Auth replaces Username and Password with other mechanism of net/smtp
	// (for example one for XOAUTH2).
	Auth netsmtp.Auth
	// Security is StartTLS by default.
	Security Security
	// TLSConfig is optional. ServerName is Host if it is empty.
	TLSConfig *tls.Config
	// Timeout is the limit for sending an email, connection included. 30
	// seconds if it is zero. The context of Send can be shorter.
	Timeout time.Duration
	// Hostname is the name we give to the server when we say hello. It is
	// "localhost" if empty.
	Hostname string
	// Now is the clock for the Date of the emails, time.Now if nil.
	Now func() time.Time
}

// Driver sends the emails to an SMTP server. It opens a connection for each
// email. It implements mail.Driver and is safe for concurrent use.
//
// The errors of the server are the ones of net/smtp, wrapped: use
// errors.As with *textproto.Error to read the code (4xx is temporary, 5xx is
// permanent).
type Driver struct {
	o    Options
	from *netmail.Address
}

var _ mail.Driver = (*Driver)(nil)

// New creates a Driver. It does not connect to the server.
func New(o Options) (*Driver, error) {
	if o.Host == "" {
		return nil, errors.New("smtp: Host is required")
	}
	if strings.ContainsAny(o.Sender, "\r\n") {
		return nil, errors.New("smtp: line break in the Sender")
	}
	from, err := netmail.ParseAddress(o.Sender)
	if err != nil {
		return nil, fmt.Errorf("smtp: Sender: %w", err)
	}
	if o.Port == 0 {
		o.Port = map[Security]int{StartTLS: 587, ImplicitTLS: 465, NoTLS: 25}[o.Security]
	}
	if o.Timeout == 0 {
		o.Timeout = 30 * time.Second
	}
	if o.Hostname == "" {
		o.Hostname = "localhost"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Driver{o: o, from: from}, nil
}

// Send writes the email in the connection as it reads the files, so a big
// attachment is not loaded in memory. The files are opened before connecting:
// if one is missing the server is not even called.
func (d *Driver) Send(ctx context.Context, req mail.Request, tmp files.Temp) error {
	out, err := d.prepare(ctx, req, tmp)
	if err != nil {
		return err
	}
	defer out.close()

	ctx, cancel := context.WithTimeout(ctx, d.o.Timeout)
	defer cancel()
	if err := d.deliver(ctx, out); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return fmt.Errorf("smtp: %w (%v)", cerr, err)
		}
		return err
	}
	return nil
}

func (d *Driver) deliver(ctx context.Context, out *outgoing) error {
	conn, err := d.dial(ctx)
	if err != nil {
		return fmt.Errorf("smtp: connecting: %w", err)
	}
	// net/smtp knows nothing about contexts: closing the connection when the
	// context ends is what stops it. There is no deadline on the connection on
	// purpose: it would run out at the same moment as the context, and a read
	// that fails first would return a bare "i/o timeout" before ctx.Err() says
	// DeadlineExceeded. Closing it from the context is always after ctx.Err().
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	c, err := netsmtp.NewClient(conn, d.o.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: greeting: %w", err)
	}
	defer c.Close()

	if err := c.Hello(d.o.Hostname); err != nil {
		return fmt.Errorf("smtp: hello: %w", err)
	}
	if d.o.Security == StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: the server does not offer STARTTLS")
		}
		if err := c.StartTLS(d.tlsConfig()); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if err := d.authenticate(c); err != nil {
		return fmt.Errorf("smtp: authentication: %w", err)
	}
	if err := c.Mail(d.from.Address); err != nil {
		return fmt.Errorf("smtp: sender: %w", err)
	}
	for _, r := range out.rcpts {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("smtp: recipient %s: %w", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	if err := out.writeTo(w, d.o.Now()); err != nil {
		// w is not closed: closing it would end the message, and the server
		// would accept it incomplete. Closing the connection discards it.
		return fmt.Errorf("smtp: writing the message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: the server did not accept the message: %w", err)
	}
	_ = c.Quit() // the message is accepted: a failure here does not matter
	return nil
}

func (d *Driver) dial(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(d.o.Host, strconv.Itoa(d.o.Port))
	nd := &net.Dialer{}
	if d.o.Security == ImplicitTLS {
		td := tls.Dialer{NetDialer: nd, Config: d.tlsConfig()}
		return td.DialContext(ctx, "tcp", addr)
	}
	return nd.DialContext(ctx, "tcp", addr)
}

func (d *Driver) tlsConfig() *tls.Config {
	var cfg *tls.Config
	if d.o.TLSConfig != nil {
		cfg = d.o.TLSConfig.Clone()
	} else {
		cfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.ServerName == "" {
		cfg.ServerName = d.o.Host
	}
	return cfg
}

func (d *Driver) authenticate(c *netsmtp.Client) error {
	a := d.o.Auth
	if a == nil {
		if d.o.Username == "" {
			return nil
		}
		ok, params := c.Extension("AUTH")
		if !ok {
			return errors.New("the server does not offer authentication")
		}
		mechanisms := strings.Fields(strings.ToUpper(params))
		switch {
		case slices.Contains(mechanisms, "PLAIN"):
			a = netsmtp.PlainAuth("", d.o.Username, d.o.Password, d.o.Host)
		case slices.Contains(mechanisms, "LOGIN"):
			a = loginAuth{d.o.Username, d.o.Password}
		default:
			return fmt.Errorf("the server offers %q: PLAIN or LOGIN are needed (or use Options.Auth)", params)
		}
	}
	return c.Auth(a)
}

// loginAuth is the LOGIN mechanism, that net/smtp does not have and some
// servers (Office 365, for example) use instead of PLAIN.
type loginAuth struct{ username, password string }

func (a loginAuth) Start(s *netsmtp.ServerInfo) (string, []byte, error) {
	if !s.TLS && !isLocalhost(s.Name) {
		return "", nil, errors.New("refusing to send the password over an unencrypted connection")
	}
	return "LOGIN", nil, nil
}

func (a loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSuffix(strings.TrimSpace(string(fromServer)), ":")) {
	case "username":
		return []byte(a.username), nil
	case "password":
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("unexpected challenge %q", fromServer)
}

func isLocalhost(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}
