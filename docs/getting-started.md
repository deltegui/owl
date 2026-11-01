# Getting started

## Install

```
go get github.com/deltegui/owl/v3
```

Go 1.25 or later. Dependencies: `httprouter`, `valtruc`, `golang.org/x/crypto`
and `github.com/deltegui/hx`.

## The smallest app

```go
package main

import (
	"log"

	"github.com/deltegui/hx"
	"github.com/deltegui/owl/v3"
	"github.com/deltegui/owl/v3/cipher"
	"github.com/deltegui/owl/v3/hxui"
)

func main() {
	cy, err := cipher.New()
	if err != nil {
		log.Fatal(err)
	}
	r := owl.New(cy)
	r.Get("/", func(ctx owl.Ctx) error {
		return ctx.View(owl.HixView(func(ctx owl.Ctx) hx.INode {
			return hxui.Page(ctx, "Hello", nil, hx.H1().Text("Hello, OWL"))
		}))
	})
	r.Listen(":8080")
}
```

- `owl.New(cy)` needs a [`core.Cipher`](security.md#cipher): OWL uses it to seal
  cookies (language, session, CSRF). `cipher.New()` makes a random key, which
  does not survive a restart; use `cipher.NewFromStrings(key)` in production.
- A **handler** is `func(ctx owl.Ctx) error`. An error that is returned is
  logged and, if nothing was written yet, answered as 500.
- `ctx.View` renders a Hix tree. If the tree is an `<html>` element, the
  doctype is written first.

## The order matters

A `Mux` is configured by calling methods on it, and some of them are *copied*
into sub-routers and routes when those are created. Do things in this order:

1. `owl.New(cy)` / `owl.NewWithInjector(cy)`.
2. `AddLocalization(...)`, `Static...(...)`.
3. `Use(...)` for the middleware that must cover everything (logger, CSRF...).
4. `Add(...)` the builders (with `Muxi`).
5. `CreateSubMux` / `CreateSubMuxi`, and register routes.
6. `Listen`.

Why:

- `Use` wraps only the routes registered **after** it. A route registered
  earlier does not get the middleware.
- A sub-router copies the localization and the list of middleware at creation.
  `AddLocalization` called afterwards does not reach it.
- A `SubMuxi` gets a copy of the container; a builder added to the parent later
  is not seen by it.

## A project layout that works

This is the layout of `OWL_DEMO`, an application that uses almost everything:

```
cmd/app/main.go            wiring: cipher, mux, middleware, one Bootstrap per feature
sys/                       what the system gives to features: config, persistence, bootstrap context
feature/<slice>/
    <slice>_bootstrap.go   registers the routes of the feature
    <slice>_handler.go     handlers (HTTP in, service call, view or JSON out)
    <slice>_service.go     the logic of the feature
    web/                   what also compiles to WASM: models, screens, islands, texts
web/layout, web/locale     the page around the features, shared and error translations
client/<bundle>/main.go    one WASM entry point per bundle: island.Run(...)
```

Each feature exposes `Bootstrap(ctx bootstrap.Context)` that registers its routes
on a sub-router; `main` lists them. Texts of a screen are a `localizer.Dict` in
the feature's `web` package, so the server and the browser build the same words.

## Where to go next

- Handlers and forms: [request context](context.md).
- Pages: [views](views.md). Interactive pieces: [islands](islands.md).
- Login and protected routes: [authentication](authentication.md).
