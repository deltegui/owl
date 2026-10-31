package files

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// memDir is a Directory in memory, only to test the chain.
type memDir map[string]string

func (m memDir) Write(_ context.Context, path string, r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	m[path] = string(b)
	return path, err
}

func (m memDir) Open(_ context.Context, path string) (io.ReadCloser, error) {
	s, ok := m[path]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(strings.NewReader(s)), nil
}

func (m memDir) Stat(_ context.Context, path string) (Info, error) {
	s, ok := m[path]
	if !ok {
		return Info{}, ErrNotFound
	}
	return Info{Name: path, Size: int64(len(s))}, nil
}

func (m memDir) Exists(_ context.Context, path string) bool { _, ok := m[path]; return ok }

func (m memDir) Delete(_ context.Context, path string) error {
	if _, ok := m[path]; !ok {
		return ErrNotFound
	}
	delete(m, path)
	return nil
}

func (m memDir) CopyTo(ctx context.Context, path, destiny string, other Directory) (string, error) {
	r, err := m.Open(ctx, path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	return other.Write(ctx, destiny, r)
}

// section is a policy that adds a fixed section and, on read, requires it.
type section string

func (s section) OnWrite(p *Path) error { p.Enqueue(string(s)); return nil }
func (s section) OnRead(p *Path) error {
	if got, ok := p.Dequeue(); !ok || got != string(s) {
		return ErrAccessDenied
	}
	return nil
}

type upper struct{}

func (upper) Rename(o string) string { return strings.ToUpper(o) }

type climb struct{}

func (climb) Rename(string) string { return "../x" }

func TestWriteAppliesPoliciesInOrderAndNaming(t *testing.T) {
	m := memDir{}
	d := Of(m).Add(section("a")).Add(section("b")).Naming(upper{}).Build()
	got, err := WriteString(context.Background(), d, "f.txt", "hi")
	if err != nil || got != "a/b/F.TXT" {
		t.Fatalf("got %q, %v", got, err)
	}
	if m["a/b/F.TXT"] != "hi" {
		t.Fatalf("not stored: %v", m)
	}
}

func TestReadChecksPolicies(t *testing.T) {
	m := memDir{"a/b/f": "x", "c/b/f": "y"}
	d := Of(m).Add(section("a")).Add(section("b")).Build()
	ctx := context.Background()
	if s, err := ReadString(ctx, d, "a/b/f"); err != nil || s != "x" {
		t.Fatalf("got %q, %v", s, err)
	}
	if _, err := ReadString(ctx, d, "c/b/f"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("other prefix: %v", err)
	}
	if _, err := ReadString(ctx, d, "a/b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no file left: %v", err)
	}
	if _, err := ReadString(ctx, d, "a"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("path shorter than policies: %v", err)
	}
	if d.Exists(ctx, "c/b/f") || !d.Exists(ctx, "a/b/f") {
		t.Fatal("Exists must follow the policies")
	}
}

func TestPathsThatClimbAreDenied(t *testing.T) {
	m := memDir{"a/f": "x", "b/secret": "s"}
	d := Of(m).Add(section("a")).Build()
	ctx := context.Background()
	for _, p := range []string{"a/../b/secret", "a/./f", "a//f", "/a/f", "a/f/", "a\\f", "", "a/\x00"} {
		if _, err := d.Open(ctx, p); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("Open(%q): %v", p, err)
		}
		if err := d.Delete(ctx, p); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("Delete(%q): %v", p, err)
		}
		if _, err := WriteString(ctx, d, p, "x"); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("Write(%q): %v", p, err)
		}
	}
	if len(m) != 2 {
		t.Fatalf("store changed: %v", m)
	}
}

func TestNamingPolicyCannotClimb(t *testing.T) {
	d := Of(memDir{}).Add(section("a")).Naming(climb{}).Build()
	if _, err := WriteString(context.Background(), d, "f", "x"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestCopyToUsesPoliciesOfBoth(t *testing.T) {
	src := memDir{"a/f": "x", "z/f": "no"}
	dst := memDir{}
	ctx := context.Background()
	from := Of(src).Add(section("a")).Build()
	to := Of(dst).Add(section("b")).Build()
	got, err := from.CopyTo(ctx, "a/f", "g", to)
	if err != nil || got != "b/g" || dst["b/g"] != "x" {
		t.Fatalf("got %q, %v, %v", got, err, dst)
	}
	if _, err := from.CopyTo(ctx, "z/f", "g", to); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestBuildIsNotAffectedByLaterChanges(t *testing.T) {
	c := Of(memDir{}).Add(section("a"))
	d := c.Build()
	c.Add(section("b"))
	if got, _ := WriteString(context.Background(), d, "f", "x"); got != "a/f" {
		t.Fatalf("got %q", got)
	}
}
