package localizer

import (
	"slices"
	"testing"
)

var view = Dict{
	"es": {"Hello": "Hola", "Shared": "Mío"},
	"en": {"Hello": "Hi"},
}

func TestDictGetIsExact(t *testing.T) {
	if got := view.Get("en").Get("Hello"); got != "Hi" {
		t.Errorf("en = %q", got)
	}
	// A language the Dict does not have is empty: the fallback is not its business.
	if got := view.Get("fr"); got == nil || got.Get("Hello") != "Hello" {
		t.Errorf("want an empty localizer that shows the key, got %v", got)
	}
	if got := Dict(nil).Get("es"); got == nil || got.Get("Hello") != "Hello" {
		t.Errorf("nil dict: %v", got)
	}
}

func TestPickFallsBackByLanguage(t *testing.T) {
	if got := Pick(view, "en", "es").Get("Hello"); got != "Hi" {
		t.Errorf("a language it has = %q", got)
	}
	if got := Pick(view, "fr", "es").Get("Hello"); got != "Hola" {
		t.Errorf("a language it does not have falls back, got %q", got)
	}
	// A language with nothing in it falls back too.
	empty := Dict{"es": {"Hello": "Hola"}, "en": {}}
	if got := Pick(empty, "en", "es").Get("Hello"); got != "Hola" {
		t.Errorf("an empty language falls back, got %q", got)
	}
	// It is by language, not by key: a key that the language lacks is not taken from the fallback.
	if got := Pick(view, "en", "es").Get("Shared"); got != "Shared" {
		t.Errorf("a missing key shows as itself, got %q", got)
	}
	// Neither language: empty, and a nil source is empty.
	if got := Pick(Dict{"it": {"A": "a"}}, "fr", "es"); got == nil || got.Get("A") != "A" {
		t.Errorf("neither = %v", got)
	}
	if got := Pick(nil, "es", "es"); got == nil || got.Get("A") != "A" {
		t.Errorf("nil source = %v", got)
	}
}

func TestCheck(t *testing.T) {
	if problems := Check(Dict{"es": {"A": "a"}, "en": {"A": "a"}}, "es", "en"); len(problems) != 0 {
		t.Errorf("complete dict: %v", problems)
	}
	got := Check(Dict{"es": {"A": "a", "B": "b"}, "en": {"A": "a", "C": "c"}})
	want := []string{`'en' has no key "B"`, `'es' has no key "C"`}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// A language of the application that the Dict does not have at all.
	got = Check(Dict{"es": {"A": "a"}, "en": {"A": "a"}}, "es", "en", "fr")
	if want := []string{"there is no language 'fr'"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Without the list of languages, only the keys are compared.
	if got := Check(Dict{"en": {"A": "a"}}); len(got) != 0 {
		t.Errorf("a single language has nothing to compare: %v", got)
	}
}
