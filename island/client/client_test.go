package client

import (
	"errors"
	"testing"

	"github.com/deltegui/hx/hxhttp"
)

func TestFieldErrorsAndMessage(t *testing.T) {
	err := &hxhttp.HTTPError{Status: 422, Body: `{"errors":{"name":["required"]}}`}
	got, ok := FieldErrors(err)
	if !ok || got["name"][0] != "required" {
		t.Fatalf("FieldErrors = %v, %v", got, ok)
	}
	if _, ok := FieldErrors(&hxhttp.HTTPError{Status: 400, Body: `{}`}); ok {
		t.Fatal("a 400 is not field errors")
	}
	msg, ok := ErrorMessage(&hxhttp.HTTPError{Status: 400, Body: `{"error":"nope"}`})
	if !ok || msg != "nope" {
		t.Fatalf("ErrorMessage = %q, %v", msg, ok)
	}
	if _, ok := ErrorMessage(errors.New("x")); ok {
		t.Fatal("not an HTTP error")
	}
}

func TestEscape(t *testing.T) {
	if got := Escape("a b&ñ"); got != "a%20b%26%C3%B1" {
		t.Fatalf("Escape = %q", got)
	}
}
