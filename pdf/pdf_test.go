package pdf

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/deltegui/owl/v3/files"
)

type fakeRenderer struct {
	rendered Rendered
	err      error
	lang     string
}

func (r *fakeRenderer) Render(_ context.Context, _ any, lang string) (Rendered, error) {
	r.lang = lang
	return r.rendered, r.err
}

// stagingDriver stages the HTML like a browser driver would and "prints" it.
type stagingDriver struct {
	base string
	req  Request
	read string
	left bool
}

func (d *stagingDriver) Render(ctx context.Context, req Request, tmp files.Temp) ([]byte, error) {
	d.req = req
	rel, cleanup, err := StageHTML(ctx, tmp, req.HTML)
	if err != nil {
		return nil, err
	}
	abs, err := LocalPath(d.base, rel)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	d.read = string(b)
	cleanup()
	_, statErr := os.Stat(abs)
	d.left = statErr == nil
	return []byte("%PDF-" + d.read), nil
}

func newTmp(t *testing.T) (files.Temp, string) {
	t.Helper()
	base := t.TempDir()
	l, err := files.NewLocal(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return files.AsTemp(l), base
}

func TestSaveRendersAndStores(t *testing.T) {
	ctx := context.Background()
	tmp, base := newTmp(t)
	r := &fakeRenderer{rendered: Rendered{HTML: "<p>hi</p>", Options: Options{Format: "A4"}}}
	d := &stagingDriver{base: base}
	svc := NewService(r, d, tmp)

	rel, err := svc.Save(ctx, struct{}{}, "reports/r.pdf", "es")
	if err != nil {
		t.Fatal(err)
	}
	got, err := files.ReadString(ctx, tmp, rel)
	if err != nil || got != "%PDF-<p>hi</p>" {
		t.Fatalf("stored %q, %v", got, err)
	}
	if r.lang != "es" || d.req.Options.Format != "A4" {
		t.Fatalf("lang %q, request %+v", r.lang, d.req)
	}
	if d.read != "<p>hi</p>" {
		t.Fatalf("the driver read %q from disk", d.read)
	}
	if d.left {
		t.Fatal("the staged html was not deleted")
	}
}

func TestRenderedDriverWins(t *testing.T) {
	tmp, base := newTmp(t)
	own := &stagingDriver{base: base}
	svc := NewService(&fakeRenderer{rendered: Rendered{HTML: "x", Driver: own}}, failDriver{}, tmp)
	if _, err := svc.Render(context.Background(), 1, ""); err != nil {
		t.Fatal(err)
	}
	if own.req.HTML != "x" {
		t.Fatal("the driver of the template was not used")
	}
}

type failDriver struct{}

func (failDriver) Render(context.Context, Request, files.Temp) ([]byte, error) {
	return nil, errors.New("must not be used")
}

type emptyDriver struct{}

func (emptyDriver) Render(context.Context, Request, files.Temp) ([]byte, error) { return nil, nil }

func TestErrors(t *testing.T) {
	ctx := context.Background()
	tmp, _ := newTmp(t)
	svc := NewService(&fakeRenderer{}, emptyDriver{}, tmp)
	if _, err := svc.Render(ctx, nil, ""); !errors.Is(err, ErrNoPayload) {
		t.Fatalf("nil payload: %v", err)
	}
	if _, err := svc.Render(ctx, 1, ""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty pdf: %v", err)
	}
	boom := errors.New("boom")
	svc = NewService(&fakeRenderer{err: boom}, failDriver{}, tmp)
	if _, err := svc.Save(ctx, 1, "a.pdf", ""); !errors.Is(err, boom) {
		t.Fatalf("renderer error: %v", err)
	}
	if tmp.Exists(ctx, "a.pdf") {
		t.Fatal("a failed render left a file")
	}
}

func TestNewServicePanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	NewService(nil, failDriver{}, files.Temp{})
}

func TestLocalPath(t *testing.T) {
	base := t.TempDir()
	if p, err := LocalPath(base, "a/b.html"); err != nil || !strings.HasSuffix(p, "/a/b.html") {
		t.Fatalf("%q, %v", p, err)
	}
	for _, bad := range []string{"../x", "/etc/passwd", "a/../../x", ""} {
		if _, err := LocalPath(base, bad); !errors.Is(err, files.ErrAccessDenied) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
