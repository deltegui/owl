package webstore

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/cypher"
	"github.com/deltegui/owl/v3/localizer"
)

var password = []byte("0123456789abcdef0123456789abcdef")

var view = localizer.Dict{
	"es": {"Hello": "Hola", "Shared": "Mío"},
	"en": {"Hello": "Hi"},
}

func request() *http.Request { return httptest.NewRequest(http.MethodGet, "/", nil) }

// requestIn is a request with the language cookie that store gives for language.
func requestIn(t *testing.T, store Store, language string) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := store.CreateCookie(rec, language); err != nil {
		t.Fatal(err)
	}
	req := request()
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	return req
}

func TestTextsAddSharedAndTheComponentWins(t *testing.T) {
	shared := localizer.Dict{"es": {"Shared": "Compartido", "Only": "Solo"}}
	web := New(shared, nil, []string{"es", "en"}, nil)
	loc := web.Texts(view, request())
	if loc.Get("Hello") != "Hola" || loc.Get("Only") != "Solo" {
		t.Errorf("merged = %v", loc)
	}
	if loc.Get("Shared") != "Mío" {
		t.Errorf("what the component says wins over shared: %v", loc)
	}
	// The result is a new map: changing it leaves the dictionaries alone.
	loc["Hello"] = "changed"
	if view["es"]["Hello"] != "Hola" || len(shared["es"]) != 2 || len(view["es"]) != 2 {
		t.Errorf("the dictionaries changed: %v %v", view, shared)
	}
}

func TestNilSharedAndErrors(t *testing.T) {
	web := New(nil, nil, []string{"es"}, nil)
	if got := web.Texts(view, request()).Get("Hello"); got != "Hola" {
		t.Errorf("without shared = %q", got)
	}
	if got := web.GetLocalizedError(core.DomainError{Code: 7, Message: "boom"}, request()); got != "boom" {
		t.Errorf("without errors the message is kept, got %q", got)
	}
}

func TestNewNeedsALanguage(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("want a panic when there are no languages")
		}
	}()
	New(nil, nil, nil, nil)
}

func TestTheFirstLanguageIsTheDefault(t *testing.T) {
	store := New(nil, nil, []string{"en", "es"}, cypher.NewWithPassword(password))
	if got := store.Default(); got != "en" {
		t.Errorf("default = %q", got)
	}
	if got := store.ReadCookie(request()); got != "en" {
		t.Errorf("a request with no cookie is in the default language, got %q", got)
	}
}

func TestCookieOnlyKeepsSupportedLanguages(t *testing.T) {
	cy := cypher.NewWithPassword(password)
	store := New(nil, nil, []string{"es", "en", "fr"}, cy)
	if got := store.ReadCookie(requestIn(t, store, "fr")); got != "fr" {
		t.Errorf("a language of the list is kept, got %q", got)
	}
	if got := store.ReadCookie(requestIn(t, store, "de")); got != "es" {
		t.Errorf("a language that is not in the list is saved as the default, got %q", got)
	}
	// The cookie was made when French was spoken. Without it, it is the default.
	later := New(nil, nil, []string{"es", "en"}, cy)
	if got := later.ReadCookie(requestIn(t, store, "fr")); got != "es" {
		t.Errorf("a language that is not spoken anymore is the default, got %q", got)
	}
	// A cookie that cannot be read is the default too.
	req := request()
	req.AddCookie(&http.Cookie{Name: cookieKey, Value: "garbage"})
	if got := store.ReadCookie(req); got != "es" {
		t.Errorf("garbage = %q", got)
	}
}

func TestASourceWithoutTheLanguageIsReadInTheDefaultOne(t *testing.T) {
	cy := cypher.NewWithPassword(password)
	spanishFirst := New(nil, nil, []string{"es", "en", "fr"}, cy)
	if got := spanishFirst.Texts(view, requestIn(t, spanishFirst, "fr")).Get("Hello"); got != "Hola" {
		t.Errorf("view has no French: want the Spanish, got %q", got)
	}
	englishFirst := New(nil, nil, []string{"en", "es", "fr"}, cy)
	if got := englishFirst.Texts(view, requestIn(t, englishFirst, "fr")).Get("Hello"); got != "Hi" {
		t.Errorf("view has no French: want the default (English), got %q", got)
	}
	// The fallback is by language: a key that Spanish has and English lacks shows as itself.
	if got := spanishFirst.Texts(view, requestIn(t, spanishFirst, "en")).Get("Shared"); got != "Shared" {
		t.Errorf("a missing key shows as itself, got %q", got)
	}
}

func TestErrorsAreTranslatedWithTheFallback(t *testing.T) {
	cy := cypher.NewWithPassword(password)
	errs := localizer.Dict{"es": {"7": "Fallo"}, "en": {"7": "Failure"}}
	store := New(nil, errs, []string{"es", "en", "fr"}, cy)
	boom := core.DomainError{Code: 7, Message: "boom"}
	if got := store.GetLocalizedError(boom, requestIn(t, store, "en")); got != "Failure" {
		t.Errorf("en = %q", got)
	}
	if got := store.GetLocalizedError(boom, requestIn(t, store, "fr")); got != "Fallo" {
		t.Errorf("fr falls back to the default, got %q", got)
	}
	if got := store.GetLocalizedError(core.DomainError{Code: 8, Message: "other"}, request()); got != "other" {
		t.Errorf("a code with no translation keeps its message, got %q", got)
	}
}
