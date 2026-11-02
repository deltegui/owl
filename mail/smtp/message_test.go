package smtp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/files"
	"github.com/deltegui/owl/v3/mail"
)

func TestDomain(t *testing.T) {
	for address, want := range map[string]string{
		"ana@example.com":     "example.com",
		"@example.com":        "example.com", // nothing before the @, still a domain after it
		"a@b@c.example":       "c.example",   // the last @ counts
		"no-at-sign":          "localhost",
		"":                    "localhost",
		"ana@":                "",
		"Little <a@host.com>": "host.com>",
	} {
		if got := domain(address); got != want {
			t.Errorf("domain(%q) = %q, want %q", address, got, want)
		}
	}
}

func TestContentType(t *testing.T) {
	for name, want := range map[string]string{
		"logo.png":         "image/png",
		"report.PDF":       "application/pdf",
		"notes.txt":        "text/plain",
		"dir/photo.jpg":    "image/jpeg",
		"data.zzzunknown":  "application/octet-stream",
		"no-extension":     "application/octet-stream",
		"archive.tar.zzzz": "application/octet-stream",
	} {
		if got := contentType(name); got != want {
			t.Errorf("contentType(%q) = %q, want %q", name, got, want)
		}
	}
}

func driverForUnit(t *testing.T) *Driver {
	t.Helper()
	return newDriver(t, newServer(t, nil), nil)
}

func TestFillRefusesFilesWithoutATempDirectory(t *testing.T) {
	d := driverForUnit(t)
	for name, req := range map[string]mail.Request{
		"one attachment": {HTML: "x", Envelope: mail.Envelope{To: []string{"a@example.com"}, Attachments: []string{"a.pdf"}}},
		"one resource":   {HTML: "x", Envelope: mail.Envelope{To: []string{"a@example.com"}}, CIDs: map[string]string{"logo": "logo.png"}},
	} {
		_, err := d.prepare(context.Background(), req, files.Temp{})
		if err == nil || !strings.Contains(err.Error(), "no Temp directory") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestFillRefusesBadContentIDs(t *testing.T) {
	d := driverForUnit(t)
	tmp := newTemp(t)
	if _, err := files.WriteString(context.Background(), tmp, "logo.png", "png"); err != nil {
		t.Fatal(err)
	}
	for _, cid := range []string{"", "a b", "a\tb", "a\nb", "a\rb", "<a>", "a>", "<a"} {
		req := mail.Request{HTML: "x", Envelope: mail.Envelope{To: []string{"a@example.com"}}, CIDs: map[string]string{cid: "logo.png"}}
		if _, err := d.prepare(context.Background(), req, tmp); err == nil || !strings.Contains(err.Error(), "bad content id") {
			t.Errorf("cid %q: %v", cid, err)
		}
	}
	req := mail.Request{HTML: "x", Envelope: mail.Envelope{To: []string{"a@example.com"}}, CIDs: map[string]string{"logo": "logo.png"}}
	out, err := d.prepare(context.Background(), req, tmp)
	if err != nil {
		t.Fatalf("a plain content id is fine: %v", err)
	}
	out.close()
}

func TestAddressesWithALineBreakAreNamed(t *testing.T) {
	d := driverForUnit(t)
	for _, list := range []string{"To", "Cc", "Bcc", "ReplyTo"} {
		e := mail.Envelope{To: []string{"a@example.com"}}
		bad := []string{"b@example.com\r\nBcc: v@example.com"}
		switch list {
		case "To":
			e.To = bad
		case "Cc":
			e.Cc = bad
		case "Bcc":
			e.Bcc = bad
		case "ReplyTo":
			e.ReplyTo = bad
		}
		_, err := d.prepare(context.Background(), mail.Request{HTML: "x", Envelope: e}, files.Temp{})
		if err == nil || !strings.Contains(err.Error(), list) || !strings.Contains(err.Error(), "line break") {
			t.Errorf("%s: %v", list, err)
		}
	}
}

// tracked is a file that says if it was closed.
type tracked struct {
	io.Reader
	closed *int
}

func (t tracked) Close() error { *t.closed++; return nil }

// trackingDir is a Directory whose files count how many times they are closed.
type trackingDir struct {
	files.Directory
	closed map[string]*int
}

func (d *trackingDir) Open(_ context.Context, p string) (io.ReadCloser, error) {
	if p == "missing" {
		return nil, files.ErrNotFound
	}
	n := new(int)
	d.closed[p] = n
	return tracked{strings.NewReader("data"), n}, nil
}

func TestFilesAreClosed(t *testing.T) {
	d := driverForUnit(t)
	ctx := context.Background()
	req := mail.Request{
		HTML: "x",
		Envelope: mail.Envelope{
			To:          []string{"a@example.com"},
			Attachments: []string{"one.pdf", "two.pdf"},
		},
		CIDs: map[string]string{"logo": "logo.png"},
	}
	dir := &trackingDir{closed: map[string]*int{}}
	out, err := d.prepare(ctx, req, files.AsTemp(dir))
	if err != nil {
		t.Fatal(err)
	}
	out.close()
	for _, p := range []string{"one.pdf", "two.pdf", "logo.png"} {
		if *dir.closed[p] != 1 {
			t.Errorf("%s was closed %d times after close()", p, *dir.closed[p])
		}
	}

	// A file that cannot be opened closes the ones opened before it.
	req.Envelope.Attachments = []string{"one.pdf", "missing"}
	dir = &trackingDir{closed: map[string]*int{}}
	if _, err := d.prepare(ctx, req, files.AsTemp(dir)); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if *dir.closed["one.pdf"] != 1 {
		t.Errorf("the file opened before the failure was closed %d times", *dir.closed["one.pdf"])
	}
}

// failWriter fails when more than limit bytes have been written.
type failWriter struct {
	limit, n int
	buf      bytes.Buffer
}

var errWrite = errors.New("write failed")

func (f *failWriter) Write(p []byte) (int, error) {
	if f.n+len(p) > f.limit {
		room := f.limit - f.n
		if room > 0 {
			f.buf.Write(p[:room])
			f.n += room
		}
		return max(room, 0), errWrite
	}
	f.n += len(p)
	f.buf.Write(p)
	return len(p), nil
}

func fullEmail(t *testing.T) (prepare func() *outgoing, whole string) {
	t.Helper()
	d := driverForUnit(t)
	tmp := newTemp(t)
	ctx := context.Background()
	for name, data := range map[string]string{"logo.png": strings.Repeat("L", 200), "report.pdf": strings.Repeat("P", 300)} {
		if _, err := files.WriteString(ctx, tmp, name, data); err != nil {
			t.Fatal(err)
		}
	}
	req := mail.Request{
		Subject: "Hola",
		HTML:    "<p>Hola</p>",
		Envelope: mail.Envelope{
			To: []string{"ana@example.com"}, Cc: []string{"cc@example.com"}, ReplyTo: []string{"reply@example.com"},
			Attachments: []string{"report.pdf"},
		},
		CIDs: map[string]string{"logo": "logo.png"},
	}
	prepare = func() *outgoing {
		out, err := d.prepare(ctx, req, tmp)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(out.close)
		return out
	}
	var b bytes.Buffer
	if err := prepare().writeTo(&b, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	return prepare, b.String()
}

// Every write can fail, and every failure must come out: a message that is cut
// short must never look like one that was sent.
func TestWriteToReportsEveryFailedWrite(t *testing.T) {
	prepare, whole := fullEmail(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for limit := range len(whole) {
		w := &failWriter{limit: limit}
		if err := prepare().writeTo(w, now); !errors.Is(err, errWrite) {
			t.Fatalf("the writer failed after %d of %d bytes and writeTo said %v", limit, len(whole), err)
		}
	}
}

func TestWriteToLeavesOutEmptyOptionalHeaders(t *testing.T) {
	d := driverForUnit(t)
	out, err := d.prepare(context.Background(), simple(), files.Temp{})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := out.writeTo(&b, time.Now()); err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(b.String(), "\r\n\r\n")
	for _, absent := range []string{"Cc:", "Reply-To:"} {
		if strings.Contains(head, absent) {
			t.Errorf("an empty %s is written:\n%s", absent, head)
		}
	}
	for _, present := range []string{"From:", "To:", "Subject:", "Date:", "Message-ID:", "MIME-Version:", "Content-Type:"} {
		if !strings.Contains(head, present) {
			t.Errorf("%s is missing:\n%s", present, head)
		}
	}
}

func TestLineWriterReportsWhatItWrote(t *testing.T) {
	var b bytes.Buffer
	lw := &lineWriter{w: &b}
	for _, size := range []int{1, 75, 1, 76, 100, 0, 152} {
		n, err := lw.Write(bytes.Repeat([]byte("x"), size))
		if err != nil || n != size {
			t.Fatalf("Write(%d bytes) = %d, %v", size, n, err)
		}
	}
	// The count carries no line breaks: only what was handed in.
	if got := strings.ReplaceAll(b.String(), "\r\n", ""); len(got) != 1+75+1+76+100+152 {
		t.Fatalf("wrote %d characters", len(got))
	}
	for _, line := range strings.Split(strings.TrimSuffix(b.String(), "\r\n"), "\r\n") {
		if len(line) > lineLength {
			t.Fatalf("a line of %d characters", len(line))
		}
	}
}

func TestLineWriterOneByteAtATime(t *testing.T) {
	var b bytes.Buffer
	lw := &lineWriter{w: &b}
	for range 80 {
		if n, err := lw.Write([]byte("a")); n != 1 || err != nil {
			t.Fatalf("n = %d, err = %v", n, err)
		}
	}
	if err := lw.finish(); err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("a", 76) + "\r\n" + strings.Repeat("a", 4) + "\r\n"; b.String() != want {
		t.Fatalf("got %q", b.String())
	}
}

func TestLineWriterFinishStartsANewLine(t *testing.T) {
	var b bytes.Buffer
	lw := &lineWriter{w: &b}
	_, _ = lw.Write([]byte("abc"))
	if err := lw.finish(); err != nil {
		t.Fatal(err)
	}
	if lw.n != 0 {
		t.Fatalf("n = %d after finish", lw.n)
	}
	before := b.Len()
	if err := lw.finish(); err != nil || b.Len() != before {
		t.Fatalf("finishing an empty line writes nothing (%v)", err)
	}
	_, _ = lw.Write(bytes.Repeat([]byte("b"), 76))
	if lw.n != 0 {
		t.Fatalf("n = %d after a full line", lw.n)
	}
	if got := b.String(); got != "abc\r\n"+strings.Repeat("b", 76)+"\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestLineWriterReportsWriteErrors(t *testing.T) {
	// The first write fails.
	lw := &lineWriter{w: &failWriter{limit: 0}}
	if n, err := lw.Write([]byte("abc")); !errors.Is(err, errWrite) || n != 0 {
		t.Errorf("first write: %d, %v", n, err)
	}
	// The line break after a full line fails: the 76 characters were written.
	lw = &lineWriter{w: &failWriter{limit: 76}}
	if n, err := lw.Write(bytes.Repeat([]byte("x"), 100)); !errors.Is(err, errWrite) || n != 76 {
		t.Errorf("break write: %d, %v", n, err)
	}
	// A later chunk fails after one full line.
	lw = &lineWriter{w: &failWriter{limit: 78}}
	if n, err := lw.Write(bytes.Repeat([]byte("x"), 200)); !errors.Is(err, errWrite) || n != 76 {
		t.Errorf("second chunk: %d, %v", n, err)
	}
	lw = &lineWriter{w: &failWriter{limit: 0}}
	_, _ = lw.Write(nil)
	lw.n = 3
	if err := lw.finish(); !errors.Is(err, errWrite) {
		t.Errorf("finish: %v", err)
	}
}
