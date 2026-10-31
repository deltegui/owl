package localizer

import (
	"fmt"
	"slices"
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

// Source is where the translations of a component come from: the Localizer of a
// language. Dict is one.
type Source interface {
	Get(language string) Localizer
}

// Dict is the translations of a component, written in code next to it, for each
// language:
//
//	var texts = localizer.Dict{
//		"es": {"Title": "Productos"},
//		"en": {"Title": "Products"},
//	}
//
// Go refuses a key that is written twice in the same language. Check tells which
// keys a language has and another one does not.
//
// What Get returns is the map of the Dict itself: read it, do not change it.
type Dict map[string]Localizer

// Get is the Localizer of the language, or an empty one if the Dict does not have
// it, so every key shows as itself. Falling back to another language is up to
// who knows the languages of the application, see Pick.
func (d Dict) Get(language string) Localizer {
	if loc, ok := d[language]; ok {
		return loc
	}
	return Localizer{}
}

// Pick is the Localizer of the language, or the one of fallback if the source
// has nothing in that language. The fallback is by language, not by key: a
// language that is missing some keys shows those keys as they are (Check finds
// them). The server and the browser both build the words of a page with Pick and
// the same fallback, so they say the same.
func Pick(src Source, language, fallback string) Localizer {
	if src == nil {
		return Localizer{}
	}
	if loc := src.Get(language); len(loc) > 0 {
		return loc
	}
	return src.Get(fallback)
}

// Check tells the problems of a Dict: the keys that a language has and another
// one does not and, if languages are given, the ones the Dict does not have at
// all. It is meant for a test, with the languages of the application:
//
//	func TestTexts(t *testing.T) {
//		for _, problem := range localizer.Check(texts, "es", "en") {
//			t.Error(problem)
//		}
//	}
func Check(d Dict, languages ...string) []string {
	var problems []string
	for _, language := range languages {
		if _, ok := d[language]; !ok {
			problems = append(problems, fmt.Sprintf("there is no language '%s'", language))
		}
	}
	keys := map[string]struct{}{}
	for _, loc := range d {
		for key := range loc {
			keys[key] = struct{}{}
		}
	}
	present := make([]string, 0, len(d))
	for language := range d {
		present = append(present, language)
	}
	slices.Sort(present)
	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	slices.Sort(sorted)
	for _, language := range present {
		for _, key := range sorted {
			if _, ok := d[language][key]; !ok {
				problems = append(problems, fmt.Sprintf("'%s' has no key %q", language, key))
			}
		}
	}
	return problems
}
