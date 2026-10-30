package owl

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestAHandlerThatFailsBeforeWritingAnswers500(t *testing.T) {
	mux := New(nil)
	mux.Get("/boom", func(ctx Ctx) error { return errors.New("boom") })
	mux.Get("/late", func(ctx Ctx) error {
		ctx.Status(http.StatusTeapot)
		return errors.New("after choosing a status")
	})
	if rec := do(mux, "GET", "/boom"); rec.Code != http.StatusInternalServerError {
		t.Errorf("/boom = %d, want 500", rec.Code)
	}
	// It chose its status: that one is kept.
	if rec := do(mux, "GET", "/late"); rec.Code != http.StatusTeapot {
		t.Errorf("/late = %d, want 418", rec.Code)
	}
}

type recordLogger struct {
	nopLogger
	errors []string
}

func TestTheErrorOfAHandlerIsLogged(t *testing.T) {
	mux := New(nil)
	logs := &recordLogger{}
	mux.Logger = logs
	mux.Get("/boom", func(ctx Ctx) error { return errors.New("boom") })
	do(mux, "GET", "/boom")
	if len(logs.errors) != 1 || !strings.Contains(logs.errors[0], "boom") {
		t.Errorf("logged %v", logs.errors)
	}
}

func TestStatusThenJSONKeepsTheContentType(t *testing.T) {
	mux := New(nil)
	mux.Get("/invalid", func(ctx Ctx) error {
		ctx.Status(http.StatusUnprocessableEntity)
		return ctx.Json(map[string]int{"a": 1})
	})
	rec := do(mux, "GET", "/invalid")
	if rec.Code != 422 || rec.Header().Get("Content-Type") != "application/json" || strings.TrimSpace(rec.Body.String()) != `{"a":1}` {
		t.Errorf("%d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestNoContentAndRedirectsStillWork(t *testing.T) {
	mux := New(nil)
	mux.Get("/empty", func(ctx Ctx) error { return ctx.NoContent() })
	mux.Get("/nothing", func(ctx Ctx) error { return nil })
	mux.Get("/go", func(ctx Ctx) error { return ctx.RedirectCode("/elsewhere", http.StatusSeeOther) })
	if rec := do(mux, "GET", "/empty"); rec.Code != 204 {
		t.Errorf("/empty = %d", rec.Code)
	}
	if rec := do(mux, "GET", "/nothing"); rec.Code != 200 {
		t.Errorf("a handler that writes nothing is a 200, got %d", rec.Code)
	}
	if rec := do(mux, "GET", "/go"); rec.Code != 303 || rec.Header().Get("Location") != "/elsewhere" {
		t.Errorf("/go = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestTextIsNotAFormat(t *testing.T) {
	mux := New(nil)
	mux.Get("/pct", func(ctx Ctx) error { return ctx.Text("100% sure, %s and %d") })
	mux.Get("/fmt", func(ctx Ctx) error { return ctx.String("%s-%d", "a", 3) })
	mux.Get("/nf", func(ctx Ctx) error { return ctx.StringNotFound("no %s", "page") })
	if got := do(mux, "GET", "/pct").Body.String(); got != "100% sure, %s and %d" {
		t.Errorf("text with a %% = %q", got)
	}
	if got := do(mux, "GET", "/fmt").Body.String(); got != "a-3" {
		t.Errorf("with arguments = %q", got)
	}
	if rec := do(mux, "GET", "/nf"); rec.Code != 404 || rec.Body.String() != "no page" {
		t.Errorf("not found = %d %q", rec.Code, rec.Body.String())
	}
}

func TestLanguageWithoutLocalizationDoesNotPanic(t *testing.T) {
	mux := New(nil)
	var lang string
	var err error
	mux.Get("/lang", func(ctx Ctx) error {
		lang = ctx.GetCurrentLanguage()
		err = ctx.ChangeLanguage("en")
		return nil
	})
	do(mux, "GET", "/lang")
	if lang != "es" || !errors.Is(err, ErrNoLocalization) {
		t.Errorf("lang = %q, err = %v", lang, err)
	}
}

func TestSubMuxesKeepPrefixesAndMiddlewareOfTheirOwn(t *testing.T) {
	mux := New(nil)
	var order []string
	mux.Use(func(next Handler) Handler {
		return func(ctx Ctx) error { order = append(order, "root"); return next(ctx) }
	})
	api := mux.CreateSubMux("/api")
	api.Use(func(next Handler) Handler {
		return func(ctx Ctx) error { order = append(order, "api"); return next(ctx) }
	})
	v1 := api.(*Mux).CreateSubMux("/v1")
	v1.Get("/ping", func(ctx Ctx) error { order = append(order, "handler"); return ctx.String("pong") })
	mux.Get("/plain", func(ctx Ctx) error { order = append(order, "plain"); return nil })

	if rec := do(mux, "GET", "/api/v1/ping"); rec.Body.String() != "pong" {
		t.Fatalf("/api/v1/ping = %q", rec.Body.String())
	}
	if got := strings.Join(order, ","); got != "root,api,handler" {
		t.Errorf("order = %s", got)
	}
	order = nil
	do(mux, "GET", "/plain")
	if got := strings.Join(order, ","); got != "root,plain" { // the middleware of a sub mux does not reach its parent
		t.Errorf("order = %s", got)
	}
}

func TestMuxiSharesTheSameBehaviour(t *testing.T) {
	mux := NewWithInjector(nil)
	mux.Get("/boom", func() Handler { return func(ctx Ctx) error { return errors.New("boom") } })
	mux.Get("/pct", func() Handler { return func(ctx Ctx) error { return ctx.Text("50%") } })
	if rec := do(mux, "GET", "/boom"); rec.Code != 500 {
		t.Errorf("/boom = %d", rec.Code)
	}
	if got := do(mux, "GET", "/pct").Body.String(); got != "50%" {
		t.Errorf("/pct = %q", got)
	}
	nested := mux.CreateSubMuxi("/a").(*Muxi).CreateSubMuxi("/b")
	nested.Get("/x", func() Handler { return func(ctx Ctx) error { return ctx.String("ok") } })
	if got := do(mux, "GET", "/a/b/x").Body.String(); got != "ok" {
		t.Errorf("a sub mux of a sub mux keeps both prefixes, got %q", got)
	}
}
