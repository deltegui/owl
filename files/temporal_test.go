package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clocked is a Temporal with a clock the test moves by hand.
type clocked struct {
	*Temporal
	base string
	now  time.Time
}

func newClocked(t *testing.T, maxAge, every time.Duration) *clocked {
	t.Helper()
	f := &clocked{base: filepath.Join(t.TempDir(), "tmp"), now: time.Now().Truncate(time.Second)}
	tm, err := NewTemporal(f.base, maxAge, WithClock(func() time.Time { return f.now }), WithCleanEvery(every))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	f.Temporal = tm
	return f
}

// put writes a file and sets how old it is at the time of the clocked clock.
func (f *clocked) put(t *testing.T, name string, age time.Duration) {
	t.Helper()
	if _, err := WriteString(context.Background(), f.Temporal, name, "x"); err != nil {
		t.Fatal(err)
	}
	mod := f.now.Add(-age)
	if err := os.Chtimes(filepath.Join(f.base, name), mod, mod); err != nil {
		t.Fatal(err)
	}
}

func (f *clocked) onDisk(name string) bool {
	_, err := os.Stat(filepath.Join(f.base, name))
	return err == nil
}

func TestTemporalDeletesOnlyWhatIsOlderThanMaxAge(t *testing.T) {
	f := newClocked(t, time.Hour, time.Minute)
	ctx := context.Background()
	_ = f.Exists(ctx, "x") // the first operation cleans: the next cleanup is due in a minute
	for _, name := range []string{"exact", "older", "younger"} {
		if _, err := WriteString(ctx, f.Temporal, name, "x"); err != nil {
			t.Fatal(err)
		}
	}
	f.now = f.now.Add(time.Minute) // a cleanup is due; set how old each file is at this moment
	for name, age := range map[string]time.Duration{"exact": time.Hour, "older": time.Hour + time.Nanosecond, "younger": time.Hour - time.Nanosecond} {
		mod := f.now.Add(-age)
		if err := os.Chtimes(filepath.Join(f.base, name), mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.Exists(ctx, "x")
	if !f.onDisk("exact") {
		t.Error("exactly maxAge old is not older than maxAge: it stays")
	}
	if !f.onDisk("younger") {
		t.Error("younger than maxAge stays")
	}
	if f.onDisk("older") {
		t.Error("older than maxAge goes")
	}
}

func TestTemporalCleansExactlyWhenTheIntervalHasPassed(t *testing.T) {
	f := newClocked(t, time.Hour, 10*time.Minute)
	ctx := context.Background()
	_ = f.Exists(ctx, "x") // cleans: last = now
	f.put(t, "old", 2*time.Hour)

	f.now = f.now.Add(10*time.Minute - time.Nanosecond)
	_ = f.Exists(ctx, "x")
	if !f.onDisk("old") {
		t.Fatal("cleaned before the interval")
	}
	f.now = f.now.Add(time.Nanosecond) // exactly the interval
	_ = f.Exists(ctx, "x")
	if f.onDisk("old") {
		t.Fatal("it must clean when the interval has passed, equality included")
	}
}

func TestTemporalEveryOperationCleans(t *testing.T) {
	ops := map[string]func(*clocked){
		"Write":  func(f *clocked) { _, _ = WriteString(context.Background(), f.Temporal, "other", "y") },
		"Open":   func(f *clocked) { _, _ = f.Open(context.Background(), "nothing") },
		"Stat":   func(f *clocked) { _, _ = f.Stat(context.Background(), "nothing") },
		"Delete": func(f *clocked) { _ = f.Delete(context.Background(), "nothing") },
		"Exists": func(f *clocked) { _ = f.Exists(context.Background(), "nothing") },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			f := newClocked(t, time.Hour, time.Minute)
			_ = f.Exists(context.Background(), "x")
			f.put(t, "old", 2*time.Hour)
			f.now = f.now.Add(time.Minute)
			op(f)
			if f.onDisk("old") {
				t.Fatalf("%s did not clean", name)
			}
		})
	}
}

func TestTemporalNeedsAWritableBaseAndAPositiveMaxAge(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if tm, err := NewTemporal(filepath.Join(file, "inside"), time.Hour); err == nil {
		_ = tm.Close()
		t.Error("a base inside a file cannot work")
	}
	if tm, err := NewTemporal(t.TempDir(), time.Nanosecond); err != nil {
		t.Errorf("any positive maxAge is valid: %v", err)
	} else {
		_ = tm.Close()
	}
	if _, err := NewTemporal(t.TempDir(), -time.Second); err == nil || !strings.Contains(err.Error(), "maxAge") {
		t.Errorf("negative maxAge: %v", err)
	}
}

func TestTemporalKeepsItsBaseFolderWhenThereIsNothingLeft(t *testing.T) {
	f := newClocked(t, time.Hour, time.Minute)
	ctx := context.Background()
	f.put(t, "a/old", 2*time.Hour)
	f.now = f.now.Add(time.Minute)
	_ = f.Exists(ctx, "x") // removes the file and its folder, and must not remove the base
	if _, err := os.Stat(f.base); err != nil {
		t.Fatalf("the base folder is gone: %v", err)
	}
	if _, err := WriteString(ctx, f.Temporal, "b/new", "y"); err != nil {
		t.Fatalf("it must still work: %v", err)
	}
}
