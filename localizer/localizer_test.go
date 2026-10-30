package localizer

import (
	"embed"
	"testing"
)

//go:embed testdata/*.json
var files embed.FS

func TestMissingOrBrokenFilesDoNotPanic(t *testing.T) {
	store := NewLocalizerStore(files)
	for _, key := range []string{"testdata/nothing", "testdata/broken", "testdata/onlyen"} {
		loc := store.Get(key, "es")
		if loc == nil || loc.Get("Hello") != "Hello" {
			t.Errorf("%s: want an empty localizer that shows the key, got %v", key, loc)
		}
	}
}

func TestGetUsesTheLanguageAndFallsBack(t *testing.T) {
	store := NewLocalizerStore(files)
	if got := store.Get("testdata/view", "en").Get("Hello"); got != "Hello" {
		t.Errorf("en = %q", got)
	}
	if got := store.Get("testdata/view", "fr").Get("Hello"); got != "Hola" {
		t.Errorf("an unknown language falls back to Spanish, got %q", got)
	}
}

func TestWebStoreMergesSharedAndSurvivesAMissingFile(t *testing.T) {
	web := NewWebLocalizerStore(files, "testdata/shared", "testdata/errors", nil)
	loc := web.Get("testdata/view", "es")
	if loc.Get("Hello") != "Hola" || loc.Get("Shared") != "Compartido" {
		t.Errorf("merged = %v", loc)
	}
	// The view file is missing: the shared translations are still there, and it does not panic.
	if got := web.Get("testdata/nothing", "es"); got.Get("Shared") != "Compartido" {
		t.Errorf("missing view = %v", got)
	}
	// So is the shared one.
	broken := NewWebLocalizerStore(files, "testdata/gone", "testdata/errors", nil)
	if got := broken.Get("testdata/view", "es"); got.Get("Hello") != "Hola" {
		t.Errorf("missing shared = %v", got)
	}
}
