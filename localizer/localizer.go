package localizer

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/cypher"
)

// Localizer is a map of key and translations.
type Localizer map[string]string

// Safely gets a localizer key. If the key is not
// defined in the localizer returns the key instead
// of returning the translation.
func (loc Localizer) Get(key string) string {
	val, ok := loc[key]
	if !ok {
		return key
	}
	return val
}

// Safely gets a localizer key. If the key is not
// defined in the localizer returns the key instead
// of returning the translation. Then, it will format
// the string replacing a format specifier (using fmt.Sprintf)
func (loc Localizer) GetFormatted(key string, args ...any) string {
	localized := loc.Get(key)
	return fmt.Sprintf(localized, args...)
}

// i18n is a map of languages and Localizer.
type i18n map[string]Localizer

const (
	LangSpanish = "es"
	LangEnglish = "en"
)

const fallbackLanguage string = LangSpanish

const cookieKey string = "language"

// A localizer store is a system to get a Localizer
// depending of a file name.
type Store struct {
	files embed.FS
}

func NewLocalizerStore(files embed.FS) Store {
	return Store{files}
}

func (ls Store) loadFile(file string) i18n {
	raw, err := ls.files.ReadFile(file)
	if err != nil {
		log.Panicln("Error while reading file ", file, err)
	}
	var values i18n
	if err = json.Unmarshal(raw, &values); err != nil {
		log.Panicln("Error while decoding localization file ", file, err)
	}
	return values
}

// Gets a Localizer reading a json file identified by 'key'
// and then reads the language defined in that file.
func (ls Store) Get(key, language string) Localizer {
	key = fmt.Sprintf("%s.json", key)
	values := ls.loadFile(key)
	val, ok := values[language]
	if !ok {
		val, ok = values[fallbackLanguage]
		if !ok {
			log.Panicf("Failed to load fallback language ('%s') localizations for key '%s'\n", fallbackLanguage, key)
		}
	}
	return val
}

func (ls Store) LoadIntoField(field **Localizer, key string, language string) {
	if *field == nil {
		localizer := ls.Get(key, language)
		*field = &localizer
	}
}

// WebStore is a Localizer store with a defined SharedKey, ErrorKey and a core.Cypher.
//
//   - SharedKey is the filename of shared translations. For example 'shared.json'. This file
//     will be used as a fallback.
//   - ErrorKey is the file of error translations. For example 'error.json'. This file will be used
//     to tranlate errors when you call the function "GetLocalizerError"
//   - core.Cypher is used to create and read localization HTTP cookie.
type WebStore struct {
	Store
	sharedKey string
	errorKey  string
	cypher    core.Cypher
}

// Creates a web localizer store. See WebStore documentation.
func NewWebLocalizerStore(files embed.FS, sharedKey, errorKey string, cypher core.Cypher) WebStore {
	return WebStore{Store{files}, sharedKey, errorKey, cypher}
}

// Gets a Localizer reading a json file identified by 'key'
// and then reads the language defined in that file. The localizer loaded will
// be merged with shared localizer.
func (ws WebStore) Get(key, language string) Localizer {
	loc := ws.Store.Get(key, language)
	shared := ws.Store.Get(ws.sharedKey, language)
	mergeLocalizers(loc, shared)
	return loc
}

// Localize a DomainError using the ErrorKey
func (ws WebStore) GetLocalizedError(err core.DomainError, req *http.Request) string {
	lang := ws.ReadCookie(req)
	key := strconv.Itoa(int(err.Code))
	localizer := ws.Store.Get(ws.errorKey, lang)
	translation, ok := localizer[key]
	if !ok {
		return err.Message
	}
	return translation
}

func mergeLocalizers(dst, origin Localizer) {
	maps.Copy(dst, origin)
}

// Get a Localizer using the language defined in HTTP cookie
func (ws WebStore) GetUsingRequest(key string, req *http.Request) Localizer {
	lang := ws.ReadCookie(req)
	return ws.Get(key, lang)
}

// Get a Localizer using the language defined in HTTP cookie without SharedKey
func (ws WebStore) GetUsingRequestWithoutShared(key string, req *http.Request) Localizer {
	lang := ws.ReadCookie(req)
	return ws.Store.Get(key, lang)
}

func (ws WebStore) LoadIntoFieldUsingRequest(field **Localizer, key string, req *http.Request) {
	lang := ws.ReadCookie(req)
	ws.LoadIntoField(field, key, lang)
}

// Creates a localizer cookie
func (ws WebStore) CreateCookie(w http.ResponseWriter, localization string) error {
	return CreateCookie(w, localization, ws.cypher)
}

// Reads a localizer cookie
func (ws WebStore) ReadCookie(req *http.Request) string {
	lang, err := ReadCookie(req, ws.cypher)
	if err != nil {
		return fallbackLanguage
	}
	return lang
}

func ReadCookie(req *http.Request, cy core.Cypher) (string, error) {
	cookie, err := req.Cookie(cookieKey)
	if err != nil {
		return fallbackLanguage, err
	}
	langBytes, err := cypher.DecodeCookie(cy, cookie.Value)
	if err != nil {
		return "", fmt.Errorf("cannot read language cookie: %w", err)
	}
	return langBytes, nil
}

func CreateCookie(w http.ResponseWriter, localization string, cy core.Cypher) error {
	lang := fallbackLanguage
	suppoertedLangauges := []string{
		LangSpanish,
		LangEnglish,
	}
	if slices.Contains(suppoertedLangauges, localization) {
		lang = localization
	}
	encode, err := cypher.EncodeCookie(cy, lang)
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
