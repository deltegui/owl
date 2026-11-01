# Routing

Package `github.com/deltegui/owl/v3` (and `owl/middleware` for the ready-made
middleware).

There are two routers with the same shape:

- **`Mux`** (`owl.New(cy)`): you register `Handler` values.
- **`Muxi`** (`owl.NewWithInjector(cy)`): you register *builders*, functions that
  return a `Handler` and whose arguments are filled by a
  [dependency injection container](#dependency-injection).

Both wrap [`httprouter`](https://github.com/julienschmidt/httprouter) and both
implement `http.Handler` (`ServeHTTP`), so they work with `httptest` or your own
`http.Server`.

## Handler and Middleware

```go
type Handler func(ctx Ctx) error
type Middleware func(next Handler) Handler
```

If a handler returns an error, OWL logs it (method, URI and the error) and, if
the handler had not chosen a status or written anything yet, answers `500`
instead of an empty `200`.

A middleware wraps a handler:

```go
func timing(next owl.Handler) owl.Handler {
	return func(ctx owl.Ctx) error {
		start := time.Now()
		err := next(ctx)
		ctx.Logger.Info("done", "took", time.Since(start))
		return err
	}
}
```

`owl.Redirect(to)` is a ready-made `Handler` that redirects (307).

## Mux

```go
func New(cy core.Cipher) *Mux
```

| Method | Purpose |
|---|---|
| `Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, `Options`, `Trace` `(pattern, handler, middlewares...)` | Register a route for that HTTP method. |
| `Handle(method, pattern, handler, middlewares...)` | Same, with the method as a value. |
| `Use(middleware)` | Add a middleware to every route registered **afterwards**. |
| `CreateSubMux(prefix) SubMux` | A router mounted under a prefix. |
| `AddLocalization(Localization)` | Translations and languages of the app, see [localization](localization.md). Panics without languages. |
| `Static(dir)` / `StaticEmbedded(fs)` | Serve files for every URL that no route matches. |
| `StaticMount(url, dir)` / `StaticMountEmbedded(url, fs)` | Serve files under `url/*filepath`. |
| `Listen(address)` | Start the server; stops gracefully on SIGINT or SIGTERM. |
| `ServeHTTP(w, req)` | Makes the mux an `http.Handler`. |

`Muxi` has the same methods taking a `Builder` instead of a `Handler` (and
`CreateSubMuxi`), plus the container methods below.

The `Logger` field of the mux (a [`logx.Logger`](utilities.md#logx)) is what
`ctx.Logger` is in handlers; a sub-router gets one tagged with its prefix.

### Patterns

Patterns are `httprouter`'s: `:name` matches one segment and `*name` the rest.
Read them with `ctx.GetURLParam("name")`.

```go
r.Get("/products/:id", showProduct)
r.Get("/files/*path", serveFile)
```

httprouter does not allow a static segment next to a parameter at the same
position: `/entries/new` conflicts with `/entries/:id`. Use another path for one
of them (`/api/entry-defaults`, for example).

### Middleware per route

Middleware given to a route wraps that route only, inside the ones added with
`Use`:

```go
r.Get("/admin", showAdmin, middleware.Admin(manager, "/login"))
```

Order of execution for a request: the `Use` middleware in the order they were
added, then the route's own middleware in the order given, then the handler.

### Sub-routers

```go
api := r.CreateSubMux("/api")
v1 := api.CreateSubMux("/v1")
v1.Get("/ping", ping)          // GET /api/v1/ping
v1.Use(middleware.CorsDefault()) // only for v1 routes registered after this
```

A `SubMux` can register routes and add middleware, and nothing else (no
`Listen`, no `AddLocalization`). It starts with a **copy** of its parent's
middleware, so call `AddLocalization` and the parent's `Use` before creating it.
A `Use` on the sub-router does not affect the parent.

### Static files

```go
// package static: the files are at the root of the embedded file system
//go:embed *
var Files embed.FS

r.StaticEmbedded(static.Files)                 // /js/app.wasm is the file js/app.wasm
r.StaticMountEmbedded("/assets", static.Files) // /assets/js/app.wasm
```

`Static` and `StaticEmbedded` install a *not found* handler that serves the file
system, so they do not conflict with routes. With `StaticEmbedded` the paths inside
the embedded file system are the URL paths, so put the files where the URL should
find them (a `js/` folder next to the Go file serves `/js/...`).

## Dependency injection

`Muxi` holds an `Injector`. You register builders with `Add`; the container
finds the one to call **by return type**.

```go
r := owl.NewWithInjector(cy)

r.Add(newProductRepository)   // func() ProductRepository
r.Add(newProductService)      // func(repo ProductRepository) ProductService

func showProducts(service ProductService) owl.Handler {
	return func(ctx owl.Ctx) error { ... }
}

r.Get("/products", showProducts)  // service is built and passed in
```

Rules:

- A builder is any function; its **first return value** is the type it provides.
  Register an interface by returning the interface type.
- Parameters are resolved recursively from the other builders.
- Handlers are resolved **once, when the route is registered**. A missing
  builder is a `panic` at startup.
- **There are no singletons.** The container calls the builder every time
  something asks for the type. To share one instance (a `session.Manager`, a
  database pool), create it outside and return it:

  ```go
  manager := session.NewManager(cy)
  r.Add(func() *session.Manager { return manager })
  ```

- A `SubMuxi` receives a copy of the container at creation. Register builders
  on the parent *before* `CreateSubMuxi`, or add them to the sub-router (which
  only affects it).

### Other ways to get a dependency

| Method | Use |
|---|---|
| `mux.Run(func(repo ProductRepository) { ... })` | Run a function with its arguments injected, right now (migrations, seeding). |
| `mux.PopulateStruct(&s)` | Fill the exported fields of a struct, by type. Pass a pointer. |
| `mux.ShowAvailableBuilders()` | Log the registered builders. |
| `Injector` (`NewInjector`, `Add`, `Get`, `GetByType`, `CallBuilder`, `ResolveHandler`) | The container on its own, if you want it without a router. |

## Built-in middleware

All in `github.com/deltegui/owl/v3/middleware`.

| Middleware | What it does |
|---|---|
| `Logger` | Logs each request (address, user agent, method, URI). Use it as `r.Use(middleware.Logger)`. |
| `Csrf(cs)` | Rejects unsafe requests without a valid token. See [security](security.md#csrf). |
| `Authorize(m, loginURL)`, `AuthorizeRoles(m, loginURL, roles...)`, `Admin(m, loginURL)` | Require a session. See [authentication](authentication.md). |
| `Cors(CorsOptions)`, `CorsDefault()` | CORS headers and preflight. See [security](security.md#cors). |
