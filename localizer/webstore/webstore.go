// Package webstore knows the translations of the whole application and the
// language of a request. It is the server side of package localizer: the cookie
// that remembers the language needs net/http and cipher code that a program in
// the browser should not carry, so it is not in localizer.
package webstore

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/cypher"
	"github.com/deltegui/owl/v3/localizer"
)

const cookieKey string = "language"

// Store knows the translations of the whole application and reads the
// language of a request.
//
//   - shared is the translations every component can use. A component's own
//     translations win over it when a key is in both.
//   - errors translates a core.DomainError: its keys are the codes of the errors.
//   - languages are the ones the application speaks. The first one is the default:
//     the language of a request with no valid cookie, and the one that is used when
//     a source has nothing in the language of the request.
//   - core.Cypher is used to create and read localization HTTP cookie.
//
// shared and errors can be nil.
type Store struct {
	shared    localizer.Source
	errors    localizer.Source
	languages []string
	cypher    core.Cypher
}

// New creates a Store. It panics if there are no languages: it is a mistake in
// the configuration of the program, and better found when it starts.
func New(shared, errors localizer.Source, languages []string, cypher core.Cypher) Store {
	if len(languages) == 0 {
		panic("owl: localization needs at least one language")
	}
	return Store{shared, errors, slices.Clone(languages), cypher}
}

// Default is the first language of the Store.
func (ws Store) Default() string { return ws.languages[0] }

func (ws Store) supported(language string) bool { return slices.Contains(ws.languages, language) }

// Texts is the Localizer of a component in the language of the HTTP cookie, with
// the shared translations added. It is a new map, so it can be changed.
func (ws Store) Texts(src localizer.Source, req *http.Request) localizer.Localizer {
	lang := ws.ReadCookie(req)
	own := localizer.Pick(src, lang, ws.Default())
	shared := localizer.Pick(ws.shared, lang, ws.Default())
	merged := make(localizer.Localizer, len(shared)+len(own))
	maps.Copy(merged, shared)
	maps.Copy(merged, own)
	return merged
}

// Localize a DomainError using the errors translations
func (ws Store) GetLocalizedError(err core.DomainError, req *http.Request) string {
	lang := ws.ReadCookie(req)
	translation, ok := localizer.Pick(ws.errors, lang, ws.Default())[strconv.Itoa(int(err.Code))]
	if !ok {
		return err.Message
	}
	return translation
}

// CreateCookie remembers the language of the user. A language that the
// application does not speak is saved as the default one.
func (ws Store) CreateCookie(w http.ResponseWriter, localization string) error {
	lang := ws.Default()
	if ws.supported(localization) {
		lang = localization
	}
	encode, err := cypher.EncodeCookie(ws.cypher, lang)
	if err != nil {
		return fmt.Errorf("cannot create language cookie: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieKey,
		Value:    encode,
		Expires:  time.Now().Add(core.OneDayDuration),
		MaxAge:   int(core.OneDayDuration.Seconds()),
		Path:     "/",
		SameSite: http.SameSiteDefaultMode,
	})
	return nil
}

// ReadCookie is the language of the request: the one in its cookie, or the
// default if there is none, it cannot be read, or the application does not speak
// that language anymore.
func (ws Store) ReadCookie(req *http.Request) string {
	cookie, err := req.Cookie(cookieKey)
	if err != nil {
		return ws.Default()
	}
	lang, err := cypher.DecodeCookie(ws.cypher, cookie.Value)
	if err != nil || !ws.supported(lang) {
		return ws.Default()
	}
	return lang
}
