package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
)

var (
	// ErrNotFound is returned when the file does not exist, or when a path
	// does not point to a file.
	ErrNotFound = errors.New("files: not found")
	// ErrAccessDenied is returned when a policy rejects a path, or when a
	// path is not a valid relative path (empty sections, "." or ".." sections...).
	ErrAccessDenied = errors.New("files: access denied")
)

// Info describes a file in a Directory.
type Info struct {
	Name string
	Size int64
}

// Directory is a place to store files. It only talks about relative paths
// with '/' as separator: it knows nothing about URLs or about where the files
// really are. Use Of to wrap a Directory with policies.
//
// Write returns the relative path where the content was stored, that can be
// different from the one requested if the directory has policies.
type Directory interface {
	Write(ctx context.Context, path string, content io.Reader) (string, error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Stat(ctx context.Context, path string) (Info, error)
	Exists(ctx context.Context, path string) bool
	Delete(ctx context.Context, path string) error
	// CopyTo copies the file in path to destiny in other. Returns the path
	// where other stored it.
	CopyTo(ctx context.Context, path, destiny string, other Directory) (string, error)
}

// WriteBytes writes a byte slice in a directory. See Directory.Write.
func WriteBytes(ctx context.Context, d Directory, path string, content []byte) (string, error) {
	return d.Write(ctx, path, bytes.NewReader(content))
}

// WriteString writes a string in a directory. See Directory.Write.
func WriteString(ctx context.Context, d Directory, path, content string) (string, error) {
	return d.Write(ctx, path, strings.NewReader(content))
}

// ReadBytes reads the whole file in path.
func ReadBytes(ctx context.Context, d Directory, path string) ([]byte, error) {
	r, err := d.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// ReadString reads the whole file in path as a string.
func ReadString(ctx context.Context, d Directory, path string) (string, error) {
	b, err := ReadBytes(ctx, d, path)
	return string(b), err
}
