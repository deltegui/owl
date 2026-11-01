// Package client is how an island talks to the JSON API of its server: the
// requests with the CSRF token that OWL's middleware asks for, and the shape of
// the answers that say what is wrong (see Errors).
//
// It compiles everywhere (outside the browser the token is empty and Do fails
// with hxhttp.ErrNotInBrowser), and it imports neither net/http nor net/url:
// they add hundreds of KB to a WASM binary.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/deltegui/hx/hxhttp"
	"github.com/deltegui/hx/hxjson"
)

// statusUnprocessableEntity is http.StatusUnprocessableEntity.
const statusUnprocessableEntity = 422

// CsrfHeader is the name OWL's CSRF middleware reads the token from.
const CsrfHeader = "X-Csrf-Token"

// Errors is the body of a 422 answer: what is wrong, by field. A rule of the
// domain that is not about a field answers with Message instead.
type Errors struct {
	Errors  map[string][]string `json:"errors,omitempty"`
	Message string              `json:"error,omitempty"`
}

// Send does what hxjson.Send does and adds the CSRF token (the one in
// <meta name="csrf-token">), which the server checks on every request that is
// not a GET. A nil body sends none; an empty answer is the zero T.
func Send[T any](ctx context.Context, method, url string, body any) (T, error) {
	var zero T
	request := hxhttp.Request{Method: method, URL: url, Header: map[string]string{
		"Accept":   "application/json",
		CsrfHeader: csrfToken(),
	}}
	if body != nil {
		encoded, err := hxjson.Body(body)
		if err != nil {
			return zero, err
		}
		request.Body = encoded
	}
	response, err := hxhttp.Do(ctx, request)
	if err != nil {
		return zero, err
	}
	if err := response.Err(); err != nil {
		return zero, err
	}
	var result T
	if len(response.Bytes()) == 0 {
		return result, nil
	}
	if err := hxjson.Decode(response, &result); err != nil {
		return zero, err
	}
	return result, nil
}

// FieldErrors reads the errors by field out of a 422 answer.
func FieldErrors(err error) (map[string][]string, bool) {
	var httpErr *hxhttp.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != statusUnprocessableEntity {
		return nil, false
	}
	var body Errors
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil {
		return nil, false
	}
	return body.Errors, true
}

// ErrorMessage reads the message of a rule of the domain out of an answer that
// has one (see Errors), or says there is none.
func ErrorMessage(err error) (string, bool) {
	var httpErr *hxhttp.HTTPError
	if !errors.As(err, &httpErr) {
		return "", false
	}
	var body Errors
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil || body.Message == "" {
		return "", false
	}
	return body.Message, true
}

// Escape percent-encodes a query value, so the WASM does not import net/url.
func Escape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
