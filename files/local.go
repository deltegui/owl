package files

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
)

const (
	dirPerm  = 0o750
	filePerm = 0o640
)

// Local is a Directory that stores the files in a folder of the file system.
// It cannot read or write outside of that folder, not even through symbolic links.
type Local struct {
	root *os.Root
}

// NewLocal creates a Local in the folder base. The folder (and its parents)
// is created if it does not exist.
func NewLocal(base string) (*Local, error) {
	if err := os.MkdirAll(base, dirPerm); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	return &Local{root}, nil
}

// Close releases the folder. The Local cannot be used after it.
func (l *Local) Close() error { return l.root.Close() }

// Write stores the content in name, replacing the file if it exists. The
// content is written in a temporal file first, so a failed write (or a
// canceled context) does not leave a half written file in name.
func (l *Local) Write(ctx context.Context, name string, content io.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := split(name); err != nil {
		return "", err
	}
	dir := path.Dir(name)
	if err := l.root.MkdirAll(dir, dirPerm); err != nil {
		return "", err
	}
	tmp := path.Join(dir, ".tmp-"+rand.Text())
	f, err := l.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, ctxReader{ctx, content})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = l.root.Rename(tmp, name)
	}
	if err != nil {
		l.root.Remove(tmp)
		return "", err
	}
	return name, nil
}

// Open opens the file for reading. The caller must close it.
func (l *Local) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := split(name); err != nil {
		return nil, err
	}
	f, err := l.root.Open(name)
	if err != nil {
		return nil, mapErr(err)
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		f.Close()
		return nil, mapErr(err, ErrNotFound)
	}
	return f, nil
}

func (l *Local) Stat(ctx context.Context, name string) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if _, err := split(name); err != nil {
		return Info{}, err
	}
	info, err := l.root.Stat(name)
	if err != nil {
		return Info{}, mapErr(err)
	}
	if info.IsDir() {
		return Info{}, ErrNotFound
	}
	return Info{Name: info.Name(), Size: info.Size()}, nil
}

func (l *Local) Exists(ctx context.Context, name string) bool {
	_, err := l.Stat(ctx, name)
	return err == nil
}

// Delete removes the file. It does not remove folders.
func (l *Local) Delete(ctx context.Context, name string) error {
	if _, err := l.Stat(ctx, name); err != nil {
		return err
	}
	return mapErr(l.root.Remove(name))
}

func (l *Local) CopyTo(ctx context.Context, name, destiny string, other Directory) (string, error) {
	return copyTo(ctx, l, name, destiny, other)
}

func copyTo(ctx context.Context, from Directory, name, destiny string, other Directory) (string, error) {
	r, err := from.Open(ctx, name)
	if err != nil {
		return "", err
	}
	defer r.Close()
	return other.Write(ctx, destiny, r)
}

// mapErr turns "does not exist" into ErrNotFound. If the error is nil
// it returns the first of def, or nil.
func mapErr(err error, def ...error) error {
	switch {
	case err == nil && len(def) > 0:
		return def[0]
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	}
	return err
}

// ctxReader stops a copy when the context is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
