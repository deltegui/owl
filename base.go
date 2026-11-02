package owl

import (
	"embed"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/localizer"
	"github.com/deltegui/owl/v3/localizer/webstore"
	"github.com/deltegui/owl/v3/logx"
	"github.com/deltegui/valtruc"
	"github.com/julienschmidt/httprouter"
)

// base is what Mux and Muxi share: the router, what a Ctx is made of, the
// middlewares and the route prefix. A sub multiplexer is a copy with its own
// prefix and its own list of middlewares.
type base struct {
	router   *httprouter.Router
	cipher   core.Cipher
	locStore *webstore.Store

	middlewares []Middleware
	routePrefix string

	Logger logx.Logger
}

func newBase(cy core.Cipher) base {
	return base{router: httprouter.New(), cipher: cy, Logger: logx.Default{}}
}

// sub makes the base of a sub multiplexer. Middlewares added to it later are not
// added to its parent.
func (b *base) sub(prefix string) base {
	child := *b
	child.middlewares = slices.Clone(b.middlewares)
	child.routePrefix = normalizePath(b.routePrefix + prefix)
	child.Logger = b.Logger.WithModuleName(prefix)
	return child
}

func (b *base) createContext(w http.ResponseWriter, req *http.Request, params httprouter.Params) Ctx {
	return Ctx{
		Req:       req,
		Res:       w,
		params:    params,
		ctx:       req.Context(),
		locstore:  b.locStore,
		validator: valtruc.New(),
		cipher:    b.cipher,
		Logger:    b.Logger,
	}
}

// register adds the route with the middlewares of the route, and then the ones of
// the multiplexer around them.
func (b *base) register(method, pattern string, handler Handler, middlewares []Middleware) {
	for _, m := range slices.Backward(middlewares) {
		handler = m(handler)
	}
	for _, m := range slices.Backward(b.middlewares) {
		handler = m(handler)
	}
	b.router.Handle(method, normalizePath(b.routePrefix+pattern), b.serve(handler))
}

// serve runs a handler for a request. What the handler returns is not lost: an
// error is logged, and if the handler failed before it chose a status or wrote
// anything, the answer is 500 and not an empty 200.
func (b *base) serve(handler Handler) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		rw := &responseWriter{ResponseWriter: w}
		ctx := b.createContext(rw, req, params)
		if err := handler(ctx); err != nil {
			ctx.Logger.ErrorContext(ctx.Context(), "handler error",
				"error", err.Error(), "method", req.Method, "uri", req.RequestURI)
			if !rw.written() {
				http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}
		rw.commit()
	}
}

// Creates a static file server in the requested dir path.
func (b *base) Static(path string) {
	b.router.NotFound = b.createNotFoundHandler(http.Dir(path))
}

// Creates a static file server with the requested embedded file system.
func (b *base) StaticEmbedded(fs embed.FS) {
	b.router.NotFound = b.createNotFoundHandler(http.FS(fs))
}

func (b *base) createNotFoundHandler(root http.FileSystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.FileServer(root).ServeHTTP(w, r)
	})
}

// Creates a static file server in the requested dir URL and path.
func (b *base) StaticMount(url, path string) {
	b.router.ServeFiles(fmt.Sprintf("%s/*filepath", url), http.Dir(path))
}

// Creates a static file server in the requested dir URL and embedded file system.
func (b *base) StaticMountEmbedded(url string, fs embed.FS) {
	b.router.ServeFiles(fmt.Sprintf("%s/*filepath", url), http.FS(fs))
}

// Listen starts owl's server and stops it on SIGINT or SIGTERM.
func (b *base) Listen(address string) {
	server := http.Server{
		Addr:    address,
		Handler: b.router,
		// Sin esto, quien manda las cabeceras a cuentagotas deja la conexión
		// abierta para siempre (Slowloris).
		ReadHeaderTimeout: 10 * time.Second,
	}
	go startServer(&server)
	waitAndStopServer(&server)
}

// Localization is the translations of the whole application.
//
//   - Shared are the ones every component can use.
//   - Errors translates the codes of a core.DomainError.
//   - Languages are the ones the application speaks, at least one. The first is
//     the default: the language of someone with no language cookie.
//
// Shared and Errors can be nil. The translations of a component are a
// localizer.Dict next to its code, see Ctx.Texts.
type Localization struct {
	Shared    localizer.Source
	Errors    localizer.Source
	Languages []string
}

// AddLocalization sets the translations and the languages of the application.
// It panics if there are no languages.
func (b *base) AddLocalization(l Localization) {
	store := webstore.New(l.Shared, l.Errors, l.Languages, b.cipher)
	b.locStore = &store
}

// Use middleware globally.
func (b *base) Use(middleware Middleware) {
	b.middlewares = append(b.middlewares, middleware)
}

// ServeHTTP makes the multiplexer an http.Handler, so it can be used with
// httptest or a server of your own.
func (b *base) ServeHTTP(w http.ResponseWriter, req *http.Request) { b.router.ServeHTTP(w, req) }
