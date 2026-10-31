package files

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"
)

// Temporal is a Local whose files expire: a file whose modification time is
// older than maxAge is deleted, and so are the folders left empty. The files
// are deleted when the directory is used, no more than once per cleanup
// interval (a minute by default), so there are no goroutines to stop.
type Temporal struct {
	local  *Local
	maxAge time.Duration
	every  time.Duration
	now    func() time.Time
	delete func(name string, info fs.FileInfo, now time.Time) bool

	last atomic.Int64 // unix nanoseconds of the last cleanup
	// mu is held for reading by every operation and for writing by the cleanup,
	// so it cannot remove the folder where a file is being written.
	mu sync.RWMutex
}

// TemporalOption changes how a Temporal works.
type TemporalOption func(*Temporal)

// WithClock sets the clock used to decide if a file is old. For tests.
func WithClock(now func() time.Time) TemporalOption {
	return func(t *Temporal) { t.now = now }
}

// WithCleanEvery sets the minimum time between two cleanups.
func WithCleanEvery(d time.Duration) TemporalOption {
	return func(t *Temporal) { t.every = d }
}

// WithShouldDelete replaces the rule that decides if a file expires. It
// receives the path of the file inside the directory, its info and the
// current time (from WithClock). By default a file expires when it is older
// than maxAge. The directories left empty are removed in any case.
func WithShouldDelete(f func(name string, info fs.FileInfo, now time.Time) bool) TemporalOption {
	return func(t *Temporal) { t.delete = f }
}

// NewTemporal creates a Temporal in the folder base. maxAge must be positive,
// even if WithShouldDelete is used.
func NewTemporal(base string, maxAge time.Duration, opts ...TemporalOption) (*Temporal, error) {
	if maxAge <= 0 {
		return nil, errors.New("files: maxAge must be positive")
	}
	local, err := NewLocal(base)
	if err != nil {
		return nil, err
	}
	t := &Temporal{local: local, maxAge: maxAge, every: time.Minute, now: time.Now}
	t.delete = t.tooOld
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// Close releases the folder. The Temporal cannot be used after it.
func (t *Temporal) Close() error { return t.local.Close() }

func (t *Temporal) Write(ctx context.Context, name string, content io.Reader) (string, error) {
	t.clean()
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.local.Write(ctx, name, content)
}

func (t *Temporal) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	t.clean()
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.local.Open(ctx, name)
}

func (t *Temporal) Stat(ctx context.Context, name string) (Info, error) {
	t.clean()
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.local.Stat(ctx, name)
}

func (t *Temporal) Exists(ctx context.Context, name string) bool {
	_, err := t.Stat(ctx, name)
	return err == nil
}

func (t *Temporal) Delete(ctx context.Context, name string) error {
	t.clean()
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.local.Delete(ctx, name)
}

// CopyTo does not hold the lock while writing in other, which can be this same Temporal.
func (t *Temporal) CopyTo(ctx context.Context, name, destiny string, other Directory) (string, error) {
	return copyTo(ctx, t, name, destiny, other)
}

func (t *Temporal) tooOld(_ string, info fs.FileInfo, now time.Time) bool {
	return now.Sub(info.ModTime()) > t.maxAge
}

// clean deletes the old files, if the last cleanup was long ago. It is best
// effort: what cannot be deleted stays for the next one.
func (t *Temporal) clean() {
	now := t.now()
	if now.UnixNano()-t.last.Load() < int64(t.every) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if now.UnixNano()-t.last.Load() < int64(t.every) {
		return
	}
	t.last.Store(now.UnixNano())

	var dirs []string
	fs.WalkDir(t.local.root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		switch {
		case d.IsDir() && name != ".":
			dirs = append(dirs, name)
		case d.Type().IsRegular():
			if info, err := d.Info(); err == nil && t.delete(name, info, now) {
				t.local.root.Remove(name)
			}
		}
		return nil
	})
	// The walk lists parents before children: remove in reverse. Remove fails
	// on a folder that is not empty, which is what we want.
	for i := len(dirs) - 1; i >= 0; i-- {
		t.local.root.Remove(dirs[i])
	}
}
