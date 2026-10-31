package files

import (
	"context"
	"testing"
	"time"
)

// Two subsystems that only share a Temp and a relative path.
func produce(ctx context.Context, tmp Temp) (string, error) {
	return WriteString(ctx, tmp, "report.pdf", "pdf")
}

func consume(ctx context.Context, tmp Temp, path string) (string, error) {
	return ReadString(ctx, tmp, path)
}

func TestTempHandsFilesByRelativePath(t *testing.T) {
	ctx := context.Background()
	tm, err := NewTemporal(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	tmp := AsTemp(Of(tm).Add(Folder("tmp")).Add(Date(fixedClock)).Naming(GUID()).Build())

	path, err := produce(ctx, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := consume(ctx, tmp, path); err != nil || s != "pdf" {
		t.Fatalf("got %q, %v", s, err)
	}

	// It is still a Directory: it can copy to another one.
	other := memDir{}
	if _, err := tmp.CopyTo(ctx, path, "kept.pdf", other); err != nil || other["kept.pdf"] != "pdf" {
		t.Fatalf("copy: %v %v", err, other)
	}
}

func TestAsTempNilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	AsTemp(nil)
}
