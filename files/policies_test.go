package files

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"
)

var fixed = time.Date(2026, 10, 31, 15, 30, 45, 123_000_000, time.UTC)

func fixedClock() time.Time { return fixed }

func TestFolderPolicy(t *testing.T) {
	m := memDir{"x/y/f": "1", "z/y/f": "2"}
	d := Of(m).Add(Folder("x", "y")).Build()
	ctx := context.Background()
	if got, err := WriteString(ctx, d, "g", "3"); err != nil || got != "x/y/g" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := ReadString(ctx, d, "x/y/f"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"z/y/f", "x/z/f", "x"} {
		if _, err := d.Open(ctx, p); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("Open(%q): %v", p, err)
		}
	}
}

func TestSystemUserPolicy(t *testing.T) {
	m := memDir{}
	d := Of(m).Add(SystemUser()).Build()
	ctx := context.Background()
	if got, _ := WriteString(ctx, d, "f", "x"); got != "0/f" {
		t.Fatalf("got %q", got)
	}
	m["1/f"] = "y"
	if _, err := d.Open(ctx, "1/f"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestUserPolicy(t *testing.T) {
	m := memDir{"42/f": "mine", "7/f": "other"}
	id := 42
	d := Of(m).Add(User(func() int { return id })).Build()
	ctx := context.Background()
	if s, err := ReadString(ctx, d, "42/f"); err != nil || s != "mine" {
		t.Fatalf("got %q, %v", s, err)
	}
	for _, p := range []string{"7/f", "42/../7/f", "4/f", "042/f"} {
		if _, err := d.Open(ctx, p); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("Open(%q): %v", p, err)
		}
	}
	id = 7 // the id is read on every operation
	if got, _ := WriteString(ctx, d, "g", "x"); got != "7/g" {
		t.Fatalf("got %q", got)
	}
	if _, err := d.Open(ctx, "42/f"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
	// Ids of other types.
	s := Of(memDir{}).Add(User(func() string { return "abc" })).Build()
	if got, _ := WriteString(ctx, s, "f", "x"); got != "abc/f" {
		t.Fatalf("got %q", got)
	}
	// An id that is not a safe section cannot write.
	for _, bad := range []string{"", "a/b", ".."} {
		b := Of(memDir{}).Add(User(func() string { return bad })).Build()
		if _, err := WriteString(ctx, b, "f", "x"); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("id %q: %v", bad, err)
		}
	}
}

func TestDatePolicy(t *testing.T) {
	m := memDir{"20250101/f": "old"}
	d := Of(m).Add(Date(fixedClock)).Build()
	ctx := context.Background()
	if got, _ := WriteString(ctx, d, "f", "x"); got != "20261031/f" {
		t.Fatalf("got %q", got)
	}
	if s, err := ReadString(ctx, d, "20250101/f"); err != nil || s != "old" {
		t.Fatalf("other day: %q, %v", s, err)
	}
	for _, p := range []string{"foo/f", "2026103/f", "20261331/f", "x"} {
		if _, err := d.Open(ctx, p); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("Open(%q): %v", p, err)
		}
	}
	if Of(memDir{}).Add(Date(nil)).Build() == nil {
		t.Fatal("nil clock must work")
	}
}

func TestPoliciesCompose(t *testing.T) {
	m := memDir{}
	d := Of(m).Add(Folder("private")).Add(User(func() int { return 5 })).Add(Date(fixedClock)).Naming(GUID()).Build()
	got, err := WriteString(context.Background(), d, "docs/a.pdf", "x")
	re := regexp.MustCompile(`^private/5/20261031/docs/[0-9a-f-]{36}\.pdf$`)
	if err != nil || !re.MatchString(got) {
		t.Fatalf("got %q, %v", got, err)
	}
	if s, err := ReadString(context.Background(), d, got); err != nil || s != "x" {
		t.Fatalf("read back: %q, %v", s, err)
	}
}

func TestGUID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		got := GUID().Rename("a/b/photo.PNG")
		name := got[len("a/b/") : len(got)-len(".PNG")]
		if got[:4] != "a/b/" || got[len(got)-4:] != ".PNG" || !re.MatchString(name) || seen[got] {
			t.Fatalf("bad or repeated: %q", got)
		}
		seen[got] = true
	}
	if got := GUID().Rename("noext"); len(got) != 36 {
		t.Fatalf("got %q", got)
	}
}

func TestTimeStamp(t *testing.T) {
	n := TimeStamp(fixedClock, "")
	cases := map[string]string{
		"report.pdf":     "report_20261031153045.123.pdf",
		"a/b/report.pdf": "a/b/report_20261031153045.123.pdf",
		"noext":          "noext_20261031153045.123",
		"a.tar.gz":       "a.tar_20261031153045.123.gz",
		".hidden":        "_20261031153045.123.hidden",
	}
	for in, want := range cases {
		if got := n.Rename(in); got != want {
			t.Errorf("Rename(%q) = %q, want %q", in, got, want)
		}
	}
	if got := TimeStamp(fixedClock, "2006-01-02").Rename("f.txt"); got != "f_2026-10-31.txt" {
		t.Fatalf("layout: %q", got)
	}
}

func TestNameWithTimeStamp(t *testing.T) {
	n := NameWithTimeStamp(fixedClock, "invoice", "")
	if got := n.Rename("a/x.pdf"); got != "a/invoice_20261031153045.123.pdf" {
		t.Fatalf("got %q", got)
	}
	if got := n.Rename("x.pdf"); got != "invoice_20261031153045.123.pdf" {
		t.Fatalf("got %q", got)
	}
}

func TestNamingCannotEscapeThroughLayout(t *testing.T) {
	d := Of(memDir{}).Naming(TimeStamp(fixedClock, "/../x")).Build()
	if _, err := WriteString(context.Background(), d, "f", "x"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
}
