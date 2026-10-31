package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newLocal(t *testing.T) (*Local, string) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "base")
	l, err := NewLocal(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, base
}

func TestLocalWriteReadStatDelete(t *testing.T) {
	l, base := newLocal(t)
	ctx := context.Background()

	got, err := WriteString(ctx, l, "a/b/f.txt", "hola")
	if err != nil || got != "a/b/f.txt" {
		t.Fatalf("got %q, %v", got, err)
	}
	if s, err := ReadString(ctx, l, "a/b/f.txt"); err != nil || s != "hola" {
		t.Fatalf("read %q, %v", s, err)
	}
	if info, err := l.Stat(ctx, "a/b/f.txt"); err != nil || info != (Info{"f.txt", 4}) {
		t.Fatalf("stat %+v, %v", info, err)
	}
	if !l.Exists(ctx, "a/b/f.txt") || l.Exists(ctx, "a/b/g.txt") {
		t.Fatal("Exists")
	}

	// Overwrite replaces the content and leaves no temporal files.
	if _, err := WriteString(ctx, l, "a/b/f.txt", "x"); err != nil {
		t.Fatal(err)
	}
	if s, _ := ReadString(ctx, l, "a/b/f.txt"); s != "x" {
		t.Fatalf("overwrite: %q", s)
	}
	entries, _ := os.ReadDir(filepath.Join(base, "a", "b"))
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}

	if err := l.Delete(ctx, "a/b/f.txt"); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, "a/b/f.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestLocalNotFound(t *testing.T) {
	l, _ := newLocal(t)
	ctx := context.Background()
	WriteString(ctx, l, "dir/f", "x")
	for _, p := range []string{"nope", "dir/nope", "nodir/f", "dir"} {
		if _, err := l.Open(ctx, p); !errors.Is(err, ErrNotFound) {
			t.Errorf("Open(%q): %v", p, err)
		}
		if _, err := l.Stat(ctx, p); !errors.Is(err, ErrNotFound) {
			t.Errorf("Stat(%q): %v", p, err)
		}
	}
	if err := l.Delete(ctx, "dir"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a folder is not a file: %v", err)
	}
}

func TestLocalCannotEscape(t *testing.T) {
	l, base := newLocal(t)
	ctx := context.Background()
	outside := filepath.Join(filepath.Dir(base), "secret")
	os.WriteFile(outside, []byte("s"), 0o600)

	for _, p := range []string{"../secret", "a/../../secret", "/etc/passwd"} {
		if _, err := l.Open(ctx, p); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("Open(%q): %v", p, err)
		}
	}

	// A symbolic link inside the folder cannot take us out either.
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Skip("no symlinks:", err)
	}
	os.Symlink(filepath.Dir(base), filepath.Join(base, "dirlink"))
	if _, err := l.Open(ctx, "link"); err == nil {
		t.Error("read through a symlink")
	}
	if _, err := WriteString(ctx, l, "dirlink/new", "x"); err == nil {
		t.Error("write through a symlink")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(base), "new")); err == nil {
		t.Error("file created outside")
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestLocalFailedWriteLeavesNothing(t *testing.T) {
	l, base := newLocal(t)
	ctx := context.Background()
	WriteString(ctx, l, "f", "old")
	if _, err := l.Write(ctx, "f", failReader{}); err == nil {
		t.Fatal("expected error")
	}
	if s, _ := ReadString(ctx, l, "f"); s != "old" {
		t.Fatalf("old content lost: %q", s)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := l.Write(canceled, "g", strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestLocalCopyTo(t *testing.T) {
	a, _ := newLocal(t)
	b, _ := newLocal(t)
	ctx := context.Background()
	WriteString(ctx, a, "x/f", "data")
	got, err := a.CopyTo(ctx, "x/f", "y/g", b)
	if err != nil || got != "y/g" {
		t.Fatalf("got %q, %v", got, err)
	}
	if s, _ := ReadString(ctx, b, "y/g"); s != "data" {
		t.Fatalf("copy: %q", s)
	}
	if _, err := a.CopyTo(ctx, "nope", "z", b); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestLocalUnderChain(t *testing.T) {
	l, base := newLocal(t)
	ctx := context.Background()
	d := Of(l).Add(section("u1")).Build()
	got, err := WriteString(ctx, d, "f", "x")
	if err != nil || got != "u1/f" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(base, "u1", "f")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Open(ctx, "u2/f"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestTemporalDeletesOldFilesAndEmptyFolders(t *testing.T) {
	base := filepath.Join(t.TempDir(), "tmp")
	offset := time.Duration(0)
	tm, err := NewTemporal(base, time.Hour,
		WithClock(func() time.Time { return time.Now().Add(offset) }),
		WithCleanEvery(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	ctx := context.Background()

	WriteString(ctx, tm, "a/b/old", "x")
	if !tm.Exists(ctx, "a/b/old") {
		t.Fatal("new file must exist")
	}

	offset = 30 * time.Minute // cleanup due, file not old yet
	if !tm.Exists(ctx, "a/b/old") {
		t.Fatal("file deleted too soon")
	}

	offset = 2 * time.Hour
	WriteString(ctx, tm, "c/new", "y") // cleans first, then writes
	if tm.Exists(ctx, "a/b/old") {
		t.Fatal("old file must be gone")
	}
	if _, err := os.Stat(filepath.Join(base, "a")); err == nil {
		t.Fatal("empty folders must be gone")
	}
	if !tm.Exists(ctx, "c/new") {
		t.Fatal("new file must stay")
	}
	if _, err := os.Stat(base); err != nil {
		t.Fatal("base must stay")
	}
}

func TestTemporalCleansOncePerInterval(t *testing.T) {
	base := filepath.Join(t.TempDir(), "tmp")
	offset := time.Duration(0)
	tm, _ := NewTemporal(base, time.Hour,
		WithClock(func() time.Time { return time.Now().Add(offset) }),
		WithCleanEvery(10*time.Hour))
	defer tm.Close()
	ctx := context.Background()
	WriteString(ctx, tm, "f", "x") // first operation cleans
	offset = 2 * time.Hour         // old, but the next cleanup is not due
	if !tm.Exists(ctx, "f") {
		t.Fatal("cleaned before the interval")
	}
	offset = 11 * time.Hour
	if tm.Exists(ctx, "f") {
		t.Fatal("must clean after the interval")
	}
}

func TestTemporalRejectsBadMaxAge(t *testing.T) {
	if _, err := NewTemporal(t.TempDir(), 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestTemporalShouldDelete(t *testing.T) {
	base := filepath.Join(t.TempDir(), "tmp")
	now := time.Now()
	var seen []string
	tm, err := NewTemporal(base, time.Hour,
		WithClock(func() time.Time { return now }),
		WithShouldDelete(func(name string, info fs.FileInfo, at time.Time) bool {
			seen = append(seen, name)
			if !at.Equal(now) || info.Size() != 1 {
				t.Errorf("bad args: %v %v", at, info.Size())
			}
			return strings.HasSuffix(name, ".tmp")
		}))
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	ctx := context.Background()
	WriteString(ctx, tm, "a/x.tmp", "1")
	WriteString(ctx, tm, "a/y.txt", "1")
	now = now.Add(2 * time.Minute) // cleanup due; both files are newer than maxAge
	if tm.Exists(ctx, "a/x.tmp") || !tm.Exists(ctx, "a/y.txt") {
		t.Fatalf("custom rule not applied (seen %v)", seen)
	}
	if len(seen) == 0 || seen[0] != "a/x.tmp" {
		t.Fatalf("name must be the path inside the directory: %v", seen)
	}
}
