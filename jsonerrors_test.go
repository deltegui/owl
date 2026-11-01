package owl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deltegui/valtruc"

	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/island/client"
	"github.com/deltegui/owl/v3/localizer"
)

type jsonLine struct {
	Account string `valtruc:"required"`
}

type jsonModel struct {
	Name  string `valtruc:"min=3, required"`
	Lines []jsonLine
}

var jsonTexts = localizer.Dict{"en": {
	"Name":                             "Name",
	"Account":                          "Account",
	string(valtruc.RequiredIdentifier): "%s is required",
	string(valtruc.MinStringLengthIdentifier): "%s is too short",
}}

func jsonMux() *Mux {
	mux := New(nil)
	mux.AddLocalization(Localization{Shared: localizer.Dict{"en": {}}, Errors: localizer.Dict{"en": {"7": "no luck"}}, Languages: []string{"en"}})
	mux.Post("/invalid", func(ctx Ctx) error {
		ctx.Validate(jsonModel{Lines: []jsonLine{{}}})
		return ctx.JsonInvalid(jsonTexts)
	})
	mux.Post("/short", func(ctx Ctx) error {
		ctx.Validate(jsonModel{Name: "ab"})
		return ctx.JsonInvalid(jsonTexts)
	})
	mux.Post("/failure", func(ctx Ctx) error {
		return ctx.JsonFailure(core.DomainError{Code: 7, Message: "fallback"})
	})
	return mux
}

func post(mux *Mux, path string) (int, client.Errors) {
	rec := httptest.NewRecorder()
	mux.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("")))
	var body client.Errors
	json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestJsonInvalid(t *testing.T) {
	status, body := post(jsonMux(), "/invalid")
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", status)
	}
	if got := body.Errors["name"]; len(got) != 1 || got[0] != "Name is required" {
		t.Errorf("name: %v (only the missing value is said)", got)
	}
	if got := body.Errors["lines[0].account"]; len(got) != 1 || got[0] != "Account is required" {
		t.Errorf("nested field: %v in %v", got, body.Errors)
	}
}

func TestJsonInvalidKeepsOtherRules(t *testing.T) {
	_, body := post(jsonMux(), "/short")
	if got := body.Errors["name"]; len(got) != 1 || !strings.Contains(got[0], "too short") {
		t.Errorf("name: %v", got)
	}
}

func TestJsonFailure(t *testing.T) {
	status, body := post(jsonMux(), "/failure")
	if status != http.StatusBadRequest || body.Message != "no luck" {
		t.Errorf("%d %+v", status, body)
	}
}
