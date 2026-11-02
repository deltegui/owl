package files

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, _ error) error {
		if strings.Contains(filepath.Base(p), ".tmp-") {
			found = append(found, p)
		}
		return nil
	})
	return found
}

func TestLocalNeedsAFolderItCanMake(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := NewLocal(filepath.Join(file, "inside")); err == nil {
		_ = l.Close()
		t.Fatal("a folder cannot be made inside a file")
	}
}

func TestLocalEveryOperationHonoursACanceledContext(t *testing.T) {
	l, base := newLocal(t)
	ctx := context.Background()
	if _, err := WriteString(ctx, l, "f", "x"); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := l.Open(canceled, "f"); !errors.Is(err, context.Canceled) {
		t.Errorf("Open: %v", err)
	}
	if _, err := l.Stat(canceled, "f"); !errors.Is(err, context.Canceled) {
		t.Errorf("Stat: %v", err)
	}
	if l.Exists(canceled, "f") {
		t.Error("Exists with a canceled context")
	}
	if err := l.Delete(canceled, "f"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete: %v", err)
	}
	if _, err := l.Write(canceled, "g", strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "f")); err != nil {
		t.Error("the file must still be there")
	}
	if _, err := os.Stat(filepath.Join(base, "g")); err == nil {
		t.Error("nothing must be written")
	}
}

func TestLocalWriteRefusesBadNamesBeforeTouchingTheDisk(t *testing.T) {
	l, base := newLocal(t)
	for _, name := range []string{"../x", "a/../../x", "/abs", "", "a//b", "."} {
		if _, err := l.Write(context.Background(), name, strings.NewReader("x")); !errors.Is(err, ErrAccessDenied) {
			t.Errorf("Write(%q): %v", name, err)
		}
	}
	if entries, _ := os.ReadDir(base); len(entries) != 0 {
		t.Errorf("nothing must be created: %v", entries)
	}
}

func TestLocalWriteFailsAndCleansUp(t *testing.T) {
	l, base := newLocal(t)
	ctx := context.Background()
	if err := os.Mkdir(filepath.Join(base, "dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteString(ctx, l, "file", "x"); err != nil {
		t.Fatal(err)
	}

	// Where a folder is needed there is a file.
	if _, err := l.Write(ctx, "file/inside", strings.NewReader("x")); err == nil {
		t.Error("a file is not a folder")
	}
	// The name is a folder: the rename of the temporary file fails and it is removed.
	if _, err := l.Write(ctx, "dir", strings.NewReader("x")); err == nil {
		t.Error("a folder cannot be replaced by a file")
	}
	if left := leftovers(t, base); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
	if s, _ := ReadString(ctx, l, "file"); s != "x" {
		t.Errorf("the other file changed: %q", s)
	}
}

func TestLocalWriteCannotCreateInAFolderThatIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	l, base := newLocal(t)
	dir := filepath.Join(base, "locked")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
	if _, err := l.Write(context.Background(), "locked/f", strings.NewReader("x")); err == nil {
		t.Fatal("expected an error")
	}
}

// cancelOnRead cancels the context while the content is being copied.
type cancelOnRead struct {
	cancel context.CancelFunc
	reads  int
}

func (c *cancelOnRead) Read(p []byte) (int, error) {
	c.reads++
	if c.reads == 1 {
		c.cancel()
		return copy(p, "first chunk"), nil
	}
	return copy(p, "more"), nil
}

func TestLocalWriteStopsWhenTheContextIsCanceledWhileCopying(t *testing.T) {
	l, base := newLocal(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &cancelOnRead{cancel: cancel}
	if _, err := l.Write(ctx, "big", io.LimitReader(src, 1<<20)); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if src.reads > 2 {
		t.Errorf("it kept reading after the cancel: %d reads", src.reads)
	}
	if _, err := os.Stat(filepath.Join(base, "big")); err == nil {
		t.Error("a partial file was left")
	}
	if left := leftovers(t, base); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func TestLocalOpenAndStatOfAFolderAreNotFound(t *testing.T) {
	l, base := newLocal(t)
	if err := os.Mkdir(filepath.Join(base, "dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := l.Open(ctx, "dir"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Open: %v", err)
	}
	if _, err := l.Stat(ctx, "dir"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat: %v", err)
	}
	if l.Exists(ctx, "dir") {
		t.Error("a folder is not a file")
	}
	if err := l.Delete(ctx, "dir"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete: %v", err)
	}
}

func opOf(t *testing.T, err error) string {
	t.Helper()
	var pe *fs.PathError
	if !errors.As(err, &pe) {
		t.Fatalf("not a path error: %v", err)
	}
	return pe.Op
}

// The error says which step failed, not a later one that fails because of it.
func TestLocalErrorsComeFromTheStepThatFailed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewLocal(filepath.Join(file, "inside"))
	if err == nil || !strings.HasPrefix(opOf(t, err), "mkdir") {
		t.Errorf("NewLocal should fail making the folder: %v", err)
	}

	l, _ := newLocal(t)
	ctx := context.Background()
	if _, err := WriteString(ctx, l, "f", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write(ctx, "f/inside", strings.NewReader("x")); err == nil || !strings.HasPrefix(opOf(t, err), "mkdir") {
		t.Errorf("Write should fail making the folder: %v", err)
	}
}

func TestLocalWriteErrorWhenTheTemporaryFileCannotBeCreated(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	l, base := newLocal(t)
	dir := filepath.Join(base, "locked")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
	_, err := l.Write(context.Background(), "locked/f", strings.NewReader("x"))
	if err == nil || !strings.HasPrefix(opOf(t, err), "open") {
		t.Fatalf("Write should fail opening the temporary file: %v", err)
	}
}

func TestLocalWriteWithACanceledContextCreatesNothing(t *testing.T) {
	l, base := newLocal(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Write(canceled, "newdir/f", strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "newdir")); err == nil {
		t.Fatal("the folder was made before looking at the context")
	}
}
