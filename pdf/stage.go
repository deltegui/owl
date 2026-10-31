package pdf

import (
	"context"
	"crypto/rand"
	"fmt"
	"path/filepath"
	"time"

	"github.com/deltegui/owl/v3/files"
)

// StageHTML writes the HTML in tmp, in a folder of its own, for the drivers
// that need a file (a browser that opens file://). It returns the relative
// path where tmp stored it (use that one: the directory may rename it) and a
// cleanup that deletes it, to call when the driver is done with it.
func StageHTML(ctx context.Context, tmp files.Directory, html string) (rel string, cleanup func(), err error) {
	rel, err = files.WriteString(ctx, tmp, rand.Text()+"/view.html", html)
	if err != nil {
		return "", nil, fmt.Errorf("pdf: staging the html: %w", err)
	}
	return rel, func() {
		// the context of the render may be canceled, and the file must go anyway
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = tmp.Delete(ctx, rel)
	}, nil
}

// LocalPath is the absolute path in the disk of a relative path of a
// directory whose files are in base (the folder of a files.Local). A
// files.Directory does not say where its files are, so a driver that needs it
// is given base apart. It fails if rel would be outside of base.
func LocalPath(base, rel string) (string, error) {
	local := filepath.FromSlash(rel)
	if !filepath.IsLocal(local) {
		return "", fmt.Errorf("pdf: %w: %q", files.ErrAccessDenied, rel)
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, local), nil
}
