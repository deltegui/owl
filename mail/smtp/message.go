package smtp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	netmail "net/mail"
	"net/textproto"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/mail"
)

// outgoing is an email ready to be written: everything is checked and the
// files are open, so the connection is not made if something is wrong.
type outgoing struct {
	from      *netmail.Address
	to, cc    []*netmail.Address
	replyTo   []*netmail.Address
	rcpts     []string // to, cc and bcc: the addresses of the envelope
	subject   string
	html      string
	files     []attachment
	inline    []attachment
	messageID string
}

type attachment struct {
	name  string
	ctype string
	cid   string // only inline
	r     io.ReadCloser
}

func (o *outgoing) close() {
	for _, a := range slices.Concat(o.files, o.inline) {
		_ = a.r.Close()
	}
}

// prepare checks the request and opens the files. On error nothing stays open.
func (d *Driver) prepare(ctx context.Context, req mail.Request, tmp files.Temp) (*outgoing, error) {
	o := &outgoing{from: d.from, subject: req.Subject, html: req.HTML}
	if err := d.fill(ctx, o, req, tmp); err != nil {
		o.close()
		return nil, err
	}
	return o, nil
}

func (d *Driver) fill(ctx context.Context, o *outgoing, req mail.Request, tmp files.Temp) error {
	if strings.ContainsAny(req.Subject, "\r\n") {
		return errors.New("smtp: line break in the subject")
	}
	e := req.Envelope
	var bcc []*netmail.Address
	for _, l := range []struct {
		name string
		in   []string
		out  *[]*netmail.Address
	}{{"To", e.To, &o.to}, {"Cc", e.Cc, &o.cc}, {"Bcc", e.Bcc, &bcc}, {"ReplyTo", e.ReplyTo, &o.replyTo}} {
		var err error
		if *l.out, err = parseAddresses(l.name, l.in); err != nil {
			return err
		}
	}
	for _, a := range slices.Concat(o.to, o.cc, bcc) {
		o.rcpts = append(o.rcpts, a.Address)
	}
	if len(o.rcpts) == 0 {
		return errors.New("smtp: no recipients")
	}

	if (len(e.Attachments) > 0 || len(req.CIDs) > 0) && tmp.Directory == nil {
		return errors.New("smtp: the email has files but there is no Temp directory")
	}
	for _, p := range e.Attachments {
		r, err := tmp.Open(ctx, p)
		if err != nil {
			return fmt.Errorf("smtp: attachment %q: %w", p, err)
		}
		o.files = append(o.files, attachment{name: path.Base(p), ctype: contentType(p), r: r})
	}
	for _, cid := range slices.Sorted(maps.Keys(req.CIDs)) {
		p := req.CIDs[cid]
		if cid == "" || strings.ContainsAny(cid, " \t\r\n<>") {
			return fmt.Errorf("smtp: bad content id %q", cid)
		}
		r, err := tmp.Open(ctx, p)
		if err != nil {
			return fmt.Errorf("smtp: resource %q (cid %s): %w", p, cid, err)
		}
		o.inline = append(o.inline, attachment{name: path.Base(p), ctype: contentType(p), cid: cid, r: r})
	}

	o.messageID = fmt.Sprintf("<%s@%s>", rand.Text(), domain(d.from.Address))
	return nil
}

func parseAddresses(name string, list []string) ([]*netmail.Address, error) {
	out := make([]*netmail.Address, 0, len(list))
	for _, s := range list {
		if strings.ContainsAny(s, "\r\n") {
			return nil, fmt.Errorf("smtp: %s %q: line break", name, s)
		}
		a, err := netmail.ParseAddress(s)
		if err != nil {
			return nil, fmt.Errorf("smtp: %s %q: %w", name, s, err)
		}
		out = append(out, a)
	}
	return out, nil
}

func domain(address string) string {
	if i := strings.LastIndex(address, "@"); i >= 0 {
		return address[i+1:]
	}
	return "localhost"
}

func contentType(name string) string {
	mt, _, err := mime.ParseMediaType(mime.TypeByExtension(path.Ext(name)))
	if err != nil {
		return "application/octet-stream"
	}
	return mt
}

// writeTo writes the whole message, with CRLF line ends.
func (o *outgoing) writeTo(w io.Writer, now time.Time) error {
	headers := []struct{ name, value string }{
		{"From", o.from.String()},
		{"To", joinAddresses(o.to)},
		{"Cc", joinAddresses(o.cc)},
		{"Reply-To", joinAddresses(o.replyTo)},
		{"Subject", mime.QEncoding.Encode("utf-8", o.subject)},
		{"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", o.messageID},
		{"MIME-Version", "1.0"},
	}
	body := o.entity()
	for _, k := range slices.Sorted(maps.Keys(body.header)) {
		headers = append(headers, struct{ name, value string }{k, body.header[k][0]})
	}
	for _, h := range headers {
		if h.value == "" { // Cc and Reply-To are optional
			continue
		}
		if _, err := io.WriteString(w, h.name+": "+h.value+"\r\n"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\r\n"); err != nil {
		return err
	}
	return body.write(w)
}

// joinAddresses writes the addresses folded after each comma, so a long
// list does not pass the line limit.
func joinAddresses(list []*netmail.Address) string {
	s := make([]string, len(list))
	for i, a := range list {
		s[i] = a.String()
	}
	return strings.Join(s, ",\r\n\t")
}

// entity is a part of the message: its headers and how to write its body.
type entity struct {
	header textproto.MIMEHeader
	write  func(w io.Writer) error
}

// entity builds the structure of the message. The html goes alone, or in a
// multipart/related with the inline resources, and that one in a
// multipart/mixed with the attachments.
func (o *outgoing) entity() entity {
	body := htmlEntity(o.html)
	if len(o.inline) > 0 {
		parts := []entity{body}
		for _, a := range o.inline {
			parts = append(parts, fileEntity(a))
		}
		body = multipartEntity("related", parts)
	}
	if len(o.files) > 0 {
		parts := []entity{body}
		for _, a := range o.files {
			parts = append(parts, fileEntity(a))
		}
		body = multipartEntity("mixed", parts)
	}
	return body
}

func htmlEntity(html string) entity {
	return entity{
		header: textproto.MIMEHeader{
			"Content-Type":              {"text/html; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		},
		write: func(w io.Writer) error {
			qw := quotedprintable.NewWriter(w)
			if _, err := io.WriteString(qw, html); err != nil {
				return err
			}
			return qw.Close()
		},
	}
}

func fileEntity(a attachment) entity {
	disposition, params := "attachment", map[string]string{"filename": a.name}
	h := textproto.MIMEHeader{
		"Content-Type":              {mime.FormatMediaType(a.ctype, map[string]string{"name": a.name})},
		"Content-Transfer-Encoding": {"base64"},
	}
	if a.cid != "" {
		disposition = "inline"
		h["Content-ID"] = []string{"<" + a.cid + ">"}
	}
	h["Content-Disposition"] = []string{mime.FormatMediaType(disposition, params)}
	return entity{header: h, write: func(w io.Writer) error {
		lw := &lineWriter{w: w}
		enc := base64.NewEncoder(base64.StdEncoding, lw)
		if _, err := io.Copy(enc, a.r); err != nil {
			return err
		}
		if err := enc.Close(); err != nil {
			return err
		}
		return lw.finish()
	}}
}

func multipartEntity(subtype string, parts []entity) entity {
	boundary := "owl-" + rand.Text()
	return entity{
		header: textproto.MIMEHeader{
			"Content-Type": {mime.FormatMediaType("multipart/"+subtype, map[string]string{"boundary": boundary})},
		},
		write: func(w io.Writer) error {
			mw := multipart.NewWriter(w)
			if err := mw.SetBoundary(boundary); err != nil {
				return err
			}
			for _, p := range parts {
				pw, err := mw.CreatePart(p.header)
				if err != nil {
					return err
				}
				if err := p.write(pw); err != nil {
					return err
				}
			}
			return mw.Close()
		},
	}
}

// lineWriter breaks what it writes in lines of 76 characters, as base64
// needs in an email.
type lineWriter struct {
	w io.Writer
	n int // characters in the current line
}

const lineLength = 76

func (l *lineWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		chunk := min(len(p), lineLength-l.n)
		if _, err := l.w.Write(p[:chunk]); err != nil {
			return written, err
		}
		written += chunk
		l.n += chunk
		p = p[chunk:]
		if l.n == lineLength {
			if _, err := io.WriteString(l.w, "\r\n"); err != nil {
				return written, err
			}
			l.n = 0
		}
	}
	return written, nil
}

// finish ends the last line, if it is not empty.
func (l *lineWriter) finish() error {
	if l.n == 0 {
		return nil
	}
	l.n = 0
	_, err := io.WriteString(l.w, "\r\n")
	return err
}
